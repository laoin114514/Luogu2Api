package fakeuseragent

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
)

// DefaultFallback 是匹配不到候选时返回的兜底 UA，与 Python 版的默认值逐字一致。
const DefaultFallback = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) " +
	"Chrome/122.0.0.0 Safari/537.36 Edg/122.0.0.0"

// ErrNoMatch 表示当前过滤条件下没有候选记录，由 Lookup 返回（errors.Is 可判定）。
//
// Browser / GetBrowser 不会返回它：那条路径按 Python 版的语义退化成兜底 UA。
var ErrNoMatch = errors.New("fakeuseragent: 没有匹配的 User-Agent")

// randomKeyword 是 Python 版里代表"任意浏览器"的魔法词（ua.getBrowser("random")）。
const randomKeyword = "random"

// poolCacheLimit 限制"多浏览器名组合"的缓存条目数，
// 避免调用方用各种名字组合把缓存撑大（候选下标是 4 字节的 int32，组合本身也很小）。
const poolCacheLimit = 64

// UserAgent 是一个 UA 生成器。
//
// New 返回后实例的内部状态不再改变，因此可以被多个 goroutine 并发使用。
// 注意：这依赖构造期算好的索引；不要绕过 API 去改它内部的东西。
type UserAgent struct {
	dataset       []Data
	browsers      []string
	os            []string
	platforms     []string
	minVersion    float64
	minPercentage float64
	fallback      string

	// intN 是取随机数的唯一入口，默认 math/rand/v2 的全局源（无锁、并发安全）
	intN func(n int) int

	// base 是"仅按实例过滤条件"筛出的候选下标；
	// byBrowser 是在 base 之上按浏览器名分组的索引。
	// 两者都在 New 里算好，因此每次取 UA 都是"取下标 + 随机"，不重复遍历全量数据。
	base      []int32
	byBrowser map[string][]int32

	// poolCache 缓存多浏览器名组合的并集（例如 Chrome 家族），只需要算一次
	poolMu    sync.RWMutex
	poolCache map[string][]int32
}

// New 构造生成器；不传 Option 时使用与 Python 版等价的默认过滤集。
//
// 返回的错误只有两类：内嵌数据损坏（构建期问题，正常不会发生）、
// 过滤条件非法（浏览器/系统名不存在、设备类型写错、阈值是 NaN/Inf、兜底 UA 为空）。
// 后者是刻意 fail fast：名字写错在 Python 版里只会静默返回兜底 UA，很难发现。
func New(opts ...Option) (*UserAgent, error) {
	items, err := loadDataset()
	if err != nil {
		return nil, err
	}

	u := &UserAgent{
		dataset:   items,
		browsers:  append([]string(nil), defaultBrowsers...),
		os:        append([]string(nil), defaultOS...),
		platforms: append([]string(nil), defaultPlatforms...),
		fallback:  DefaultFallback,
		intN:      rand.IntN,
		poolCache: make(map[string][]int32),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(u)
		}
	}

	if err := u.validate(); err != nil {
		return nil, err
	}
	u.buildIndex()
	return u, nil
}

// MustNew 与 New 相同，但把错误变成 panic，适合包级变量初始化或测试。
func MustNew(opts ...Option) *UserAgent {
	u, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return u
}

var (
	defaultOnce      sync.Once
	defaultUserAgent *UserAgent
)

// Default 返回进程级共享的默认实例（懒加载，并发安全）。
//
// 默认实例只读取内嵌数据，构造不会失败；真失败了说明内嵌数据损坏，
// 属于构建期问题，这里直接 panic 而不是把错误藏起来。
func Default() *UserAgent {
	defaultOnce.Do(func() { defaultUserAgent = MustNew() })
	return defaultUserAgent
}

// Random 用默认实例返回一条随机 UA 字符串。
func Random() string { return Default().Random() }

// Browser 用默认实例按浏览器名取一条 UA 字符串。
func Browser(names ...string) string { return Default().Browser(names...) }

// Lookup 用默认实例按浏览器名取一条完整记录。
func Lookup(names ...string) (Data, error) { return Default().Lookup(names...) }

// GetBrowser 用默认实例按浏览器名取一条完整记录，无匹配时返回兜底记录。
func GetBrowser(names ...string) Data { return Default().GetBrowser(names...) }

// GetRandom 用默认实例返回一条随机完整记录。
func GetRandom() Data { return Default().GetRandom() }

// validate 校验过滤条件；名字必须真实存在于数据里，否则报错并列出可选值
func (u *UserAgent) validate() error {
	if len(u.browsers) == 0 {
		return errors.New("fakeuseragent: browsers 不能为空（不传参数表示使用默认集合）")
	}
	for _, name := range u.browsers {
		if _, ok := browserSet[name]; !ok {
			return fmt.Errorf("fakeuseragent: 数据里没有浏览器 %q；可选: %s",
				name, strings.Join(availableBrowsers, "、"))
		}
	}

	if len(u.os) == 0 {
		return errors.New("fakeuseragent: os 不能为空（不传参数表示使用默认集合）")
	}
	for _, name := range u.os {
		if _, ok := osSet[name]; !ok {
			return fmt.Errorf("fakeuseragent: 数据里没有操作系统 %q；可选: %s",
				name, strings.Join(availableOS, "、"))
		}
	}

	if len(u.platforms) == 0 {
		return errors.New("fakeuseragent: platforms 不能为空（不传参数表示使用默认集合）")
	}
	for _, p := range u.platforms {
		switch p {
		case TypeDesktop, TypeMobile, TypeTablet:
		default:
			return fmt.Errorf("fakeuseragent: 未知设备类型 %q；可选: %s、%s、%s",
				p, TypeDesktop, TypeMobile, TypeTablet)
		}
	}

	if math.IsNaN(u.minVersion) || math.IsInf(u.minVersion, 0) {
		return fmt.Errorf("fakeuseragent: minVersion 必须是有限数值，当前 %v", u.minVersion)
	}
	if math.IsNaN(u.minPercentage) || math.IsInf(u.minPercentage, 0) {
		return fmt.Errorf("fakeuseragent: minPercentage 必须是有限数值，当前 %v", u.minPercentage)
	}
	if u.fallback == "" {
		return errors.New("fakeuseragent: fallback 不能为空字符串")
	}
	return nil
}

// buildIndex 按实例过滤条件预先算好候选下标，等价于 Python 的 _filter_useragents()
func (u *UserAgent) buildIndex() {
	browserAllowed := make(map[string]struct{}, len(u.browsers))
	for _, name := range u.browsers {
		browserAllowed[name] = struct{}{}
	}
	osAllowed := make(map[string]struct{}, len(u.os))
	for _, name := range u.os {
		osAllowed[name] = struct{}{}
	}
	platformAllowed := make(map[string]struct{}, len(u.platforms))
	for _, name := range u.platforms {
		platformAllowed[name] = struct{}{}
	}

	u.base = make([]int32, 0, len(u.dataset))
	u.byBrowser = make(map[string][]int32, len(u.browsers))
	for i := range u.dataset {
		item := &u.dataset[i]
		if _, ok := browserAllowed[item.Browser]; !ok {
			continue
		}
		if _, ok := osAllowed[item.OS]; !ok {
			continue
		}
		if _, ok := platformAllowed[item.Type]; !ok {
			continue
		}
		if item.BrowserVersionMajorMinor < u.minVersion {
			continue
		}
		if item.Percent < u.minPercentage {
			continue
		}
		idx := int32(i) // 数据量 1e4 量级，int32 足够
		u.base = append(u.base, idx)
		u.byBrowser[item.Browser] = append(u.byBrowser[item.Browser], idx)
	}
}

// pool 返回一组浏览器名对应的候选下标（并集），与 Python 的 _filter_useragents(browsers_to_filter=...) 等价。
func (u *UserAgent) pool(names []string) []int32 {
	names = dedupe(names)
	if len(names) == 0 || (len(names) == 1 && names[0] == randomKeyword) {
		return u.base
	}
	if len(names) == 1 {
		return u.byBrowser[names[0]]
	}

	key := strings.Join(names, "\x00")
	u.poolMu.RLock()
	cached, ok := u.poolCache[key]
	u.poolMu.RUnlock()
	if ok {
		return cached
	}

	union := make([]int32, 0, 64)
	for _, name := range names {
		union = append(union, u.byBrowser[name]...)
	}

	u.poolMu.Lock()
	if len(u.poolCache) < poolCacheLimit {
		u.poolCache[key] = union
	}
	u.poolMu.Unlock()
	return union
}

// dedupe 去掉重复名字：Python 端的过滤是"是否在名单里"，重复名字不会让记录出现两次
func dedupe(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// Browser 按浏览器名取一条 UA 字符串；names 省略或只有 "random" 时在全部候选里随机。
//
// 与 Python 的 ua["Chrome"] / ua.getBrowser("Chrome") 一致：名字大小写敏感，
// 匹配不到时返回兜底 UA，不报错；需要区分"没匹配到"请用 Lookup。
func (u *UserAgent) Browser(names ...string) string {
	return u.GetBrowser(names...).UserAgent
}

// Random 返回一条随机 UA 字符串（受实例过滤条件约束）。
func (u *UserAgent) Random() string { return u.Browser(randomKeyword) }

// Chrome 返回 Chrome 家族（Chrome / Chrome Mobile / Chrome Mobile iOS）的一条 UA。
func (u *UserAgent) Chrome() string {
	return u.Browser(BrowserChrome, BrowserChromeMobile, BrowserChromeMobileIOS)
}

// GoogleChrome 是 Chrome 的别名，对应 Python 版的 ua.googlechrome。
func (u *UserAgent) GoogleChrome() string { return u.Chrome() }

// Firefox 返回 Firefox 家族（Firefox / Firefox Mobile / Firefox iOS）的一条 UA。
func (u *UserAgent) Firefox() string {
	return u.Browser(BrowserFirefox, BrowserFirefoxMobile, BrowserFirefoxIOS)
}

// FF 是 Firefox 的别名，对应 Python 版的 ua.ff。
func (u *UserAgent) FF() string { return u.Firefox() }

// Safari 返回 Safari / Mobile Safari 的一条 UA。
func (u *UserAgent) Safari() string { return u.Browser(BrowserSafari, BrowserMobileSafari) }

// Opera 返回 Opera / Opera Mobile 的一条 UA。
func (u *UserAgent) Opera() string { return u.Browser(BrowserOpera, BrowserOperaMobile) }

// Google 返回 Google App 的一条 UA。
func (u *UserAgent) Google() string { return u.Browser(BrowserGoogle) }

// Edge 返回 Edge / Edge Mobile 的一条 UA。
func (u *UserAgent) Edge() string { return u.Browser(BrowserEdge, BrowserEdgeMobile) }

// GetChrome 与 Chrome 相同，但返回完整记录。
func (u *UserAgent) GetChrome() Data {
	return u.GetBrowser(BrowserChrome, BrowserChromeMobile, BrowserChromeMobileIOS)
}

// GetFirefox 与 Firefox 相同，但返回完整记录。
func (u *UserAgent) GetFirefox() Data {
	return u.GetBrowser(BrowserFirefox, BrowserFirefoxMobile, BrowserFirefoxIOS)
}

// GetSafari 与 Safari 相同，但返回完整记录。
func (u *UserAgent) GetSafari() Data {
	return u.GetBrowser(BrowserSafari, BrowserMobileSafari)
}

// GetOpera 与 Opera 相同，但返回完整记录。
func (u *UserAgent) GetOpera() Data {
	return u.GetBrowser(BrowserOpera, BrowserOperaMobile)
}

// GetGoogle 与 Google 相同，但返回完整记录。
func (u *UserAgent) GetGoogle() Data { return u.GetBrowser(BrowserGoogle) }

// GetEdge 与 Edge 相同，但返回完整记录。
func (u *UserAgent) GetEdge() Data {
	return u.GetBrowser(BrowserEdge, BrowserEdgeMobile)
}

// GetRandom 返回一条随机记录（受实例过滤条件约束）。
func (u *UserAgent) GetRandom() Data { return u.GetBrowser(randomKeyword) }

// Lookup 按浏览器名取一条完整记录；names 省略或只有 "random" 时在全部候选里随机。
//
// 没有任何候选时返回错误（errors.Is(err, ErrNoMatch)），不会退化成兜底 UA。
func (u *UserAgent) Lookup(names ...string) (Data, error) {
	candidates := u.pool(names)
	if len(candidates) == 0 {
		return Data{}, fmt.Errorf("%w（%s）", ErrNoMatch, u.describe(names))
	}
	return u.dataset[candidates[u.intN(len(candidates))]], nil
}

// GetBrowser 与 Lookup 相同，但无匹配时返回兜底记录（与 Python 的 getBrowser 一致）。
func (u *UserAgent) GetBrowser(names ...string) Data {
	item, err := u.Lookup(names...)
	if err != nil {
		return u.fallbackData()
	}
	return item
}

// Filter 返回当前过滤条件下的全部候选记录（每次调用都会拷贝一份，勿放热路径）。
func (u *UserAgent) Filter() []Data { return u.materialize(u.base) }

// FilterFor 在 Filter 的基础上再按浏览器名过滤。
func (u *UserAgent) FilterFor(names ...string) []Data {
	return u.materialize(u.pool(names))
}

// Browsers 返回当前浏览器名集合的副本。
func (u *UserAgent) Browsers() []string { return append([]string(nil), u.browsers...) }

// OS 返回当前操作系统名集合的副本。
func (u *UserAgent) OS() []string { return append([]string(nil), u.os...) }

// Platforms 返回当前设备类型集合的副本。
func (u *UserAgent) Platforms() []string { return append([]string(nil), u.platforms...) }

// MinVersion 返回当前的主次版本号下限。
func (u *UserAgent) MinVersion() float64 { return u.minVersion }

// MinPercentage 返回当前的 Percent 下限。
func (u *UserAgent) MinPercentage() float64 { return u.minPercentage }

// Fallback 返回当前的兜底 UA。
func (u *UserAgent) Fallback() string { return u.fallback }

// materialize 把候选下标还原成记录副本
func (u *UserAgent) materialize(index []int32) []Data {
	out := make([]Data, len(index))
	for i, idx := range index {
		out[i] = u.dataset[idx]
	}
	return out
}

// fallbackData 构造兜底记录，字段与 Python 版的 fallback 字典一致
func (u *UserAgent) fallbackData() Data {
	return Data{
		UserAgent:                u.fallback,
		Percent:                  100,
		Type:                     TypeDesktop,
		Browser:                  BrowserEdge,
		BrowserVersion:           "122.0.0.0",
		BrowserVersionMajorMinor: 122,
		OS:                       "win32",
		OSVersion:                "10",
		Platform:                 "Win32",
	}
}

// describe 生成"为什么没匹配到"的说明，用于错误信息
func (u *UserAgent) describe(names []string) string {
	scope := "全部浏览器"
	if names = dedupe(names); len(names) > 0 {
		scope = "browsers=" + strings.Join(names, "、")
	}
	return fmt.Sprintf("%s；当前过滤：os=%s platforms=%s min_version=%g min_percentage=%g",
		scope, strings.Join(u.os, "、"), strings.Join(u.platforms, "、"), u.minVersion, u.minPercentage)
}

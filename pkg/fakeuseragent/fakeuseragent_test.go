package fakeuseragent

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestDefaultInstanceIsShared(t *testing.T) {
	ua := Default()
	if ua == nil {
		t.Fatal("Default() 返回 nil")
	}
	if Default() != ua {
		t.Error("Default() 应当返回同一个共享实例")
	}
	if got := ua.Random(); got == "" {
		t.Error("Random() 返回了空字符串")
	}
}

func TestPackageLevelHelpers(t *testing.T) {
	if Random() == "" {
		t.Error("Random() 返回了空字符串")
	}
	if Browser(BrowserChrome) == "" {
		t.Error("Browser(Chrome) 返回了空字符串")
	}
	if GetRandom().UserAgent == "" {
		t.Error("GetRandom() 返回了空记录")
	}
	if GetBrowser(BrowserFirefox).Browser != BrowserFirefox {
		t.Error("GetBrowser(Firefox) 没有返回 Firefox 记录")
	}
	if _, err := Lookup(BrowserSafari); err != nil {
		t.Errorf("Lookup(Safari) 报错: %v", err)
	}
}

func TestBrowserFallsBackOnUnknownName(t *testing.T) {
	ua := Default()

	if got := ua.Browser("Netscape"); got != DefaultFallback {
		t.Errorf("未知浏览器应当返回兜底 UA，得到 %q", got)
	}

	// 兜底记录的字段与 Python 版的 fallback 字典一致
	item := ua.GetBrowser("Netscape")
	want := Data{
		UserAgent:                DefaultFallback,
		Percent:                  100,
		Type:                     TypeDesktop,
		Browser:                  BrowserEdge,
		BrowserVersion:           "122.0.0.0",
		BrowserVersionMajorMinor: 122,
		OS:                       "win32",
		OSVersion:                "10",
		Platform:                 "Win32",
	}
	if item != want {
		t.Errorf("兜底记录与 Python 版不一致: got %+v, want %+v", item, want)
	}
}

func TestLookupNoMatchIsAnError(t *testing.T) {
	ua := Default()

	_, err := ua.Lookup("Netscape")
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("期望 ErrNoMatch，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "Netscape") {
		t.Errorf("错误信息应当带上请求的浏览器名: %v", err)
	}

	// 过滤条件过严导致候选为空时，Lookup 报错、Browser 退化成兜底
	strict := MustNew(WithMinVersion(9999))
	if _, err := strict.Lookup(); !errors.Is(err, ErrNoMatch) {
		t.Errorf("候选为空时期望 ErrNoMatch，得到 %v", err)
	}
	if got := strict.Random(); got != DefaultFallback {
		t.Errorf("候选为空时应当返回兜底 UA，得到 %q", got)
	}
	if items := strict.Filter(); len(items) != 0 {
		t.Errorf("候选集应为空，得到 %d 条", len(items))
	}
}

func TestShortcutsStayInFamily(t *testing.T) {
	ua := Default()
	cases := []struct {
		name    string
		get     func() string
		members []string
	}{
		{"Chrome", ua.Chrome, []string{BrowserChrome, BrowserChromeMobile, BrowserChromeMobileIOS}},
		{"Firefox", ua.Firefox, []string{BrowserFirefox, BrowserFirefoxMobile, BrowserFirefoxIOS}},
		{"Safari", ua.Safari, []string{BrowserSafari, BrowserMobileSafari}},
		{"Opera", ua.Opera, []string{BrowserOpera, BrowserOperaMobile}},
		{"Google", ua.Google, []string{BrowserGoogle}},
		{"Edge", ua.Edge, []string{BrowserEdge, BrowserEdgeMobile}},
	}

	for _, c := range cases {
		allowed := make(map[string]bool)
		for _, item := range ua.FilterFor(c.members...) {
			allowed[item.UserAgent] = true
		}
		if len(allowed) == 0 {
			t.Fatalf("%s 家族没有任何候选", c.name)
		}
		for i := 0; i < 100; i++ {
			if got := c.get(); !allowed[got] {
				t.Fatalf("%s 返回了家族之外的 UA: %s", c.name, got)
			}
		}
	}
}

func TestGetShortcutsReturnRecords(t *testing.T) {
	ua := Default()
	cases := []struct {
		name string
		get  func() Data
		want func(Data) bool
	}{
		{"GetChrome", ua.GetChrome, func(d Data) bool { return strings.Contains(d.Browser, "Chrome") }},
		{"GetFirefox", ua.GetFirefox, func(d Data) bool { return strings.Contains(d.Browser, "Firefox") }},
		{"GetSafari", ua.GetSafari, func(d Data) bool { return strings.Contains(d.Browser, "Safari") }},
		{"GetOpera", ua.GetOpera, func(d Data) bool { return strings.Contains(d.Browser, "Opera") }},
		{"GetGoogle", ua.GetGoogle, func(d Data) bool { return d.Browser == BrowserGoogle }},
		{"GetEdge", ua.GetEdge, func(d Data) bool { return strings.Contains(d.Browser, "Edge") }},
	}
	for _, c := range cases {
		item := c.get()
		if item.UserAgent == "" {
			t.Errorf("%s 返回了空记录", c.name)
		}
		if !c.want(item) {
			t.Errorf("%s 返回了不属于该家族的记录: %+v", c.name, item)
		}
	}
	if item := ua.GetRandom(); item.UserAgent == "" {
		t.Error("GetRandom() 返回了空记录")
	}
}

func TestFiltersApply(t *testing.T) {
	ua := MustNew(
		WithBrowsers(BrowserChrome),
		WithOS(OSWindows),
		WithPlatforms(TypeDesktop),
		WithMinVersion(130),
	)

	items := ua.Filter()
	if len(items) == 0 {
		t.Fatal("过滤后没有候选，测试前提不成立")
	}
	for _, item := range items {
		switch {
		case item.Browser != BrowserChrome:
			t.Fatalf("浏览器过滤失效: %+v", item)
		case item.OS != OSWindows:
			t.Fatalf("系统过滤失效: %+v", item)
		case item.Type != TypeDesktop:
			t.Fatalf("设备类型过滤失效: %+v", item)
		case item.BrowserVersionMajorMinor < 130:
			t.Fatalf("版本过滤失效: %+v", item)
		}
	}

	for i := 0; i < 50; i++ {
		item, err := ua.Lookup()
		if err != nil {
			t.Fatalf("Lookup() 报错: %v", err)
		}
		if item.Browser != BrowserChrome || item.OS != OSWindows {
			t.Fatalf("取到的记录不满足过滤条件: %+v", item)
		}
	}
}

func TestAccessors(t *testing.T) {
	ua := MustNew(WithBrowsers(BrowserEdge), WithOS(OSLinux), WithPlatforms(TypeMobile), WithMinVersion(120), WithMinPercentage(0.001))

	if got := ua.Browsers(); !slices.Equal(got, []string{BrowserEdge}) {
		t.Errorf("Browsers() = %v", got)
	}
	if got := ua.OS(); !slices.Equal(got, []string{OSLinux}) {
		t.Errorf("OS() = %v", got)
	}
	if got := ua.Platforms(); !slices.Equal(got, []string{TypeMobile}) {
		t.Errorf("Platforms() = %v", got)
	}
	if ua.MinVersion() != 120 {
		t.Errorf("MinVersion() = %v", ua.MinVersion())
	}
	if ua.MinPercentage() != 0.001 {
		t.Errorf("MinPercentage() = %v", ua.MinPercentage())
	}
	if ua.Fallback() != DefaultFallback {
		t.Errorf("Fallback() = %q", ua.Fallback())
	}
}

func TestWithEmptyArgsKeepsDefaults(t *testing.T) {
	ua := MustNew(WithBrowsers(), WithOS(), WithPlatforms())

	if !slices.Equal(ua.Browsers(), DefaultBrowsers()) {
		t.Error("WithBrowsers() 不传参数时应当沿用默认集合")
	}
	if !slices.Equal(ua.OS(), DefaultOS()) {
		t.Error("WithOS() 不传参数时应当沿用默认集合")
	}
	if !slices.Equal(ua.Platforms(), DefaultPlatforms()) {
		t.Error("WithPlatforms() 不传参数时应当沿用默认集合")
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	ua := MustNew(WithBrowsers(BrowserChrome))

	names := ua.Browsers()
	names[0] = "Hacked"
	if ua.Browsers()[0] != BrowserChrome {
		t.Error("Browsers() 必须返回副本，不能暴露内部切片")
	}

	items := ua.Filter()
	if len(items) == 0 {
		t.Fatal("没有候选")
	}
	items[0].UserAgent = "Hacked"
	if ua.Filter()[0].UserAgent == "Hacked" {
		t.Error("Filter() 必须返回副本，改动不能影响内嵌数据")
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name string
		opt  Option
		want string
	}{
		{"小写浏览器名", WithBrowsers("chrome"), "数据里没有浏览器"},
		{"数据里不存在的浏览器", WithBrowsers("Android"), "数据里没有浏览器"},
		{"数据里不存在的系统", WithOS("Ubuntu"), "数据里没有操作系统"},
		{"未知设备类型", WithPlatforms("watch"), "未知设备类型"},
		{"NaN 版本", WithMinVersion(math.NaN()), "minVersion"},
		{"Inf 占比", WithMinPercentage(math.Inf(1)), "minPercentage"},
		{"空兜底 UA", WithFallback(""), "fallback"},
	}

	for _, c := range cases {
		_, err := New(c.opt)
		if err == nil {
			t.Errorf("%s: 期望 New 报错，实际通过", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 错误信息 %q 不含 %q", c.name, err.Error(), c.want)
		}
	}

	// 报错要给出可选值，否则名字写错了根本不知道该写什么
	_, err := New(WithBrowsers("chrome"))
	if err == nil || !strings.Contains(err.Error(), BrowserSafari) {
		t.Errorf("错误信息应当列出可用浏览器名: %v", err)
	}
}

func TestMinPercentageParityWithPython(t *testing.T) {
	// 上游 percent 已不是"占比百分比"（实测全部 < 1），阈值 >= 1 会淘汰所有记录，
	// 结果恒为兜底 UA —— 与 Python 版行为一致，这里把它钉住，避免被当成 bug"修掉"。
	ua := MustNew(WithMinPercentage(1))
	if items := ua.Filter(); len(items) != 0 {
		t.Fatalf("期望候选为空，得到 %d 条", len(items))
	}
	if got := ua.Random(); got != DefaultFallback {
		t.Errorf("期望兜底 UA，得到 %q", got)
	}
}

func TestDuplicateNamesDoNotDoubleCount(t *testing.T) {
	ua := Default()
	once := ua.FilterFor(BrowserChrome)
	twice := ua.FilterFor(BrowserChrome, BrowserChrome)
	if len(once) == 0 {
		t.Fatal("Chrome 没有候选")
	}
	if len(once) != len(twice) {
		t.Errorf("重复浏览器名不应让记录出现两次: %d vs %d", len(once), len(twice))
	}
}

func TestPoolCacheIsBounded(t *testing.T) {
	ua := Default()
	names := AvailableBrowsers()

	// 用大量不同的名字组合去撑缓存，缓存必须有上限
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			ua.pool([]string{names[i], names[j]})
		}
	}

	ua.poolMu.RLock()
	size := len(ua.poolCache)
	ua.poolMu.RUnlock()
	if size > poolCacheLimit {
		t.Errorf("缓存条目 %d 超过上限 %d", size, poolCacheLimit)
	}
}

func TestWithRandIsDeterministic(t *testing.T) {
	a := MustNew(WithRand(rand.New(rand.NewPCG(1, 2))))
	b := MustNew(WithRand(rand.New(rand.NewPCG(1, 2))))

	for i := 0; i < 20; i++ {
		if a.Random() != b.Random() {
			t.Fatalf("相同种子应当得到相同序列（第 %d 次）", i)
		}
	}

	// 别名与本体走同一条路径
	c := MustNew(WithRand(rand.New(rand.NewPCG(7, 7))))
	d := MustNew(WithRand(rand.New(rand.NewPCG(7, 7))))
	if c.Chrome() != d.GoogleChrome() {
		t.Error("GoogleChrome() 应当与 Chrome() 等价")
	}
	e := MustNew(WithRand(rand.New(rand.NewPCG(9, 9))))
	f := MustNew(WithRand(rand.New(rand.NewPCG(9, 9))))
	if e.Firefox() != f.FF() {
		t.Error("FF() 应当与 Firefox() 等价")
	}
}

func TestWithRandNilIsIgnored(t *testing.T) {
	ua := MustNew(WithRand(nil))
	if ua.Random() == "" {
		t.Error("WithRand(nil) 之后仍应能取到 UA")
	}
}

func TestConcurrentUse(t *testing.T) {
	ua := MustNew(WithBrowsers(BrowserChrome, BrowserEdge, BrowserFirefox))

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if ua.Random() == "" {
					t.Error("并发调用返回了空 UA")
					return
				}
				_ = ua.Chrome()
				_ = ua.GetEdge()
				_ = ua.FilterFor(BrowserChrome, BrowserEdge)
			}
		}()
	}
	wg.Wait()
}

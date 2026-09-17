package fakeuseragent

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// 设备类型，对应 Data.Type。
const (
	TypeDesktop = "desktop"
	TypeMobile  = "mobile"
	TypeTablet  = "tablet"
)

// 浏览器名常量。
//
// 上游的浏览器名大小写敏感（Python 版 v2.0.0 起同样如此），写错等于永远匹配不到，
// 因此把内嵌数据里真实存在的名字都列在这里，供调用方引用而不是手写字符串。
//
// 注意 Python 版默认列表里的 Android / MiuiBrowser / Whale 在数据里并不存在，
// 本包不提供这几个常量，默认集合里也不包含它们（见 DefaultBrowsers）。
const (
	BrowserAmazonSilk            = "Amazon Silk"
	BrowserAppleMail             = "Apple Mail"
	BrowserChrome                = "Chrome"
	BrowserChromeMobile          = "Chrome Mobile"
	BrowserChromeMobileIOS       = "Chrome Mobile iOS"
	BrowserChromeMobileWebView   = "Chrome Mobile WebView"
	BrowserDuckDuckGoMobile      = "DuckDuckGo Mobile"
	BrowserEdge                  = "Edge"
	BrowserEdgeMobile            = "Edge Mobile"
	BrowserFacebook              = "Facebook"
	BrowserFirefox               = "Firefox"
	BrowserFirefoxIOS            = "Firefox iOS"
	BrowserFirefoxMobile         = "Firefox Mobile"
	BrowserGoogle                = "Google"
	BrowserMobileSafari          = "Mobile Safari"
	BrowserMobileSafariWKWebView = "Mobile Safari UI/WKWebView"
	BrowserOpera                 = "Opera"
	BrowserOperaMobile           = "Opera Mobile"
	BrowserSafari                = "Safari"
	BrowserSamsungInternet       = "Samsung Internet"
	BrowserSnapchat              = "Snapchat"
	BrowserTwitter               = "Twitter"
	BrowserYandex                = "Yandex Browser"
)

// 操作系统名常量，对应 Data.OS。
const (
	OSAndroid  = "Android"
	OSChromeOS = "Chrome OS"
	OSIOS      = "iOS"
	OSLinux    = "Linux"
	OSMacOSX   = "Mac OS X"
	OSWindows  = "Windows"
)

// defaultBrowsers 默认浏览器集合，顺序沿用 Python 版的默认列表。
//
// 与 Python 版默认值**等价**但不逐字相同：去掉了上游数据里根本不存在的
// Android / MiuiBrowser / Whale 三个名字（以及 OS 默认里的 Ubuntu）。
// 它们在 Python 版里只是永远匹配不到的死条目，留着会让"默认值看起来可用实则无效"。
var defaultBrowsers = []string{
	BrowserGoogle,
	BrowserChrome,
	BrowserFirefox,
	BrowserEdge,
	BrowserOpera,
	BrowserSafari,
	BrowserYandex,
	BrowserSamsungInternet,
	BrowserOperaMobile,
	BrowserMobileSafari,
	BrowserFirefoxMobile,
	BrowserFirefoxIOS,
	BrowserChromeMobile,
	BrowserChromeMobileIOS,
	BrowserMobileSafariWKWebView,
	BrowserEdgeMobile,
	BrowserDuckDuckGoMobile,
	BrowserTwitter,
	BrowserFacebook,
	BrowserAmazonSilk,
}

// defaultOS 默认操作系统集合（Python 版默认列表去掉数据里不存在的 Ubuntu）。
var defaultOS = []string{
	OSWindows,
	OSLinux,
	OSChromeOS,
	OSMacOSX,
	OSAndroid,
	OSIOS,
}

// defaultPlatforms 默认设备类型集合。
var defaultPlatforms = []string{TypeDesktop, TypeMobile, TypeTablet}

// DefaultBrowsers 返回默认浏览器名集合的副本。
func DefaultBrowsers() []string { return append([]string(nil), defaultBrowsers...) }

// DefaultOS 返回默认操作系统名集合的副本。
func DefaultOS() []string { return append([]string(nil), defaultOS...) }

// DefaultPlatforms 返回默认设备类型集合的副本。
func DefaultPlatforms() []string { return append([]string(nil), defaultPlatforms...) }

// Data 是一条 UA 记录，字段与上游 browsers.jsonl 一一对应（json tag 保留上游命名）。
//
// 上游的 device_brand / browser / os / os_version 允许为 null，Go 侧统一表现为空字符串。
type Data struct {
	UserAgent                string  `json:"useragent"`
	Percent                  float64 `json:"percent"`
	Type                     string  `json:"type"`
	DeviceBrand              string  `json:"device_brand"`
	Browser                  string  `json:"browser"`
	BrowserVersion           string  `json:"browser_version"`
	BrowserVersionMajorMinor float64 `json:"browser_version_major_minor"`
	OS                       string  `json:"os"`
	OSVersion                string  `json:"os_version"`
	Platform                 string  `json:"platform"`
}

// browsersJSONL 是上游脚本产出的数据（scripts/gen_ua_data.py 生成，gzip 只为了省二进制体积）。
//
//go:embed data/browsers.jsonl.gz
var browsersJSONL []byte

var (
	datasetOnce sync.Once
	dataset     []Data
	datasetErr  error

	availableBrowsers []string
	availableOS       []string
	browserSet        map[string]struct{}
	osSet             map[string]struct{}
)

// loadDataset 解析内嵌数据，进程内只做一次。
//
// Python 版每构造一个 UserAgent 就把整份 JSONL 重新读一遍；这里解析结果由所有实例共享，
// 返回的切片在进程内只读，调用方不要改写它。
func loadDataset() ([]Data, error) {
	datasetOnce.Do(func() {
		dataset, datasetErr = parseDataset(browsersJSONL)
		if datasetErr != nil {
			return
		}

		seenBrowsers := make(map[string]struct{}, 32)
		seenOS := make(map[string]struct{}, 8)
		for i := range dataset {
			seenBrowsers[dataset[i].Browser] = struct{}{}
			seenOS[dataset[i].OS] = struct{}{}
		}
		browserSet, availableBrowsers = flatten(seenBrowsers)
		osSet, availableOS = flatten(seenOS)
	})
	return dataset, datasetErr
}

// flatten 把集合变成"集合 + 字典序切片"两个视图：前者用于校验，后者用于报错提示
func flatten(set map[string]struct{}) (map[string]struct{}, []string) {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return set, names
}

// parseDataset 解析 JSON Lines：任何一行不合法都直接失败，绝不返回"半份数据"
// （内嵌数据损坏属于构建期问题，应由 pkg/fakeuseragent 的测试而不是运行期用户发现）。
func parseDataset(gzipped []byte) ([]Data, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		return nil, fmt.Errorf("fakeuseragent: 内嵌数据无法解压: %w", err)
	}
	defer zr.Close()

	scanner := bufio.NewScanner(zr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	items := make([]Data, 0, 10000)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item Data
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("fakeuseragent: 内嵌数据第 %d 行解析失败: %w", len(items)+1, err)
		}
		if item.UserAgent == "" {
			return nil, fmt.Errorf("fakeuseragent: 内嵌数据第 %d 行缺少 useragent", len(items)+1)
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("fakeuseragent: 读取内嵌数据失败: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("fakeuseragent: 内嵌数据为空")
	}
	return items, nil
}

// AvailableBrowsers 返回内嵌数据里出现过的全部浏览器名（字典序）。
//
// 数据加载失败时返回 nil——内嵌数据损坏属于构建期问题，正常构建下不会发生。
func AvailableBrowsers() []string {
	if _, err := loadDataset(); err != nil {
		return nil
	}
	return append([]string(nil), availableBrowsers...)
}

// AvailableOS 返回内嵌数据里出现过的全部操作系统名（字典序）。
func AvailableOS() []string {
	if _, err := loadDataset(); err != nil {
		return nil
	}
	return append([]string(nil), availableOS...)
}

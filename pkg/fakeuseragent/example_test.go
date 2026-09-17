package fakeuseragent_test

import (
	"fmt"
	"net/http"

	"github.com/laoin114514/luogu2api/pkg/fakeuseragent"
)

func ExampleRandom() {
	ua := fakeuseragent.Random()
	fmt.Println(len(ua) > 0)
	// Output: true
}

func ExampleDefault() {
	req, err := http.NewRequest(http.MethodGet, "https://www.luogu.com.cn/", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("User-Agent", fakeuseragent.Default().Random())
	fmt.Println(req.Header.Get("User-Agent") != "")
	// Output: true
}

func ExampleNew() {
	ua, err := fakeuseragent.New(
		fakeuseragent.WithBrowsers(fakeuseragent.BrowserChrome, fakeuseragent.BrowserEdge),
		fakeuseragent.WithOS(fakeuseragent.OSWindows, fakeuseragent.OSLinux),
		fakeuseragent.WithPlatforms(fakeuseragent.TypeDesktop),
		fakeuseragent.WithMinVersion(120),
	)
	if err != nil {
		panic(err)
	}

	// Browser 返回字符串，GetBrowser 返回完整记录
	fmt.Println(ua.Browser(fakeuseragent.BrowserEdge) != "", ua.GetBrowser(fakeuseragent.BrowserEdge).OS != "")
	// Output: true true
}

func ExampleUserAgent_Lookup() {
	ua, err := fakeuseragent.New(fakeuseragent.WithBrowsers(fakeuseragent.BrowserChrome))
	if err != nil {
		panic(err)
	}

	item, err := ua.Lookup()
	fmt.Println(err == nil, item.Browser)
	// Output: true Chrome
}

func ExampleUserAgent_Browser() {
	ua := fakeuseragent.Default()

	// 名字大小写敏感，匹配不到时退化成兜底 UA（与 Python 版一致）
	fmt.Println(ua.Browser("chrome") == fakeuseragent.DefaultFallback)
	// Output: true
}

func ExampleUserAgent_Filter() {
	ua, err := fakeuseragent.New(fakeuseragent.WithBrowsers(fakeuseragent.BrowserChrome))
	if err != nil {
		panic(err)
	}
	count := len(ua.Filter())
	fmt.Println(count > 0)
	// Output: true
}

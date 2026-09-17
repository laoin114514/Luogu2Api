package fakeuseragent

import (
	"slices"
	"testing"
)

func TestEmbeddedDatasetIsSane(t *testing.T) {
	items, err := loadDataset()
	if err != nil {
		t.Fatalf("内嵌数据加载失败: %v", err)
	}
	if len(items) < 5000 {
		t.Fatalf("内嵌数据只有 %d 条，疑似被裁剪", len(items))
	}

	for i, item := range items {
		switch {
		case item.UserAgent == "":
			t.Fatalf("第 %d 条缺少 useragent", i)
		case item.Browser == "":
			t.Fatalf("第 %d 条缺少 browser: %+v", i, item)
		case item.OS == "":
			t.Fatalf("第 %d 条缺少 os: %+v", i, item)
		case item.Type != TypeDesktop && item.Type != TypeMobile && item.Type != TypeTablet:
			t.Fatalf("第 %d 条设备类型非法: %q", i, item.Type)
		case item.BrowserVersionMajorMinor < 0:
			t.Fatalf("第 %d 条版本号为负: %+v", i, item)
		case item.Percent < 0:
			t.Fatalf("第 %d 条 percent 为负: %+v", i, item)
		}
	}
}

// TestDatasetKeepsUpstreamDuplicates 钉住一条刻意保留的上游特征：
// 同一个 UA 字符串会重复出现多次，Python 版的 random.choice 是在"行"上等概率取样，
// 重复行因此承担了加权作用。谁要是"顺手去重"，随机分布就与 Python 版不一致了。
func TestDatasetKeepsUpstreamDuplicates(t *testing.T) {
	items, err := loadDataset()
	if err != nil {
		t.Fatalf("内嵌数据加载失败: %v", err)
	}

	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[item.UserAgent]++
	}
	duplicated := 0
	for _, n := range counts {
		if n > 1 {
			duplicated += n - 1
		}
	}
	if duplicated == 0 {
		t.Fatal("数据里没有重复行，疑似被去重：随机分布会与 Python 版不一致")
	}
	t.Logf("数据 %d 行，去重后 %d 条 UA，重复行 %d 行", len(items), len(counts), duplicated)
}

func TestAvailableNames(t *testing.T) {
	browsers := AvailableBrowsers()
	if !slices.IsSorted(browsers) {
		t.Error("AvailableBrowsers() 应当按字典序返回")
	}
	for _, want := range []string{BrowserChrome, BrowserMobileSafari, BrowserSnapchat, BrowserAppleMail, BrowserChromeMobileWebView} {
		if !slices.Contains(browsers, want) {
			t.Errorf("数据里应当有浏览器 %q", want)
		}
	}
	// Python 默认列表里并不存在、本包也不再列出的名字
	for _, gone := range []string{"Android", "MiuiBrowser", "Whale"} {
		if slices.Contains(browsers, gone) {
			t.Errorf("数据里不该有浏览器 %q", gone)
		}
	}

	osNames := AvailableOS()
	if !slices.IsSorted(osNames) {
		t.Error("AvailableOS() 应当按字典序返回")
	}
	for _, want := range []string{OSWindows, OSLinux, OSMacOSX, OSChromeOS, OSAndroid, OSIOS} {
		if !slices.Contains(osNames, want) {
			t.Errorf("数据里应当有操作系统 %q", want)
		}
	}
	if slices.Contains(osNames, "Ubuntu") {
		t.Error("数据里不该有 Ubuntu：Python 默认列表里的这一项永远匹配不到")
	}
}

func TestDefaultSetsMatchPython(t *testing.T) {
	if got := len(DefaultBrowsers()); got != 20 {
		t.Errorf("默认浏览器数量 = %d，期望 20（Python 的 23 个减去数据里不存在的 3 个）", got)
	}
	if got := len(DefaultOS()); got != 6 {
		t.Errorf("默认操作系统数量 = %d，期望 6（Python 的 7 个减去数据里不存在的 Ubuntu）", got)
	}
	if got := DefaultPlatforms(); !slices.Equal(got, []string{TypeDesktop, TypeMobile, TypeTablet}) {
		t.Errorf("默认设备类型 = %v", got)
	}

	// 默认集合里不允许有"永远匹配不到"的死条目
	browsers := AvailableBrowsers()
	for _, name := range DefaultBrowsers() {
		if !slices.Contains(browsers, name) {
			t.Errorf("默认浏览器 %q 不在数据里", name)
		}
	}
	osNames := AvailableOS()
	for _, name := range DefaultOS() {
		if !slices.Contains(osNames, name) {
			t.Errorf("默认操作系统 %q 不在数据里", name)
		}
	}

	// 访问器返回的必须是副本
	names := DefaultBrowsers()
	names[0] = "Hacked"
	if DefaultBrowsers()[0] == "Hacked" {
		t.Error("DefaultBrowsers() 必须返回副本")
	}
}

func TestDefaultInstanceHasCandidatesForEveryDefaultBrowser(t *testing.T) {
	ua := Default()
	for _, name := range ua.Browsers() {
		if len(ua.FilterFor(name)) == 0 {
			t.Errorf("默认浏览器 %q 在默认过滤条件下没有任何候选", name)
		}
	}
	if len(ua.Filter()) == 0 {
		t.Fatal("默认实例的候选集为空")
	}
}

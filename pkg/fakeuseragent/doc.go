// Package fakeuseragent 提供内置真实世界数据的 User-Agent 生成器。
//
// 这是 Python 库 fake-useragent（v2.x）的 Go 移植：数据随包发布、运行期零网络请求，
// 按浏览器 / 操作系统 / 设备类型过滤后，在候选记录上等概率随机取一条。
//
// 与 Python 版保持的语义：过滤名大小写敏感、无匹配时退化成兜底 UA（DefaultFallback）、
// 候选列表里保留上游的重复行（重复行承担加权作用，因此不做去重）。
//
// 最常见的用法：
//
//	req.Header.Set("User-Agent", fakeuseragent.Random())
//
// 需要固定过滤条件并复用时（实例构造后只读，可并发使用）：
//
//	ua, err := fakeuseragent.New(
//		fakeuseragent.WithBrowsers(fakeuseragent.BrowserChrome, fakeuseragent.BrowserEdge),
//		fakeuseragent.WithOS(fakeuseragent.OSWindows),
//		fakeuseragent.WithMinVersion(130),
//	)
//	if err != nil {
//		return err
//	}
//	req.Header.Set("User-Agent", ua.Random())
//
// 数据来源、许可与再生成方式见 pkg/fakeuseragent/README.md。
package fakeuseragent

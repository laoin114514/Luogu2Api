package fakeuseragent

import (
	"math/rand/v2"
	"sync"
)

// Option 在 New 构造期间调整生成器的过滤条件。
//
// 所有 Option 都只是赋值，合法性（名字是否存在、阈值是否可用）统一由 New 校验；
// 构造函数返回后实例不再变化，因此可以并发使用。
type Option func(*UserAgent)

// WithBrowsers 限定浏览器名（大小写敏感，建议用 BrowserChrome 之类的常量）。
//
// 不传参数表示沿用默认集合（DefaultBrowsers）。传了数据里不存在的名字，
// New 会直接报错并列出可选值——在 Python 版里这种写法只会静默退化成兜底 UA。
func WithBrowsers(browsers ...string) Option {
	return func(u *UserAgent) {
		if len(browsers) == 0 {
			return
		}
		u.browsers = append([]string(nil), browsers...)
	}
}

// WithOS 限定操作系统名（大小写敏感，建议用 OSWindows 之类的常量），
// 不传参数表示沿用默认集合（DefaultOS）。
func WithOS(names ...string) Option {
	return func(u *UserAgent) {
		if len(names) == 0 {
			return
		}
		u.os = append([]string(nil), names...)
	}
}

// WithPlatforms 限定设备类型（TypeDesktop / TypeMobile / TypeTablet），
// 不传参数表示沿用默认集合（DefaultPlatforms）。
func WithPlatforms(platforms ...string) Option {
	return func(u *UserAgent) {
		if len(platforms) == 0 {
			return
		}
		u.platforms = append([]string(nil), platforms...)
	}
}

// WithMinVersion 只保留主次版本号 >= v 的记录（比较 Data.BrowserVersionMajorMinor），
// 0 表示不过滤，与 Python 版的 min_version 参数一致。
func WithMinVersion(v float64) Option {
	return func(u *UserAgent) { u.minVersion = v }
}

// WithMinPercentage 只保留 Data.Percent >= p 的记录，0 表示不过滤。
//
// 保留这个参数只为与 Python 版对齐：上游现在的 percent 已经不是"使用占比百分比"，
// 实测取值在 0.0004 ~ 0.094 之间（全部小于 1），任何 >= 1 的阈值都会淘汰所有记录，
// 让结果恒为兜底 UA。新代码不建议使用。
func WithMinPercentage(p float64) Option {
	return func(u *UserAgent) { u.minPercentage = p }
}

// WithFallback 设置匹配不到候选时返回的兜底 UA，默认为 DefaultFallback。
func WithFallback(ua string) Option {
	return func(u *UserAgent) { u.fallback = ua }
}

// WithRand 注入随机源（便于测试固定序列），默认为 math/rand/v2 的全局源。
//
// 传入的 *rand.Rand 会被加锁串行使用：本包承诺实例可并发调用，
// 而 rand.Rand 自身不是并发安全的。传 nil 表示不做任何修改。
func WithRand(r *rand.Rand) Option {
	if r == nil {
		return func(*UserAgent) {}
	}
	var mu sync.Mutex
	return func(u *UserAgent) {
		u.intN = func(n int) int {
			mu.Lock()
			defer mu.Unlock()
			return r.IntN(n)
		}
	}
}

package service

import "errors"

// 业务层对外的哨兵错误，供 handler 映射 HTTP 状态码与业务码。
//
// 目的：把"上游/依赖的问题"统一翻译成业务语义，让 handler 不必 import client，
// 也就不会把"洛谷 SDK 的错误长什么样"泄漏到 HTTP 层。
var (
	// ErrInvalidParam 请求参数不合法
	ErrInvalidParam = errors.New("请求参数不合法")
	// ErrPoolUnavailable 号池中没有可用账号（全部离线/待重登/停用）
	ErrPoolUnavailable = errors.New("号池暂无可用的洛谷账号")
	// ErrUpstreamUnauthorized 洛谷侧登录态失效（换号重试后仍失败）
	ErrUpstreamUnauthorized = errors.New("洛谷登录态失效")
)

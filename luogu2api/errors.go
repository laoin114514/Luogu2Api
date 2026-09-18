package luogu2api

import (
	"errors"
	"fmt"
)

// 业务码，与 internal/response 的约定一致。
//
// HTTP 状态码表达传输层语义，业务码表达业务语义，两者刻意分开：号池不可用时是
// HTTP 503 + 业务码 1001，调用方按业务码分支即可。
const (
	// CodeOK 成功
	CodeOK = 0
	// CodeInvalidParam 参数错误
	CodeInvalidParam = 400
	// CodeUnauthorized 令牌无效（X-Admin-Token 缺失或不对）
	CodeUnauthorized = 401
	// CodeNotFound 资源不存在
	CodeNotFound = 404
	// CodeConflict 资源冲突
	CodeConflict = 409
	// CodeInternalError 服务内部错误
	CodeInternalError = 500
	// CodePoolExhausted 号池中没有可用账号（HTTP 503）
	CodePoolExhausted = 1001
	// CodeUpstreamUnauthorized 洛谷登录态失效（换号重试后仍失败，HTTP 502）
	CodeUpstreamUnauthorized = 1002
)

// Error 是 SDK 返回的统一业务错误。
//
// 两种情况都会得到 *Error：
//  1. 服务端以非 0 业务码回应，HTTPStatus 是真实的 HTTP 状态码；
//  2. 参数在本地就被判定为非法（例如 pid 为空），HTTPStatus 为 0——没有发出请求，
//     也就没有状态码可谈。
//
// 网络错误、响应体不是合法 JSON、context 取消等不是 *Error，而是被 fmt.Errorf 包装的
// 原始错误，errors.Is / errors.As 仍能判断（例如 errors.Is(err, context.DeadlineExceeded)）。
type Error struct {
	// HTTPStatus HTTP 状态码；本地参数校验失败（未发出请求）时为 0
	HTTPStatus int
	// Code 业务码，见 Code* 常量
	Code int
	// Message 服务端返回的文案（或本地校验失败的说明）
	Message string
}

// Error 实现 error 接口
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.HTTPStatus == 0 {
		return fmt.Sprintf("luogu2api: code=%d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("luogu2api: http=%d code=%d: %s", e.HTTPStatus, e.Code, e.Message)
}

// CodeOf 返回错误的业务码；err 不是 *Error 或是 nil 时返回 -1。
//
// 成功码是 0，所以用 -1 表示"这不是服务端业务错误"，两者不会混淆。
func CodeOf(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return -1
}

// HTTPStatusOf 返回错误的 HTTP 状态码；本地校验错误与非 *Error 错误返回 0。
func HTTPStatusOf(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatus
	}
	return 0
}

func hasCode(err error, code int) bool {
	return err != nil && CodeOf(err) == code
}

// IsInvalidParam 参数不合法（业务码 400）。本地校验失败也走这个业务码。
func IsInvalidParam(err error) bool { return hasCode(err, CodeInvalidParam) }

// IsUnauthorized 令牌无效（业务码 401）。SDK 不重试令牌错误：重试也没有用。
func IsUnauthorized(err error) bool { return hasCode(err, CodeUnauthorized) }

// IsNotFound 资源不存在（业务码 404）
func IsNotFound(err error) bool { return hasCode(err, CodeNotFound) }

// IsConflict 资源冲突（业务码 409）
func IsConflict(err error) bool { return hasCode(err, CodeConflict) }

// IsPoolExhausted 号池中没有可用账号（HTTP 503 + 业务码 1001）：稍后重试即可。
func IsPoolExhausted(err error) bool { return hasCode(err, CodePoolExhausted) }

// IsUpstreamUnauthorized 洛谷侧登录态全部失效（HTTP 502 + 业务码 1002）：
// 号池里所有账号的 cookie 都不可用，需要人工处理。
func IsUpstreamUnauthorized(err error) bool { return hasCode(err, CodeUpstreamUnauthorized) }

// newInvalidParam 构造"本地参数校验失败"的错误（不会发出请求）
func newInvalidParam(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidParam, Message: fmt.Sprintf(format, args...)}
}

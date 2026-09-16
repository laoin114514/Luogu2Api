package luoguclient

import "fmt"

// AuthError 登录/认证失败
type AuthError struct {
	Code    int
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth error [%d]: %s", e.Code, e.Message)
}

// CSRFError CSRF token 获取或过期
type CSRFError struct {
	Err error
}

func (e *CSRFError) Error() string {
	return fmt.Sprintf("csrf error: %v", e.Err)
}

func (e *CSRFError) Unwrap() error {
	return e.Err
}

// NetworkError 网络请求失败
type NetworkError struct {
	Err error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("network error: %v", e.Err)
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}

// UnauthorizedError 未登录（或权限不足）时调用需认证的 API
//
// 洛谷对未登录访问受保护页面（/record/*、/problem/solution/*、/training/{id}、
// /user/setting 等）直接返回 401，SDK 将其统一转换成本类型。
type UnauthorizedError struct {
	StatusCode int    // 触发该错误的 HTTP 状态码（401/403）
	Message    string // 触发该错误的操作描述，例如 "get problem P1001"
}

func (e *UnauthorizedError) Error() string {
	if e.Message == "" {
		// 保持零值行为与历史版本一致
		return "unauthorized: please login first"
	}
	return fmt.Sprintf("unauthorized: %s (status %d)", e.Message, e.StatusCode)
}

package luoguclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AuthError 登录/认证失败
//
// 洛谷登录失败（HTTP 400）响应形如：
//
//	{"errorCode":400,"errorType":"LuoguWeb\\Spilopelia\\Exception\\CaptchaNotMatchException",
//	 "errorMessage":"图形验证码错误","errorData":{":":0}}
//
// Type 即 errorType 中的异常类名，可用于区分失败原因，
// 例如 CaptchaNotMatchException（验证码错误，换一张重试即可）。
type AuthError struct {
	Code    int
	Type    string
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth error [%d]: %s", e.Code, e.Message)
}

// APIError 洛谷返回的业务错误（非 200，响应体是 errorCode/errorType/errorMessage 结构）
//
// 用于需要已登录的写接口：HTTP 状态码本身是 4xx，但真正的原因在响应体里。
// 例如偏好设置更新时把 openSource 改回 0/-1：
//
//	{"errorCode":400,"errorType":"Symfony\\Component\\HttpKernel\\Exception\\BadRequestHttpException",
//	 "errorMessage":"加入代码公开计划未满 30 天，不能退出","errorData":{":":0}}
//
// 未登录/权限不足（401/403）不产生本类型，而是统一映射成 *UnauthorizedError。
type APIError struct {
	StatusCode int    // HTTP 状态码，如 400
	Code       int    // 响应体里的 errorCode（通常与 StatusCode 相同，取不到时为 0）
	Type       string // 洛谷异常类名
	Message    string // 洛谷 errorMessage（业务可读文案）
}

func (e *APIError) Error() string {
	if e.Code != 0 && e.Code != e.StatusCode {
		return fmt.Sprintf("luogu api error [%d, code %d]: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("luogu api error [%d]: %s", e.StatusCode, e.Message)
}

// parseAPIError 把洛谷的 JSON 业务错误响应转成 *APIError（调用方负责关闭 resp.Body）
func parseAPIError(resp *http.Response, format string, args ...interface{}) error {
	op := fmt.Sprintf(format, args...)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode, Message: op + " (读取响应体失败: " + err.Error() + ")"}
	}

	var payload struct {
		ErrorCode    int    `json:"errorCode"`
		ErrorType    string `json:"errorType"`
		ErrorMessage string `json:"errorMessage"`
		Code         int    `json:"code"`
		Message      string `json:"message"`
	}
	if json.Unmarshal(data, &payload) == nil {
		code := payload.ErrorCode
		if code == 0 {
			code = payload.Code
		}
		message := payload.ErrorMessage
		if message == "" {
			message = payload.Message
		}
		if payload.ErrorType != "" || message != "" {
			return &APIError{
				StatusCode: resp.StatusCode,
				Code:       code,
				Type:       payload.ErrorType,
				Message:    message,
			}
		}
	}

	// 响应不是预期 JSON：保留原文片段，避免丢失失败原因
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("%s: %s", op, truncateRunes(string(data), 200)),
	}
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
// /user/setting 等）直接返回 401，Client 将其统一转换成本类型。
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

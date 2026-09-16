// Package response 定义统一的 HTTP 响应格式。
//
// 约定：HTTP 状态码表达传输层语义，Body.Code 表达业务码（0 = 成功）。
// 业务码与 HTTP 状态码刻意分开：号池不可用（1001）与实际 HTTP 语义（503）
// 是两件事，前端按业务码分支即可。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 业务码
const (
	// CodeOK 成功
	CodeOK = 0
	// CodeInvalidParam 参数错误
	CodeInvalidParam = 400
	// CodeUnauthorized 未授权（管理令牌无效等）
	CodeUnauthorized = 401
	// CodeNotFound 资源不存在
	CodeNotFound = 404
	// CodeConflict 资源冲突
	CodeConflict = 409
	// CodeInternalError 服务内部错误
	CodeInternalError = 500
	// CodePoolExhausted 号池中没有可用账号
	CodePoolExhausted = 1001
	// CodeUpstreamUnauthorized 洛谷登录态失效（换号重试后仍失败）
	CodeUpstreamUnauthorized = 1002
)

// Body 统一响应体
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// JSON 输出指定 HTTP 状态码与业务码的响应
func JSON(c *gin.Context, httpStatus, code int, message string, data interface{}) {
	c.JSON(httpStatus, Body{Code: code, Message: message, Data: data})
}

// OK 成功响应（HTTP 200、业务码 0）
func OK(c *gin.Context, data interface{}) {
	JSON(c, http.StatusOK, CodeOK, "ok", data)
}

// FailCode 失败响应（显式指定业务码）
func FailCode(c *gin.Context, httpStatus, code int, message string) {
	JSON(c, httpStatus, code, message, nil)
}

// Fail 失败响应，业务码默认与 HTTP 状态码一致
func Fail(c *gin.Context, httpStatus int, message string) {
	FailCode(c, httpStatus, httpStatus, message)
}

// NotFound 404
func NotFound(c *gin.Context) {
	Fail(c, http.StatusNotFound, "接口不存在")
}

// MethodNotAllowed 405
func MethodNotAllowed(c *gin.Context) {
	Fail(c, http.StatusMethodNotAllowed, "方法不允许")
}

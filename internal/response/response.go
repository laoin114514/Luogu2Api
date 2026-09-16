// Package response 定义统一的 HTTP 响应格式。
//
// 约定：HTTP 状态码表达传输层语义，Body.Code 表达业务码（0 = 成功）。
// 后续可以在此基础上扩展独立的业务错误码表。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CodeOK 业务成功码
const CodeOK = 0

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

// Fail 失败响应，业务码默认与 HTTP 状态码一致
func Fail(c *gin.Context, httpStatus int, message string) {
	JSON(c, httpStatus, httpStatus, message, nil)
}

// NotFound 404
func NotFound(c *gin.Context) {
	Fail(c, http.StatusNotFound, "接口不存在")
}

// MethodNotAllowed 405
func MethodNotAllowed(c *gin.Context) {
	Fail(c, http.StatusMethodNotAllowed, "方法不允许")
}

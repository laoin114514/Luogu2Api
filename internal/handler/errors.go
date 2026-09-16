package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// Fail 把 service 层的哨兵错误映射成 HTTP 状态码 + 业务码。
//
// 未知错误只回通用文案（不把内部细节抛给调用方），完整错误写进服务日志。
func Fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidParam):
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, err.Error())
	case errors.Is(err, model.ErrAccountNotFound):
		response.FailCode(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	case errors.Is(err, model.ErrAccountExists):
		response.FailCode(c, http.StatusConflict, response.CodeConflict, err.Error())
	case errors.Is(err, service.ErrPoolUnavailable):
		response.FailCode(c, http.StatusServiceUnavailable, response.CodePoolExhausted, err.Error())
	case errors.Is(err, service.ErrUpstreamUnauthorized):
		response.FailCode(c, http.StatusBadGateway, response.CodeUpstreamUnauthorized, err.Error())
	default:
		slog.Error("请求处理失败", "err", err)
		response.FailCode(c, http.StatusInternalServerError, response.CodeInternalError, "服务内部错误")
	}
}

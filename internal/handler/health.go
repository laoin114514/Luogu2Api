// Package handler 是 HTTP 处理层：解析请求、调用 service、渲染响应。
// 不在这里写业务逻辑，也不直接访问数据库。
package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// HealthChecker 健康检查业务能力（由 service 层实现，便于测试替换）
type HealthChecker interface {
	Check(ctx context.Context) service.HealthReport
}

// HealthHandler 健康检查处理器
type HealthHandler struct {
	health HealthChecker
}

// NewHealthHandler 创建健康检查处理器
func NewHealthHandler(health HealthChecker) *HealthHandler {
	return &HealthHandler{health: health}
}

// Get 处理 GET /healthz
//
// 依赖正常返回 200；数据库不可用返回 503，便于探活/负载均衡摘除实例。
func (h *HealthHandler) Get(c *gin.Context) {
	report := h.health.Check(c.Request.Context())

	httpStatus := http.StatusOK
	if report.Status != service.StatusOK {
		httpStatus = http.StatusServiceUnavailable
	}
	response.JSON(c, httpStatus, response.CodeOK, report.Status, report)
}

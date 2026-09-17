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

// Live 处理 GET /livez
//
// 纯存活探针：进程还能处理 HTTP 就返回 200，刻意不看数据库与号池——/healthz 是
// 就绪探针（依赖不可用时 503），容器 HEALTHCHECK 用 /livez 才不会因为"首次部署
// 还没导账号"把实例判成 unhealthy。
func (h *HealthHandler) Live(c *gin.Context) {
	response.OK(c, gin.H{"status": service.StatusOK})
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

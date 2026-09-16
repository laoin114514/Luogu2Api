package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/response"
)

// PoolReporter 号池状态能力（由 client.Pool 实现）
type PoolReporter interface {
	Stats() client.Stats
}

// PoolHandler 号池状态接口处理器
type PoolHandler struct {
	pool PoolReporter
}

// NewPoolHandler 创建号池状态处理器
func NewPoolHandler(pool PoolReporter) *PoolHandler {
	return &PoolHandler{pool: pool}
}

// Status 处理 GET /api/v1/pool/status
//
// 只读内存快照，供运维观察在线账号数与最近一轮扫描结果；不含任何凭据。
func (h *PoolHandler) Status(c *gin.Context) {
	response.OK(c, h.pool.Stats())
}

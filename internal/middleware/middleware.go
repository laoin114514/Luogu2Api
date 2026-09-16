// Package middleware 存放 gin 中间件（请求 ID、访问日志等）。
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// HeaderRequestID 请求 ID 的响应头
	HeaderRequestID = "X-Request-ID"
	// ContextKeyRequestID 请求 ID 在 gin.Context 中的键
	ContextKeyRequestID = "request_id"
)

// RequestID 透传上游请求 ID，没有则生成一个
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" {
			id = newID()
		}
		c.Set(ContextKeyRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// AccessLog 用 slog 记录访问日志
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"cost_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", c.GetString(ContextKeyRequestID),
		)
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

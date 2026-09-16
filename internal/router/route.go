// Package router 负责组装 gin 引擎与路由表。
//
// 路由注册集中在本文件，handler 只关心各自的处理逻辑。
package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/handler"
	"github.com/laoin114514/luogu2api/internal/middleware"
	"github.com/laoin114514/luogu2api/internal/response"
)

// Deps 路由依赖。后续新增 handler 时在这里扩展，由 main 注入。
type Deps struct {
	Logger *slog.Logger
	Health *handler.HealthHandler
	Env    string // dev / test / prod，用于决定 gin 运行模式
}

// New 构建 gin 引擎并注册路由
func New(deps Deps) *gin.Engine {
	gin.SetMode(ginMode(deps.Env))

	r := gin.New()
	r.Use(
		gin.Recovery(),
		middleware.RequestID(),
		middleware.AccessLog(deps.Logger),
	)

	// 未匹配路由与方法
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) { response.NotFound(c) })
	r.NoMethod(func(c *gin.Context) { response.MethodNotAllowed(c) })

	// 健康检查：供探活使用，无需认证
	r.GET("/healthz", deps.Health.Get)

	// 业务路由待接口设计完成后在此注册，例如：
	//
	//	v1 := r.Group("/api/v1")
	//	v1.GET("/problems/:pid", problemHandler.Get)
	//	v1.GET("/records/:rid", recordHandler.Get)

	return r
}

func ginMode(env string) string {
	if env == "prod" {
		return gin.ReleaseMode
	}
	if env == "test" {
		return gin.TestMode
	}
	return gin.DebugMode
}

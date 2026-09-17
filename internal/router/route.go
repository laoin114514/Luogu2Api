// Package router 负责组装 gin 引擎与路由表。
//
// 路由注册集中在本文件，handler 只关心各自的处理逻辑。
package router

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/handler"
	"github.com/laoin114514/luogu2api/internal/middleware"
	"github.com/laoin114514/luogu2api/internal/response"
)

// Deps 路由依赖。后续新增 handler 时在这里扩展，由 main 注入。
type Deps struct {
	Logger  *slog.Logger
	Health  *handler.HealthHandler
	Problem *handler.ProblemHandler
	Record  *handler.RecordHandler
	Pool    *handler.PoolHandler
	Account *handler.AccountHandler
	Env     string // dev / test / prod，用于决定 gin 运行模式
	// DashboardDir 是 Pool-Dashboard 的 Vite 构建目录。为空时不注册静态站点，
	// 便于 HTTP 路由单测以及只运行 API 的场景。
	DashboardDir string
	// AdminToken 是 /api/v1 全部接口的访问令牌（请求头 X-Admin-Token）。
	// 为空时中间件拒绝所有 API 请求，且管理路由不注册（fail closed），
	// 避免出现无鉴权的接口。
	AdminToken string
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

	// 探活接口：无需认证，是仅有的两个公开入口。
	//   /healthz 是就绪探针：数据库不可用或号池没有在线账号时按设计返回 503；
	//   /livez   是存活探针：只看进程还能不能处理 HTTP，恒定 200。
	r.GET("/healthz", deps.Health.Get)
	r.GET("/livez", deps.Health.Live)

	// 除上面两个探活接口外，/api/v1 下的**所有**接口都要令牌：业务只读接口
	// （号池状态、题目、提交记录）与管理接口一视同仁，统一在分组上挂中间件，
	// 后续新增的路由默认就在保护范围内，不需要各自记得加。
	v1 := r.Group("/api/v1", middleware.AdminAuth(deps.AdminToken))

	// 号池状态：只读、不含凭据，便于运维查看在线账号数
	v1.GET("/pool/status", deps.Pool.Status)

	// 业务路由（示例：题目读取，走号池选号 + 失效换号重试）
	v1.GET("/problems", deps.Problem.Search)
	v1.GET("/problems/:pid", deps.Problem.Get)

	// 用户提交记录：按洛谷 UID 查询，可选按题目 / 状态过滤
	v1.GET("/users/:uid/records", deps.Record.ListByUser)

	// 管理路由：号池导入/启停/改密/强制重登。令牌校验已由 v1 分组统一负责；
	// ADMIN_TOKEN 为空时整组不注册（保持 fail closed：返回 404 而不是 401，
	// 便于管理台区分"服务端没启用管理接口"与"令牌不对"）。
	if deps.AdminToken != "" && deps.Account != nil {
		admin := v1.Group("/admin")
		admin.GET("/accounts", deps.Account.List)
		admin.POST("/accounts", deps.Account.Create)
		admin.GET("/accounts/:id", deps.Account.Get)
		admin.PATCH("/accounts/:id", deps.Account.Update)
		admin.PUT("/accounts/:id/password", deps.Account.UpdatePassword)
		admin.DELETE("/accounts/:id", deps.Account.Delete)
		admin.POST("/accounts/:id/relogin", deps.Account.Relogin)
	}

	// 管理站点单独挂在 /dashboard/，不占用 API 根路径。Vite 的 base 也固定为
	// 该前缀，所以构建后的 JS/CSS 均由同一 Gin 进程按同源方式提供。
	if deps.DashboardDir != "" {
		r.GET("/dashboard", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, "/dashboard/")
		})
		r.StaticFS("/dashboard", http.Dir(deps.DashboardDir))
	}

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

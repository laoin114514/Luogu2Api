package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
)

// HeaderAdminToken 接口令牌的请求头。
//
// /api/v1 下的全部接口（业务接口与管理接口）共用这一个请求头；只有 /healthz、
// /livez 这类探活接口不需要。
const HeaderAdminToken = "X-Admin-Token"

// AdminAuth 校验 API 令牌。
//
// 挂在 /api/v1 整个分组上：题目、提交记录、号池状态等业务接口与管理接口一律
// 需要 X-Admin-Token，没有例外。
//
// 用常量时间比较，避免通过响应时间差猜出令牌；令牌来自 ADMIN_TOKEN，为空时
// 这里拒绝一切请求（fail closed），不会出现"忘配置令牌就裸奔"的窗口。
func AdminAuth(token string) gin.HandlerFunc {
	expected := []byte(token)

	return func(c *gin.Context) {
		got := []byte(c.GetHeader(HeaderAdminToken))
		if len(expected) == 0 || subtle.ConstantTimeCompare(got, expected) != 1 {
			response.FailCode(c, http.StatusUnauthorized, response.CodeUnauthorized, "令牌无效")
			c.Abort()
			return
		}
		c.Next()
	}
}

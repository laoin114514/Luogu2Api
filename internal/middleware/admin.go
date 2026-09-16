package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
)

// HeaderAdminToken 管理接口的令牌请求头
const HeaderAdminToken = "X-Admin-Token"

// AdminAuth 校验管理接口令牌。
//
// 用常量时间比较，避免通过响应时间差猜出令牌；ADMIN_TOKEN 为空时路由根本
// 不会注册（fail closed），因此这里的空令牌分支只是兜底。
func AdminAuth(token string) gin.HandlerFunc {
	expected := []byte(token)

	return func(c *gin.Context) {
		got := []byte(c.GetHeader(HeaderAdminToken))
		if len(expected) == 0 || subtle.ConstantTimeCompare(got, expected) != 1 {
			response.FailCode(c, http.StatusUnauthorized, response.CodeUnauthorized, "管理令牌无效")
			c.Abort()
			return
		}
		c.Next()
	}
}

package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
)

func newAdminEngine(token string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/admin", AdminAuth(token))
	group.GET("/accounts", func(c *gin.Context) { response.OK(c, gin.H{"ok": true}) })
	return r
}

func TestAdminAuthAcceptsValidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	req.Header.Set(HeaderAdminToken, "s3cret")

	rec := httptest.NewRecorder()
	newAdminEngine("s3cret").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

// token 为空时管理路由本就不该注册；这里再兜一层，确保不会"无令牌即放行"
func TestAdminAuthRejectsMissingOrWrongToken(t *testing.T) {
	tests := []struct {
		name   string
		config string
		header string
	}{
		{"缺少请求头", "s3cret", ""},
		{"令牌错误", "s3cret", "wrong"},
		{"令牌前缀相同但不完整", "s3cret", "s3cre"},
		{"服务端未配置令牌", "", "s3cret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
			if tt.header != "" {
				req.Header.Set(HeaderAdminToken, tt.header)
			}

			rec := httptest.NewRecorder()
			newAdminEngine(tt.config).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}

			var body response.Body
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if body.Code != response.CodeUnauthorized {
				t.Errorf("code = %d, want %d", body.Code, response.CodeUnauthorized)
			}
		})
	}
}

package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/handler"
	"github.com/laoin114514/luogu2api/internal/middleware"
	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

type stubHealth struct{ report service.HealthReport }

func (s stubHealth) Check(context.Context) service.HealthReport { return s.report }

type stubProblem struct{}

func (stubProblem) Get(context.Context, string) (*client.Problem, error) {
	return &client.Problem{PID: "P1001", Title: "A+B Problem"}, nil
}

func (stubProblem) Search(context.Context, string, int, int) (*client.SearchResult, error) {
	return &client.SearchResult{Total: 0, Page: 1}, nil
}

type stubRecord struct{}

func (stubRecord) ListByUser(context.Context, int, string, int, int) (*service.RecordListDTO, error) {
	return &service.RecordListDTO{UID: 42, Page: 1, Count: 1}, nil
}

type stubPool struct{}

func (stubPool) Stats() client.Stats { return client.Stats{Total: 1, Online: 1} }

type stubAccounts struct{}

func (stubAccounts) Create(context.Context, string, string, string) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1, Username: "user1"}, nil
}
func (stubAccounts) Get(context.Context, uint) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1, Username: "user1"}, nil
}
func (stubAccounts) List(context.Context) ([]service.AccountDTO, error) {
	return []service.AccountDTO{{ID: 1, Username: "user1"}}, nil
}
func (stubAccounts) SetEnabled(context.Context, uint, bool) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1}, nil
}
func (stubAccounts) UpdatePassword(context.Context, uint, string) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1}, nil
}
func (stubAccounts) Delete(context.Context, uint) error { return nil }
func (stubAccounts) Relogin(context.Context, uint) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1}, nil
}

func newEngine(token string) *gin.Engine {
	return New(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health: handler.NewHealthHandler(stubHealth{report: service.HealthReport{
			Status: service.StatusOK,
			DB:     service.ComponentStatus{Status: service.StatusOK},
			Luogu:  service.LuoguStatus{Status: service.StatusAuthenticated, Total: 1, Online: 1},
		}}),
		Problem:    handler.NewProblemHandler(stubProblem{}),
		Record:     handler.NewRecordHandler(stubRecord{}),
		Pool:       handler.NewPoolHandler(stubPool{}),
		Account:    handler.NewAccountHandler(stubAccounts{}),
		Env:        "test",
		AdminToken: token,
	})
}

func do(engine *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// withToken 是携带正确令牌的请求头
func withToken(token string) map[string]string {
	return map[string]string{middleware.HeaderAdminToken: token}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) response.Body {
	t.Helper()

	var body response.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return body
}

// 探活接口是仅有的公开入口：/healthz（就绪，会 503）与 /livez（存活，恒定 200）
func TestHealthEndpointsArePublic(t *testing.T) {
	engine := newEngine("s3cret")

	for _, path := range []string{"/healthz", "/livez"} {
		rec := do(engine, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (body=%s)", path, rec.Code, rec.Body.String())
		}
	}
}

// 除探活外，所有接口都必须带令牌：业务只读接口与管理接口一视同仁
func TestAPIRoutesRequireToken(t *testing.T) {
	engine := newEngine("s3cret")

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/pool/status"},
		{http.MethodGet, "/api/v1/problems?keyword=排序"},
		{http.MethodGet, "/api/v1/problems/P1001"},
		{http.MethodGet, "/api/v1/users/42/records"},
		{http.MethodGet, "/api/v1/users/42/records?pid=P1001&status=12&page=2"},
		{http.MethodGet, "/api/v1/admin/accounts"},
		{http.MethodGet, "/api/v1/admin/accounts/1"},
		{http.MethodPost, "/api/v1/admin/accounts/1/relogin"},
	}

	for _, tt := range tests {
		rec := do(engine, tt.method, tt.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 缺少令牌 = %d, want 401 (body=%s)", tt.method, tt.path, rec.Code, rec.Body.String())
			continue
		}
		if body := decode(t, rec); body.Code != response.CodeUnauthorized {
			t.Errorf("%s %s 业务码 = %d, want %d", tt.method, tt.path, body.Code, response.CodeUnauthorized)
		}

		rec = do(engine, tt.method, tt.path, withToken("wrong"))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 令牌错误 = %d, want 401 (body=%s)", tt.method, tt.path, rec.Code, rec.Body.String())
		}
	}
}

// 带上正确令牌后，业务与管理接口照常工作
func TestAPIRoutesWorkWithToken(t *testing.T) {
	engine := newEngine("s3cret")

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/v1/pool/status", http.StatusOK},
		{http.MethodGet, "/api/v1/problems?keyword=排序", http.StatusOK},
		{http.MethodGet, "/api/v1/problems/P1001", http.StatusOK},
		{http.MethodGet, "/api/v1/users/42/records", http.StatusOK},
		{http.MethodGet, "/api/v1/users/42/records?pid=P1001&status=12&page=2", http.StatusOK},
		{http.MethodGet, "/api/v1/admin/accounts", http.StatusOK},
	}

	for _, tt := range tests {
		rec := do(engine, tt.method, tt.path, withToken("s3cret"))
		if rec.Code != tt.want {
			t.Errorf("%s %s = %d, want %d (body=%s)", tt.method, tt.path, rec.Code, tt.want, rec.Body.String())
		}
	}
}

func TestUnknownRouteAndMethod(t *testing.T) {
	engine := newEngine("")

	rec := do(engine, http.MethodGet, "/api/v1/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if body := decode(t, rec); body.Code != response.CodeNotFound {
		t.Errorf("code = %d", body.Code)
	}

	rec = do(engine, http.MethodPost, "/healthz", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// ADMIN_TOKEN 未配置时不注册管理路由（fail closed）
func TestAdminRoutesAreNotRegisteredWithoutToken(t *testing.T) {
	engine := newEngine("")

	rec := do(engine, http.MethodGet, "/api/v1/admin/accounts", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// 服务端没配置令牌时，业务接口也一律拒绝：客户端自带什么令牌都不放行（fail closed）
func TestAPIRoutesAreClosedWithoutConfiguredToken(t *testing.T) {
	engine := newEngine("")

	paths := []string{
		"/api/v1/pool/status",
		"/api/v1/problems/P1001",
		"/api/v1/users/42/records",
	}

	for _, path := range paths {
		rec := do(engine, http.MethodGet, path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s = %d, want 401（服务端未配置令牌时也必须拒绝）", path, rec.Code)
		}

		rec = do(engine, http.MethodGet, path, withToken("anything"))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s（自带令牌）= %d, want 401", path, rec.Code)
		}
	}

	// 探活不受影响
	if rec := do(engine, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
}

func TestAdminRoutesRequireToken(t *testing.T) {
	engine := newEngine("s3cret")

	rec := do(engine, http.MethodGet, "/api/v1/admin/accounts", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("缺少令牌时 status = %d, want 401", rec.Code)
	}

	rec = do(engine, http.MethodGet, "/api/v1/admin/accounts", withToken("wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误令牌时 status = %d, want 401", rec.Code)
	}

	rec = do(engine, http.MethodGet, "/api/v1/admin/accounts", withToken("s3cret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("正确令牌时 status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Dashboard 构建产物应由 Gin 同源托管，避免管理页面与 API 之间再配置跨域。
func TestDashboardStaticHosting(t *testing.T) {
	dashboardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dashboardDir, "index.html"), []byte("<h1>Pool Dashboard</h1>"), 0o600); err != nil {
		t.Fatalf("write dashboard index: %v", err)
	}

	// 此测试传入独立临时目录，防止其他路由单测依赖本地构建产物。
	engine := New(Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:       handler.NewHealthHandler(stubHealth{report: service.HealthReport{Status: service.StatusOK}}),
		Problem:      handler.NewProblemHandler(stubProblem{}),
		Pool:         handler.NewPoolHandler(stubPool{}),
		Account:      handler.NewAccountHandler(stubAccounts{}),
		Env:          "test",
		DashboardDir: dashboardDir,
	})

	rec := do(engine, http.MethodGet, "/dashboard", nil)
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/dashboard/" {
		t.Fatalf("dashboard redirect = %d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = do(engine, http.MethodGet, "/dashboard/", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "<h1>Pool Dashboard</h1>" {
		t.Fatalf("dashboard index = %d body=%q", rec.Code, rec.Body.String())
	}
}

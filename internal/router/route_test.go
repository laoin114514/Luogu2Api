package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
func (stubAccounts) Delete(context.Context, uint) error { return nil }
func (stubAccounts) Relogin(context.Context, uint) (service.AccountDTO, error) {
	return service.AccountDTO{ID: 1}, nil
}

func newEngine(adminToken string) *gin.Engine {
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
		AdminToken: adminToken,
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

func decode(t *testing.T, rec *httptest.ResponseRecorder) response.Body {
	t.Helper()

	var body response.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestRoutesAreRegistered(t *testing.T) {
	engine := newEngine("")

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/api/v1/pool/status", http.StatusOK},
		{http.MethodGet, "/api/v1/problems/P1001", http.StatusOK},
		{http.MethodGet, "/api/v1/problems?keyword=排序", http.StatusOK},
		{http.MethodGet, "/api/v1/users/42/records", http.StatusOK},
		{http.MethodGet, "/api/v1/users/42/records?pid=P1001&status=12&page=2", http.StatusOK},
	}

	for _, tt := range tests {
		rec := do(engine, tt.method, tt.path, nil)
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

// ADMIN_TOKEN 未配置时管理路由根本不注册（fail closed）
func TestAdminRoutesAreNotRegisteredWithoutToken(t *testing.T) {
	engine := newEngine("")

	rec := do(engine, http.MethodGet, "/api/v1/admin/accounts", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAdminRoutesRequireToken(t *testing.T) {
	engine := newEngine("s3cret")

	rec := do(engine, http.MethodGet, "/api/v1/admin/accounts", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("缺少令牌时 status = %d, want 401", rec.Code)
	}

	rec = do(engine, http.MethodGet, "/api/v1/admin/accounts", map[string]string{
		middleware.HeaderAdminToken: "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误令牌时 status = %d, want 401", rec.Code)
	}

	rec = do(engine, http.MethodGet, "/api/v1/admin/accounts", map[string]string{
		middleware.HeaderAdminToken: "s3cret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("正确令牌时 status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

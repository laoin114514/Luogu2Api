package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

type stubChecker struct {
	report service.HealthReport
}

func (s stubChecker) Check(context.Context) service.HealthReport { return s.report }

func newHealthEngine(report service.HealthReport) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", NewHealthHandler(stubChecker{report: report}).Get)
	return r
}

func doGet(t *testing.T, r *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return rec
}

// /livez 是纯存活探针：不调用健康检查业务，依赖全挂也返回 200
func TestLiveAlwaysReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/livez", NewHealthHandler(stubChecker{report: service.HealthReport{
		Status: service.StatusDegraded,
		DB:     service.ComponentStatus{Status: service.StatusError, Error: "connection refused"},
	}}).Live)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var body response.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Code != response.CodeOK || body.Message != service.StatusOK {
		t.Errorf("body = %+v", body)
	}
}

func TestHealthOK(t *testing.T) {
	report := service.HealthReport{
		Status: service.StatusOK,
		DB:     service.ComponentStatus{Status: service.StatusOK},
		Luogu: service.LuoguStatus{
			Status: service.StatusAuthenticated,
			Total:  2,
			Online: 2,
		},
	}

	rec := doGet(t, newHealthEngine(report))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var body response.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Code != response.CodeOK || body.Message != service.StatusOK {
		t.Errorf("body = %+v", body)
	}
	if !strings.Contains(rec.Body.String(), `"online":2`) {
		t.Errorf("响应应包含号池在线数: %s", rec.Body.String())
	}
}

func TestHealthDegradedReturns503(t *testing.T) {
	report := service.HealthReport{
		Status: service.StatusDegraded,
		DB:     service.ComponentStatus{Status: service.StatusError, Error: "connection refused"},
		Luogu:  service.LuoguStatus{Status: service.StatusUnavailable},
	}

	rec := doGet(t, newHealthEngine(report))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("响应应包含失败原因: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), service.StatusUnavailable) {
		t.Errorf("响应应包含号池不可用状态: %s", rec.Body.String())
	}
}

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

func (s stubChecker) Check(ctx context.Context) service.HealthReport { return s.report }

func newTestEngine(report service.HealthReport) *gin.Engine {
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

func TestHealthOK(t *testing.T) {
	report := service.HealthReport{
		Status: service.StatusOK,
		DB:     service.ComponentStatus{Status: service.StatusOK},
		Luogu:  service.LuoguStatus{Status: service.StatusAuthenticated, UID: 1965145},
	}

	rec := doGet(t, newTestEngine(report))

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
	if !strings.Contains(rec.Body.String(), "1965145") {
		t.Errorf("响应应包含 UID: %s", rec.Body.String())
	}
}

func TestHealthDegradedReturns503(t *testing.T) {
	report := service.HealthReport{
		Status: service.StatusDegraded,
		DB:     service.ComponentStatus{Status: service.StatusError, Error: "connection refused"},
	}

	rec := doGet(t, newTestEngine(report))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("响应应包含失败原因: %s", rec.Body.String())
	}
}

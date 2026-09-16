package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
)

type stubPoolReporter struct {
	stats client.Stats
}

func (s stubPoolReporter) Stats() client.Stats { return s.stats }

func TestPoolStatusOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/pool/status", NewPoolHandler(stubPoolReporter{stats: client.Stats{
		Total:          3,
		Online:         2,
		ReloginPending: 1,
		LastSweepAt:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		LastSweep:      &client.SweepResult{Checked: 3, OK: 2, Pending: 1},
	}}).Status)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pool/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"total":3`, `"online":2`, `"checked":3`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("响应缺少 %s: %s", want, rec.Body.String())
		}
	}
}

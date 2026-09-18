package luogu2api

import (
	"context"
	"net/http"
	"testing"
)

// healthFixture 是 GET /healthz 的正常响应
const healthFixture = `{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "ok",
    "db": {"status": "ok"},
    "luogu": {
      "status": "authenticated",
      "total": 3,
      "online": 2,
      "reloginPending": 1,
      "reloginFailed": 0,
      "disabled": 0,
      "banned": 0,
      "lastSweepAt": "2026-03-01T12:00:00Z"
    }
  }
}`

// degradedHealthFixture 是号池没有在线账号时的响应：HTTP 503 + 业务码 0 + 完整报告
const degradedHealthFixture = `{
  "code": 0,
  "message": "degraded",
  "data": {
    "status": "degraded",
    "db": {"status": "error", "error": "dial tcp: connect: connection refused"},
    "luogu": {
      "status": "unavailable",
      "total": 3,
      "online": 0,
      "reloginPending": 2,
      "reloginFailed": 1,
      "disabled": 0,
      "banned": 0
    }
  }
}`

func TestHealthCheckOK(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r)
		if r.URL.Path != "/healthz" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeRaw(w, http.StatusOK, healthFixture)
	})

	report, err := sdk.Health.Check(context.Background())
	if err != nil {
		t.Fatalf("Health.Check: %v", err)
	}
	if report.Status != StatusOK || report.DB.Status != StatusOK {
		t.Errorf("report = %+v", report)
	}
	if report.Luogu.Status != StatusAuthenticated || report.Luogu.Online != 2 || report.Luogu.Total != 3 {
		t.Errorf("luogu = %+v", report.Luogu)
	}
	if report.Luogu.LastSweepAt != "2026-03-01T12:00:00Z" {
		t.Errorf("lastSweepAt = %q", report.Luogu.LastSweepAt)
	}
}

// 503 是就绪探针的预期结果之一，不是错误：调用方要能拿到 degraded 报告本身
func TestHealthCheckDegradedIsNotAnError(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeRaw(w, http.StatusServiceUnavailable, degradedHealthFixture)
	})

	report, err := sdk.Health.Check(context.Background())
	if err != nil {
		t.Fatalf("degraded 报告不应当成错误: %v", err)
	}
	if report.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", report.Status, StatusDegraded)
	}
	if report.Luogu.Status != StatusUnavailable || report.Luogu.Online != 0 {
		t.Errorf("luogu = %+v", report.Luogu)
	}
	if report.DB.Status != StatusError || report.DB.Error == "" {
		t.Errorf("db = %+v", report.DB)
	}
}

// 探活接口不豁免业务码：令牌不对时仍然是错误
func TestHealthCheckBusinessError(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusUnauthorized, CodeUnauthorized, "令牌无效", nil)
	})

	if _, err := sdk.Health.Check(context.Background()); !IsUnauthorized(err) {
		t.Errorf("err = %v, want 401", err)
	}
}

func TestHealthLive(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/livez" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeRaw(w, http.StatusOK, `{"code":0,"message":"ok","data":{"status":"ok"}}`)
	})

	live, err := sdk.Health.Live(context.Background())
	if err != nil {
		t.Fatalf("Health.Live: %v", err)
	}
	if live.Status != StatusOK {
		t.Errorf("status = %q", live.Status)
	}
}

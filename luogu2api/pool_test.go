package luogu2api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// poolFixture 是 GET /api/v1/pool/status 的真实响应形状
const poolFixture = `{
  "code": 0,
  "message": "ok",
  "data": {
    "total": 3,
    "online": 2,
    "reloginPending": 1,
    "reloginFailed": 0,
    "disabled": 0,
    "banned": 1,
    "lastSweepAt": "2026-03-01T12:00:00Z",
    "lastSweep": {
      "at": "2026-03-01T12:00:00Z",
      "checked": 3,
      "ok": 2,
      "relogged": 1,
      "declared": 0,
      "pending": 1,
      "disabled": 0,
      "banned": 0,
      "transient": 0,
      "busy": 0,
      "durationNs": 1500000000
    }
  }
}`

func TestPoolStatus(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/pool/status" {
			t.Errorf("请求 = %s %s", r.Method, r.URL.Path)
		}
		writeRaw(w, http.StatusOK, poolFixture)
	})

	status, err := sdk.Pool.Status(context.Background())
	if err != nil {
		t.Fatalf("Pool.Status: %v", err)
	}

	if status.Total != 3 || status.Online != 2 || status.ReloginPending != 1 || status.Banned != 1 {
		t.Errorf("号池计数 = %+v", status)
	}

	want := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if !status.LastSweepAt.Equal(want) {
		t.Errorf("lastSweepAt = %v, want %v", status.LastSweepAt, want)
	}
	if status.LastSweep == nil {
		t.Fatal("lastSweep 应当解析成非空指针")
	}
	if status.LastSweep.Checked != 3 || status.LastSweep.OK != 2 || status.LastSweep.Relogged != 1 {
		t.Errorf("lastSweep = %+v", status.LastSweep)
	}
	if status.LastSweep.Duration != 1500*time.Millisecond {
		t.Errorf("duration = %v, want 1.5s", status.LastSweep.Duration)
	}
	if !status.LastSweep.At.Equal(want) {
		t.Errorf("lastSweep.at = %v, want %v", status.LastSweep.At, want)
	}
}

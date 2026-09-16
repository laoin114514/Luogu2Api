package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/laoin114514/luogu2api/internal/client"
)

type stubPinger struct {
	err   error
	block bool
}

func (s stubPinger) Ping(ctx context.Context) error {
	if s.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.err
}

type stubSession struct {
	info client.SessionInfo
}

func (s stubSession) Session() client.SessionInfo { return s.info }

func TestCheckDBDisabled(t *testing.T) {
	svc := NewHealthService(nil, stubSession{info: client.SessionInfo{Configured: true}})

	report := svc.Check(context.Background())

	if report.Status != StatusOK {
		t.Errorf("Status = %q, want %q", report.Status, StatusOK)
	}
	if report.DB.Status != StatusDisabled {
		t.Errorf("DB.Status = %q, want %q", report.DB.Status, StatusDisabled)
	}
	if report.Luogu.Status != StatusAnonymous {
		t.Errorf("Luogu.Status = %q, want %q", report.Luogu.Status, StatusAnonymous)
	}
}

func TestCheckAllOK(t *testing.T) {
	svc := NewHealthService(
		stubPinger{},
		stubSession{info: client.SessionInfo{Configured: true, UID: 1965145, CookieFile: "cookies.json"}},
	)

	report := svc.Check(context.Background())

	if report.Status != StatusOK || report.DB.Status != StatusOK {
		t.Errorf("report = %+v", report)
	}
	if report.Luogu.Status != StatusAuthenticated || report.Luogu.UID != 1965145 {
		t.Errorf("Luogu = %+v", report.Luogu)
	}
	if report.Luogu.CookieFile != "cookies.json" {
		t.Errorf("CookieFile = %q", report.Luogu.CookieFile)
	}
}

func TestCheckDBErrorIsDegraded(t *testing.T) {
	svc := NewHealthService(stubPinger{err: errors.New("connection refused")}, nil)

	report := svc.Check(context.Background())

	if report.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", report.Status, StatusDegraded)
	}
	if report.DB.Status != StatusError {
		t.Errorf("DB.Status = %q, want %q", report.DB.Status, StatusError)
	}
	if report.DB.Error == "" {
		t.Error("DB.Error 应带上失败原因")
	}
	if report.Luogu.Status != StatusDisabled {
		t.Errorf("Luogu.Status = %q, want %q", report.Luogu.Status, StatusDisabled)
	}
}

func TestCheckLuoguUnavailable(t *testing.T) {
	svc := NewHealthService(nil, stubSession{info: client.SessionInfo{Configured: false}})

	if got := svc.Check(context.Background()).Luogu.Status; got != StatusUnavailable {
		t.Errorf("Luogu.Status = %q, want %q", got, StatusUnavailable)
	}
}

// 数据库卡住时，健康检查必须在 pingTimeout 内返回，而不是一直等
func TestCheckPingTimeout(t *testing.T) {
	svc := NewHealthService(stubPinger{block: true}, nil)
	svc.pingTimeout = 20 * time.Millisecond

	start := time.Now()
	report := svc.Check(context.Background())
	elapsed := time.Since(start)

	if report.DB.Status != StatusError {
		t.Errorf("DB.Status = %q, want %q", report.DB.Status, StatusError)
	}
	if elapsed > time.Second {
		t.Errorf("耗时 %v，应受 pingTimeout 限制", elapsed)
	}
}

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

type stubPool struct {
	stats client.Stats
}

func (s stubPool) Stats() client.Stats { return s.stats }

func okPool(online int) stubPool {
	return stubPool{stats: client.Stats{Total: online, Online: online}}
}

func TestCheckAllOK(t *testing.T) {
	svc := NewHealthService(stubPinger{}, stubPool{stats: client.Stats{
		Total:          4,
		Online:         2,
		ReloginPending: 1,
		Banned:         1,
		LastSweepAt:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}})

	report := svc.Check(context.Background())

	if report.Status != StatusOK || report.DB.Status != StatusOK {
		t.Errorf("report = %+v", report)
	}
	if report.Luogu.Status != StatusAuthenticated {
		t.Errorf("Luogu.Status = %q", report.Luogu.Status)
	}
	if report.Luogu.Total != 4 || report.Luogu.Online != 2 || report.Luogu.ReloginPending != 1 {
		t.Errorf("Luogu = %+v", report.Luogu)
	}
	if report.Luogu.Banned != 1 {
		t.Errorf("Luogu.Banned = %d, want 1", report.Luogu.Banned)
	}
	if report.Luogu.LastSweepAt != "2026-03-01T12:00:00Z" {
		t.Errorf("LastSweepAt = %q", report.Luogu.LastSweepAt)
	}
}

func TestCheckDBDisabled(t *testing.T) {
	svc := NewHealthService(nil, okPool(1))

	report := svc.Check(context.Background())

	if report.Status != StatusOK {
		t.Errorf("Status = %q, want %q", report.Status, StatusOK)
	}
	if report.DB.Status != StatusDisabled {
		t.Errorf("DB.Status = %q, want %q", report.DB.Status, StatusDisabled)
	}
}

func TestCheckDBErrorIsDegraded(t *testing.T) {
	svc := NewHealthService(stubPinger{err: errors.New("connection refused")}, okPool(1))

	report := svc.Check(context.Background())

	if report.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", report.Status, StatusDegraded)
	}
	if report.DB.Status != StatusError || report.DB.Error == "" {
		t.Errorf("DB = %+v", report.DB)
	}
}

// 号池一个在线账号都没有时，业务接口必然失败，健康检查必须报 degraded（503 摘实例）
func TestCheckPoolWithoutOnlineAccountsIsDegraded(t *testing.T) {
	tests := []struct {
		name  string
		stats client.Stats
	}{
		{"空池", client.Stats{}},
		{"全部待重登", client.Stats{Total: 3, ReloginPending: 3}},
		{"全部重登失败", client.Stats{Total: 2, ReloginFailed: 2}},
		{"只剩停用账号", client.Stats{Total: 1, Disabled: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewHealthService(stubPinger{}, stubPool{stats: tt.stats})

			report := svc.Check(context.Background())

			if report.Status != StatusDegraded {
				t.Errorf("Status = %q, want %q", report.Status, StatusDegraded)
			}
			if report.Luogu.Status != StatusUnavailable {
				t.Errorf("Luogu.Status = %q, want %q", report.Luogu.Status, StatusUnavailable)
			}
		})
	}
}

func TestCheckPoolDisabled(t *testing.T) {
	svc := NewHealthService(stubPinger{}, nil)

	report := svc.Check(context.Background())

	if report.Luogu.Status != StatusDisabled {
		t.Errorf("Luogu.Status = %q, want %q", report.Luogu.Status, StatusDisabled)
	}
	if report.Status != StatusOK {
		t.Errorf("Status = %q", report.Status)
	}
}

func TestCheckNoSweepYetOmitsTimestamp(t *testing.T) {
	svc := NewHealthService(stubPinger{}, okPool(1))

	if got := svc.Check(context.Background()).Luogu.LastSweepAt; got != "" {
		t.Errorf("LastSweepAt = %q, want empty", got)
	}
}

// 数据库卡住时，健康检查必须在 pingTimeout 内返回，而不是一直等
func TestCheckPingTimeout(t *testing.T) {
	svc := NewHealthService(stubPinger{block: true}, okPool(1))
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

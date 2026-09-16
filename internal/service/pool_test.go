package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laoin114514/luogu2api/internal/client"
)

type stubSweeper struct {
	calls   atomic.Int32
	release chan struct{}
	result  client.SweepResult
	err     error
}

func (s *stubSweeper) Sweep(context.Context) (client.SweepResult, error) {
	s.calls.Add(1)
	if s.release != nil {
		<-s.release
	}
	return s.result, s.err
}

func newTestPoolService(sweeper PoolSweeper, interval time.Duration) *PoolService {
	return NewPoolService(sweeper, interval, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSweepOnceRunsAndReports(t *testing.T) {
	sweeper := &stubSweeper{result: client.SweepResult{Checked: 2, OK: 1, Relogged: 1}}
	svc := newTestPoolService(sweeper, time.Minute)

	res, ran, err := svc.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if !ran {
		t.Error("ran 应为 true")
	}
	if res.Checked != 2 || res.OK != 1 || res.Relogged != 1 {
		t.Errorf("result = %+v", res)
	}
}

// 上一轮还没结束时，下一个 tick 必须被跳过，避免扫描叠加
func TestSweepOnceSkipsWhenPreviousStillRunning(t *testing.T) {
	release := make(chan struct{})
	sweeper := &stubSweeper{release: release}
	svc := newTestPoolService(sweeper, time.Minute)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ran, _ := svc.SweepOnce(context.Background()); !ran {
			t.Error("第一轮应当执行")
		}
	}()

	waitUntil(t, "第一轮进入扫描", func() bool { return sweeper.calls.Load() == 1 })

	_, ran, err := svc.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if ran {
		t.Error("上一轮未结束时 ran 应为 false")
	}
	if got := sweeper.calls.Load(); got != 1 {
		t.Errorf("扫描次数 = %d, want 1", got)
	}

	close(release)
	<-done
}

func TestSweepOncePropagatesError(t *testing.T) {
	boom := errors.New("db down")
	svc := newTestPoolService(&stubSweeper{err: boom}, time.Minute)

	if _, _, err := svc.SweepOnce(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestRunSweeperStopsOnContextCancel(t *testing.T) {
	sweeper := &stubSweeper{}
	svc := newTestPoolService(sweeper, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunSweeper(ctx)
	}()

	// 启动时应立刻先扫一轮
	waitUntil(t, "启动后立即扫描", func() bool { return sweeper.calls.Load() >= 1 })

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSweeper 未随 ctx 取消而退出")
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

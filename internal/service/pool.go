package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/laoin114514/luogu2api/internal/client"
)

// PoolSweeper 号池扫描能力（由 client.Pool 实现）
type PoolSweeper interface {
	Sweep(ctx context.Context) (client.SweepResult, error)
}

// PoolService 号池扫描器：只负责"什么时候扫"（周期 + 重入保护 + 日志），
// "扫的时候怎么判断账号状态"由 client.Pool 负责——错误分类必须留在唯一
// 接触 SDK 的包里。
type PoolService struct {
	pool     PoolSweeper
	logger   *slog.Logger
	interval time.Duration
	running  atomic.Bool
}

// NewPoolService 创建扫描器
func NewPoolService(pool PoolSweeper, interval time.Duration, logger *slog.Logger) *PoolService {
	return &PoolService{pool: pool, logger: logger, interval: interval}
}

// SweepOnce 执行一轮扫描。
//
// 返回值 ran=false 表示上一轮还没结束，本次 tick 被跳过（避免扫描叠加、
// 也避免同一个账号被两个扫描任务同时验证）。
func (s *PoolService) SweepOnce(ctx context.Context) (client.SweepResult, bool, error) {
	if !s.running.CompareAndSwap(false, true) {
		return client.SweepResult{}, false, nil
	}
	defer s.running.Store(false)

	res, err := s.pool.Sweep(ctx)
	if err != nil {
		return res, true, err
	}

	s.logger.Info("号池扫描完成",
		"checked", res.Checked,
		"ok", res.OK,
		"relogged", res.Relogged,
		"offline", res.Declared,
		"pending", res.Pending,
		"disabled", res.Disabled,
		"transient", res.Transient,
		"busy", res.Busy,
		"cost_ms", res.Duration.Milliseconds(),
	)
	return res, true, nil
}

// RunSweeper 周期执行扫描直到 ctx 取消（阻塞调用，请放到 goroutine 里）。
//
// 启动时先立刻跑一轮：进程上次被杀时留下的失效 cookie 应当尽快恢复，
// 而不是等一个完整周期。
func (s *PoolService) RunSweeper(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("号池扫描器已启动", "interval", s.interval.String())
	s.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("号池扫描器已停止")
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *PoolService) sweep(ctx context.Context) {
	if _, ran, err := s.SweepOnce(ctx); err != nil {
		s.logger.Error("号池扫描失败", "err", err)
	} else if !ran {
		s.logger.Warn("上一轮号池扫描尚未结束，跳过本次")
	}
}

// Package service 是业务逻辑层：编排 repository 与 client，
// 不接触 gin、GORM 等具体实现（只依赖自己定义的窄接口）。
package service

import (
	"context"
	"time"

	"github.com/laoin114514/luogu2api/internal/client"
)

// 状态取值
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"

	StatusDisabled      = "disabled"
	StatusError         = "error"
	StatusAuthenticated = "authenticated"
	StatusUnavailable   = "unavailable"
)

// DBPinger 数据访问层的连通性检查能力
type DBPinger interface {
	Ping(ctx context.Context) error
}

// PoolStats 号池状态能力（由 client.Pool 实现）
type PoolStats interface {
	Stats() client.Stats
}

// ComponentStatus 单个依赖的状态
type ComponentStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// LuoguStatus 号池状态。
//
// 只读内存快照，不发任何网络请求：健康检查会被频繁调用，
// 不能因为探活把洛谷打爆，也不能因为探活本身把账号验证一遍。
type LuoguStatus struct {
	Status         string `json:"status"`
	Total          int    `json:"total"`
	Online         int    `json:"online"`
	ReloginPending int    `json:"reloginPending"`
	ReloginFailed  int    `json:"reloginFailed"`
	Disabled       int    `json:"disabled"`
	LastSweepAt    string `json:"lastSweepAt,omitempty"`
}

// HealthReport 健康检查结果
type HealthReport struct {
	Status string          `json:"status"`
	DB     ComponentStatus `json:"db"`
	Luogu  LuoguStatus     `json:"luogu"`
}

// HealthService 健康检查业务
type HealthService struct {
	db          DBPinger // nil 表示未启用数据库（防御性分支，正常启动恒非 nil）
	pool        PoolStats
	pingTimeout time.Duration
}

// NewHealthService 创建健康检查服务
func NewHealthService(db DBPinger, pool PoolStats) *HealthService {
	return &HealthService{db: db, pool: pool, pingTimeout: 2 * time.Second}
}

// Check 汇总各依赖状态。
//
// 数据库不可用或号池没有在线账号时整体为 degraded（HTTP 层返回 503）：
// 没有可用 cookie 时业务接口必然失败，探活就该把实例摘掉。
func (s *HealthService) Check(ctx context.Context) HealthReport {
	report := HealthReport{
		Status: StatusOK,
		DB:     s.checkDB(ctx),
		Luogu:  s.checkPool(),
	}
	if report.DB.Status == StatusError || report.Luogu.Status == StatusUnavailable {
		report.Status = StatusDegraded
	}
	return report
}

func (s *HealthService) checkDB(ctx context.Context) ComponentStatus {
	if s.db == nil {
		return ComponentStatus{Status: StatusDisabled}
	}
	ctx, cancel := context.WithTimeout(ctx, s.pingTimeout)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		return ComponentStatus{Status: StatusError, Error: err.Error()}
	}
	return ComponentStatus{Status: StatusOK}
}

func (s *HealthService) checkPool() LuoguStatus {
	if s.pool == nil {
		return LuoguStatus{Status: StatusDisabled}
	}

	stats := s.pool.Stats()
	out := LuoguStatus{
		Total:          stats.Total,
		Online:         stats.Online,
		ReloginPending: stats.ReloginPending,
		ReloginFailed:  stats.ReloginFailed,
		Disabled:       stats.Disabled,
	}
	if !stats.LastSweepAt.IsZero() {
		out.LastSweepAt = stats.LastSweepAt.UTC().Format(time.RFC3339)
	}

	// 一个在线账号都没有 = 业务接口必然 503，健康检查必须如实反映
	if stats.Online == 0 {
		out.Status = StatusUnavailable
		return out
	}
	out.Status = StatusAuthenticated
	return out
}

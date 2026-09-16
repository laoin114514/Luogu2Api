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
	StatusAnonymous     = "anonymous"
	StatusAuthenticated = "authenticated"
	StatusUnavailable   = "unavailable"
)

// DBPinger 数据访问层的连通性检查能力
type DBPinger interface {
	Ping(ctx context.Context) error
}

// LuoguSession 洛谷会话的本地状态
type LuoguSession interface {
	Session() client.SessionInfo
}

// ComponentStatus 单个依赖的状态
type ComponentStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// LuoguStatus 洛谷会话状态
type LuoguStatus struct {
	Status     string `json:"status"`
	UID        int    `json:"uid,omitempty"`
	CookieFile string `json:"cookieFile,omitempty"`
}

// HealthReport 健康检查结果
type HealthReport struct {
	Status string          `json:"status"`
	DB     ComponentStatus `json:"db"`
	Luogu  LuoguStatus     `json:"luogu"`
}

// HealthService 健康检查业务
type HealthService struct {
	db          DBPinger // nil 表示未启用数据库
	luogu       LuoguSession
	pingTimeout time.Duration
}

// NewHealthService 创建健康检查服务
func NewHealthService(db DBPinger, luogu LuoguSession) *HealthService {
	return &HealthService{db: db, luogu: luogu, pingTimeout: 2 * time.Second}
}

// Check 汇总各依赖状态。
//
// 数据库不可用时整体为 degraded（HTTP 层会返回 503）；
// 未启用数据库（本地开发）视为正常。
func (s *HealthService) Check(ctx context.Context) HealthReport {
	report := HealthReport{
		Status: StatusOK,
		DB:     s.checkDB(ctx),
		Luogu:  s.checkLuogu(),
	}
	if report.DB.Status == StatusError {
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

// checkLuogu 只读取本地会话状态，不做网络请求（健康检查会被频繁调用）
func (s *HealthService) checkLuogu() LuoguStatus {
	if s.luogu == nil {
		return LuoguStatus{Status: StatusDisabled}
	}
	info := s.luogu.Session()
	switch {
	case !info.Configured:
		return LuoguStatus{Status: StatusUnavailable}
	case info.UID > 0:
		return LuoguStatus{Status: StatusAuthenticated, UID: info.UID, CookieFile: info.CookieFile}
	default:
		return LuoguStatus{Status: StatusAnonymous, CookieFile: info.CookieFile}
	}
}

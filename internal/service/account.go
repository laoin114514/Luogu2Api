package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/laoin114514/luogu2api/internal/model"
)

// AccountStore 账号管理所需的数据访问能力
type AccountStore interface {
	// Create 新建账号；若同名账号此前被软删除则复活它，revived=true
	Create(ctx context.Context, acc *model.Account) (revived bool, err error)
	GetByID(ctx context.Context, id uint) (*model.Account, error)
	ListAll(ctx context.Context) ([]*model.Account, error)
	SetEnabled(ctx context.Context, id uint, enabled bool) error
	UpdatePoolState(ctx context.Context, id uint, state model.PoolState) error
	SoftDelete(ctx context.Context, id uint) error
}

// AccountPool 账号在号池中的生命周期操作
type AccountPool interface {
	Load(ctx context.Context, id uint) error
	Remove(id uint)
	ForceRelogin(ctx context.Context, id uint) error
}

// AccountDTO 对外的账号视图。
//
// 刻意不是 model.Account：密码与 cookie 等同于登录态，
// 任何 HTTP 响应里都不能出现（包括管理接口）。
type AccountDTO struct {
	ID             uint       `json:"id"`
	Username       string     `json:"username"`
	LuoguUID       int64      `json:"luoguUid"`
	Nickname       string     `json:"nickname"`
	Online         bool       `json:"online"`
	Status         string     `json:"status"`
	Enabled        bool       `json:"enabled"`
	Weight         int32      `json:"weight"`
	FailureCount   int32      `json:"failureCount"`
	LastError      string     `json:"lastError,omitempty"`
	NextVerifyAt   *time.Time `json:"nextVerifyAt,omitempty"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`

	// 代码公开计划：账号在洛谷是否已加入。每次登录/验证后都会只读同步一次远端
	// 偏好设置，因此开关关闭时它反映账号的真实状态；开启后未加入的账号会被补做加入。
	// 恒为 false 说明远端确实未加入（读取失败只记日志，账号本身照常可用）。
	OpenSourceJoined   bool       `json:"openSourceJoined"`
	OpenSourceJoinedAt *time.Time `json:"openSourceJoinedAt,omitempty"`

	// 洛谷平台用户字段
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	Slogan     string `json:"slogan"`
	Badge      string `json:"badge"`
	Color      string `json:"color"`
	IsAdmin    bool   `json:"isAdmin"`
	IsBanned   bool   `json:"isBanned"`
	CCFLevel   int32  `json:"ccfLevel"`
	XCPCLevel  int32  `json:"xcpcLevel"`
	Background string `json:"background"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NewAccountDTO 把实体转换成对外视图（不含凭据）
func NewAccountDTO(acc *model.Account) AccountDTO {
	if acc == nil {
		return AccountDTO{}
	}
	return AccountDTO{
		ID:                 acc.ID,
		Username:           acc.Username,
		LuoguUID:           acc.UIDValue(),
		Nickname:           acc.Nickname,
		Online:             acc.Online,
		Status:             acc.Status,
		Enabled:            acc.Enabled,
		Weight:             acc.Weight,
		FailureCount:       acc.FailureCount,
		LastError:          acc.LastError,
		NextVerifyAt:       acc.NextVerifyAt,
		LastLoginAt:        acc.LastLoginAt,
		LastVerifiedAt:     acc.LastVerifiedAt,
		OpenSourceJoined:   acc.OpenSourceJoined,
		OpenSourceJoinedAt: acc.OpenSourceJoinedAt,
		Name:               acc.Name,
		Avatar:             acc.Avatar,
		Slogan:             acc.Slogan,
		Badge:              acc.Badge,
		Color:              acc.Color,
		IsAdmin:            acc.IsAdmin,
		IsBanned:           acc.IsBanned,
		CCFLevel:           acc.CCFLevel,
		XCPCLevel:          acc.XCPCLevel,
		Background:         acc.Background,
		CreatedAt:          acc.CreatedAt,
		UpdatedAt:          acc.UpdatedAt,
	}
}

// AccountService 账号（号池）管理业务
type AccountService struct {
	store  AccountStore
	pool   AccountPool
	logger *slog.Logger
}

// NewAccountService 创建账号管理服务
func NewAccountService(store AccountStore, pool AccountPool, logger *slog.Logger) *AccountService {
	return &AccountService{store: store, pool: pool, logger: logger}
}

// Create 新增账号：入库 → 入池 → 立即尝试首次登录。
//
// 首次登录失败不让接口失败（OCR 抖动/网络抖动都可能发生）：账号会留在
// new/relogin_pending 状态，由扫描器继续重试，调用方从返回的 status 就能看到。
func (s *AccountService) Create(ctx context.Context, username, password, nickname string) (AccountDTO, error) {
	username = strings.TrimSpace(username)
	nickname = strings.TrimSpace(nickname)

	if username == "" {
		return AccountDTO{}, fmt.Errorf("%w: username 不能为空", ErrInvalidParam)
	}
	if password == "" {
		return AccountDTO{}, fmt.Errorf("%w: password 不能为空", ErrInvalidParam)
	}
	if len(username) > 64 || len(nickname) > 64 {
		return AccountDTO{}, fmt.Errorf("%w: username/nickname 不能超过 64 字节", ErrInvalidParam)
	}

	acc := &model.Account{
		Username: username,
		Password: password,
		Nickname: nickname,
		Enabled:  true,
		Weight:   1,
		Status:   model.AccountStatusNew,
	}
	revived, err := s.store.Create(ctx, acc)
	if err != nil {
		return AccountDTO{}, err
	}
	if revived {
		s.logger.Info("同名账号此前已被删除，已复活并重置为待登录",
			"account_id", acc.ID, "username", username)
	}

	if err := s.pool.Load(ctx, acc.ID); err != nil {
		s.logger.Warn("账号已入库但未能进入号池", "account_id", acc.ID, "err", err)
	} else if err := s.pool.ForceRelogin(ctx, acc.ID); err != nil {
		// 分清"稍后会自动重试"与"需要人工处理"：banned/disabled 不会被扫描器重试，
		// 日志说错了会让人一直等一个永远不会到来的自动恢复。
		s.logAuthFailure(ctx, acc.ID, username, err)
	}

	return s.Get(ctx, acc.ID)
}

// logAuthFailure 首次登录失败时按落库状态给出准确的提示
func (s *AccountService) logAuthFailure(ctx context.Context, id uint, username string, cause error) {
	current, err := s.store.GetByID(ctx, id)
	if err != nil {
		s.logger.Warn("账号首次登录未成功，将由扫描器重试",
			"account_id", id, "username", username, "err", cause)
		return
	}

	switch current.Status {
	case model.AccountStatusBanned:
		s.logger.Error("账号已被洛谷封禁/限制，未进入号池（解封后用 PATCH enabled=true 重新启用）",
			"account_id", id, "username", username, "err", cause)
	case model.AccountStatusDisabled:
		s.logger.Error("账号凭据不可用，未进入号池（需人工处理：密码错误/账号锁定/二次验证）",
			"account_id", id, "username", username, "err", cause)
	default:
		s.logger.Warn("账号首次登录未成功，将由扫描器重试",
			"account_id", id, "username", username, "status", current.Status, "err", cause)
	}
}

// Get 查询单个账号
func (s *AccountService) Get(ctx context.Context, id uint) (AccountDTO, error) {
	acc, err := s.store.GetByID(ctx, id)
	if err != nil {
		return AccountDTO{}, err
	}
	return NewAccountDTO(acc), nil
}

// List 列出全部账号（含离线与停用，供运维排查）
func (s *AccountService) List(ctx context.Context) ([]AccountDTO, error) {
	accounts, err := s.store.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]AccountDTO, 0, len(accounts))
	for _, acc := range accounts {
		out = append(out, NewAccountDTO(acc))
	}
	return out, nil
}

// SetEnabled 启停账号。
//
// 停用会立刻把账号移出号池（不再被选中）。
// 启用则重新加载；若账号此前是 disabled（密码错/锁定等，扫描器不会自动重试），
// 同时把状态复位成 new 并清空退避，让扫描器重新验证/重登一次——
// 否则运维修好密码后账号也永远回不了池子。
func (s *AccountService) SetEnabled(ctx context.Context, id uint, enabled bool) (AccountDTO, error) {
	acc, err := s.store.GetByID(ctx, id)
	if err != nil {
		return AccountDTO{}, err
	}
	if err := s.store.SetEnabled(ctx, id, enabled); err != nil {
		return AccountDTO{}, err
	}

	if !enabled {
		s.pool.Remove(id)
		return s.Get(ctx, id)
	}

	// disabled（密码错/锁定）与 banned（洛谷封禁）都不会被扫描器自动重试，
	// 手工重新启用时必须复位状态，否则运维修好问题后账号也永远回不了池子。
	if acc.Status == model.AccountStatusDisabled || acc.Status == model.AccountStatusBanned {
		if err := s.store.UpdatePoolState(ctx, id, model.PoolState{
			Online:       false,
			Status:       model.AccountStatusNew,
			FailureCount: 0,
			LastError:    "",
			NextVerifyAt: nil, // 立即到期，交给扫描器
		}); err != nil {
			return AccountDTO{}, err
		}
		s.logger.Info("账号已重新启用，将重新尝试验证/登录",
			"account_id", id, "username", acc.Username, "prev_status", acc.Status)
	}

	if err := s.pool.Load(ctx, id); err != nil {
		s.logger.Warn("启用账号后未能载入号池", "account_id", id, "err", err)
	}

	return s.Get(ctx, id)
}

// Delete 软删除账号并移出号池
func (s *AccountService) Delete(ctx context.Context, id uint) error {
	if err := s.store.SoftDelete(ctx, id); err != nil {
		return err
	}
	s.pool.Remove(id)
	return nil
}

// Relogin 强制立即重登（运维手动触发）
func (s *AccountService) Relogin(ctx context.Context, id uint) (AccountDTO, error) {
	// 重登失败也把最新状态返回给调用方，便于看到失败原因
	if err := s.pool.ForceRelogin(ctx, id); err != nil {
		s.logger.Warn("手动重登未成功", "account_id", id, "err", err)
	}
	return s.Get(ctx, id)
}

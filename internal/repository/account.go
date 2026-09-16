package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/secret"
)

// AccountRepository 账号（号池）仓储。
//
// 本仓储是唯一接触 accounts 表的地方，也是唯一进行凭据加解密的地方：
// 写入时把明文 Password/Cookie 封成密文列，读出时还原到明文字段。
//
// 所有更新都走 Updates(map) 只碰需要的列——扫描器与请求路径会并发操作同一
// 账号，整行 Save 会丢更新（后写覆盖先写），因此这里不提供 Save 语义的方法。
type AccountRepository struct {
	db     *gorm.DB
	cipher *secret.Cipher
}

// NewAccountRepository 创建账号仓储
func NewAccountRepository(db *gorm.DB, cipher *secret.Cipher) *AccountRepository {
	return &AccountRepository{db: db, cipher: cipher}
}

// Create 插入账号（密码/可选 cookie，写入前加密）
func (r *AccountRepository) Create(ctx context.Context, acc *model.Account) error {
	if acc == nil {
		return errors.New("repository: acc 不能为 nil")
	}

	password, err := r.cipher.Seal(acc.Password)
	if err != nil {
		return fmt.Errorf("加密密码失败: %w", err)
	}
	cookie, err := r.cipher.Seal(acc.Cookie)
	if err != nil {
		return fmt.Errorf("加密 cookie 失败: %w", err)
	}

	row := *acc
	row.PasswordSecret = password
	row.CookieSecret = cookie
	if row.Status == "" {
		row.Status = model.AccountStatusNew
	}

	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("%w: %s", model.ErrAccountExists, acc.Username)
		}
		return fmt.Errorf("创建账号失败: %w", err)
	}

	// 回填自增主键与时间戳，供调用方继续使用同一个对象
	acc.ID = row.ID
	acc.Status = row.Status
	acc.CreatedAt = row.CreatedAt
	acc.UpdatedAt = row.UpdatedAt
	return nil
}

// GetByID 按主键读取（含解密后的凭据）
func (r *AccountRepository) GetByID(ctx context.Context, id uint) (*model.Account, error) {
	var row model.Account
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: id=%d", model.ErrAccountNotFound, id)
		}
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}
	if err := r.decrypt(&row); err != nil {
		return nil, err
	}
	return &row, nil
}

// GetByUsername 按登录名读取（含解密后的凭据）
func (r *AccountRepository) GetByUsername(ctx context.Context, username string) (*model.Account, error) {
	var row model.Account
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: username=%s", model.ErrAccountNotFound, username)
		}
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}
	if err := r.decrypt(&row); err != nil {
		return nil, err
	}
	return &row, nil
}

// ListAll 列出全部账号（管理接口用，按 id 升序）
func (r *AccountRepository) ListAll(ctx context.Context) ([]*model.Account, error) {
	var rows []*model.Account
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询账号列表失败: %w", err)
	}
	for _, row := range rows {
		if err := r.decrypt(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// ListActive 列出需要进入号池的账号（启用且非 disabled）。
//
// 启动预热使用：即使 cookie 为空/无效也会返回，由号池标记为待重登。
func (r *AccountRepository) ListActive(ctx context.Context) ([]*model.Account, error) {
	var rows []*model.Account
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND status <> ?", true, model.AccountStatusDisabled).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("查询启用账号失败: %w", err)
	}
	for _, row := range rows {
		if err := r.decrypt(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// ListDueForVerify 捞出到期待验证/重登的账号。
//
// 到期判定：next_verify_at 为空（从未验证）或已过期；disabled 只能人工恢复，
// 因此不在扫描范围内。relogin_failed 的 next_verify_at 由退避算出，
// 天然形成"慢速重试"而不是每 5 分钟热循环。
func (r *AccountRepository) ListDueForVerify(ctx context.Context, now time.Time, limit int) ([]*model.Account, error) {
	if limit <= 0 {
		limit = 1
	}

	var rows []*model.Account
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Where("status IN ?", model.AccountSweepableStatuses).
		Where("next_verify_at IS NULL OR next_verify_at <= ?", now).
		Order("next_verify_at IS NULL DESC, next_verify_at ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("查询到期账号失败: %w", err)
	}
	for _, row := range rows {
		if err := r.decrypt(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// SaveSession 登录/验证成功后一次性落库：新 cookie + 平台档案 + 状态复位。
func (r *AccountRepository) SaveSession(
	ctx context.Context,
	id uint,
	cookie string,
	uid int,
	profile model.LuoguProfile,
	now time.Time,
	nextVerifyAt time.Time,
) error {
	if cookie == "" {
		return errors.New("repository: cookie 不能为空")
	}

	encrypted, err := r.cipher.Seal(cookie)
	if err != nil {
		return fmt.Errorf("加密 cookie 失败: %w", err)
	}

	updates := map[string]any{
		"cookie_enc":        encrypted,
		"online":            true,
		"status":            model.AccountStatusActive,
		"failure_count":     0,
		"last_error":        "",
		"last_login_at":     now,
		"last_verified_at":  now,
		"cookie_updated_at": now,
		"next_verify_at":    nextVerifyAt,
		"profile_json":      profile.RawJSON,
		"name":              profile.Name,
		"avatar":            profile.Avatar,
		"slogan":            profile.Slogan,
		"badge":             profile.Badge,
		"color":             profile.Color,
		"is_admin":          profile.IsAdmin,
		"is_banned":         profile.IsBanned,
		"ccf_level":         int32(profile.CCFLevel),
		"xcpc_level":        int32(profile.XCPCLevel),
		"background":        profile.Background,
	}
	if uid > 0 {
		updates["luogu_uid"] = int64(uid)
	}

	return r.update(ctx, id, updates)
}

// MarkVerified 验证通过但不需要换 cookie：复位可用性并推迟下次验证。
//
// 只碰状态与时间列，不碰 cookie_enc，因此与并发的重登写入不会互相覆盖。
func (r *AccountRepository) MarkVerified(ctx context.Context, id uint, now, nextVerifyAt time.Time) error {
	return r.update(ctx, id, map[string]any{
		"online":           true,
		"status":           model.AccountStatusActive,
		"failure_count":    0,
		"last_error":       "",
		"last_verified_at": now,
		"next_verify_at":   nextVerifyAt,
	})
}

// UpdatePoolState 更新号池运行状态（不动 cookie 与档案）
func (r *AccountRepository) UpdatePoolState(ctx context.Context, id uint, state model.PoolState) error {
	return r.update(ctx, id, map[string]any{
		"online":         state.Online,
		"status":         state.Status,
		"failure_count":  state.FailureCount,
		"last_error":     state.LastError,
		"next_verify_at": state.NextVerifyAt,
	})
}

// MarkTransientFailure 记录环境类失败（网络/OCR/洛谷抖动）。
//
// 刻意不改 online / status / failure_count：这类失败与 cookie 是否有效无关，
// 改动它们会让一次网络抖动把整个号池标记为离线。
func (r *AccountRepository) MarkTransientFailure(ctx context.Context, id uint, errMsg string, nextVerifyAt time.Time) error {
	return r.update(ctx, id, map[string]any{
		"last_error":     errMsg,
		"next_verify_at": nextVerifyAt,
	})
}

// SetEnabled 人工启停账号
func (r *AccountRepository) SetEnabled(ctx context.Context, id uint, enabled bool) error {
	return r.update(ctx, id, map[string]any{"enabled": enabled})
}

// SoftDelete 软删除账号
func (r *AccountRepository) SoftDelete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.Account{}, id)
	if res.Error != nil {
		return fmt.Errorf("删除账号失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", model.ErrAccountNotFound, id)
	}
	return nil
}

// update 统一的列级更新入口：所有写路径都必须经过它
func (r *AccountRepository) update(ctx context.Context, id uint, updates map[string]any) error {
	res := r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新账号失败 (id=%d): %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", model.ErrAccountNotFound, id)
	}
	return nil
}

func (r *AccountRepository) decrypt(row *model.Account) error {
	if row == nil {
		return errors.New("repository: row 不能为 nil")
	}

	password, err := r.cipher.Open(row.PasswordSecret)
	if err != nil {
		return fmt.Errorf("解密密码失败 (account_id=%d): %w", row.ID, err)
	}
	cookie, err := r.cipher.Open(row.CookieSecret)
	if err != nil {
		return fmt.Errorf("解密 cookie 失败 (account_id=%d): %w", row.ID, err)
	}

	row.Password = password
	row.Cookie = cookie
	return nil
}

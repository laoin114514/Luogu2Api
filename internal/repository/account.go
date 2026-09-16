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

// Create 插入账号（密码/可选 cookie，写入前加密）。
//
// 若同名账号此前被"软删除"，则原地复活它而不是报冲突：重置凭据与运行状态、
// 清空 cookie/UID/平台档案，让它像新账号一样重新登录（主键沿用旧行）。
// 返回值 revived 表示走的是复活分支，调用方据此打出更准确的日志。
func (r *AccountRepository) Create(ctx context.Context, acc *model.Account) (revived bool, err error) {
	if acc == nil {
		return false, errors.New("repository: acc 不能为 nil")
	}

	password, err := r.cipher.Seal(acc.Password)
	if err != nil {
		return false, fmt.Errorf("加密密码失败: %w", err)
	}
	cookie, err := r.cipher.Seal(acc.Cookie)
	if err != nil {
		return false, fmt.Errorf("加密 cookie 失败: %w", err)
	}

	row := *acc
	row.PasswordSecret = password
	row.CookieSecret = cookie
	if row.Status == "" {
		row.Status = model.AccountStatusNew
	}

	// 先看有没有"同名但已软删除"的行：有就复活它。
	// 刻意放在 INSERT 之前——否则每次复活都要先撞一次唯一键冲突，
	// 数据库日志里会留下一条看着像故障的 "duplicated key not allowed"。
	revived, err = r.reviveSoftDeleted(ctx, acc, password)
	if err != nil {
		return false, err
	}
	if revived {
		return true, nil
	}

	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return false, fmt.Errorf("创建账号失败: %w", err)
		}
		// 走到这里说明同名账号还在用（或 uid 撞了），是真冲突；
		// 并发下"别人刚复活/刚插入"也会落到这里，属于预期结果。
		return false, fmt.Errorf("%w: %s", model.ErrAccountExists, acc.Username)
	}

	// 回填自增主键与时间戳，供调用方继续使用同一个对象
	acc.ID = row.ID
	acc.Status = row.Status
	acc.CreatedAt = row.CreatedAt
	acc.UpdatedAt = row.UpdatedAt
	// 让调用方手上的对象与落库结果一致（明文仍只在 acc.Password / acc.Cookie）
	acc.PasswordSecret = password
	acc.CookieSecret = cookie
	return false, nil
}

// reviveSoftDeleted 若存在"同名且已软删除"的行，则原地复活它并返回 true。
//
// 整体重置成"刚导入"的状态：新密码、cookie/UID/平台档案清空、状态回到 new、
// online 归零并清掉退避与错误，最后把 deleted_at 置空。这样删号重建既不会撞
// username 唯一键，也不会把上一轮的封禁/停用/失败计数带过来。
// created_at 保留旧值（这行确实存在了很久），调用方如需展示可自行说明。
func (r *AccountRepository) reviveSoftDeleted(ctx context.Context, acc *model.Account, passwordSecret string) (bool, error) {
	var row model.Account
	err := r.db.WithContext(ctx).Unscoped().
		Where("username = ?", acc.Username).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil // 没有同名的历史行
		}
		return false, fmt.Errorf("查询同名账号失败: %w", err)
	}
	if !row.DeletedAt.Valid {
		return false, nil // 同名且仍在用 → 交给 INSERT 去报真冲突
	}

	nickname := acc.Nickname
	updates := map[string]any{
		"password_enc":      passwordSecret,
		"cookie_enc":        nil,
		"luogu_uid":         nil,
		"nickname":          nickname,
		"status":            model.AccountStatusNew,
		"online":            false,
		"failure_count":     0,
		"last_error":        "",
		"enabled":           true,
		"weight":            1,
		"next_verify_at":    nil,
		"last_login_at":     nil,
		"last_verified_at":  nil,
		"cookie_updated_at": nil,
		"name":              "",
		"avatar":            "",
		"slogan":            "",
		"badge":             "",
		"color":             "",
		"is_admin":          false,
		"is_banned":         false,
		"ccf_level":         0,
		"xcpc_level":        0,
		"background":        "",
		"profile_json":      "",
		"deleted_at":        nil,
		// 代码公开计划：重置成"未确认"。同一个 username 就是同一个洛谷账号，
		// 远端加入状态其实不会因删号消失，但复活语义是"当作新导入重新确认一次"，
		// 而重新确认只是多一次读（远端已是 1 时不会再写），代价可忽略。
		"open_source_joined":    false,
		"open_source_joined_at": nil,
	}

	res := r.db.WithContext(ctx).Unscoped().
		Model(&model.Account{}).
		Where("id = ? AND deleted_at IS NOT NULL", row.ID).
		Updates(updates)
	if res.Error != nil {
		return false, fmt.Errorf("复活同名账号失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return false, nil // 并发下已被别人复活/重建 → 按冲突处理
	}

	acc.ID = row.ID
	acc.Status = model.AccountStatusNew
	acc.Enabled = true
	acc.Weight = 1
	acc.Online = false
	acc.FailureCount = 0
	acc.LastError = ""
	acc.NextVerifyAt = nil
	acc.LuoguUID = nil
	acc.CookieSecret = ""
	acc.PasswordSecret = passwordSecret
	acc.CreatedAt = row.CreatedAt
	acc.OpenSourceJoined = false
	acc.OpenSourceJoinedAt = nil
	return true, nil
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

// ListActive 列出需要进入号池的账号（启用且非 disabled/banned）。
//
// 启动预热使用：即使 cookie 为空/无效也会返回，由号池标记为待重登。
func (r *AccountRepository) ListActive(ctx context.Context) ([]*model.Account, error) {
	var rows []*model.Account
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND status NOT IN ?", true, model.AccountInactiveStatuses).
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

	// 注意必须给 OR 加括号：GORM 拼接多个 Where 时不会自动加括号，
	// 否则会变成 (enabled AND status IN (...) AND next_verify_at IS NULL) OR next_verify_at <= ?
	// ——后半段不带任何过滤条件，会把停用/封禁账号也一起捞出来。
	var rows []*model.Account
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Where("status IN ?", model.AccountSweepableStatuses).
		Where("(next_verify_at IS NULL OR next_verify_at <= ?)", now).
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

// MarkOpenSourceJoined 记录账号已加入"代码公开计划"。
//
// 刻意不走 update()：那里的 RowsAffected==0 会被当成"账号不存在"报错，而这里是
// 幂等标记——并发下别人刚写过、或值本来就相同（MySQL 对"没改变任何值"的
// UPDATE 返回 0 行）都属于正常情况。带 WHERE 守卫让它在数据库层也是幂等的。
func (r *AccountRepository) MarkOpenSourceJoined(ctx context.Context, id uint, joinedAt time.Time) error {
	res := r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ? AND open_source_joined = ?", id, false).
		Updates(map[string]any{
			"open_source_joined":    true,
			"open_source_joined_at": joinedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("记录代码公开计划状态失败 (id=%d): %w", id, res.Error)
	}
	return nil
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

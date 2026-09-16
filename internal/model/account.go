// Package model 定义数据库实体（GORM Model）与表结构映射。
//
// 该包只放结构体、表名约定与持久化取值常量，不写任何业务逻辑与查询。
package model

import (
	"time"

	"gorm.io/gorm"
)

// 账号在号池中的状态（account.status 的取值）
const (
	// AccountStatusNew 刚入库、还没成功登录过
	AccountStatusNew = "new"
	// AccountStatusActive 登录态有效，可被请求选中
	AccountStatusActive = "active"
	// AccountStatusReloginPending cookie 已失效、等待重登（环境原因导致本轮没成功，稍后重试）
	AccountStatusReloginPending = "relogin_pending"
	// AccountStatusReloginFailed 重登尝试用尽仍失败（慢速重试，不再高频打洛谷）
	AccountStatusReloginFailed = "relogin_failed"
	// AccountStatusDisabled 凭据/账号级问题或人工停用，不再自动重试
	AccountStatusDisabled = "disabled"
	// AccountStatusBanned 洛谷封禁/限制：账号密码仍能登录，但所有接口都返回 403。
	// 这种账号自动重登只会无限循环（登录成功 → 又被 403），因此同样退出自动重试，
	// 等人工处理（解封后用 PATCH enabled=true 重新入池验证）。
	AccountStatusBanned = "banned"
)

// AccountSweepableStatuses 定时任务会去验证/重登的状态集合。
//
// disabled / banned 只能人工恢复，不在扫描范围内——否则封禁账号会陷入
// "验证 403 → 重登成功 → 再验证 403" 的死循环。
var AccountSweepableStatuses = []string{
	AccountStatusNew,
	AccountStatusActive,
	AccountStatusReloginPending,
	AccountStatusReloginFailed,
}

// AccountInactiveStatuses 不参与号池的状态（启动预热与选号都跳过）
var AccountInactiveStatuses = []string{
	AccountStatusDisabled,
	AccountStatusBanned,
}

// PoolState 号池运行状态的更新内容（不含 cookie 与平台档案）
type PoolState struct {
	Online       bool
	Status       string
	FailureCount int32
	LastError    string
	NextVerifyAt *time.Time
}

// LuoguProfile 洛谷平台用户信息（SDK UserService.Get 暴露的字段 + 原始 JSON 快照）
type LuoguProfile struct {
	Name       string // 昵称
	Avatar     string
	Slogan     string
	Badge      string
	Color      string
	IsAdmin    bool
	IsBanned   bool
	CCFLevel   int
	XCPCLevel  int
	Background string
	RawJSON    string // 原始 JSON，便于以后扩展字段而不用改表
}

// Account 洛谷账号（号池）。
//
// 一行的三种关注点刻意放在同一张表里：登录凭据（password_enc）、平台用户档案
// （name/avatar/... 由登录后回填）、号池运行状态（online/status/next_verify_at）；
// 本项目规模下拆表带来的 join 成本大于收益。
//
// 凭据列存密文：PasswordSecret / CookieSecret 由 repository 加解密，
// 明文只出现在 Password / Cookie 这两个 gorm:"-" 字段上，
// 且只有经 repository 读出的对象才带明文。
type Account struct {
	ID uint `gorm:"column:id;primaryKey;autoIncrement"`

	// --- 登录凭据（密文落库） ---
	Username       string `gorm:"column:username;type:varchar(64);not null;uniqueIndex:uk_account_username;comment:洛谷登录名"`
	PasswordSecret string `gorm:"column:password_enc;type:varchar(512);not null;comment:AES-GCM 加密后的密码"`
	CookieSecret   string `gorm:"column:cookie_enc;type:text;comment:AES-GCM 加密后的 cookie（JSON）"`

	// Password / Cookie 为解密后的明文，仅存在于内存
	Password string `gorm:"-"`
	Cookie   string `gorm:"-"`

	// --- 号池运行状态 ---
	// LuoguUID 可空：唯一索引允许多个 NULL，但不允许重复的 0，因此未登录时必须为 NULL
	LuoguUID     *int64 `gorm:"column:luogu_uid;uniqueIndex:uk_account_luogu_uid;comment:洛谷 UID，未登录为 NULL"`
	Nickname     string `gorm:"column:nickname;type:varchar(64);not null;default:'';comment:昵称"`
	Online       bool   `gorm:"column:online;not null;default:false;index:idx_account_online;comment:是否可被请求选中"`
	Status       string `gorm:"column:status;type:varchar(24);not null;default:'new';index:idx_account_status"`
	FailureCount int32  `gorm:"column:failure_count;not null;default:0;comment:连续重登失败的轮数"`
	LastError    string `gorm:"column:last_error;type:varchar(512);not null;default:'';comment:最近一次错误摘要（不含凭据）"`
	// Enabled / Weight 刻意不写 default：GORM 在 INSERT 时会跳过"带 default 的零值字段"，
	// 一旦写成 default:true/1，就永远建不出 enabled=false 的账号（会被数据库默认值覆盖）。
	Enabled        bool       `gorm:"column:enabled;not null;comment:人工启停开关"`
	Weight         int32      `gorm:"column:weight;not null;comment:预留的加权轮询权重"`
	NextVerifyAt   *time.Time `gorm:"column:next_verify_at;index:idx_account_next_verify;comment:到期验证时间"`
	LastLoginAt    *time.Time `gorm:"column:last_login_at"`
	LastVerifiedAt *time.Time `gorm:"column:last_verified_at"`
	CookieUpdated  *time.Time `gorm:"column:cookie_updated_at"`

	// --- 代码公开计划（可选能力，由 ACCOUNT_JOIN_OPEN_SOURCE 开启） ---
	// 加入后洛谷限制 30 天内不能退出，因此这是"一生只做一次"的动作：
	// 成功后不再请求，只靠这两列记录结果与解锁时间点。
	//
	// OpenSourceJoined 不写 default：与 Enabled 同理，GORM 会跳过"带 default 的
	// 零值字段"，这里的零值 false 正是新账号想要的初始值。
	OpenSourceJoined   bool       `gorm:"column:open_source_joined;not null;comment:是否已加入代码公开计划"`
	OpenSourceJoinedAt *time.Time `gorm:"column:open_source_joined_at;comment:洛谷记录的加入时间（30 天锁定的解锁基准）"`

	// --- 洛谷平台用户字段（登录后回填） ---
	Name        string `gorm:"column:name;type:varchar(64);not null;default:''"`
	Avatar      string `gorm:"column:avatar;type:varchar(512);not null;default:''"`
	Slogan      string `gorm:"column:slogan;type:varchar(512);not null;default:''"`
	Badge       string `gorm:"column:badge;type:varchar(128);not null;default:''"`
	Color       string `gorm:"column:color;type:varchar(32);not null;default:''"`
	IsAdmin     bool   `gorm:"column:is_admin;not null;default:false"`
	IsBanned    bool   `gorm:"column:is_banned;not null;default:false"`
	CCFLevel    int32  `gorm:"column:ccf_level;not null;default:0"`
	XCPCLevel   int32  `gorm:"column:xcpc_level;not null;default:0"`
	Background  string `gorm:"column:background;type:varchar(512);not null;default:''"`
	ProfileJSON string `gorm:"column:profile_json;type:text;comment:平台用户原始 JSON 快照"`

	CreatedAt time.Time      `gorm:"column:created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index:idx_account_deleted_at"`
}

// TableName 指定表名
func (Account) TableName() string { return "accounts" }

// UIDValue 返回可直接用于日志/JSON 的 UID（未登录为 0）
func (a Account) UIDValue() int64 {
	if a.LuoguUID == nil {
		return 0
	}
	return *a.LuoguUID
}

// Serving 判断该行当前是否允许被请求路径选中
func (a Account) Serving() bool {
	return a.Enabled && a.Online && a.Status == AccountStatusActive && a.Cookie != ""
}

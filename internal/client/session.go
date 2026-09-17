// Package client 封装对外部服务的访问（目前只有洛谷客户端）。
//
// 本包是唯一直接依赖洛谷 SDK 的包：对外只暴露号池（Pool）、业务适配方法
// （GetProblem 等）与少量类型别名，其余各层不感知 SDK 细节。
//
// 号池的核心约定：一个账号一个长期存活的 sdk.Client。绝不在请求路径上给
// 共享 client 反复 ImportCookies——SDK 的 cookie jar 是 client 级可变状态，
// 并发注入不同账号会让在途请求"变成"另一个账号。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/laoin114514/luoguClient"

	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/model"
)

// SessionClient 池内单个账号的会话能力。
//
// 刻意定义成窄接口：SDK 的 baseURL 未导出，外部包无法把它指向 httptest 服务器，
// 因此号池的测试替身必须从这一层注入（见 pool_test.go）。
type SessionClient interface {
	UID() int
	ExportCookies() ([]byte, error)
	ImportCookies(data []byte) error
	ClearCookies() error
	RefreshCSRF() error
	GetCaptcha() ([]byte, error)
	Login(username, password, captcha string) error
	Verify() error
	UserProfile() (model.LuoguProfile, error)

	// CodePublicStatus 只读读取远端偏好，判断账号是否已加入"代码公开计划"。
	//
	// 与 EnsureCodePublic 刻意分开：读偏好是幂等的只读请求，用来让本地
	// accounts.open_source_joined 与洛谷保持一致；而"加入"是不可逆动作
	// （加入后 30 天内不能退出），只能由明确开启的开关触发写入。
	//
	// joined=false 表示远端确认未加入；err != nil 表示这次没读到，调用方
	// 必须按"状态未知"处理，绝不能据此判定未加入。
	CodePublicStatus() (joined bool, joinedAt time.Time, err error)

	// EnsureCodePublic 确保账号已加入洛谷"代码公开计划"（openSource=1），幂等。
	//
	// 远端已是 1 时只读一次偏好、不写；否则读-改-写整份偏好（该接口是
	// 全量替换语义，只发一个字段会把其它偏好重置成平台默认值）。
	// 返回洛谷记录的加入时间；洛谷未给出时返回零值，由调用方兜底。
	EnsureCodePublic() (time.Time, error)

	// SDK 返回底层客户端，仅供本包的业务适配方法使用
	SDK() *sdk.Client
}

// ClientFactory 按 cookie 构造会话；生产实现返回真实 SDK 客户端，测试注入替身
type ClientFactory func(cookie []byte) (SessionClient, error)

// newSDKFactory 生产用的会话工厂。
//
// ctx 是进程级上下文：SDK 的 WithContext 只在构造期生效，构造完成后无法按
// 单次请求取消，因此业务侧只能用 HTTP 超时兜底（见 README 的已知限制）。
//
// newUA 在每次建会话时取一条 UA：SDK 的 User-Agent 在构造期写死、之后改不了，
// 而池子的约定是"一个账号一个长期存活的 client"，于是"一号一会话"天然就是
// "一会话一条 UA"——同一账号的所有请求共用一条 UA，中途不会换指纹。
// UA 由内嵌数据生成，构造期不发起任何网络请求。
func newSDKFactory(ctx context.Context, cfg config.Luogu, newUA func() string) ClientFactory {
	return func(cookie []byte) (SessionClient, error) {
		c, err := sdk.NewClient(
			sdk.WithContext(ctx),
			sdk.WithTimeout(cfg.Timeout),
			sdk.WithRetry(cfg.Retry, nil),
			sdk.WithUserAgent(newUA()),
			sdk.WithCookies(cookie),
		)
		if err != nil {
			return nil, fmt.Errorf("创建洛谷客户端失败: %w", err)
		}
		return &sdkSession{client: c}, nil
	}
}

// sdkSession 基于 SDK 的会话实现
type sdkSession struct {
	client *sdk.Client
}

func (s *sdkSession) UID() int                        { return s.client.UID() }
func (s *sdkSession) ExportCookies() ([]byte, error)  { return s.client.ExportCookies() }
func (s *sdkSession) ImportCookies(data []byte) error { return s.client.ImportCookies(data) }
func (s *sdkSession) ClearCookies() error             { return s.client.ClearCookies() }
func (s *sdkSession) RefreshCSRF() error              { return s.client.Auth.RefreshCSRF() }
func (s *sdkSession) GetCaptcha() ([]byte, error)     { return s.client.Auth.GetCaptcha() }
func (s *sdkSession) Verify() error                   { return s.client.Auth.Verify() }
func (s *sdkSession) SDK() *sdk.Client                { return s.client }

// Login 登录；SDK 的失败原因在 *sdk.AuthError 里（含验证码错误的具体类型）
func (s *sdkSession) Login(username, password, captcha string) error {
	if _, err := s.client.Auth.Login(username, password, captcha); err != nil {
		return err
	}
	return nil
}

// UserProfile 拉取当前会话的洛谷用户资料（需要已登录）
func (s *sdkSession) UserProfile() (model.LuoguProfile, error) {
	uid := s.client.UID()
	if uid <= 0 {
		return model.LuoguProfile{}, errors.New("当前会话没有 UID，无法获取用户资料")
	}

	detail, err := s.client.User.Get(uid)
	if err != nil {
		return model.LuoguProfile{}, err
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		return model.LuoguProfile{}, fmt.Errorf("序列化用户资料失败: %w", err)
	}

	return model.LuoguProfile{
		Name:       detail.Name,
		Avatar:     detail.Avatar,
		Slogan:     detail.Slogan,
		Badge:      detail.Badge,
		Color:      detail.Color,
		IsAdmin:    detail.IsAdmin,
		IsBanned:   detail.IsBanned,
		CCFLevel:   detail.CCFLevel,
		XCPCLevel:  detail.XCPCLevel,
		Background: detail.Background,
		RawJSON:    string(raw),
	}, nil
}

// CodePublicStatus 只读读取远端偏好里的"代码公开计划"状态。
//
// 走 SDK 的 UserService.GetPreference（个人设置 → 偏好设置页），不做任何写入；
// openSource 不是 1 时返回 joined=false，调用方据此知道"远端确实没加入"。
func (s *sdkSession) CodePublicStatus() (bool, time.Time, error) {
	pref, err := s.client.User.GetPreference()
	if err != nil {
		return false, time.Time{}, err
	}
	return pref.OpenSource == sdk.OpenSourceEnabled, unixTime(pref.OpenSourceJoinTime), nil
}

// EnsureCodePublic 确保账号已加入洛谷"代码公开计划"（幂等）。
//
// 读-改-写与"必须复核落库结果"的逻辑放在 SDK 的 UserService.JoinOpenSourcePlan
// （那里能用 httptest 覆盖；本包无法把 SDK 指向测试服务器，见 SessionClient 的说明），
// 这里只做 int64 时间戳到 time.Time 的转换与零值语义统一。
func (s *sdkSession) EnsureCodePublic() (time.Time, error) {
	joinTime, err := s.client.User.JoinOpenSourcePlan()
	if err != nil {
		return time.Time{}, err
	}
	return unixTime(joinTime), nil
}

// unixTime 把洛谷的 Unix 秒时间戳转成 time.Time（<=0 视为未知，返回零值）
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

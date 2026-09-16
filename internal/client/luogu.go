// Package client 封装对外部服务的访问（目前只有洛谷客户端）。
//
// 业务层通过本包访问洛谷，不直接依赖 SDK 细节，便于后续替换或加缓存。
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	sdk "github.com/laoin114514/luoguClient"

	"github.com/laoin114514/luogu2api/internal/config"
)

// Luogu 包装洛谷 SDK 客户端
type Luogu struct {
	client     *sdk.Client
	cookieFile string
	logger     *slog.Logger
}

// SessionInfo 洛谷会话的本地状态（只读内存/cookie，不发起网络请求）
type SessionInfo struct {
	Configured bool   // 客户端是否初始化成功
	UID        int    // 当前会话 UID，0 表示未登录
	CookieFile string // cookie 持久化位置
}

// NewLuogu 创建洛谷客户端，并尽力从 cookie 文件恢复登录态
func NewLuogu(ctx context.Context, cfg config.Luogu, logger *slog.Logger) (*Luogu, error) {
	c, err := sdk.NewClient(sdk.WithContext(ctx), sdk.WithTimeout(cfg.Timeout))
	if err != nil {
		return nil, fmt.Errorf("创建洛谷客户端失败: %w", err)
	}

	l := &Luogu{client: c, cookieFile: cfg.CookieFile, logger: logger}

	// SDK 只在内存中保存 cookie，落盘由本服务负责
	data, err := os.ReadFile(cfg.CookieFile)
	switch {
	case err == nil:
		if err := c.ImportCookies(data); err != nil {
			logger.Warn("恢复 cookie 失败，以匿名身份运行", "file", cfg.CookieFile, "err", err)
		} else {
			logger.Info("已加载 cookie", "file", cfg.CookieFile, "uid", c.UID())
		}
	case errors.Is(err, os.ErrNotExist):
		logger.Info("未找到 cookie 文件，以匿名身份运行", "file", cfg.CookieFile)
	default:
		logger.Warn("读取 cookie 文件失败，以匿名身份运行", "file", cfg.CookieFile, "err", err)
	}

	return l, nil
}

// SDK 返回底层 SDK 客户端，供需要具体类型的场景使用
func (l *Luogu) SDK() *sdk.Client { return l.client }

// Session 返回会话的本地状态（读 _uid cookie，不发网络请求）
func (l *Luogu) Session() SessionInfo {
	if l.client == nil {
		return SessionInfo{Configured: false, CookieFile: l.cookieFile}
	}
	return SessionInfo{Configured: true, UID: l.client.UID(), CookieFile: l.cookieFile}
}

// VerifyLogin 向洛谷校验登录态是否仍有效。
//
// ⚠️ 会产生一次网络请求，不要放在健康检查里调用。
func (l *Luogu) VerifyLogin() error {
	if err := l.client.Auth.Verify(); err != nil {
		return fmt.Errorf("洛谷登录态校验失败: %w", err)
	}
	return nil
}

// SaveCookies 把当前会话 cookie 落盘（登录成功后调用）
func (l *Luogu) SaveCookies() error {
	data, err := l.client.ExportCookies()
	if err != nil {
		return fmt.Errorf("导出 cookie 失败: %w", err)
	}
	if err := os.WriteFile(l.cookieFile, data, 0o600); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", l.cookieFile, err)
	}
	return nil
}

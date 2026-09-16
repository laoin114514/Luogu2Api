// Command api 是 Luogu2Api 的 HTTP 服务入口。
//
// 启动顺序：加载配置 → 初始化日志 → 连接 MySQL（可选）→ 初始化洛谷客户端
// → 组装 service/handler → 启动 gin → 等待退出信号 → 优雅关闭。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/handler"
	"github.com/laoin114514/luogu2api/internal/repository"
	"github.com/laoin114514/luogu2api/internal/router"
	"github.com/laoin114514/luogu2api/internal/service"
)

func main() {
	// 配置以环境变量为准（见 configs/env.example），-addr 仅作本地调试时的覆盖
	addr := flag.String("addr", "", "覆盖 HTTP_ADDR，例如 127.0.0.1:8080")
	flag.Parse()

	if err := run(*addr); err != nil {
		slog.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

func run(addrOverride string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if addrOverride != "" {
		cfg.HTTP.Addr = addrOverride
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	// 收到 SIGINT/SIGTERM 后取消 ctx，触发优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 基础设施：MySQL（未配置 DB_HOST 时为 nil，以无数据库模式运行）
	db, err := initDB(cfg, logger)
	if err != nil {
		return err
	}
	if db != nil {
		defer func() {
			if err := repository.Close(db); err != nil {
				logger.Warn("关闭数据库连接失败", "err", err)
			}
		}()
	}

	// 基础设施：洛谷客户端
	luoguClient, err := client.NewLuogu(ctx, cfg.Luogu, logger)
	if err != nil {
		return err
	}

	// 依赖注入：service 依赖 repository/client 的窄接口，handler 依赖 service
	// 注意：db 为 nil 时不能直接塞进接口（会得到非 nil 的接口值），这里显式判断
	var pinger service.DBPinger
	if db != nil {
		pinger = repository.NewHealthRepository(db)
	}
	healthService := service.NewHealthService(pinger, luoguClient)
	healthHandler := handler.NewHealthHandler(healthService)

	engine := router.New(router.Deps{
		Logger: logger,
		Health: healthHandler,
		Env:    cfg.App.Env,
	})

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务启动",
			"addr", cfg.HTTP.Addr,
			"env", cfg.App.Env,
			"db_enabled", cfg.DB.Enabled(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP 服务异常: %w", err)
	case <-ctx.Done():
		logger.Info("收到退出信号，开始关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	logger.Info("已退出")
	return nil
}

// initDB 连接 MySQL 并按需执行迁移；未配置 DB_HOST 时返回 nil
func initDB(cfg config.Config, logger *slog.Logger) (*gorm.DB, error) {
	if !cfg.DB.Enabled() {
		logger.Warn("未配置 MySQL（DB_HOST 为空），以无数据库模式启动")
		return nil, nil
	}

	db, err := repository.NewDB(cfg.DB)
	if err != nil {
		return nil, err
	}

	if cfg.DB.AutoMigrate {
		if err := repository.Migrate(db); err != nil {
			return nil, err
		}
		logger.Info("AutoMigrate 完成")
	}

	logger.Info("已连接 MySQL",
		"host", cfg.DB.Host,
		"port", cfg.DB.Port,
		"database", cfg.DB.Name,
	)
	return db, nil
}

// newLogger 生产环境输出 JSON，开发环境输出易读文本
func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.App.IsProd() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// Command api 是 Luogu2Api 的 HTTP 服务入口。
//
// 启动顺序：加载配置 → 初始化日志 → 连接 MySQL（必填，号池依赖）→ 初始化
// 凭据加解密 → 组装号池并预热 → 组装 service/handler → 启动 gin 与号池扫描器
// → 等待退出信号 → 优雅关闭（先停 HTTP，再停扫描器，最后关数据库）。
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
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/handler"
	"github.com/laoin114514/luogu2api/internal/repository"
	"github.com/laoin114514/luogu2api/internal/router"
	"github.com/laoin114514/luogu2api/internal/secret"
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

	// 收到 SIGINT/SIGTERM 后取消 ctx：既是 SDK 请求的默认 context，也触发优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 基础设施：MySQL（号池依赖它，配置校验保证 DB_HOST 必填）
	db, err := initDB(cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := repository.Close(db); err != nil {
			logger.Warn("关闭数据库连接失败", "err", err)
		}
	}()

	// 凭据加解密（config.Load 已校验密钥可用，这里只做构造）
	cipher, err := secret.NewCipher(cfg.Account.SecretKey)
	if err != nil {
		return fmt.Errorf("初始化凭据加解密失败: %w", err)
	}
	accountRepo := repository.NewAccountRepository(db, cipher)

	// 号池：一个账号一个 SDK client，先预热再开始服务
	pool := client.NewPool(ctx, cfg, accountRepo, logger)
	warm, err := pool.Warmup(ctx)
	if err != nil {
		return fmt.Errorf("号池预热失败: %w", err)
	}
	logger.Info("号池预热完成",
		"total", warm.Total, "serving", warm.Serving, "pending", warm.Pending, "failed", warm.Failed)
	if warm.Total == 0 {
		logger.Warn("号池为空：请通过管理接口导入账号（未配置 ADMIN_TOKEN 时管理路由不会注册）")
	}

	// 依赖注入：service 依赖 repository/client 的窄接口，handler 依赖 service
	healthService := service.NewHealthService(repository.NewHealthRepository(db), pool)
	accountService := service.NewAccountService(accountRepo, pool, logger)
	problemService := service.NewProblemService(pool)
	poolService := service.NewPoolService(pool, cfg.Account.SweepInterval, logger)

	engine := router.New(router.Deps{
		Logger:     logger,
		Health:     handler.NewHealthHandler(healthService),
		Problem:    handler.NewProblemHandler(problemService),
		Pool:       handler.NewPoolHandler(pool),
		Account:    handler.NewAccountHandler(accountService),
		Env:        cfg.App.Env,
		AdminToken: cfg.Admin.Token,
	})

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 定时扫描号池。用独立 ctx：关闭时先停 HTTP，再停扫描器，最后关数据库，
	// 否则会出现"数据库已关闭、扫描器还在写"的报错噪音。
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	var sweeperWG sync.WaitGroup
	sweeperWG.Add(1)
	go func() {
		defer sweeperWG.Done()
		poolService.RunSweeper(sweeperCtx)
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务启动",
			"addr", cfg.HTTP.Addr,
			"env", cfg.App.Env,
			"admin_enabled", cfg.Admin.Enabled(),
			"sweep_interval", cfg.Account.SweepInterval.String(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		stopSweeper()
		sweeperWG.Wait()
		return fmt.Errorf("HTTP 服务异常: %w", err)
	case <-ctx.Done():
		logger.Info("收到退出信号，开始关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)

	// 等在途的一轮扫描结束、后台恢复流程收尾，再让 defer 关闭数据库
	stopSweeper()
	sweeperWG.Wait()
	if !pool.WaitBackground(5 * time.Second) {
		logger.Warn("后台账号恢复任务未在超时内结束，继续关闭")
	}

	if shutdownErr != nil {
		return fmt.Errorf("优雅关闭失败: %w", shutdownErr)
	}
	logger.Info("已退出")
	return nil
}

// initDB 连接 MySQL 并按需执行迁移
func initDB(cfg config.Config, logger *slog.Logger) (*gorm.DB, error) {
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

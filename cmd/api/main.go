// Command api 是 Luogu2Api 的 HTTP 服务入口。
//
// 启动顺序：加载配置 → 初始化日志 →（允许改结构时）建库 → 连接 MySQL（必填，号池依赖）
// → 校验/迁移库结构 → 初始化凭据加解密 → 组装号池并预热 → 组装 service/handler →
// 启动 gin 与号池扫描器 → 等待退出信号 → 优雅关闭（先停 HTTP，再停扫描器，最后关数据库）。
//
// 另有三个只做一件事的入口：-migrate（执行库结构变更后退出）、
// -schema-status（只打印结构与模型的差异，只读、不建库）与 -addr（本地调试时覆盖监听地址）。
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
	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/repository"
	"github.com/laoin114514/luogu2api/internal/router"
	"github.com/laoin114514/luogu2api/internal/schema"
	"github.com/laoin114514/luogu2api/internal/secret"
	"github.com/laoin114514/luogu2api/internal/service"
)

func main() {
	// 配置以环境变量为准（见 configs/env.example），-addr 仅作本地调试时的覆盖
	addr := flag.String("addr", "", "覆盖 HTTP_ADDR，例如 127.0.0.1:8080")
	migrateOnly := flag.Bool("migrate", false, "只执行库结构变更（建库/建表/加列/建索引）然后退出")
	schemaStatus := flag.Bool("schema-status", false, "只打印库结构与模型的差异然后退出（只读：不改结构、不写审计）")
	flag.Parse()

	if err := run(*addr, *migrateOnly, *schemaStatus); err != nil {
		slog.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

func run(addrOverride string, migrateOnly, schemaStatus bool) error {
	// 配置只来自环境变量（见 configs/env.example）。本地开发用 scripts/dev.ps1
	// 把 configs/.env 导出到当前会话；容器里由 compose/k8s 注入——二进制刻意
	// 不读 .env，避免"镜像里残留一个 .env 就把忘记配置的变量悄悄补上"。
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

	// 建库：DSN 直接指向 DB_NAME，库本身不存在时连接阶段就是 1049，schema 那套
	// "表/列/索引"的比对根本没机会跑。建库不会丢数据，因此与"建表/加列"同一个
	// 把关口——只有允许改结构的两个入口（-migrate、DB_MIGRATE_ON_START）会自动建。
	// -schema-status 是只读入口，刻意不建库。
	if !schemaStatus && (migrateOnly || cfg.DB.MigrateOnStart) {
		created, err := repository.EnsureDatabase(ctx, cfg.DB)
		if err != nil {
			return err
		}
		if created {
			logger.Info("数据库不存在，已自动创建", "database", cfg.DB.Name)
		}
	}

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

	// 库结构：model 包里的模型是唯一来源（没有任何手写的迁移 SQL）。
	// 放在号池预热之前——结构不对就没必要去连洛谷了。
	if schemaStatus {
		return reportSchemaStatus(ctx, db, logger)
	}

	if cfg.App.IsProd() && cfg.DB.MigrateOnStart {
		logger.Warn("生产环境开启了启动自动迁移（DB_MIGRATE_ON_START=true）",
			"hint", "结构变更已用 MySQL 命名锁串行化，但多副本同时启动时仍建议只让一个实例来跑，或改用 api -migrate")
	}
	report, err := schema.Ensure(ctx, db, schema.Options{
		Apply:  migrateOnly || cfg.DB.MigrateOnStart,
		Strict: migrateOnly || cfg.DB.SchemaStrict,
		Logger: logger,
	}, model.All()...)
	logSchemaReport(logger, report)
	if err != nil {
		return err
	}
	if migrateOnly {
		logger.Info("库结构检查完成（-migrate），退出（未启动 HTTP 服务）")
		return nil
	}

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
	recordService := service.NewRecordService(pool)
	poolService := service.NewPoolService(pool, cfg.Account.SweepInterval, logger)

	engine := router.New(router.Deps{
		Logger:     logger,
		Health:     handler.NewHealthHandler(healthService),
		Problem:    handler.NewProblemHandler(problemService),
		Record:     handler.NewRecordHandler(recordService),
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
			"join_open_source", cfg.Account.JoinOpenSource,
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

// initDB 只负责连接 MySQL；库结构的校验与迁移由 internal/schema 负责，
// 这样"连不上数据库"与"库结构不对"是两个语义清晰的失败。
func initDB(cfg config.Config, logger *slog.Logger) (*gorm.DB, error) {
	db, err := repository.NewDB(cfg.DB)
	if err != nil {
		return nil, err
	}

	logger.Info("已连接 MySQL",
		"host", cfg.DB.Host,
		"port", cfg.DB.Port,
		"database", cfg.DB.Name,
	)
	return db, nil
}

// reportSchemaStatus 打印库结构与模型的差异（只读：不改结构、不写审计），
// 两者不一致时返回错误（进程退出码非 0），便于 CI/巡检脚本直接使用。
func reportSchemaStatus(ctx context.Context, db *gorm.DB, logger *slog.Logger) error {
	report, err := schema.Status(ctx, db, model.All()...)
	if err != nil {
		return err
	}
	logSchemaReport(logger, report)
	if !report.Consistent() {
		return errors.New("库结构与模型不一致（见上方报告）")
	}
	return nil
}

// logSchemaReport 输出结构检查结果；一致时也只说一句，避免刷屏
func logSchemaReport(logger *slog.Logger, report schema.Report) {
	for _, line := range report.Lines() {
		logger.Info("库结构", "change", line)
	}
	if report.Consistent() {
		logger.Info("库结构与模型一致",
			"tables", len(report.Expected.Tables), "fingerprint", shortHash(report.Expected.Hash))
	}
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
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

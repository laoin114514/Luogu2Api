// Package repository 是数据访问层：本包是唯一直接依赖 GORM / MySQL 的地方，
// 对上层只暴露仓储方法与领域结构体。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/laoin114514/luogu2api/internal/config"
)

// NewDB 建立 MySQL 连接并配置连接池
func NewDB(cfg config.DB) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel(cfg.LogLevel)),
		// 把驱动错误翻译成 gorm.ErrDuplicatedKey 等，上层无需判断 MySQL 错误码
		TranslateError: true,
	})
	if err != nil {
		return nil, connectError(cfg, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return db, nil
}

// Close 关闭连接池
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
	}
	return sqlDB.Close()
}

// connectError 给"库不存在"这类首次部署最常见的失败补上可操作提示。
//
// 单独识别 1049（Unknown database）：DSN 直接指向 DB_NAME，库还没建时连接在握手
// 阶段就被拒绝，驱动原文只有一句 Unknown database，很难联想到"要先建库"。
func connectError(cfg config.DB, err error) error {
	var mysqlErr *driver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1049 {
		return fmt.Errorf("连接 MySQL 失败：数据库 %s 不存在"+
			"（api -migrate 或 DB_MIGRATE_ON_START=true 会自动建库，也可手工 CREATE DATABASE，见 README「本地运行」）: %w",
			cfg.Name, err)
	}
	return fmt.Errorf("连接 MySQL 失败: %w", err)
}

// EnsureDatabase 确保目标数据库存在，返回本次是否真的建了库。
//
// internal/schema 管的是"库内部"的表/列/索引，管不到库本身：DSN 直接指向 DB_NAME，
// 库不存在时 MySQL 在建立连接阶段就报 1049，任何一条建表 DDL 都无从执行。所以全新
// 的 MySQL 上必须先有这一句 CREATE DATABASE——它和"加列/建表"一样不会丢数据，因此
// 归入同一类安全变更，由 -migrate / DB_MIGRATE_ON_START 这个开关放行。
//
// 字符集与 README 的手工建库命令一致（utf8mb4），排序规则交给服务端默认：MySQL
// 5.7 / 8.0 / 9.x 的默认 collation 并不相同，写死一个反而会在别的版本上挑错。
func EnsureDatabase(ctx context.Context, cfg config.DB) (created bool, err error) {
	// 库名会被拼进 DDL，先按配置规则校验一遍（config.Load 已挡过，这里是 SQL 边界的兜底）
	if err := config.ValidateDBName(cfg.Name); err != nil {
		return false, err
	}

	// 不选库连接：库还不存在时只有这种连接连得上
	server := cfg
	server.Name = ""
	db, err := gorm.Open(mysql.Open(server.DSN()), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(logLevel(cfg.LogLevel)),
		TranslateError: true,
	})
	if err != nil {
		return false, fmt.Errorf("连接 MySQL 失败（建库前的不选库连接）: %w", err)
	}
	defer func() { _ = Close(db) }()

	var exists int64
	if err := db.WithContext(ctx).
		Raw("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", cfg.Name).
		Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("查询数据库 %s 是否存在失败: %w", cfg.Name, err)
	}
	if exists > 0 {
		return false, nil
	}

	// IF NOT EXISTS：多实例同时启动时另一边可能刚建完，这里不该因此报错
	ddl := "CREATE DATABASE IF NOT EXISTS " + quoteIdentifier(cfg.Name) + " DEFAULT CHARACTER SET utf8mb4"
	if err := db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return false, fmt.Errorf("创建数据库 %s 失败（连接账号需要 CREATE 权限）: %w", cfg.Name, err)
	}
	return true, nil
}

// quoteIdentifier 把库名包成反引号标识符；名字里真有反引号时按 MySQL 规则双写转义
// （config.ValidateDBName 已经挡掉了，这里是 SQL 边界的兜底）。
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func logLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(level) {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

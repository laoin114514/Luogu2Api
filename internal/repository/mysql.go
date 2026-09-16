// Package repository 是数据访问层：本包是唯一直接依赖 GORM / MySQL 的地方，
// 对上层只暴露仓储方法与领域结构体。
package repository

import (
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/model"
)

// NewDB 建立 MySQL 连接并配置连接池
func NewDB(cfg config.DB) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel(cfg.LogLevel)),
		// 把驱动错误翻译成 gorm.ErrDuplicatedKey 等，上层无需判断 MySQL 错误码
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
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

// Migrate 执行 AutoMigrate。建议只在开发环境或首次部署时开启（DB_AUTO_MIGRATE=true）。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.All()...); err != nil {
		return fmt.Errorf("AutoMigrate 失败: %w", err)
	}
	return nil
}

// Close 关闭连接池
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
	}
	return sqlDB.Close()
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

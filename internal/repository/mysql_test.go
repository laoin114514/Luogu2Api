package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/laoin114514/luogu2api/internal/config"
)

// 库名非法时必须在建立连接之前就被挡掉：库名会被拼进 CREATE DATABASE 的 DDL
func TestEnsureDatabaseRejectsUnsafeName(t *testing.T) {
	for _, name := range []string{"", "bad;name", "bad name", "bad/name"} {
		if _, err := EnsureDatabase(context.Background(), config.DB{Name: name}); err == nil {
			t.Errorf("EnsureDatabase(%q) 应报错", name)
		}
	}
}

// 建库的集成测试：需要一台真实 MySQL（TEST_DB_DSN 指过去），未设置时跳过。
// 它会真的建一个临时库、跑完删掉。
func TestEnsureDatabaseCreatesMissingDatabase(t *testing.T) {
	cfg := serverConfigFromTestDSN(t)
	cfg.Name = fmt.Sprintf("luogu2api_ensure_%d", time.Now().UnixNano())

	server := openServerDB(t, cfg)
	t.Cleanup(func() {
		if err := server.Exec("DROP DATABASE IF EXISTS " + quoteIdentifier(cfg.Name)).Error; err != nil {
			t.Errorf("清理临时库失败: %v", err)
		}
	})

	created, err := EnsureDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("EnsureDatabase: %v", err)
	}
	if !created {
		t.Fatal("库不存在时应报告 created=true")
	}

	// 幂等：库已经在了就不该再报 created，也不该报错
	created, err = EnsureDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("EnsureDatabase（第二次）: %v", err)
	}
	if created {
		t.Error("库已存在时不该报告 created=true")
	}

	// 建出来的库要能连上，且字符集与 README 的手工建库命令一致（utf8mb4）
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连接新建的库失败: %v", err)
	}
	defer func() { _ = Close(db) }()

	var charset string
	err = db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?",
		cfg.Name).Scan(&charset).Error
	if err != nil {
		t.Fatalf("查询字符集失败: %v", err)
	}
	if charset != "utf8mb4" {
		t.Errorf("默认字符集 = %q, want utf8mb4", charset)
	}
}

// serverConfigFromTestDSN 由 TEST_DB_DSN 反推出"同一台服务器、但不选库"的 config.DB，
// 供建库测试换成一个临时库名使用。
func serverConfigFromTestDSN(t *testing.T) config.DB {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv("TEST_DB_DSN"))
	if raw == "" {
		t.Skip("未设置 TEST_DB_DSN，跳过需要数据库的测试")
	}
	dsn, err := driver.ParseDSN(raw)
	if err != nil {
		t.Fatalf("解析 TEST_DB_DSN 失败: %v", err)
	}
	host, port, ok := strings.Cut(dsn.Addr, ":")
	if !ok {
		t.Fatalf("TEST_DB_DSN 的地址应为 host:port，实际 %q", dsn.Addr)
	}

	// dsn.Params 是 map[string]string（不是 url.Values），自己拼回连接参数
	params := make([]string, 0, len(dsn.Params))
	for k, v := range dsn.Params {
		params = append(params, k+"="+v)
	}
	sort.Strings(params)

	return config.DB{
		Host:     host,
		Port:     port,
		User:     dsn.User,
		Password: dsn.Passwd,
		Params:   strings.Join(params, "&"),
	}
}

// openServerDB 打开一个"不选库"的连接，用于执行建库/删库这类管理语句
func openServerDB(t *testing.T, cfg config.DB) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(mysql.Open(cfg.ServerDSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return db
}

package config

import (
	"strings"
	"testing"
	"time"
)

// clearEnv 清空相关环境变量，避免测试受外部环境干扰
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_NAME", "APP_ENV", "HTTP_ADDR", "HTTP_SHUTDOWN_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PARAMS",
		"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME",
		"DB_AUTO_MIGRATE", "DB_LOG_LEVEL",
		"LUOGU_COOKIE_FILE", "LUOGU_TIMEOUT", "LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.App.Name != "luogu2api" || cfg.App.Env != "dev" {
		t.Errorf("App = %+v", cfg.App)
	}
	if cfg.HTTP.Addr != ":8080" || cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Errorf("HTTP = %+v", cfg.HTTP)
	}
	if cfg.DB.Enabled() {
		t.Error("DB_HOST 为空时不应启用 MySQL")
	}
	if cfg.DB.Port != "3306" || cfg.DB.MaxOpenConns != 50 || cfg.DB.MaxIdleConns != 10 {
		t.Errorf("DB 默认值 = %+v", cfg.DB)
	}
	if cfg.Luogu.CookieFile != "cookies.json" || cfg.Luogu.Timeout != 30*time.Second {
		t.Errorf("Luogu = %+v", cfg.Luogu)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_USER", "root")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "luogu2api")
	t.Setenv("DB_AUTO_MIGRATE", "true")
	t.Setenv("DB_MAX_OPEN_CONNS", "100")
	t.Setenv("DB_MAX_IDLE_CONNS", "20")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.App.IsProd() {
		t.Error("APP_ENV=prod 应被识别为生产环境")
	}
	if cfg.HTTP.Addr != "127.0.0.1:9000" || cfg.HTTP.ShutdownTimeout != 3*time.Second {
		t.Errorf("HTTP = %+v", cfg.HTTP)
	}
	if !cfg.DB.Enabled() || !cfg.DB.AutoMigrate {
		t.Errorf("DB.Enabled/AutoMigrate = %v/%v", cfg.DB.Enabled(), cfg.DB.AutoMigrate)
	}
	if cfg.DB.MaxOpenConns != 100 || cfg.DB.MaxIdleConns != 20 {
		t.Errorf("连接池 = %d/%d", cfg.DB.MaxOpenConns, cfg.DB.MaxIdleConns)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}

func TestLoadMissingDBFields(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "127.0.0.1")

	_, err := Load()
	if err == nil {
		t.Fatal("设置了 DB_HOST 但缺少 DB_USER/DB_NAME 时应报错")
	}
	for _, want := range []string{"DB_USER", "DB_NAME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应包含 %s: %v", want, err)
		}
	}
}

func TestLoadInvalidPoolConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "10")

	if _, err := Load(); err == nil {
		t.Error("空闲连接数大于最大连接数时应报错")
	}
}

func TestDBDSN(t *testing.T) {
	db := DB{
		Host: "127.0.0.1", Port: "3306",
		User: "root", Password: "pwd", Name: "luogu2api",
	}
	want := "root:pwd@tcp(127.0.0.1:3306)/luogu2api?charset=utf8mb4&parseTime=True&loc=Local"
	if got := db.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}

	// 未显式指定 Params 时应回退到默认参数
	db.Params = "charset=utf8mb4"
	if got := db.DSN(); !strings.HasSuffix(got, "charset=utf8mb4") {
		t.Errorf("DSN() = %q", got)
	}
}

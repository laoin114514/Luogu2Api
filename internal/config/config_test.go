package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// 测试用密钥：32 字节，base64 编码
var testSecretKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))

// clearEnv 清空相关环境变量，避免测试受外部环境干扰
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_NAME", "APP_ENV", "HTTP_ADDR", "HTTP_SHUTDOWN_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PARAMS",
		"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME",
		"DB_AUTO_MIGRATE", "DB_LOG_LEVEL",
		"LUOGU_TIMEOUT", "LUOGU_RETRY", "LUOGU_OCR_URL", "LUOGU_OCR_TOKEN", "LUOGU_OCR_TIMEOUT",
		"LUOGU_OCR_MODE",
		"ACCOUNT_SECRET_KEY", "ACCOUNT_SWEEP_INTERVAL", "ACCOUNT_VERIFY_INTERVAL",
		"ACCOUNT_VERIFY_JITTER", "ACCOUNT_VERIFY_CONCURRENCY", "ACCOUNT_LOGIN_MAX_ATTEMPTS",
		"ACCOUNT_LOGIN_BACKOFF", "ACCOUNT_FAILED_RETRY", "ACCOUNT_REQUEST_MAX_TRY",
		"ACCOUNT_SWEEP_BATCH_LIMIT", "ACCOUNT_JOIN_OPEN_SOURCE",
		"ADMIN_TOKEN", "LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
}

// setRequired 填上必填项：号池依赖 MySQL，且加密密钥/OCR 地址必填
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_USER", "root")
	t.Setenv("DB_NAME", "luogu2api")
	t.Setenv("ACCOUNT_SECRET_KEY", testSecretKey)
	t.Setenv("LUOGU_OCR_URL", "http://127.0.0.1:9898/ocr")
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	setRequired(t)

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
	if !cfg.DB.Enabled() {
		t.Error("设置了 DB_HOST 应启用 MySQL")
	}
	if cfg.DB.Port != "3306" || cfg.DB.MaxOpenConns != 50 || cfg.DB.MaxIdleConns != 10 {
		t.Errorf("DB 默认值 = %+v", cfg.DB)
	}
	if cfg.Luogu.Timeout != 30*time.Second || cfg.Luogu.Retry != 1 {
		t.Errorf("Luogu = %+v", cfg.Luogu)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}

	wantAccount := Account{
		SecretKey:         testSecretKey,
		SweepInterval:     5 * time.Minute,
		VerifyInterval:    30 * time.Minute,
		VerifyJitter:      0.2,
		VerifyConcurrency: 1,
		LoginMaxAttempts:  5,
		LoginBackoff:      time.Minute,
		FailedRetry:       time.Hour,
		RequestMaxTry:     3,
		SweepBatchLimit:   200,
		// 加入"代码公开计划"是不可逆动作，默认必须关闭
		JoinOpenSource: false,
	}
	if cfg.Account != wantAccount {
		t.Errorf("Account 默认值 = %+v, want %+v", cfg.Account, wantAccount)
	}
	if cfg.Account.JoinOpenSource {
		t.Error("ACCOUNT_JOIN_OPEN_SOURCE 未设置时应为 false（默认不做不可逆的隐私设置变更）")
	}
	if cfg.OCR.URL != "http://127.0.0.1:9898/ocr" || cfg.OCR.Timeout != 5*time.Second {
		t.Errorf("OCR = %+v", cfg.OCR)
	}
	if cfg.OCR.Mode != OCRModeBase64 {
		t.Errorf("OCR.Mode = %q, want %q", cfg.OCR.Mode, OCRModeBase64)
	}
	if cfg.Admin.Enabled() {
		t.Error("ADMIN_TOKEN 为空时管理接口应不可用（fail closed）")
	}
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	setRequired(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_MIGRATE_ON_START", "true")
	t.Setenv("DB_SCHEMA_STRICT", "false")
	t.Setenv("DB_MAX_OPEN_CONNS", "100")
	t.Setenv("DB_MAX_IDLE_CONNS", "20")
	t.Setenv("ACCOUNT_SWEEP_INTERVAL", "1m")
	t.Setenv("ACCOUNT_VERIFY_INTERVAL", "10m")
	t.Setenv("ACCOUNT_VERIFY_JITTER", "0.5")
	t.Setenv("ACCOUNT_LOGIN_MAX_ATTEMPTS", "2")
	t.Setenv("ACCOUNT_REQUEST_MAX_TRY", "5")
	t.Setenv("LUOGU_RETRY", "0")
	t.Setenv("ADMIN_TOKEN", "s3cret")
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
	if !cfg.DB.Enabled() || !cfg.DB.MigrateOnStart || cfg.DB.SchemaStrict {
		t.Errorf("DB.Enabled/MigrateOnStart/SchemaStrict = %v/%v/%v",
			cfg.DB.Enabled(), cfg.DB.MigrateOnStart, cfg.DB.SchemaStrict)
	}
	if cfg.DB.MaxOpenConns != 100 || cfg.DB.MaxIdleConns != 20 {
		t.Errorf("连接池 = %d/%d", cfg.DB.MaxOpenConns, cfg.DB.MaxIdleConns)
	}
	if cfg.Account.SweepInterval != time.Minute || cfg.Account.VerifyInterval != 10*time.Minute {
		t.Errorf("Account 周期 = %+v", cfg.Account)
	}
	if cfg.Account.VerifyJitter != 0.5 || cfg.Account.LoginMaxAttempts != 2 || cfg.Account.RequestMaxTry != 5 {
		t.Errorf("Account = %+v", cfg.Account)
	}
	if cfg.Luogu.Retry != 0 {
		t.Errorf("Luogu.Retry = %d, want 0", cfg.Luogu.Retry)
	}
	if !cfg.Admin.Enabled() {
		t.Error("设置了 ADMIN_TOKEN 后管理接口应可用")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}

// 加入"代码公开计划"必须显式开启（不可逆动作，不做默认副作用）
func TestLoadJoinOpenSource(t *testing.T) {
	clearEnv(t)
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account.JoinOpenSource {
		t.Error("默认应为关闭")
	}

	t.Setenv("ACCOUNT_JOIN_OPEN_SOURCE", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Account.JoinOpenSource {
		t.Error("ACCOUNT_JOIN_OPEN_SOURCE=true 应开启")
	}

	t.Setenv("ACCOUNT_JOIN_OPEN_SOURCE", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account.JoinOpenSource {
		t.Error("ACCOUNT_JOIN_OPEN_SOURCE=false 应关闭")
	}
}

func TestLoadRequiresDatabase(t *testing.T) {
	clearEnv(t)
	t.Setenv("ACCOUNT_SECRET_KEY", testSecretKey)
	t.Setenv("LUOGU_OCR_URL", "http://127.0.0.1:9898/ocr")

	_, err := Load()
	if err == nil {
		t.Fatal("未配置 DB_HOST 时应报错（号池依赖 MySQL）")
	}
	if !strings.Contains(err.Error(), "DB_HOST") {
		t.Errorf("错误信息应提到 DB_HOST: %v", err)
	}
}

func TestLoadMissingDBFields(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("ACCOUNT_SECRET_KEY", testSecretKey)
	t.Setenv("LUOGU_OCR_URL", "http://127.0.0.1:9898/ocr")

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
	setRequired(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "10")

	if _, err := Load(); err == nil {
		t.Error("空闲连接数大于最大连接数时应报错")
	}
}

func TestLoadRequiresSecretKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_USER", "root")
	t.Setenv("DB_NAME", "luogu2api")
	t.Setenv("LUOGU_OCR_URL", "http://127.0.0.1:9898/ocr")

	_, err := Load()
	if err == nil {
		t.Fatal("缺少 ACCOUNT_SECRET_KEY 时应报错")
	}
	if !strings.Contains(err.Error(), "ACCOUNT_SECRET_KEY") {
		t.Errorf("错误信息应提到 ACCOUNT_SECRET_KEY: %v", err)
	}
}

func TestLoadRejectsBrokenSecretKey(t *testing.T) {
	clearEnv(t)
	setRequired(t)
	t.Setenv("ACCOUNT_SECRET_KEY", "not-a-valid-key")

	if _, err := Load(); err == nil {
		t.Error("密钥长度/编码非法时应报错")
	}
}

func TestLoadRequiresOCRURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_USER", "root")
	t.Setenv("DB_NAME", "luogu2api")
	t.Setenv("ACCOUNT_SECRET_KEY", testSecretKey)

	_, err := Load()
	if err == nil {
		t.Fatal("缺少 LUOGU_OCR_URL 时应报错")
	}
	if !strings.Contains(err.Error(), "LUOGU_OCR_URL") {
		t.Errorf("错误信息应提到 LUOGU_OCR_URL: %v", err)
	}
}

func TestLoadRejectsBadOCRURL(t *testing.T) {
	clearEnv(t)
	setRequired(t)
	t.Setenv("LUOGU_OCR_URL", "127.0.0.1:9898")

	if _, err := Load(); err == nil {
		t.Error("OCR 地址缺少协议头时应报错")
	}
}

func TestLoadOCRMode(t *testing.T) {
	clearEnv(t)
	setRequired(t)
	t.Setenv("LUOGU_OCR_MODE", "RAW") // 大小写不敏感

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OCR.Mode != OCRModeRaw {
		t.Errorf("OCR.Mode = %q, want %q", cfg.OCR.Mode, OCRModeRaw)
	}

	t.Setenv("LUOGU_OCR_MODE", "multipart")
	if _, err := Load(); err == nil {
		t.Error("未知入参形态应报错")
	}
}

func TestAccountValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T)
		want   string
	}{
		{"验证间隔小于扫描间隔", func(t *testing.T) {
			t.Setenv("ACCOUNT_VERIFY_INTERVAL", "1m")
			t.Setenv("ACCOUNT_SWEEP_INTERVAL", "5m")
		}, "ACCOUNT_VERIFY_INTERVAL"},
		{"抖动越界", func(t *testing.T) {
			t.Setenv("ACCOUNT_VERIFY_JITTER", "1.5")
		}, "ACCOUNT_VERIFY_JITTER"},
		{"并发数为 0", func(t *testing.T) {
			t.Setenv("ACCOUNT_VERIFY_CONCURRENCY", "0")
		}, "ACCOUNT_VERIFY_CONCURRENCY"},
		{"尝试次数为 0", func(t *testing.T) {
			t.Setenv("ACCOUNT_LOGIN_MAX_ATTEMPTS", "0")
		}, "ACCOUNT_LOGIN_MAX_ATTEMPTS"},
		{"换号次数为 0", func(t *testing.T) {
			t.Setenv("ACCOUNT_REQUEST_MAX_TRY", "0")
		}, "ACCOUNT_REQUEST_MAX_TRY"},
		{"批量上限为 0", func(t *testing.T) {
			t.Setenv("ACCOUNT_SWEEP_BATCH_LIMIT", "0")
		}, "ACCOUNT_SWEEP_BATCH_LIMIT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			setRequired(t)
			tt.mutate(t)

			_, err := Load()
			if err == nil {
				t.Fatal("应报错")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("错误信息应包含 %s: %v", tt.want, err)
			}
		})
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

// Package config 负责从环境变量加载服务配置。
//
// 所有配置均可用环境变量覆盖，便于容器化部署；本地开发不设置 DB_HOST 即以
// 无数据库模式启动（health 中 db 会显示 disabled）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// 默认值
const (
	defaultAppName         = "luogu2api"
	defaultAppEnv          = "dev"
	defaultHTTPAddr        = ":8080"
	defaultShutdownTimeout = 10 * time.Second
	defaultDBPort          = "3306"
	defaultDBParams        = "charset=utf8mb4&parseTime=True&loc=Local"
	defaultDBMaxOpenConns  = 50
	defaultDBMaxIdleConns  = 10
	defaultDBConnLifetime  = time.Hour
	defaultDBLogLevel      = "warn"
	defaultLuoguCookieFile = "cookies.json"
	defaultLuoguTimeout    = 30 * time.Second
	defaultLogLevel        = "info"
)

// Config 服务总配置
type Config struct {
	App      App
	HTTP     HTTP
	DB       DB
	Luogu    Luogu
	LogLevel string
}

// App 应用信息
type App struct {
	Name string
	Env  string // dev / test / prod
}

// IsProd 是否为生产环境
func (a App) IsProd() bool { return a.Env == "prod" }

// HTTP HTTP 服务配置
type HTTP struct {
	Addr            string
	ShutdownTimeout time.Duration
}

// DB MySQL 配置
type DB struct {
	Host            string
	Port            string
	User            string
	Password        string
	Name            string
	Params          string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
	LogLevel        string // silent / error / warn / info
}

// Enabled 是否启用 MySQL。
//
// 未设置 DB_HOST 视为不启用，这样本地没有数据库也能把服务跑起来。
func (d DB) Enabled() bool { return d.Host != "" }

// DSN 拼接 MySQL 连接串
func (d DB) DSN() string {
	params := d.Params
	if params == "" {
		params = defaultDBParams
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", d.User, d.Password, d.Host, d.Port, d.Name, params)
}

// Luogu 洛谷客户端配置
type Luogu struct {
	CookieFile string
	Timeout    time.Duration
}

// Load 从环境变量读取配置并校验
func Load() (Config, error) {
	cfg := Config{
		App: App{
			Name: env("APP_NAME", defaultAppName),
			Env:  env("APP_ENV", defaultAppEnv),
		},
		HTTP: HTTP{
			Addr:            env("HTTP_ADDR", defaultHTTPAddr),
			ShutdownTimeout: envDuration("HTTP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		},
		DB: DB{
			Host:            strings.TrimSpace(os.Getenv("DB_HOST")),
			Port:            env("DB_PORT", defaultDBPort),
			User:            os.Getenv("DB_USER"),
			Password:        os.Getenv("DB_PASSWORD"),
			Name:            os.Getenv("DB_NAME"),
			Params:          env("DB_PARAMS", defaultDBParams),
			MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns),
			MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns),
			ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", defaultDBConnLifetime),
			AutoMigrate:     envBool("DB_AUTO_MIGRATE", false),
			LogLevel:        env("DB_LOG_LEVEL", defaultDBLogLevel),
		},
		Luogu: Luogu{
			CookieFile: env("LUOGU_COOKIE_FILE", defaultLuoguCookieFile),
			Timeout:    envDuration("LUOGU_TIMEOUT", defaultLuoguTimeout),
		},
		LogLevel: env("LOG_LEVEL", defaultLogLevel),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.HTTP.Addr == "" {
		return fmt.Errorf("config: HTTP_ADDR 不能为空")
	}
	if c.DB.Enabled() {
		var missing []string
		if c.DB.User == "" {
			missing = append(missing, "DB_USER")
		}
		if c.DB.Name == "" {
			missing = append(missing, "DB_NAME")
		}
		if len(missing) > 0 {
			return fmt.Errorf("config: 已设置 DB_HOST，但缺少 %s", strings.Join(missing, "、"))
		}
	}
	if c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		return fmt.Errorf("config: DB_MAX_IDLE_CONNS(%d) 不应大于 DB_MAX_OPEN_CONNS(%d)",
			c.DB.MaxIdleConns, c.DB.MaxOpenConns)
	}
	return nil
}

// --- 环境变量读取辅助 ---

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

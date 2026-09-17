// Package config 负责从环境变量加载服务配置。
//
// 所有配置均可用环境变量覆盖，便于容器化部署。号池依赖 MySQL，
// 因此 DB_HOST / ACCOUNT_SECRET_KEY / LUOGU_OCR_URL 都是必填项，
// 缺失时启动即报错（fail fast），避免"服务起来了但业务全 503"。
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/laoin114514/luogu2api/internal/secret"
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
	// 启动时自动补齐库结构（建表/加列/建索引）默认关闭：改结构是有后果的动作，
	// 该由部署步骤显式决定；默认只校验，发现落后就让启动失败。
	defaultDBMigrateOnStart = false
	// 发现需要人工确认的差异（删列/改类型）时是否拒绝启动。默认 true：
	// 库结构与模型不一致时不该带病运行。
	defaultDBSchemaStrict = true
	defaultLuoguTimeout   = 30 * time.Second
	defaultLuoguRetry     = 1
	defaultLogLevel       = "info"

	// 号池
	defaultAccountSweepInterval   = 5 * time.Minute
	defaultAccountVerifyInterval  = 30 * time.Minute
	defaultAccountVerifyJitter    = 0.2
	defaultAccountVerifyConcurr   = 1
	defaultAccountLoginAttempts   = 5
	defaultAccountLoginBackoff    = time.Minute
	defaultAccountFailedRetry     = time.Hour
	defaultAccountRequestMaxTry   = 3
	defaultAccountSweepBatchLimit = 200
	// 加入"代码公开计划"是不可逆动作（洛谷限制 30 天内不能退出），
	// 因此默认关闭，必须显式开启。开关只决定"要不要写"：远端是否已加入
	// 始终会被只读同步（否则导入早已加入的账号会永远被记成未加入）。
	defaultAccountJoinOpenSource = false

	// 验证码识别服务
	defaultOCRTimeout = 5 * time.Second
	defaultOCRMode    = OCRModeBase64
)

// OCR 入参形态
const (
	// OCRModeBase64 以 JSON {"image_base64":"..."} 提交图片（默认，配合现用服务）
	OCRModeBase64 = "base64"
	// OCRModeRaw 直接提交原始 JPEG 字节（Content-Type: image/jpeg）
	OCRModeRaw = "raw"
)

// Config 服务总配置
type Config struct {
	App      App
	HTTP     HTTP
	DB       DB
	Luogu    Luogu
	Account  Account
	OCR      OCR
	Admin    Admin
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
	// MigrateOnStart 启动时自动执行安全的库结构变更（建表/加列/建索引）。
	// 关闭时只做校验：库结构落后于代码会让启动直接失败（见 internal/schema）。
	MigrateOnStart bool
	// SchemaStrict 发现『需要人工确认』的结构差异时拒绝启动（删列、改类型、
	// 索引变化等自动执行有丢数据风险的操作）。
	SchemaStrict bool
	LogLevel     string // silent / error / warn / info
}

// Enabled 是否启用 MySQL
func (d DB) Enabled() bool { return d.Host != "" }

// DSN 拼接 MySQL 连接串
func (d DB) DSN() string {
	params := d.Params
	if params == "" {
		params = defaultDBParams
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", d.User, d.Password, d.Host, d.Port, d.Name, params)
}

// ServerDSN 拼接"不选库"的连接串（user:pwd@tcp(host:port)/?params）。
//
// 用于库本身还不存在时的操作——建库就属于这种：DSN 里的库名是连接要选的默认库，
// 库不存在时 MySQL 在握手阶段就以 1049 拒绝，连一条 SELECT 都发不出去。
func (d DB) ServerDSN() string {
	server := d
	server.Name = ""
	return server.DSN()
}

// dbNamePattern 库名允许的字符。
//
// 库名最终会被拼进 CREATE DATABASE 的 DDL，所以在配置这一层就挡住引号/分号/空白
// 之类的字符，而不是指望每个拼接点自己记得转义。
var dbNamePattern = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)

// ValidateDBName 校验库名可用：非空且只含字母、数字、下划线、$ 与 -
func ValidateDBName(name string) error {
	if name == "" {
		return fmt.Errorf("config: DB_NAME 不能为空")
	}
	if !dbNamePattern.MatchString(name) {
		return fmt.Errorf("config: DB_NAME %q 含有非法字符（只允许字母、数字、下划线、$ 与 -）", name)
	}
	return nil
}

// Luogu 洛谷客户端配置
type Luogu struct {
	Timeout time.Duration
	Retry   int // SDK 内部重试次数（探活/重登固定为 0，由号池自己控制尝试次数）
}

// Account 号池配置
type Account struct {
	SecretKey         string        // AES-GCM 密钥（hex 或 base64）
	SweepInterval     time.Duration // 扫描器 tick 周期
	VerifyInterval    time.Duration // 单账号多久验证一次登录态
	VerifyJitter      float64       // 验证间隔抖动比例 [0,1)
	VerifyConcurrency int           // 一轮扫描内的并发账号数
	LoginMaxAttempts  int           // 单次重登任务的尝试次数上限
	LoginBackoff      time.Duration // 重登失败的退避基数（指数增长）
	FailedRetry       time.Duration // relogin_failed 的慢速重试间隔上限
	RequestMaxTry     int           // 请求路径最多换几个账号
	SweepBatchLimit   int           // 单轮扫描最多处理多少个账号

	// JoinOpenSource 是否让池内账号加入洛谷"代码公开计划"（openSource=1）。
	//
	// 开启后由号池在"登录成功后 / 每轮验证成功后"幂等地补做，成功一次即永久跳过；
	// 失败只记日志、下轮重试，不影响账号可用性。默认关闭：加入后洛谷限制 30 天
	// 内不能退出，属于不可逆的隐私设置变更，不该是导入账号的默认副作用。
	//
	// 这个开关只决定"要不要写"：账号在洛谷的真实加入状态（偏好设置里的 openSource）
	// 每次验证都会只读同步进 accounts.open_source_joined，与开关无关。
	JoinOpenSource bool
}

// OCR 验证码识别服务配置（SDK 不内置 OCR，自动重登依赖它）
type OCR struct {
	URL     string
	Token   string
	Mode    string // base64 / raw
	Timeout time.Duration
}

// Admin HTTP 管理接口配置
type Admin struct {
	Token string // 为空时不注册管理路由（fail closed）
}

// Enabled 管理接口是否可用
func (a Admin) Enabled() bool { return a.Token != "" }

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
			MigrateOnStart:  envBool("DB_MIGRATE_ON_START", defaultDBMigrateOnStart),
			SchemaStrict:    envBool("DB_SCHEMA_STRICT", defaultDBSchemaStrict),
			LogLevel:        env("DB_LOG_LEVEL", defaultDBLogLevel),
		},
		Luogu: Luogu{
			Timeout: envDuration("LUOGU_TIMEOUT", defaultLuoguTimeout),
			Retry:   envInt("LUOGU_RETRY", defaultLuoguRetry),
		},
		Account: Account{
			SecretKey:         strings.TrimSpace(os.Getenv("ACCOUNT_SECRET_KEY")),
			SweepInterval:     envDuration("ACCOUNT_SWEEP_INTERVAL", defaultAccountSweepInterval),
			VerifyInterval:    envDuration("ACCOUNT_VERIFY_INTERVAL", defaultAccountVerifyInterval),
			VerifyJitter:      envFloat("ACCOUNT_VERIFY_JITTER", defaultAccountVerifyJitter),
			VerifyConcurrency: envInt("ACCOUNT_VERIFY_CONCURRENCY", defaultAccountVerifyConcurr),
			LoginMaxAttempts:  envInt("ACCOUNT_LOGIN_MAX_ATTEMPTS", defaultAccountLoginAttempts),
			LoginBackoff:      envDuration("ACCOUNT_LOGIN_BACKOFF", defaultAccountLoginBackoff),
			FailedRetry:       envDuration("ACCOUNT_FAILED_RETRY", defaultAccountFailedRetry),
			RequestMaxTry:     envInt("ACCOUNT_REQUEST_MAX_TRY", defaultAccountRequestMaxTry),
			SweepBatchLimit:   envInt("ACCOUNT_SWEEP_BATCH_LIMIT", defaultAccountSweepBatchLimit),
			JoinOpenSource:    envBool("ACCOUNT_JOIN_OPEN_SOURCE", defaultAccountJoinOpenSource),
		},
		OCR: OCR{
			URL:     strings.TrimSpace(os.Getenv("LUOGU_OCR_URL")),
			Token:   strings.TrimSpace(os.Getenv("LUOGU_OCR_TOKEN")),
			Mode:    strings.ToLower(env("LUOGU_OCR_MODE", defaultOCRMode)),
			Timeout: envDuration("LUOGU_OCR_TIMEOUT", defaultOCRTimeout),
		},
		Admin: Admin{
			Token: strings.TrimSpace(os.Getenv("ADMIN_TOKEN")),
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

	// 号池依赖 MySQL：不再支持"无数据库模式"
	if !c.DB.Enabled() {
		return fmt.Errorf("config: 必须配置 DB_HOST（号池依赖 MySQL）")
	}
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
	if err := ValidateDBName(c.DB.Name); err != nil {
		return err
	}
	if c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		return fmt.Errorf("config: DB_MAX_IDLE_CONNS(%d) 不应大于 DB_MAX_OPEN_CONNS(%d)",
			c.DB.MaxIdleConns, c.DB.MaxOpenConns)
	}

	if c.Account.SecretKey == "" {
		return fmt.Errorf("config: ACCOUNT_SECRET_KEY 必填（用 openssl rand -base64 32 生成）")
	}
	if _, err := secret.NewCipher(c.Account.SecretKey); err != nil {
		return fmt.Errorf("config: ACCOUNT_SECRET_KEY 不可用: %w", err)
	}

	if err := c.Account.validate(); err != nil {
		return err
	}

	if c.OCR.URL == "" {
		return fmt.Errorf("config: LUOGU_OCR_URL 必填（SDK 不内置 OCR，自动重登依赖验证码识别服务）")
	}
	if !strings.HasPrefix(c.OCR.URL, "http://") && !strings.HasPrefix(c.OCR.URL, "https://") {
		return fmt.Errorf("config: LUOGU_OCR_URL 必须以 http:// 或 https:// 开头")
	}
	if c.OCR.Timeout <= 0 {
		return fmt.Errorf("config: LUOGU_OCR_TIMEOUT 必须大于 0")
	}
	if c.OCR.Mode != OCRModeBase64 && c.OCR.Mode != OCRModeRaw {
		return fmt.Errorf("config: LUOGU_OCR_MODE 只能是 %s 或 %s，当前 %q",
			OCRModeBase64, OCRModeRaw, c.OCR.Mode)
	}

	if c.Luogu.Timeout <= 0 {
		return fmt.Errorf("config: LUOGU_TIMEOUT 必须大于 0")
	}
	if c.Luogu.Retry < 0 {
		return fmt.Errorf("config: LUOGU_RETRY 不能为负数")
	}

	return nil
}

func (a Account) validate() error {
	if a.SweepInterval <= 0 {
		return fmt.Errorf("config: ACCOUNT_SWEEP_INTERVAL 必须大于 0")
	}
	if a.VerifyInterval <= 0 {
		return fmt.Errorf("config: ACCOUNT_VERIFY_INTERVAL 必须大于 0")
	}
	if a.VerifyInterval < a.SweepInterval {
		return fmt.Errorf("config: ACCOUNT_VERIFY_INTERVAL(%s) 不应小于 ACCOUNT_SWEEP_INTERVAL(%s)",
			a.VerifyInterval, a.SweepInterval)
	}
	if a.VerifyJitter < 0 || a.VerifyJitter >= 1 {
		return fmt.Errorf("config: ACCOUNT_VERIFY_JITTER 必须在 [0,1) 之间，当前 %v", a.VerifyJitter)
	}
	if a.VerifyConcurrency < 1 {
		return fmt.Errorf("config: ACCOUNT_VERIFY_CONCURRENCY 至少为 1")
	}
	if a.LoginMaxAttempts < 1 {
		return fmt.Errorf("config: ACCOUNT_LOGIN_MAX_ATTEMPTS 至少为 1")
	}
	if a.LoginBackoff <= 0 {
		return fmt.Errorf("config: ACCOUNT_LOGIN_BACKOFF 必须大于 0")
	}
	if a.FailedRetry <= 0 {
		return fmt.Errorf("config: ACCOUNT_FAILED_RETRY 必须大于 0")
	}
	if a.RequestMaxTry < 1 {
		return fmt.Errorf("config: ACCOUNT_REQUEST_MAX_TRY 至少为 1")
	}
	if a.SweepBatchLimit < 1 {
		return fmt.Errorf("config: ACCOUNT_SWEEP_BATCH_LIMIT 至少为 1")
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

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
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

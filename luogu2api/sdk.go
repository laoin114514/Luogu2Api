package luogu2api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HeaderAdminToken 是访问令牌的请求头名称，与服务的 middleware.HeaderAdminToken 一致。
//
// /api/v1 下的全部接口（业务 + 管理）共用它；只有 /healthz、/livez 不需要。
const HeaderAdminToken = "X-Admin-Token"

// 默认值
const (
	// defaultTimeout 单次请求超时（含建连、写、读）。
	// 服务端自己的超时是读 30s / 写 60s，客户端留一份等长的预算避免无限等待。
	defaultTimeout = 30 * time.Second
	// defaultUserAgent 便于服务端访问日志区分"SDK 调用"与浏览器/curl。
	defaultUserAgent = "luogu2api-sdk/1.0"
	// defaultMaxResponseBytes 单次响应允许读取的最大字节数。
	// 题面可能很大，但总要有上限；超出时明确报错，而不是悄悄截断让 JSON 解析报出
	// 莫名其妙的语法错误。
	defaultMaxResponseBytes = 16 << 20
)

// Client 是 Luogu2Api 的 HTTP 客户端（SDK）。
//
// 零值不可用，请用 NewSDK 构造。构造完成后 Client 是可并发使用的：内部只有不可变的
// 配置与 *http.Client，没有需要加锁的状态；子服务同理。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	ua      string
	maxBody int64

	// 业务接口按资源分组，由 NewSDK 一次性装配；调用方只读，不要替换这些字段。
	Problem *ProblemService
	Record  *RecordService
	Pool    *PoolService
	Health  *HealthService
}

// options 汇总 NewSDK 的可选配置：先收集再装配，避免 Option 之间的顺序依赖。
type options struct {
	httpClient *http.Client
	timeout    time.Duration
	// timeoutSet 区分"没传 WithTimeout"（沿用调用方 client 自己的超时）与
	// "显式传了 WithTimeout"（即使传的恰好等于默认值，也要覆盖）。
	timeoutSet bool
	userAgent  string
	maxBody    int64
}

// Option 是 NewSDK 的可选参数
type Option func(*options)

// WithHTTPClient 使用调用方提供的 http.Client：代理、连接池、埋点等全部由它决定。
//
// SDK 默认不修改它；若同时传了 WithTimeout，才会给它的副本设置 Timeout（原件不动）。
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) { o.httpClient = hc }
}

// WithTimeout 设置单次请求超时（默认 30s），会覆盖 http.Client 自带的 Timeout。
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
		o.timeoutSet = true
	}
}

// WithUserAgent 自定义 User-Agent；传空字符串表示退回默认值。
func WithUserAgent(ua string) Option {
	return func(o *options) { o.userAgent = strings.TrimSpace(ua) }
}

// WithMaxResponseBytes 设置单次响应允许读取的最大字节数（默认 16 MiB）。
func WithMaxResponseBytes(n int64) Option {
	return func(o *options) { o.maxBody = n }
}

// NewSDK 创建 SDK 实例。
//
//	baseURL    服务地址，例如 http://127.0.0.1:8080（允许带尾部斜杠）；
//	           必须带 http:// 或 https:// 协议头，否则本地解析出来的请求地址会是错的。
//	adminToken 访问令牌（服务端的 ADMIN_TOKEN）。/api/v1 下的接口全部需要它，
//	           因此为空时直接报错（fail fast），而不是等到第一次业务调用才发现全是 401。
//
// 令牌是构造期注入的：之后每个请求都会带 X-Admin-Token 请求头。
func NewSDK(baseURL, adminToken string, opts ...Option) (*Client, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return nil, fmt.Errorf("luogu2api: baseURL 不能为空")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("luogu2api: baseURL 不是合法 URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("luogu2api: baseURL 必须以 http:// 或 https:// 开头，当前 %q", baseURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("luogu2api: baseURL 缺少主机名，当前 %q", baseURL)
	}

	token := strings.TrimSpace(adminToken)
	if token == "" {
		return nil, fmt.Errorf("luogu2api: adminToken 不能为空（/api/v1 下的接口全部需要 %s）", HeaderAdminToken)
	}

	cfg := options{
		timeout:   defaultTimeout,
		userAgent: defaultUserAgent,
		maxBody:   defaultMaxResponseBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.timeout <= 0 {
		return nil, fmt.Errorf("luogu2api: 超时必须大于 0，当前 %v", cfg.timeout)
	}
	if cfg.maxBody <= 0 {
		return nil, fmt.Errorf("luogu2api: 响应体上限必须大于 0，当前 %d", cfg.maxBody)
	}
	if cfg.userAgent == "" {
		cfg.userAgent = defaultUserAgent
	}

	hc := cfg.httpClient
	switch {
	case hc == nil:
		hc = &http.Client{Timeout: cfg.timeout}
	case cfg.timeoutSet:
		// 复制一份再改超时：调用方的 client 可能还在别处用着，不能就地改它的 Timeout。
		clone := *hc
		clone.Timeout = cfg.timeout
		hc = &clone
	}

	c := &Client{
		baseURL: strings.TrimRight(parsed.String(), "/"),
		token:   token,
		http:    hc,
		ua:      cfg.userAgent,
		maxBody: cfg.maxBody,
	}
	c.Problem = &ProblemService{client: c}
	c.Record = &RecordService{client: c}
	c.Pool = &PoolService{client: c}
	c.Health = &HealthService{client: c}
	return c, nil
}

// rawResponse 是统一响应体 {code,message,data} 的解析结果
type rawResponse struct {
	status  int
	code    int
	message string
	data    json.RawMessage
}

// apiError 把响应转成 *Error：HTTP 状态码与业务码都保留，调用方按需判断
func (r *rawResponse) apiError() *Error {
	msg := r.message
	if msg == "" {
		msg = http.StatusText(r.status)
	}
	return &Error{HTTPStatus: r.status, Code: r.code, Message: msg}
}

// get 发起 GET 请求并解析统一响应体。
//
// 这里只负责"传输 + 信封"：HTTP 状态码与业务码是否算失败由调用方决定——/healthz 在号池
// 不可用时**按设计**以 503 返回一份有意义的报告，不能一律当成错误。
func (c *Client) get(ctx context.Context, path string, query url.Values) (*rawResponse, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("luogu2api: 构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(HeaderAdminToken, c.token)
	req.Header.Set("User-Agent", c.ua)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("luogu2api: GET %s 失败: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := readBody(resp.Body, c.maxBody)
	if err != nil {
		return nil, fmt.Errorf("luogu2api: GET %s 读取响应失败: %w", path, err)
	}

	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("luogu2api: GET %s 的响应不是合法 JSON（http=%d）: %w", path, resp.StatusCode, err)
	}
	return &rawResponse{status: resp.StatusCode, code: env.Code, message: env.Message, data: env.Data}, nil
}

// getData 发起 GET 并按业务接口的规则判失败：业务码非 0，或 HTTP 状态码 >= 400。
func (c *Client) getData(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.get(ctx, path, query)
	if err != nil {
		return err
	}
	if raw.code != CodeOK || raw.status >= http.StatusBadRequest {
		return raw.apiError()
	}
	return decodeData(path, raw, out)
}

// getProbe 探活接口专用：503 是预期结果（degraded），只有业务码非 0 与传输失败才算错误。
func (c *Client) getProbe(ctx context.Context, path string, out any) error {
	raw, err := c.get(ctx, path, nil)
	if err != nil {
		return err
	}
	if raw.code != CodeOK {
		return raw.apiError()
	}
	return decodeData(path, raw, out)
}

// decodeData 把响应的 data 解码到 out；data 缺省（错误响应）时什么都不做。
func decodeData(path string, raw *rawResponse, out any) error {
	if out == nil || len(raw.data) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw.data, out); err != nil {
		return fmt.Errorf("luogu2api: GET %s 的 data 解析失败: %w", path, err)
	}
	return nil
}

// readBody 读取响应体，超过 limit 字节就报错（不是截断）
func readBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("响应体超过 %d 字节上限", limit)
	}
	return body, nil
}

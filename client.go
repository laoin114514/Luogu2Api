package luoguclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36 Edg/148.0.0.0"
const luoguBaseURL = "https://www.luogu.com.cn/"

// Client 洛谷 Client 客户端
//
// Client 可以并发使用：csrfToken 由 mu 保护，cookie jar 自身并发安全。
// 配置项（WithXxx）只在 NewClient 构造期间生效，构造完成后不应再改动；
// 运行期需要变更的登录态请使用 SetCSRF / ImportCookies / ClearCookies。
type Client struct {
	httpClient *http.Client
	cookieJar  *ExportableCookieJar
	csrfToken  string
	maxRetries int
	backoffFn  func(int) time.Duration
	userAgent  string
	ctx        context.Context
	optionErr  error // Option 执行期间的错误，由 NewClient 返回

	mu      sync.RWMutex // 保护 csrfToken
	baseURL string       // 请求根地址，默认 luoguBaseURL（测试时指向 httptest 服务器）

	Auth     *AuthService
	Problem  *ProblemService
	Record   *RecordService
	Training *TrainingService
	User     *UserService
	Discuss  *DiscussService
	Contest  *ContestService
}

// ClientOption 客户端配置函数
type ClientOption func(*Client)

// WithRetry 设置重试参数
func WithRetry(maxRetries int, backoff func(int) time.Duration) ClientOption {
	return func(c *Client) {
		c.maxRetries = maxRetries
		if backoff != nil {
			c.backoffFn = backoff
		}
	}
}

// WithTimeout 设置 HTTP 超时
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithUserAgent 设置自定义 User-Agent
func WithUserAgent(ua string) ClientOption {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithContext 设置请求的默认 context（用于超时控制/取消）
func WithContext(ctx context.Context) ClientOption {
	return func(c *Client) {
		c.ctx = ctx
	}
}

// WithCookies 创建客户端时注入 cookie（JSON 格式，与 ExportCookies 的输出一致）。
// 空数据视为不注入，便于直接传入 os.ReadFile 的结果；JSON 非法时 NewClient 返回错误。
//
//	data, _ := client.ExportCookies()
//	client, _ := luogu.NewClient(luogu.WithCookies(data))
func WithCookies(data []byte) ClientOption {
	return func(c *Client) {
		if c.optionErr != nil {
			return
		}
		if err := c.cookieJar.Import(data); err != nil {
			c.optionErr = fmt.Errorf("import cookies: %w", err)
		}
	}
}

// NewClient 创建新的洛谷客户端
//
// cookie 仅保存在内存中，客户端不会读写任何文件；如需跨进程复用登录态，
// 请调用方自行保存 ExportCookies 的结果，并在下次创建客户端后 ImportCookies。
func NewClient(opts ...ClientOption) (*Client, error) {
	jar, err := newExportableCookieJar()
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	c := &Client{
		cookieJar:  jar,
		maxRetries: 3,
		backoffFn:  defaultBackoff,
		userAgent:  defaultUA,
		ctx:        context.Background(),
		baseURL:    luoguBaseURL,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}
	if c.optionErr != nil {
		return nil, c.optionErr
	}

	c.Auth = &AuthService{client: c}
	c.Problem = &ProblemService{client: c}
	c.Record = &RecordService{client: c}
	c.Training = &TrainingService{client: c}
	c.User = &UserService{client: c}
	c.Discuss = &DiscussService{client: c}
	c.Contest = &ContestService{client: c}

	return c, nil
}

// base 返回请求根地址，默认 luoguBaseURL
func (c *Client) base() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return luoguBaseURL
}

// newRequest 创建带默认请求头的 HTTP 请求
func (c *Client) newRequest(method, path string, body interface{}) (*http.Request, error) {
	urlStr := c.base() + strings.TrimPrefix(path, "/")

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(c.ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Referer", luoguBaseURL)

	if token := c.csrf(); token != "" && method != "GET" {
		req.Header.Set("X-CSRF-TOKEN", token)
	}

	return req, nil
}

// do 执行 HTTP 请求，带重试逻辑
func (c *Client) do(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// 重试时重置请求体（POST 等带 body 的请求，body reader 已被上一轮消费）
		if attempt > 0 && req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, &NetworkError{Err: fmt.Errorf("reset request body for retry: %w", err)}
			}
			req.Body = body
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if shouldRetry(err) && attempt < c.maxRetries {
				lastErr = err
				time.Sleep(c.backoffFn(attempt))
				continue
			}
			return nil, &NetworkError{Err: err}
		}

		if shouldRetryStatus(resp.StatusCode) && attempt < c.maxRetries {
			lastErr = fmt.Errorf("server error: HTTP %d", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(c.backoffFn(attempt))
			continue
		}

		return resp, nil
	}
	return nil, &NetworkError{Err: fmt.Errorf("max retries exceeded: %w", lastErr)}
}

// get 发送 GET 请求
func (c *Client) get(path string) (*http.Response, error) {
	req, err := c.newRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// post 发送 POST 请求
func (c *Client) post(path string, body interface{}) (*http.Response, error) {
	req, err := c.newRequest("POST", path, body)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// parseBody 解析响应体 JSON（调用方负责关闭 resp.Body）
func parseBody(resp *http.Response, v interface{}) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		preview := string(data)
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		return fmt.Errorf("unmarshal response (status=%d, body=%s): %w", resp.StatusCode, preview, err)
	}
	return nil
}

// checkResponse 根据响应状态码返回统一错误：
// 401/403 → *UnauthorizedError（未登录或权限不足），其余非 200 → 普通错误。
// format/args 描述当前操作，例如 checkResponse(resp, "get problem %s", pid)。
func checkResponse(resp *http.Response, format string, args ...interface{}) error {
	op := fmt.Sprintf(format, args...)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &UnauthorizedError{StatusCode: resp.StatusCode, Message: op}
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: status %d", op, resp.StatusCode)
	}
	return nil
}

// parseLentilleContext 从 HTML 页面中提取 <script id="lentille-context"> 内的 JSON 数据（调用方负责关闭 resp.Body）
func parseLentilleContext(resp *http.Response, v interface{}) error {
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return fmt.Errorf("parse HTML: %w", err)
	}

	jsonStr := doc.Find("script#lentille-context").Text()
	if jsonStr == "" {
		return fmt.Errorf("lentille-context script not found in page")
	}

	if err := json.Unmarshal([]byte(jsonStr), v); err != nil {
		return fmt.Errorf("unmarshal lentille-context: %w", err)
	}
	return nil
}

// getSimple 发送 GET 请求，使用非浏览器 UA 以获取服务端渲染的 HTML
func (c *Client) getSimple(path string) (*http.Response, error) {
	urlStr := c.base() + strings.TrimPrefix(path, "/")
	req, err := http.NewRequestWithContext(c.ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Go-http-client/2.0")
	return c.do(req)
}

// refreshCSRF 从首页获取 CSRF token
func (c *Client) refreshCSRF() error {
	resp, err := c.getSimple("/")
	if err != nil {
		return &CSRFError{Err: err}
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return &CSRFError{Err: fmt.Errorf("parse HTML: %w", err)}
	}

	token, exists := doc.Find("meta[name=csrf-token]").Attr("content")
	if !exists {
		return &CSRFError{Err: fmt.Errorf("csrf token not found in page")}
	}

	c.setCSRF(token)
	return nil
}

// setCSRF 保存 CSRF token（并发安全）
func (c *Client) setCSRF(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.csrfToken = token
}

// csrf 返回当前 CSRF token（并发安全）
func (c *Client) csrf() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.csrfToken
}

// SetCSRF 手动设置 CSRF token（用于已知 token 时跳过 RefreshCSRF）
func (c *Client) SetCSRF(token string) {
	c.setCSRF(token)
}

// verifyAuth 校验当前 cookie 是否仍有效
//
// 洛谷当前对未登录访问 /user/setting 直接返回 401；旧行为是 302 重定向到登录页，
// 两种都要兼容，因此先看状态码，再兜底检查重定向目标。
func (c *Client) verifyAuth() error {
	resp, err := c.get("/user/setting")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &UnauthorizedError{StatusCode: resp.StatusCode, Message: "verify auth"}
	}

	// 兼容旧行为：未认证 → 302 重定向到登录页
	if resp.Request != nil && strings.Contains(resp.Request.URL.Path, "/login") {
		return &UnauthorizedError{StatusCode: http.StatusFound, Message: "verify auth"}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("verify auth: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ExportCookies 导出当前会话的全部 cookie（JSON），供调用方自行持久化
func (c *Client) ExportCookies() ([]byte, error) {
	return c.cookieJar.Export()
}

// ImportCookies 从 JSON 导入 cookie 到当前会话（用于恢复调用方保存的登录态）
func (c *Client) ImportCookies(data []byte) error {
	return c.cookieJar.Import(data)
}

// ClearCookies 清空内存中的 cookie（例如登出后）
func (c *Client) ClearCookies() error {
	return c.cookieJar.Clear()
}

// UID 返回当前会话的用户 UID（读取 _uid cookie），未登录时返回 0
//
// 洛谷登录响应体里不含 uid，会话身份只体现在 cookie 中，因此这里是获取自身 UID 的正确方式。
func (c *Client) UID() int {
	u, err := url.Parse(c.base())
	if err != nil {
		return 0
	}
	for _, ck := range c.cookieJar.Cookies(u) {
		if ck.Name == "_uid" {
			uid, err := strconv.Atoi(ck.Value)
			if err != nil {
				return 0
			}
			return uid
		}
	}
	return 0
}

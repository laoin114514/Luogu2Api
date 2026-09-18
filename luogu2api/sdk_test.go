package luogu2api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testToken 是测试注入的令牌；假服务端用 requireToken 断言请求确实带上了它。
const testToken = "test-admin-token"

// newTestSDK 起一个假服务端，返回指向它的 SDK；handler 收到的请求由测试自己断言。
func newTestSDK(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	sdk, err := NewSDK(srv.URL, testToken, opts...)
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	return sdk
}

// requireToken 断言请求带上了 NewSDK 注入的令牌
func requireToken(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get(HeaderAdminToken); got != testToken {
		t.Errorf("%s = %q, want %q", HeaderAdminToken, got, testToken)
	}
}

// writeRaw 原样回包：用于发送与服务端一致的完整响应体（fixture），或故意构造的坏内容
func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeEnvelope 按统一响应体 {code,message,data} 回包
func writeEnvelope(t *testing.T, w http.ResponseWriter, httpStatus, code int, message string, data any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	body := map[string]any{"code": code, "message": message}
	if data != nil {
		body["data"] = data
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("编码响应失败: %v", err)
	}
}

// roundTripFunc 便于在测试里替换传输层
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewSDKRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		token   string
	}{
		{"空地址", "", testToken},
		{"没有协议头", "127.0.0.1:8080", testToken},
		{"协议不支持", "ftp://127.0.0.1:8080", testToken},
		{"没有主机名", "http://", testToken},
		{"没有令牌", "http://127.0.0.1:8080", ""},
		{"令牌只有空白", "http://127.0.0.1:8080", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSDK(tt.baseURL, tt.token); err == nil {
				t.Fatalf("NewSDK(%q, %q) 应当报错", tt.baseURL, tt.token)
			}
		})
	}
}

func TestNewSDKRejectsBadOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
	}{
		{"超时为 0", WithTimeout(0)},
		{"超时为负", WithTimeout(-time.Second)},
		{"响应体上限为 0", WithMaxResponseBytes(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSDK("http://127.0.0.1:8080", testToken, tt.opt); err == nil {
				t.Fatal("NewSDK 应当报错")
			}
		})
	}
}

// 默认值要按文档生效；WithHTTPClient 与 WithTimeout 组合时不得修改调用方的 client
func TestNewSDKAppliesOptions(t *testing.T) {
	sdk, err := NewSDK("http://127.0.0.1:8080/", testToken)
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	if sdk.baseURL != "http://127.0.0.1:8080" {
		t.Errorf("baseURL = %q, want 去掉尾部斜杠", sdk.baseURL)
	}
	if sdk.http.Timeout != defaultTimeout {
		t.Errorf("默认超时 = %v, want %v", sdk.http.Timeout, defaultTimeout)
	}
	if sdk.ua != defaultUserAgent {
		t.Errorf("默认 UA = %q, want %q", sdk.ua, defaultUserAgent)
	}

	original := &http.Client{}
	sdk, err = NewSDK("http://127.0.0.1:8080", testToken,
		WithHTTPClient(original), WithTimeout(5*time.Second), WithUserAgent("my-agent"))
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	if sdk.http == original {
		t.Error("显式设置超时时应当使用副本，不能就地改调用方的 client")
	}
	if sdk.http.Timeout != 5*time.Second {
		t.Errorf("超时 = %v, want 5s", sdk.http.Timeout)
	}
	if original.Timeout != 0 {
		t.Errorf("调用方 client 的 Timeout 被改成了 %v", original.Timeout)
	}
	if sdk.ua != "my-agent" {
		t.Errorf("UA = %q, want my-agent", sdk.ua)
	}

	// 只传 client 时原样使用：超时交给调用方决定
	passthrough := &http.Client{Timeout: 7 * time.Second}
	sdk, err = NewSDK("http://127.0.0.1:8080", testToken, WithHTTPClient(passthrough))
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	if sdk.http != passthrough {
		t.Error("未显式设置超时时应当直接使用调用方的 client")
	}
}

func TestRequestCarriesTokenAndHeaders(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		requireToken(t, r)
		if ua := r.Header.Get("User-Agent"); ua != defaultUserAgent {
			t.Errorf("User-Agent = %q, want %q", ua, defaultUserAgent)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept = %q", accept)
		}
		writeEnvelope(t, w, http.StatusOK, CodeOK, "ok", map[string]any{"status": StatusOK})
	})

	if _, err := sdk.Pool.Status(context.Background()); err != nil {
		t.Fatalf("Pool.Status: %v", err)
	}
}

// 业务码 → *Error：真实的 HTTP 状态码与业务码都要保留，判定函数能区分错误类型
func TestServerBusinessCodeBecomesError(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		code       int
		message    string
		is         func(error) bool
	}{
		{"参数错误", http.StatusBadRequest, CodeInvalidParam, "keyword 不能为空", IsInvalidParam},
		{"令牌无效", http.StatusUnauthorized, CodeUnauthorized, "令牌无效", IsUnauthorized},
		{"资源不存在", http.StatusNotFound, CodeNotFound, "记录不存在", IsNotFound},
		{"资源冲突", http.StatusConflict, CodeConflict, "账号已存在", IsConflict},
		{"号池不可用", http.StatusServiceUnavailable, CodePoolExhausted, "号池暂无可用的洛谷账号", IsPoolExhausted},
		{"上游登录态失效", http.StatusBadGateway, CodeUpstreamUnauthorized, "洛谷登录态失效", IsUpstreamUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
				writeEnvelope(t, w, tt.httpStatus, tt.code, tt.message, nil)
			})

			_, err := sdk.Pool.Status(context.Background())
			if err == nil {
				t.Fatal("应当返回错误")
			}
			if !tt.is(err) {
				t.Errorf("判定函数不认这个错误: %v", err)
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("err 不是 *Error: %v", err)
			}
			if apiErr.HTTPStatus != tt.httpStatus || apiErr.Code != tt.code || apiErr.Message != tt.message {
				t.Errorf("err = %+v, want {HTTPStatus:%d Code:%d Message:%q}",
					apiErr, tt.httpStatus, tt.code, tt.message)
			}
			if CodeOf(err) != tt.code || HTTPStatusOf(err) != tt.httpStatus {
				t.Errorf("CodeOf = %d, HTTPStatusOf = %d", CodeOf(err), HTTPStatusOf(err))
			}
		})
	}
}

// 服务端没给文案时用标准状态文案兜底，不能返回空字符串
func TestAPIErrorFallsBackToStatusText(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeRaw(w, http.StatusInternalServerError, `{"code":500}`)
	})

	_, err := sdk.Pool.Status(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err 不是 *Error: %v", err)
	}
	if apiErr.Message != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("message = %q", apiErr.Message)
	}
}

// 传输层失败不是业务错误：原始错误链保留，CodeOf 用 -1 表示"不是业务码"
func TestTransportFailureKeepsOriginalError(t *testing.T) {
	dialErr := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	sdk, err := NewSDK("http://127.0.0.1:1", testToken, WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, dialErr }),
	}))
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}

	_, err = sdk.Pool.Status(context.Background())
	if !errors.Is(err, dialErr) {
		t.Fatalf("err = %v, want 保留原始错误链", err)
	}
	if CodeOf(err) != -1 || HTTPStatusOf(err) != 0 {
		t.Errorf("CodeOf = %d, HTTPStatusOf = %d, want -1 / 0", CodeOf(err), HTTPStatusOf(err))
	}
}

// context 要真正传到 HTTP 请求上：服务端等客户端断开，客户端等自己的 deadline
func TestRequestHonorsCallerContext(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := sdk.Pool.Status(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestInvalidJSONResponseIsNotBusinessError(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeRaw(w, http.StatusOK, "<html>反向代理的错误页</html>")
	})

	_, err := sdk.Pool.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "不是合法 JSON") {
		t.Fatalf("err = %v, want 提示响应不是合法 JSON", err)
	}
	if CodeOf(err) != -1 {
		t.Errorf("CodeOf = %d, want -1", CodeOf(err))
	}
}

func TestResponseSizeLimit(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeRaw(w, http.StatusOK, `{"code":0,"message":"ok","data":"`+strings.Repeat("x", 4096)+`"}`)
	}, WithMaxResponseBytes(256))

	_, err := sdk.Pool.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v, want 提示超过上限", err)
	}
}

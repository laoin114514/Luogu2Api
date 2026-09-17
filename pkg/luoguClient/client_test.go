package luoguclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientNewRequestHeaders(t *testing.T) {
	jar, _ := newExportableCookieJar()
	c := &Client{
		cookieJar:  jar,
		csrfToken:  "test-csrf-token",
		maxRetries: 3,
		backoffFn:  defaultBackoff,
		userAgent:  "test-ua",
		ctx:        context.Background(),
		httpClient: &http.Client{Jar: jar},
	}

	req, err := c.newRequest("POST", "/test", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if ref := req.Header.Get("Referer"); ref != luoguBaseURL {
		t.Errorf("Referer = %q, want %s", ref, luoguBaseURL)
	}
	if csrf := req.Header.Get("X-CSRF-TOKEN"); csrf != "test-csrf-token" {
		t.Errorf("X-CSRF-TOKEN = %q, want test-csrf-token", csrf)
	}
	if ua := req.Header.Get("User-Agent"); ua == "" {
		t.Error("User-Agent should not be empty")
	}
}

func TestClientNewRequestNoCSRFForGET(t *testing.T) {
	jar, _ := newExportableCookieJar()
	c := &Client{
		cookieJar:  jar,
		csrfToken:  "test-csrf-token",
		maxRetries: 3,
		backoffFn:  defaultBackoff,
		userAgent:  "test-ua",
		ctx:        context.Background(),
		httpClient: &http.Client{Jar: jar},
	}

	req, err := c.newRequest("GET", "/test", nil)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if req.Header.Get("X-CSRF-TOKEN") != "" {
		t.Error("GET requests should not have X-CSRF-TOKEN header")
	}
}

func TestClientNoRetryOn4xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	jar, _ := newExportableCookieJar()
	c := &Client{
		cookieJar:  jar,
		maxRetries: 3,
		backoffFn:  func(int) time.Duration { return 0 },
		userAgent:  "test-ua",
		ctx:        context.Background(),
		httpClient: &http.Client{Jar: jar},
	}

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp, _ := c.do(req)
	resp.Body.Close()

	if callCount != 1 {
		t.Errorf("4xx should not retry, called %d times", callCount)
	}
}

func TestClientRetryOn5xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	jar, _ := newExportableCookieJar()
	c := &Client{
		cookieJar:  jar,
		maxRetries: 2,
		backoffFn:  func(int) time.Duration { return 0 },
		userAgent:  "test-ua",
		ctx:        context.Background(),
		httpClient: &http.Client{Jar: jar},
	}

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp, _ := c.do(req)
	resp.Body.Close()

	if callCount != 3 {
		t.Errorf("5xx should retry, expected 3 calls, got %d", callCount)
	}
}

func TestClientCookieExportImportClear(t *testing.T) {
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := c.ImportCookies([]byte(`[{"name":"_uid","value":"42","domain":".luogu.com.cn","path":"/"}]`)); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}

	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	if !strings.Contains(string(data), `"42"`) {
		t.Errorf("exported cookies missing imported value: %s", data)
	}

	if err := c.ClearCookies(); err != nil {
		t.Fatalf("ClearCookies: %v", err)
	}
	data, err = c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies after clear: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("cookies not cleared, got %s", data)
	}
}

func TestWithCookiesOption(t *testing.T) {
	c, err := NewClient(WithCookies([]byte(`[{"name":"_uid","value":"7","domain":".luogu.com.cn","path":"/"}]`)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	if !strings.Contains(string(data), `"7"`) {
		t.Errorf("cookies injected at construction not present: %s", data)
	}
}

func TestWithCookiesOptionInvalidJSON(t *testing.T) {
	if _, err := NewClient(WithCookies([]byte("not json"))); err == nil {
		t.Error("NewClient should return error for invalid cookie JSON")
	}
}

func TestWithCookiesOptionEmpty(t *testing.T) {
	// 常见用法：os.ReadFile 失败时 data 为 nil，此时应视为无 cookie 而非报错
	c, err := NewClient(WithCookies(nil))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("expected no cookies, got %s", data)
	}
}

func TestClientUIDFromCookie(t *testing.T) {
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if uid := c.UID(); uid != 0 {
		t.Errorf("no session should give UID 0, got %d", uid)
	}

	if err := c.ImportCookies([]byte(`[{"name":"_uid","value":"1965145","domain":".luogu.com.cn","path":"/"}]`)); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if uid := c.UID(); uid != 1965145 {
		t.Errorf("UID = %d, want 1965145", uid)
	}

	// 非法值不应 panic，返回 0
	if err := c.ImportCookies([]byte(`[{"name":"_uid","value":"not-a-number","domain":".luogu.com.cn","path":"/"}]`)); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if uid := c.UID(); uid != 0 {
		t.Errorf("invalid _uid should give 0, got %d", uid)
	}

	if err := c.ClearCookies(); err != nil {
		t.Fatalf("ClearCookies: %v", err)
	}
	if uid := c.UID(); uid != 0 {
		t.Errorf("after clear UID = %d, want 0", uid)
	}
}

package luoguclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient 返回一个把请求指向测试服务器的 Client（不会访问真实洛谷）
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.baseURL = server.URL + "/"
	return c
}

// lentilleHTML 用给定 JSON 构造带 lentille-context 的页面
func lentilleHTML(jsonBody string) string {
	return `<!DOCTYPE html><html><head></head><body>` +
		`<script id="lentille-context" type="application/json">` + jsonBody + `</script>` +
		`</body></html>`
}

// serveLentille 返回固定输出 lentille 页面的测试服务器
func serveLentille(t *testing.T, jsonBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(lentilleHTML(jsonBody)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveStatus 返回固定状态码的测试服务器
func serveStatus(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(srv.Close)
	return srv
}

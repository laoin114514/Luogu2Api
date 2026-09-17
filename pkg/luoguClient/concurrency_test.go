package luoguclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Client 应可并发使用：CSRF、cookie、请求构建同时进行不应产生竞态。
// 用 `go test -race` 运行才有意义。
func TestClientConcurrentUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>` +
			`<meta name="csrf-token" content="fresh-token">` +
			`</head><body><script id="lentille-context" type="application/json">` +
			`{"data":{"records":{"result":[],"count":0}}}` +
			`</script></body></html>`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	const (
		goroutines = 8
		rounds     = 25
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				c.SetCSRF(fmt.Sprintf("token-%d-%d", g, i))
				_ = c.csrf()

				req, err := c.newRequest("POST", "/do-auth/password", map[string]string{"username": "u"})
				if err != nil {
					t.Errorf("newRequest: %v", err)
					return
				}
				if req.Header.Get("X-CSRF-TOKEN") == "" {
					t.Error("POST request should carry X-CSRF-TOKEN")
					return
				}

				if _, err := c.Problem.Search(SearchParams{Keyword: "x", Page: 1}); err != nil {
					t.Errorf("Search: %v", err)
					return
				}

				if err := c.ImportCookies([]byte(`[{"name":"_uid","value":"1","domain":".luogu.com.cn","path":"/"}]`)); err != nil {
					t.Errorf("ImportCookies: %v", err)
					return
				}
				if _, err := c.ExportCookies(); err != nil {
					t.Errorf("ExportCookies: %v", err)
					return
				}
				if err := c.Auth.RefreshCSRF(); err != nil {
					t.Errorf("RefreshCSRF: %v", err)
					return
				}
				if i%10 == 0 {
					if err := c.ClearCookies(); err != nil {
						t.Errorf("ClearCookies: %v", err)
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

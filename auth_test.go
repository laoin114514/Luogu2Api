package luoguclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantUnauth bool
	}{
		{name: "200", statusCode: http.StatusOK},
		{name: "401", statusCode: http.StatusUnauthorized, wantUnauth: true},
		{name: "403", statusCode: http.StatusForbidden, wantUnauth: true},
		{name: "404", statusCode: http.StatusNotFound},
		{name: "500", statusCode: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.statusCode}
			err := checkResponse(resp, "get problem %s", "P1001")

			if tt.statusCode == http.StatusOK {
				if err != nil {
					t.Fatalf("200 should not error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}

			var unauthorized *UnauthorizedError
			if gotUnauth := errors.As(err, &unauthorized); gotUnauth != tt.wantUnauth {
				t.Fatalf("UnauthorizedError = %v, want %v (err=%v)", gotUnauth, tt.wantUnauth, err)
			}
			if tt.wantUnauth && unauthorized.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", unauthorized.StatusCode, tt.statusCode)
			}
			if !strings.Contains(err.Error(), "get problem P1001") {
				t.Errorf("error should describe the operation: %v", err)
			}
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("error should include status: %v", err)
			}
		})
	}
}

// 所有需要登录的接口都应把 401 统一映射为 *UnauthorizedError
//
// Record 的两个接口单独在 record_test.go 中覆盖（它们依赖 lentille-context 解析的改动）。
func TestServicesMapUnauthorizedStatus(t *testing.T) {
	c := newTestClient(t, serveStatus(t, http.StatusUnauthorized))

	calls := map[string]func() error{
		"Problem.Get":               func() error { _, err := c.Problem.Get("P1001"); return err },
		"Problem.Search":            func() error { _, err := c.Problem.Search(SearchParams{Keyword: "a"}); return err },
		"Problem.GetSolutions":      func() error { _, err := c.Problem.GetSolutions("P1001", 1); return err },
		"Problem.GetSolutionDetail": func() error { _, err := c.Problem.GetSolutionDetail("abc"); return err },
		"Problem.GetTranslation":    func() error { _, err := c.Problem.GetTranslation("P1001"); return err },
		"Problem.GetFull":           func() error { _, _, err := c.Problem.GetFull("P1001"); return err },
		"Training.GetList":          func() error { _, err := c.Training.GetList(TrainingListParams{Page: 1}); return err },
		"Training.GetDetail":        func() error { _, err := c.Training.GetDetail(1); return err },
		"User.Get":                  func() error { _, err := c.User.Get(1); return err },
		"User.GetRanking":           func() error { _, err := c.User.GetRanking(1); return err },
		"Discuss.GetList":           func() error { _, err := c.Discuss.GetList(1); return err },
		"Discuss.GetDetail":         func() error { _, err := c.Discuss.GetDetail(1, 1); return err },
		"Contest.GetList":           func() error { _, err := c.Contest.GetList(1); return err },
		"Contest.GetDetail":         func() error { _, err := c.Contest.GetDetail(1); return err },
	}

	for name, call := range calls {
		err := call()
		var unauthorized *UnauthorizedError
		if !errors.As(err, &unauthorized) {
			t.Errorf("%s: err = %v (%T), want *UnauthorizedError", name, err, err)
			continue
		}
		if unauthorized.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: StatusCode = %d, want 401", name, unauthorized.StatusCode)
		}
	}
}

// 403 也视为权限不足
func TestServiceMapsForbiddenStatus(t *testing.T) {
	c := newTestClient(t, serveStatus(t, http.StatusForbidden))

	_, err := c.Problem.Get("P1001")
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("err = %v (%T), want *UnauthorizedError", err, err)
	}
}

// 其他非 200 状态码保持普通错误，且带上原始状态码
func TestServiceNonUnauthorizedStatusError(t *testing.T) {
	c := newTestClient(t, serveStatus(t, http.StatusNotFound))

	_, err := c.Problem.Get("P1001")
	if err == nil {
		t.Fatal("expected error")
	}
	var unauthorized *UnauthorizedError
	if errors.As(err, &unauthorized) {
		t.Fatalf("404 should not be UnauthorizedError: %v", err)
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("error should include status: %v", err)
	}
}

// 洛谷当前对未登录的 /user/setting 直接返回 401
func TestVerifyAuthUnauthorizedOn401(t *testing.T) {
	c := newTestClient(t, serveStatus(t, http.StatusUnauthorized))

	err := c.Auth.Verify()
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("Verify error = %v (%T), want *UnauthorizedError", err, err)
	}
	if c.Auth.IsAuthenticated() {
		t.Error("IsAuthenticated should be false on 401")
	}
}

// 兼容旧行为：302 重定向到登录页
func TestVerifyAuthUnauthorizedOnLoginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>login</body></html>"))
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	err := c.Auth.Verify()
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("Verify error = %v (%T), want *UnauthorizedError", err, err)
	}
	if c.Auth.IsAuthenticated() {
		t.Error("IsAuthenticated should be false when redirected to login")
	}
}

// 已登录：/user/setting 返回 200
func TestVerifyAuthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>settings</body></html>"))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	if err := c.Auth.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !c.Auth.IsAuthenticated() {
		t.Error("IsAuthenticated should be true on 200")
	}
}

// 零值 UnauthorizedError 的文案保持向后兼容
func TestUnauthorizedErrorMessage(t *testing.T) {
	if got := (&UnauthorizedError{}).Error(); got != "unauthorized: please login first" {
		t.Errorf("zero value message = %q", got)
	}
}

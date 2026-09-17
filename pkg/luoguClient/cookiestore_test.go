package luoguclient

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestExportableCookieJarRoundTrip(t *testing.T) {
	jar, err := newExportableCookieJar()
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}

	u, _ := url.Parse(luoguBaseURL)
	jar.SetCookies(u, []*http.Cookie{
		{Name: "_uid", Value: "12345", Domain: ".luogu.com.cn", Path: "/"},
		{Name: "__client_id", Value: "abc123", Domain: ".luogu.com.cn", Path: "/"},
	})

	data, err := jar.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	jar2, _ := newExportableCookieJar()
	if err := jar2.Import(data); err != nil {
		t.Fatalf("import: %v", err)
	}

	cookies := jar2.Cookies(u)
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	findCookie := func(name string) *http.Cookie {
		for _, c := range cookies {
			if c.Name == name {
				return c
			}
		}
		return nil
	}

	if c := findCookie("_uid"); c == nil || c.Value != "12345" {
		t.Error("_uid cookie not restored correctly")
	}
	if c := findCookie("__client_id"); c == nil || c.Value != "abc123" {
		t.Error("__client_id cookie not restored correctly")
	}
}

func TestCallerSidePersistenceRoundTrip(t *testing.T) {
	jar, _ := newExportableCookieJar()
	u, _ := url.Parse(luoguBaseURL)
	jar.SetCookies(u, []*http.Cookie{
		{Name: "_uid", Value: "99999", Domain: ".luogu.com.cn", Path: "/"},
	})

	// 调用方自行落盘
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cookies.json")
	data, err := jar.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 新会话从调用方保存的内容恢复
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	jar2, _ := newExportableCookieJar()
	if err := jar2.Import(raw); err != nil {
		t.Fatalf("import: %v", err)
	}

	cookies := jar2.Cookies(u)
	if len(cookies) != 1 || cookies[0].Value != "99999" {
		t.Errorf("cookie not restored correctly")
	}
}

func TestExportImportHostOnlyCookie(t *testing.T) {
	jar, _ := newExportableCookieJar()
	u, _ := url.Parse(luoguBaseURL)
	// 模拟服务端无 domain/path 属性的 Set-Cookie（host-only cookie）
	jar.SetCookies(u, []*http.Cookie{
		{Name: "_uid", Value: "12345"},
		{Name: "__client_id", Value: "abc123"},
	})

	data, err := jar.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	jar2, _ := newExportableCookieJar()
	if err := jar2.Import(data); err != nil {
		t.Fatalf("import: %v", err)
	}

	cookies := jar2.Cookies(u)
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	findCookie := func(name string) *http.Cookie {
		for _, c := range cookies {
			if c.Name == name {
				return c
			}
		}
		return nil
	}

	if c := findCookie("_uid"); c == nil || c.Value != "12345" {
		t.Error("_uid host-only cookie not restored correctly")
	}
	if c := findCookie("__client_id"); c == nil || c.Value != "abc123" {
		t.Error("__client_id host-only cookie not restored correctly")
	}
}

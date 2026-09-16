package luoguclient

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// exportableCookie 可序列化的 cookie 结构
type exportableCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure,omitempty"`
	HttpOnly bool   `json:"http_only,omitempty"`
}

// ExportableCookieJar 包装 cookiejar.Jar，支持将 cookie 导出为 JSON 或从 JSON 导入。
// cookie 仅保存在内存中，持久化（写文件、数据库等）由调用方自行负责。
//
// 所有方法都可以并发调用：内部的 cookiejar.Jar 本身并发安全，指针替换（Clear）
// 由 mu 保护。
type ExportableCookieJar struct {
	mu  sync.RWMutex
	jar *cookiejar.Jar
}

func newExportableCookieJar() (*ExportableCookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &ExportableCookieJar{jar: jar}, nil
}

// SetCookies 设置 cookie 到内存（不落盘，调用方可通过 Client.ExportCookies 导出）
func (j *ExportableCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	j.jar.SetCookies(u, cookies)
}

func (j *ExportableCookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jar.Cookies(u)
}

// Clear 清空内存中的 cookie
func (j *ExportableCookieJar) Clear() error {
	newJar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar = newJar
	return nil
}

// Export 将所有 cookie 导出为 JSON 字节
func (j *ExportableCookieJar) Export() ([]byte, error) {
	u, err := url.Parse(luoguBaseURL)
	if err != nil {
		return nil, err
	}
	cookies := j.Cookies(u)
	exported := make([]exportableCookie, 0, len(cookies))
	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			domain = u.Host // host-only cookie，写入时改为显式 host
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		exported = append(exported, exportableCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     path,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		})
	}
	return json.Marshal(exported)
}

// Import 从 JSON 字节导入 cookie（空输入视为无 cookie 可导入，不改变现有会话）
func (j *ExportableCookieJar) Import(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var cookies []exportableCookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return err
	}
	u, err := url.Parse(luoguBaseURL)
	if err != nil {
		return err
	}
	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			domain = u.Host // 兼容旧文件（domain 为空时回退为 host-only）
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		j.SetCookies(u, []*http.Cookie{{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     path,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		}})
	}
	return nil
}

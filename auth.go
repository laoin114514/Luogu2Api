package luoguclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AuthService 认证服务
type AuthService struct {
	client *Client
}

// RefreshCSRF 刷新 CSRF token（需要先调用才能登录）
func (a *AuthService) RefreshCSRF() error {
	return a.client.refreshCSRF()
}

// GetCaptcha 获取验证码图片，返回 JPEG 字节
func (a *AuthService) GetCaptcha() ([]byte, error) {
	resp, err := a.client.get("/lg4/captcha")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// Login 使用用户名、密码、验证码登录
func (a *AuthService) Login(username, password, captcha string) (*LoginResponse, error) {
	body := &LoginRequest{
		Username: username,
		Password: password,
		Captcha:  captcha,
	}

	resp, err := a.client.post("/do-auth/password", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, loginError(resp)
	}

	var result LoginResponse
	if err := parseBody(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// loginError 把登录失败响应转换成 *AuthError（调用方负责关闭 resp.Body）
//
// 实测失败响应（HTTP 400）为：
//
//	{"errorCode":400,"errorType":"LuoguWeb\\Spilopelia\\Exception\\CaptchaNotMatchException",
//	 "errorMessage":"图形验证码错误","errorData":{":":0}}
//
// 旧字段 code/message 仍作兼容解析。
func loginError(resp *http.Response) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &AuthError{Code: resp.StatusCode, Message: "login failed (read body: " + err.Error() + ")"}
	}

	var errResp struct {
		ErrorCode    int    `json:"errorCode"`
		ErrorType    string `json:"errorType"`
		ErrorMessage string `json:"errorMessage"`
		Code         int    `json:"code"`
		Message      string `json:"message"`
	}
	if json.Unmarshal(data, &errResp) == nil {
		code := errResp.ErrorCode
		if code == 0 {
			code = errResp.Code
		}
		message := errResp.ErrorMessage
		if message == "" {
			message = errResp.Message
		}
		if message == "" {
			message = "login failed"
		}
		if errResp.ErrorType != "" || message != "login failed" {
			return &AuthError{Code: code, Type: errResp.ErrorType, Message: message}
		}
	}

	// 响应不是预期 JSON：保留原文片段，避免丢失失败原因
	return &AuthError{
		Code:    resp.StatusCode,
		Message: fmt.Sprintf("login failed, body: %s", truncateRunes(strings.TrimSpace(string(data)), 200)),
	}
}

// truncateRunes 按字符截断，避免切断 UTF-8
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// LoginWithSolver 使用 CaptchaSolver 自动获取验证码并登录
func (a *AuthService) LoginWithSolver(username, password string, solver CaptchaSolver) (*LoginResponse, error) {
	image, err := a.GetCaptcha()
	if err != nil {
		return nil, err
	}

	captcha, err := solver(image)
	if err != nil {
		return nil, &AuthError{Code: 0, Message: "captcha solve failed: " + err.Error()}
	}

	return a.Login(username, password, captcha)
}

// Logout 登出当前会话并清空内存中的 cookie
func (a *AuthService) Logout() error {
	resp, err := a.client.post("/auth/logout", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if err := a.client.ClearCookies(); err != nil {
		return fmt.Errorf("clear cookies: %w", err)
	}
	return nil
}

// Verify 验证当前登录状态是否有效
func (a *AuthService) Verify() error {
	return a.client.verifyAuth()
}

// IsAuthenticated 检查是否已经登录
func (a *AuthService) IsAuthenticated() bool {
	return a.client.verifyAuth() == nil
}

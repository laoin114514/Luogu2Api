package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/laoin114514/luogu2api/internal/config"
)

// ErrSolver 验证码识别失败（环境类问题：OCR 服务不可用/未识别出结果）。
//
// 刻意与"账号密码错误"区分：OCR 挂了只应让账号保持待重登，
// 绝不能据此停用账号。
var ErrSolver = errors.New("验证码识别失败")

// CaptchaSolver 验证码识别能力（SDK 不内置 OCR，需要外部服务）
type CaptchaSolver interface {
	Solve(ctx context.Context, image []byte) (string, error)
}

// OCR 响应体上限：验证码识别结果只有几个字符，超出即为异常响应
const maxOCRResponse = 1 << 10 // 1 KiB

// 识别结果长度上限，防止把超长内容塞进登录请求
const maxCaptchaLength = 16

// OCRClient 调用外部 OCR 服务的 CaptchaSolver 实现。
//
// 约定的接口：POST 原始 JPEG 字节（Content-Type: image/jpeg），
// 可选 Authorization: Bearer <token>；响应为纯文本或
// {"result":"..."} / {"code":"..."} / {"data":"..."} / {"text":"..."}。
// 若你的服务形态不同，只需改本文件。
type OCRClient struct {
	url   string
	token string
	http  *http.Client
}

// NewOCRClient 创建 OCR 客户端
func NewOCRClient(cfg config.OCR) *OCRClient {
	return &OCRClient{
		url:   cfg.URL,
		token: cfg.Token,
		http:  &http.Client{Timeout: cfg.Timeout},
	}
}

// Solve 识别验证码
func (c *OCRClient) Solve(ctx context.Context, image []byte) (string, error) {
	if len(image) == 0 {
		return "", fmt.Errorf("%w: 验证码图片为空", ErrSolver)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(image))
	if err != nil {
		return "", fmt.Errorf("%w: 构造请求失败: %v", ErrSolver, err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: 调用 OCR 服务失败: %v", ErrSolver, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOCRResponse))
	if err != nil {
		return "", fmt.Errorf("%w: 读取 OCR 响应失败: %v", ErrSolver, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: OCR 服务返回 HTTP %d: %s",
			ErrSolver, resp.StatusCode, truncate(strings.TrimSpace(string(body)), 100))
	}

	code := parseOCRResult(body)
	if code == "" {
		return "", fmt.Errorf("%w: OCR 服务未返回可用结果", ErrSolver)
	}
	return code, nil
}

// parseOCRResult 兼容纯文本与几种常见 JSON 字段
func parseOCRResult(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "{") {
		var payload struct {
			Result string `json:"result"`
			Code   string `json:"code"`
			Data   string `json:"data"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return ""
		}
		for _, candidate := range []string{payload.Result, payload.Code, payload.Data, payload.Text} {
			if v := normalizeCaptcha(candidate); v != "" {
				return v
			}
		}
		return ""
	}

	return normalizeCaptcha(trimmed)
}

// normalizeCaptcha 去掉空白并限长
func normalizeCaptcha(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > maxCaptchaLength {
		s = s[:maxCaptchaLength]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

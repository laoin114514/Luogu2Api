package client

import (
	"bytes"
	"context"
	"encoding/base64"
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

// OCR 响应体上限：识别结果只有几个字符，超出即为异常响应
const maxOCRResponse = 4 << 10 // 4 KiB

// 识别结果长度上限，防止把超长内容塞进登录请求
const maxCaptchaLength = 16

// OCRClient 调用外部 OCR 服务的 CaptchaSolver 实现。
//
// 两种入参形态（LUOGU_OCR_MODE）：
//   - base64（默认）：POST application/json，body {"image_base64":"<base64 JPEG>"}
//   - raw：POST image/jpeg，body 为原始图片字节
//
// 响应兼容嵌套 data.text 与顶层 result/code/text，也接受纯文本；
// 服务端显式返回失败（如 {"sucess":false,"message":"..."}）会被翻译成
// 带原因的 ErrSolver。
type OCRClient struct {
	url   string
	token string
	mode  string
	http  *http.Client
}

// NewOCRClient 创建 OCR 客户端
func NewOCRClient(cfg config.OCR) *OCRClient {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != config.OCRModeRaw {
		mode = config.OCRModeBase64
	}
	return &OCRClient{
		url:   cfg.URL,
		token: cfg.Token,
		mode:  mode,
		http:  &http.Client{Timeout: cfg.Timeout},
	}
}

// Solve 识别验证码
func (c *OCRClient) Solve(ctx context.Context, image []byte) (string, error) {
	if len(image) == 0 {
		return "", fmt.Errorf("%w: 验证码图片为空", ErrSolver)
	}

	body, contentType, err := c.encode(image)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: 构造请求失败: %v", ErrSolver, err)
	}
	req.Header.Set("Content-Type", contentType)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: 调用 OCR 服务失败: %v", ErrSolver, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOCRResponse))
	if err != nil {
		return "", fmt.Errorf("%w: 读取 OCR 响应失败: %v", ErrSolver, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: OCR 服务返回 HTTP %d: %s",
			ErrSolver, resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 100))
	}

	code, reason := parseOCRResult(raw)
	if code != "" {
		return code, nil
	}
	if reason != "" {
		return "", fmt.Errorf("%w: %s", ErrSolver, reason)
	}
	return "", fmt.Errorf("%w: OCR 服务未返回可用结果: %s",
		ErrSolver, truncate(strings.TrimSpace(string(raw)), 100))
}

// encode 按配置的形态编码请求体
func (c *OCRClient) encode(image []byte) ([]byte, string, error) {
	if c.mode == config.OCRModeRaw {
		return image, "image/jpeg", nil
	}

	payload, err := json.Marshal(map[string]string{
		"image_base64": base64.StdEncoding.EncodeToString(image),
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: 构造 base64 请求失败: %v", ErrSolver, err)
	}
	return payload, "application/json", nil
}

// parseOCRResult 解析识别结果，返回 (验证码, 服务端给出的失败原因)。
//
// 兼容过的真实形状：
//
//	{"sucess":true,"message":"识别成功","data":{"text":"anm"}}   ← sucess 是服务端的拼写
//	{"result":"1234"} / {"code":"1234"} / {"text":"1234"} / {"data":"1234"}
//	1234                                                        ← 纯文本
func parseOCRResult(body []byte) (code string, reason string) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", ""
	}

	if !strings.HasPrefix(trimmed, "{") {
		return normalizeCaptcha(trimmed), ""
	}

	var payload struct {
		Success *bool           `json:"sucess"` // 服务端字段名就是 sucess
		Message string          `json:"message"`
		Result  string          `json:"result"`
		Code    string          `json:"code"`
		Text    string          `json:"text"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", ""
	}

	// data 既可能是对象（{"text":...}）也可能是字符串
	if len(payload.Data) > 0 {
		var asString string
		if json.Unmarshal(payload.Data, &asString) == nil {
			if v := normalizeCaptcha(asString); v != "" {
				return v, ""
			}
		} else {
			var nested struct {
				Text   string `json:"text"`
				Result string `json:"result"`
				Code   string `json:"code"`
				Data   string `json:"data"`
			}
			if json.Unmarshal(payload.Data, &nested) == nil {
				for _, candidate := range []string{nested.Text, nested.Result, nested.Code, nested.Data} {
					if v := normalizeCaptcha(candidate); v != "" {
						return v, ""
					}
				}
			}
		}
	}

	for _, candidate := range []string{payload.Result, payload.Code, payload.Text} {
		if v := normalizeCaptcha(candidate); v != "" {
			return v, ""
		}
	}

	// 没有识别结果：把服务端的失败原因带出去（例如"未识别出字符"）
	if payload.Success != nil && !*payload.Success {
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return "", msg
		}
		return "", "OCR 服务返回识别失败"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return "", msg
	}
	return "", ""
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

package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laoin114514/luogu2api/internal/config"
)

func newOCRServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *OCRClient) {
	t.Helper()
	return newOCRServerMode(t, handler, config.OCRModeRaw)
}

func newOCRServerMode(t *testing.T, handler http.HandlerFunc, mode string) (*httptest.Server, *OCRClient) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewOCRClient(config.OCR{URL: server.URL, Mode: mode, Timeout: 2 * time.Second})
	return server, client
}

// 默认形态是 base64 JSON（配合现用的远程 OCR 服务）
func TestOCRDefaultModeIsBase64(t *testing.T) {
	var gotContentType string
	var gotBody map[string]string

	_, ocr := newOCRServerMode(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"sucess":true,"message":"识别成功","data":{"text":"anm"}}`))
	}, "")

	code, err := ocr.Solve(context.Background(), []byte("JPEGDATA"))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if code != "anm" {
		t.Errorf("code = %q, want anm", code)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["image_base64"] != base64.StdEncoding.EncodeToString([]byte("JPEGDATA")) {
		t.Errorf("image_base64 = %q", gotBody["image_base64"])
	}
}

func TestOCRSolveRawMode(t *testing.T) {
	_, ocr := newOCRServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "image/jpeg" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "JPEGDATA" {
			t.Errorf("body = %q", string(body))
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("未配置 token 时不应带 Authorization: %q", got)
		}
		_, _ = w.Write([]byte("  a1B2 \n"))
	})

	code, err := ocr.Solve(context.Background(), []byte("JPEGDATA"))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if code != "a1B2" {
		t.Errorf("code = %q, want a1B2", code)
	}
}

// 现用服务真实返回形状
func TestOCRSolveRealServiceShape(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"成功", `{"sucess":true,"message":"识别成功","data":{"text":"anm"}}`, "anm"},
		{"四字符", `{"sucess":true,"message":"识别成功","data":{"text":"a1B2"}}`, "a1B2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ocr := newOCRServerMode(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}, "")

			code, err := ocr.Solve(context.Background(), []byte("jpeg"))
			if err != nil {
				t.Fatalf("Solve: %v", err)
			}
			if code != tt.want {
				t.Errorf("code = %q, want %q", code, tt.want)
			}
		})
	}
}

// 服务端明确说识别失败时，错误里要带上原因，便于日志定位
func TestOCRSolveServerReportedFailure(t *testing.T) {
	_, ocr := newOCRServerMode(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sucess":false,"message":"image_base64 不能为空","data":{}}`))
	}, "")

	_, err := ocr.Solve(context.Background(), []byte("jpeg"))
	if !errors.Is(err, ErrSolver) {
		t.Fatalf("err = %v, want ErrSolver", err)
	}
	if !strings.Contains(err.Error(), "image_base64 不能为空") {
		t.Errorf("错误应带服务端原因: %v", err)
	}
}

func TestOCRSolveJSONShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"result", `{"result":"1234"}`, "1234"},
		{"code", `{"code":"abcd"}`, "abcd"},
		{"data-string", `{"data":"XY12"}`, "XY12"},
		{"data-object-result", `{"data":{"result":"77"},"sucess":true}`, "77"},
		{"text", `{"text":"9999"}`, "9999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ocr := newOCRServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})

			code, err := ocr.Solve(context.Background(), []byte("jpeg"))
			if err != nil {
				t.Fatalf("Solve: %v", err)
			}
			if code != tt.want {
				t.Errorf("code = %q, want %q", code, tt.want)
			}
		})
	}
}

func TestOCRSolveForwardsToken(t *testing.T) {
	server, _ := newOCRServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte("0000"))
	})

	ocr := NewOCRClient(config.OCR{URL: server.URL, Token: "secret", Mode: config.OCRModeRaw, Timeout: 2 * time.Second})
	if _, err := ocr.Solve(context.Background(), []byte("jpeg")); err != nil {
		t.Fatalf("Solve: %v", err)
	}
}

func TestOCRSolveFailuresAreSolverErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		image   []byte
	}{
		{"http-500", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}, []byte("jpeg")},
		{"empty-body", func(w http.ResponseWriter, _ *http.Request) {}, []byte("jpeg")},
		{"blank-body", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("   \n"))
		}, []byte("jpeg")},
		{"broken-json", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":`))
		}, []byte("jpeg")},
		{"json-without-value", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"other":"x"}`))
		}, []byte("jpeg")},
		{"empty-image", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("1234"))
		}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ocr := newOCRServer(t, tt.handler)

			_, err := ocr.Solve(context.Background(), tt.image)
			if err == nil {
				t.Fatal("应报错")
			}
			if !errors.Is(err, ErrSolver) {
				t.Errorf("错误必须可用 errors.Is(err, ErrSolver) 判定，得到 %v", err)
			}
		})
	}
}

func TestOCRSolveTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("1234"))
	}))
	defer server.Close()

	ocr := NewOCRClient(config.OCR{URL: server.URL, Mode: config.OCRModeRaw, Timeout: 20 * time.Millisecond})
	_, err := ocr.Solve(context.Background(), []byte("jpeg"))
	if !errors.Is(err, ErrSolver) {
		t.Errorf("超时应归类为验证码识别失败，得到 %v", err)
	}
}

func TestOCRSolveCapsResponseAndResult(t *testing.T) {
	_, ocr := newOCRServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	})

	code, err := ocr.Solve(context.Background(), []byte("jpeg"))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(code) > maxCaptchaLength {
		t.Errorf("识别结果应被限长到 %d，实际 %d", maxCaptchaLength, len(code))
	}
}

func TestParseOCRResult(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantCode   string
		wantReason string
	}{
		{"真实成功响应", `{"sucess":true,"message":"识别成功","data":{"text":"anm"}}`, "anm", ""},
		{"服务端失败", `{"sucess":false,"message":"识别失败：图片过小","data":{}}`, "", "识别失败：图片过小"},
		{"失败但无原因", `{"sucess":false,"data":{}}`, "", "OCR 服务返回识别失败"},
		{"纯文本", `1234`, "1234", ""},
		{"带空格", ` 12 34 `, "12 34", ""},
		{"data 字符串", `{"data":"5678"}`, "5678", ""},
		{"data 对象无文本", `{"data":{}}`, "", ""},
		{"空对象", `{}`, "", ""},
		{"空串", ``, "", ""},
		{"非对象", `[1,2,3]`, "[1,2,3]", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, reason := parseOCRResult([]byte(tt.in))
			if code != tt.wantCode || reason != tt.wantReason {
				t.Errorf("parseOCRResult(%q) = (%q, %q), want (%q, %q)",
					tt.in, code, reason, tt.wantCode, tt.wantReason)
			}
		})
	}
}

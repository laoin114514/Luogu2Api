package client

import (
	"context"
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

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewOCRClient(config.OCR{URL: server.URL, Timeout: 2 * time.Second})
	return server, client
}

func TestOCRSolvePlainText(t *testing.T) {
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

func TestOCRSolveJSONShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"result", `{"result":"1234"}`, "1234"},
		{"code", `{"code":"abcd"}`, "abcd"},
		{"data", `{"data":"XY12"}`, "XY12"},
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

	ocr := NewOCRClient(config.OCR{URL: server.URL, Token: "secret", Timeout: 2 * time.Second})
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

	ocr := NewOCRClient(config.OCR{URL: server.URL, Timeout: 20 * time.Millisecond})
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
		in   string
		want string
	}{
		{`1234`, "1234"},
		{` 12 34 `, "12 34"},
		{`{"result":"","code":"5678"}`, "5678"},
		{`{}`, ""},
		{``, ""},
		{`[1,2,3]`, "[1,2,3]"},
	}

	for _, tt := range tests {
		if got := parseOCRResult([]byte(tt.in)); got != tt.want {
			t.Errorf("parseOCRResult(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

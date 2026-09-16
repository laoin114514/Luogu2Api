package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// stubProblemReader 题目读取替身
type stubProblemReader struct {
	problem *client.Problem
	result  *client.SearchResult
	err     error
}

func (s stubProblemReader) Get(context.Context, string) (*client.Problem, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.problem, nil
}

func (s stubProblemReader) Search(context.Context, string, int, int) (*client.SearchResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func newProblemEngine(reader ProblemReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewProblemHandler(reader)
	r.GET("/problems/:pid", h.Get)
	r.GET("/problems", h.Search)
	return r
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) response.Body {
	t.Helper()

	var body response.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return body
}

// fakeProvider 号池替身（用于把真实 service 接进 handler 测试）
type fakeProvider struct {
	problem *client.Problem
	result  *client.SearchResult
	err     error

	gotParams client.SearchParams
}

func (f *fakeProvider) GetProblem(_ context.Context, _ string) (*client.Problem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.problem, nil
}

func (f *fakeProvider) SearchProblems(_ context.Context, params client.SearchParams) (*client.SearchResult, error) {
	f.gotParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// realServiceEngine 用真实 service 构建路由：参数校验与错误翻译都在 service 层，
// 只测 handler 替身是测不到的，所以这里接真家伙。
func realServiceEngine(provider *fakeProvider) *gin.Engine {
	return newProblemEngine(service.NewProblemService(provider))
}

func TestProblemGetOK(t *testing.T) {
	engine := realServiceEngine(&fakeProvider{
		problem: &client.Problem{PID: "P1001", Title: "A+B Problem", Difficulty: 1},
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems/P1001", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body.Code != response.CodeOK {
		t.Errorf("code = %d", body.Code)
	}
	if !strings.Contains(rec.Body.String(), "A+B Problem") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// 号池没有可用账号 → 503 + 专门的业务码，便于调用方区分"上游没号"与"参数错"
func TestProblemGetPoolExhaustedMapsTo503(t *testing.T) {
	engine := realServiceEngine(&fakeProvider{
		err: fmt.Errorf("获取题目 P1001: %w", client.ErrPoolExhausted),
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems/P1001", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodePoolExhausted {
		t.Errorf("code = %d, want %d", body.Code, response.CodePoolExhausted)
	}
}

// 全部账号登录态失效 → 502 + 专门的业务码
func TestProblemGetUpstreamUnauthorizedMapsTo502(t *testing.T) {
	engine := realServiceEngine(&fakeProvider{
		err: &client.UnauthorizedError{StatusCode: 401, Message: "get problem P1001"},
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems/P1001", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodeUpstreamUnauthorized {
		t.Errorf("code = %d, want %d", body.Code, response.CodeUpstreamUnauthorized)
	}
}

func TestProblemSearchRejectsBadPageSize(t *testing.T) {
	engine := realServiceEngine(&fakeProvider{result: &client.SearchResult{}})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems?keyword=排序&pageSize=1000", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestProblemSearchRejectsEmptyKeyword(t *testing.T) {
	engine := realServiceEngine(&fakeProvider{result: &client.SearchResult{}})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestProblemSearchOK(t *testing.T) {
	provider := &fakeProvider{result: &client.SearchResult{Total: 1, Page: 1, PerPage: 20}}
	engine := realServiceEngine(provider)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems?keyword=排序", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Total":1`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	// 未传 page/pageSize 时用默认值
	if provider.gotParams.Page != 1 || provider.gotParams.PageSize != 20 {
		t.Errorf("params = %+v", provider.gotParams)
	}
}

// 未知错误只回通用文案：内部细节留在日志里，不能通过响应泄漏
func TestProblemGetUnknownErrorHidesDetails(t *testing.T) {
	engine := newProblemEngine(stubProblemReader{
		err: errors.New("数据库密码是 hunter2，连接被拒绝"),
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems/P1001", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("响应泄漏了内部细节: %s", rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodeInternalError {
		t.Errorf("code = %d", body.Code)
	}
}

// 各类 service 哨兵错误 → HTTP 状态码 + 业务码的完整映射
func TestErrorMappingTable(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   int
	}{
		{"参数错误", fmt.Errorf("%w: pid 为空", service.ErrInvalidParam), http.StatusBadRequest, response.CodeInvalidParam},
		{"账号不存在", model.ErrAccountNotFound, http.StatusNotFound, response.CodeNotFound},
		{"账号冲突", model.ErrAccountExists, http.StatusConflict, response.CodeConflict},
		{"号池不可用", service.ErrPoolUnavailable, http.StatusServiceUnavailable, response.CodePoolExhausted},
		{"上游登录态失效", service.ErrUpstreamUnauthorized, http.StatusBadGateway, response.CodeUpstreamUnauthorized},
		{"未知错误", errors.New("boom"), http.StatusInternalServerError, response.CodeInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newProblemEngine(stubProblemReader{err: tt.err}).
				ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problems/P1001", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if body := decodeBody(t, rec); body.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", body.Code, tt.wantCode)
			}
		})
	}
}

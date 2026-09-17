package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// stubRecordReader 提交记录读取替身
type stubRecordReader struct {
	result *service.RecordListDTO
	err    error

	gotUID    int
	gotPID    string
	gotStatus int
	gotPage   int
}

func (s *stubRecordReader) ListByUser(_ context.Context, uid int, pid string, status, page int) (*service.RecordListDTO, error) {
	s.gotUID, s.gotPID, s.gotStatus, s.gotPage = uid, pid, status, page
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func newRecordEngine(reader RecordReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewRecordHandler(reader)
	r.GET("/users/:uid/records", h.ListByUser)
	return r
}

// fakeRecordProvider 号池替身（用于把真实 service 接进 handler 测试）
type fakeRecordProvider struct {
	list      *client.RecordList
	err       error
	gotParams client.RecordListParams
}

func (f *fakeRecordProvider) ListRecords(_ context.Context, params client.RecordListParams) (*client.RecordList, error) {
	f.gotParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

// realRecordEngine 用真实 service 构建路由：参数校验与错误翻译都在 service 层，
// 只测 handler 替身是测不到的。
func realRecordEngine(provider *fakeRecordProvider) *gin.Engine {
	return newRecordEngine(service.NewRecordService(provider))
}

func TestRecordListByUserOK(t *testing.T) {
	provider := &fakeRecordProvider{list: &client.RecordList{
		Count: 178,
		Records: []client.RecordSummary{{
			ID:         101,
			Status:     client.RecordStatus(12),
			Score:      100,
			SubmitTime: 1750000000,
			Problem:    client.ProblemRef{PID: "P1001", Title: "A+B Problem"},
			User:       client.UserInfo{UID: 1582049, Name: "tester"},
		}},
	}}
	engine := realRecordEngine(provider)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/1582049/records?pid=p1001&status=12&page=9", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodeOK {
		t.Errorf("code = %d", body.Code)
	}

	want := client.RecordListParams{User: 1582049, Problem: "P1001", Status: client.RecordStatus(12), Page: 9}
	if provider.gotParams != want {
		t.Errorf("params = %+v, want %+v", provider.gotParams, want)
	}

	// 178 条、每页 20 → 9 页，第 9 页（末页）18 条
	text := rec.Body.String()
	for _, wantPart := range []string{
		"\"uid\":1582049", "\"pid\":\"P1001\"", "\"page\":9", "\"pageSize\":20",
		"\"totalPages\":9", "\"count\":178", "\"pageRecordCount\":18",
		"\"records\"", "\"name\":\"tester\"",
	} {
		if !strings.Contains(text, wantPart) {
			t.Errorf("响应缺少 %s: %s", wantPart, text)
		}
	}
}

// 只给 uid：pid/status 不过滤，page 默认第 1 页
func TestRecordListByUserDefaults(t *testing.T) {
	provider := &fakeRecordProvider{list: &client.RecordList{}}
	engine := realRecordEngine(provider)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/records", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if provider.gotParams.User != 42 || provider.gotParams.Page != 1 {
		t.Errorf("params = %+v", provider.gotParams)
	}
	if provider.gotParams.Problem != "" || provider.gotParams.Status != 0 {
		t.Errorf("未传的过滤条件不应生效: %+v", provider.gotParams)
	}
}

func TestRecordListByUserRejectsBadUID(t *testing.T) {
	for _, uid := range []string{"abc", "0", "-1", "2147483648"} {
		t.Run(uid, func(t *testing.T) {
			engine := realRecordEngine(&fakeRecordProvider{list: &client.RecordList{}})

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/"+uid+"/records", nil))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("uid=%s status = %d, want 400 (body=%s)", uid, rec.Code, rec.Body.String())
			}
			if body := decodeBody(t, rec); body.Code != response.CodeInvalidParam {
				t.Errorf("code = %d, want %d", body.Code, response.CodeInvalidParam)
			}
		})
	}
}

// status 写错时不能静默当成"不过滤"：那会返回一份看似正常、实际不对的数据
func TestRecordListByUserRejectsBadStatus(t *testing.T) {
	for _, status := range []string{"abc", "-1", "12.5"} {
		t.Run(status, func(t *testing.T) {
			engine := realRecordEngine(&fakeRecordProvider{list: &client.RecordList{}})

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/records?status="+status, nil))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%s 时 status = %d, want 400 (body=%s)", status, rec.Code, rec.Body.String())
			}
		})
	}
}

// 号池没有可用账号 → 503 + 专门的业务码
func TestRecordListByUserPoolExhaustedMapsTo503(t *testing.T) {
	engine := realRecordEngine(&fakeRecordProvider{
		err: fmt.Errorf("查询用户 42 的提交记录: %w", client.ErrPoolExhausted),
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/records", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodePoolExhausted {
		t.Errorf("code = %d, want %d", body.Code, response.CodePoolExhausted)
	}
}

// 全部账号登录态失效 → 502 + 专门的业务码
func TestRecordListByUserUpstreamUnauthorizedMapsTo502(t *testing.T) {
	engine := realRecordEngine(&fakeRecordProvider{
		err: &client.UnauthorizedError{StatusCode: 401, Message: "get record list"},
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/records", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body.Code != response.CodeUpstreamUnauthorized {
		t.Errorf("code = %d, want %d", body.Code, response.CodeUpstreamUnauthorized)
	}
}

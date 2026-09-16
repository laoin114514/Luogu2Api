package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	luogu "github.com/laoin114514/luoguClient"
)

// stubProblems 替换真实洛谷客户端，便于离线测试
type stubProblems struct {
	problem *luogu.Problem
	err     error
}

func (s stubProblems) Problem(pid string) (*luogu.Problem, error) {
	return s.problem, s.err
}

func newTestHandler(stub stubProblems) http.Handler {
	return New(stub, log.New(io.Discard, "", 0))
}

func doRequest(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := doRequest(t, newTestHandler(stubProblems{}), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v", body)
	}
}

func TestProblemOK(t *testing.T) {
	stub := stubProblems{problem: &luogu.Problem{
		PID:        "P1001",
		Title:      "A+B Problem",
		Difficulty: 1,
		Tags:       []int{1},
		Samples:    [][]string{{"1 2", "3"}},
		Limits:     luogu.ProblemLimits{Time: []int{1000}, Memory: []int{524288}},
	}}

	rec := doRequest(t, newTestHandler(stub), http.MethodGet, "/api/problem/P1001")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var dto ProblemDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dto.PID != "P1001" || dto.Title != "A+B Problem" || dto.Difficulty != 1 {
		t.Errorf("dto = %+v", dto)
	}
	if dto.TimeLimit != 1000 || dto.MemoryLimit != 524288 {
		t.Errorf("limits = %d/%d", dto.TimeLimit, dto.MemoryLimit)
	}
	if len(dto.Samples) != 1 || dto.Samples[0][0] != "1 2" {
		t.Errorf("samples = %v", dto.Samples)
	}
}

func TestProblemErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "未登录映射 401",
			err:        &luogu.UnauthorizedError{StatusCode: http.StatusUnauthorized, Message: "get problem P1001"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "题目不存在映射 404",
			err:        errors.New("get problem P999999: status 404"),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "其它错误映射 502",
			err:        errors.New("boom"),
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(stubProblems{err: tt.err}), http.MethodGet, "/api/problem/P1001")

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if body["error"] == "" {
				t.Errorf("error message should not be empty: %s", rec.Body.String())
			}
		})
	}
}

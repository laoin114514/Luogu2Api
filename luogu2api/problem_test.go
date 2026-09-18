package luogu2api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// problemFixture 是 GET /api/v1/problems/:pid 的真实响应形状（题面字段名沿用洛谷的 contenu）
const problemFixture = `{
  "code": 0,
  "message": "ok",
  "data": {
    "pid": "P1001",
    "name": "A+B Problem",
    "difficulty": 1,
    "tags": [1, 2],
    "samples": [["1 2", "3"]],
    "limits": {"time": [1000], "memory": [131072]},
    "provider": {"uid": 2, "name": "root", "avatar": "https://cdn.luogu.com.cn/avatar.png"},
    "contenu": {
      "description": "输入两个整数 a,b，输出它们的和。",
      "formatI": "两个整数",
      "formatO": "一个整数",
      "hint": "无",
      "background": ""
    }
  }
}`

func TestProblemGet(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/problems/P1001" {
			t.Errorf("请求 = %s %s", r.Method, r.URL.Path)
		}
		writeRaw(w, http.StatusOK, problemFixture)
	})

	// pid 两端有空白：应当本地去掉后再拼 URL
	problem, err := sdk.Problem.Get(context.Background(), " P1001 ")
	if err != nil {
		t.Fatalf("Problem.Get: %v", err)
	}
	if problem.PID != "P1001" || problem.Title != "A+B Problem" || problem.Difficulty != 1 {
		t.Errorf("题目基本信息 = %+v", problem)
	}
	if len(problem.Tags) != 2 || len(problem.Samples) != 1 || problem.Samples[0][0] != "1 2" || problem.Samples[0][1] != "3" {
		t.Errorf("tags/samples = %+v / %+v", problem.Tags, problem.Samples)
	}
	if len(problem.Limits.Time) != 1 || problem.Limits.Time[0] != 1000 ||
		len(problem.Limits.Memory) != 1 || problem.Limits.Memory[0] != 131072 {
		t.Errorf("limits = %+v", problem.Limits)
	}
	if problem.Provider.UID != 2 || problem.Provider.Name != "root" {
		t.Errorf("provider = %+v", problem.Provider)
	}
	if !strings.Contains(problem.Content.Description, "输入两个整数") || problem.Content.InputFormat != "两个整数" {
		t.Errorf("content = %+v", problem.Content)
	}
}

func TestProblemGetRejectsEmptyPID(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("本地校验失败时不应发出请求: %s", r.URL)
	})

	if _, err := sdk.Problem.Get(context.Background(), "   "); !IsInvalidParam(err) {
		t.Errorf("err = %v, want 参数错误", err)
	}
}

// problemSearchFixture 同样是真实形状：服务端把上游 SDK 的结构体（没有 JSON 标签）
// 直接序列化，字段名是大写的 Problems/Total/Page/PerPage。
const problemSearchFixture = `{
  "code": 0,
  "message": "ok",
  "data": {
    "Problems": [
      {"pid": "P1001", "name": "A+B Problem", "difficulty": 1, "tags": [1]}
    ],
    "Total": 1,
    "Page": 1,
    "PerPage": 20
  }
}`

func TestProblemSearch(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r)
		if r.URL.Path != "/api/v1/problems" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("keyword") != "A+B" {
			t.Errorf("keyword = %q", q.Get("keyword"))
		}
		if q.Get("page") != "2" || q.Get("pageSize") != "30" {
			t.Errorf("page/pageSize = %q/%q", q.Get("page"), q.Get("pageSize"))
		}
		writeRaw(w, http.StatusOK, problemSearchFixture)
	})

	result, err := sdk.Problem.Search(context.Background(), SearchParams{Keyword: " A+B ", Page: 2, PageSize: 30})
	if err != nil {
		t.Fatalf("Problem.Search: %v", err)
	}
	if result.Total != 1 || result.Page != 1 || result.PerPage != 20 {
		t.Errorf("分页 = %+v", result)
	}
	if len(result.Problems) != 1 || result.Problems[0].PID != "P1001" || result.Problems[0].Title != "A+B Problem" {
		t.Errorf("problems = %+v", result.Problems)
	}
}

// 分页参数没传时不要发空参数，让服务端用它自己的默认值（page=1、pageSize=20）
func TestProblemSearchOmitsZeroPaging(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if len(q) != 1 || q.Get("keyword") != "排序" {
			t.Errorf("query = %v, want 只有 keyword", q)
		}
		writeRaw(w, http.StatusOK, problemSearchFixture)
	})

	if _, err := sdk.Problem.Search(context.Background(), SearchParams{Keyword: "排序"}); err != nil {
		t.Fatalf("Problem.Search: %v", err)
	}
}

func TestProblemSearchRejectsEmptyKeyword(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("本地校验失败时不应发出请求: %s", r.URL)
	})

	if _, err := sdk.Problem.Search(context.Background(), SearchParams{Keyword: " "}); !IsInvalidParam(err) {
		t.Errorf("err = %v, want 参数错误", err)
	}
}

// 号池不可用（1001）与上游登录态失效（1002）要能被区分：这是调用方决定"重试还是报警"的依据
func TestProblemErrorsAreDistinguishable(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		code       int
		is         func(error) bool
	}{
		{"号池没有可用账号", http.StatusServiceUnavailable, CodePoolExhausted, IsPoolExhausted},
		{"全部账号登录态失效", http.StatusBadGateway, CodeUpstreamUnauthorized, IsUpstreamUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
				writeEnvelope(t, w, tt.httpStatus, tt.code, tt.name, nil)
			})

			if _, err := sdk.Problem.Get(context.Background(), "P1001"); !tt.is(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

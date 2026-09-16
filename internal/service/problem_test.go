package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/laoin114514/luogu2api/internal/client"
)

type fakeProblemPool struct {
	problem   *client.Problem
	result    *client.SearchResult
	err       error
	gotPid    string
	gotParams client.SearchParams
}

func (p *fakeProblemPool) GetProblem(_ context.Context, pid string) (*client.Problem, error) {
	p.gotPid = pid
	if p.err != nil {
		return nil, p.err
	}
	return p.problem, nil
}

func (p *fakeProblemPool) SearchProblems(_ context.Context, params client.SearchParams) (*client.SearchResult, error) {
	p.gotParams = params
	if p.err != nil {
		return nil, p.err
	}
	return p.result, nil
}

func TestProblemGetNormalizesPID(t *testing.T) {
	pool := &fakeProblemPool{problem: &client.Problem{PID: "P1001", Title: "A+B Problem"}}
	svc := NewProblemService(pool)

	problem, err := svc.Get(context.Background(), " p1001 ")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pool.gotPid != "P1001" {
		t.Errorf("传给号池的 pid = %q, want P1001", pool.gotPid)
	}
	if problem.Title != "A+B Problem" {
		t.Errorf("problem = %+v", problem)
	}
}

func TestProblemGetRejectsBadPID(t *testing.T) {
	svc := NewProblemService(&fakeProblemPool{})

	for _, pid := range []string{"", "   ", "P1001P1001P1001P1001P1001P1001P1001P1001P"} {
		if _, err := svc.Get(context.Background(), pid); !errors.Is(err, ErrInvalidParam) {
			t.Errorf("pid=%q err=%v, want ErrInvalidParam", pid, err)
		}
	}
}

func TestProblemGetTranslatesUpstreamErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "号池无可用账号",
			err:  fmt.Errorf("获取题目 P1001: %w", client.ErrPoolExhausted),
			want: ErrPoolUnavailable,
		},
		{
			name: "洛谷登录态全部失效",
			err:  fmt.Errorf("获取题目 P1001: %w", &client.UnauthorizedError{StatusCode: 401}),
			want: ErrUpstreamUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewProblemService(&fakeProblemPool{err: tt.err})

			_, err := svc.Get(context.Background(), "P1001")
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestProblemGetPassesThroughUnknownErrors(t *testing.T) {
	boom := errors.New("lentille-context script not found in page")
	svc := NewProblemService(&fakeProblemPool{err: boom})

	_, err := svc.Get(context.Background(), "P1001")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if errors.Is(err, ErrPoolUnavailable) {
		t.Error("未知错误不应被误判为号池不可用")
	}
}

func TestProblemSearchRejectsEmptyKeyword(t *testing.T) {
	svc := NewProblemService(&fakeProblemPool{})

	if _, err := svc.Search(context.Background(), "  ", 1, 20); !errors.Is(err, ErrInvalidParam) {
		t.Errorf("err = %v, want ErrInvalidParam", err)
	}
}

func TestProblemSearchRejectsOversizedPageSize(t *testing.T) {
	svc := NewProblemService(&fakeProblemPool{})

	if _, err := svc.Search(context.Background(), "排序", 1, 101); !errors.Is(err, ErrInvalidParam) {
		t.Errorf("err = %v, want ErrInvalidParam", err)
	}
}

func TestProblemSearchAppliesDefaults(t *testing.T) {
	pool := &fakeProblemPool{result: &client.SearchResult{Total: 3}}
	svc := NewProblemService(pool)

	if _, err := svc.Search(context.Background(), "排序", 0, 0); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if pool.gotParams.Page != 1 || pool.gotParams.PageSize != 20 {
		t.Errorf("params = %+v, want page=1 pageSize=20", pool.gotParams)
	}
	if pool.gotParams.Keyword != "排序" {
		t.Errorf("keyword = %q", pool.gotParams.Keyword)
	}
}

package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/laoin114514/luogu2api/internal/client"
)

// ProblemProvider 题目读取能力（由 client.Pool 实现）
type ProblemProvider interface {
	GetProblem(ctx context.Context, pid string) (*client.Problem, error)
	SearchProblems(ctx context.Context, params client.SearchParams) (*client.SearchResult, error)
}

// ProblemService 题目业务：把号池的上游错误翻译成业务语义
type ProblemService struct {
	pool ProblemProvider
}

// NewProblemService 创建题目服务
func NewProblemService(pool ProblemProvider) *ProblemService {
	return &ProblemService{pool: pool}
}

// Get 获取题目详情
func (s *ProblemService) Get(ctx context.Context, pid string) (*client.Problem, error) {
	pid = strings.ToUpper(strings.TrimSpace(pid))
	if pid == "" || len(pid) > 32 {
		return nil, fmt.Errorf("%w: 题目编号不合法", ErrInvalidParam)
	}

	problem, err := s.pool.GetProblem(ctx, pid)
	if err != nil {
		return nil, translateUpstream(err)
	}
	return problem, nil
}

// Search 搜索题目
func (s *ProblemService) Search(ctx context.Context, keyword string, page, pageSize int) (*client.SearchResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, fmt.Errorf("%w: keyword 不能为空", ErrInvalidParam)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return nil, fmt.Errorf("%w: pageSize 不能超过 100", ErrInvalidParam)
	}

	result, err := s.pool.SearchProblems(ctx, client.SearchParams{
		Keyword:  keyword,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, translateUpstream(err)
	}
	return result, nil
}

// translateUpstream 把号池错误翻译成 service 层哨兵错误
func translateUpstream(err error) error {
	switch {
	case err == nil:
		return nil
	case client.IsPoolExhausted(err):
		return fmt.Errorf("%w: %v", ErrPoolUnavailable, err)
	case client.IsUnauthorized(err):
		return fmt.Errorf("%w: %v", ErrUpstreamUnauthorized, err)
	default:
		return err
	}
}

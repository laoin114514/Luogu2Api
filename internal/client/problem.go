package client

import (
	"context"
	"fmt"

	sdk "github.com/laoin114514/luoguClient"
)

// 类型别名：让 service/handler 层无需直接 import SDK 就能消费返回数据。
//
// 返回值形状来自 SDK（洛谷页面结构的映射）；若将来要把 SDK 完全隔离，
// 只需在这一层改成自定义 DTO 再映射，调用方不用动。
type (
	// Problem 题目详情
	Problem = sdk.Problem
	// ProblemSummary 题目摘要
	ProblemSummary = sdk.ProblemSummary
	// SearchParams 题目搜索参数
	SearchParams = sdk.SearchParams
	// SearchResult 题目搜索结果
	SearchResult = sdk.SearchResult

	// UnauthorizedError 洛谷未授权（登录态失效）
	UnauthorizedError = sdk.UnauthorizedError
	// AuthError 洛谷认证/凭据错误
	AuthError = sdk.AuthError
	// NetworkError 网络错误
	NetworkError = sdk.NetworkError
)

// GetProblem 获取题目详情（自动从号池选号；命中失效 cookie 会换号重试）
func (p *Pool) GetProblem(ctx context.Context, pid string) (*Problem, error) {
	var out *Problem
	err := p.withSession(ctx, fmt.Sprintf("获取题目 %s", pid), func(c SessionClient) error {
		prob, err := c.SDK().Problem.Get(pid)
		if err != nil {
			return err
		}
		out = prob
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SearchProblems 搜索题目
func (p *Pool) SearchProblems(ctx context.Context, params SearchParams) (*SearchResult, error) {
	var out *SearchResult
	err := p.withSession(ctx, fmt.Sprintf("搜索题目 %q", params.Keyword), func(c SessionClient) error {
		res, err := c.SDK().Problem.Search(params)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

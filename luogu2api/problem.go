package luogu2api

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// ProblemService 题目接口（只读）
type ProblemService struct {
	client *Client
}

// SearchParams 题目搜索参数
type SearchParams struct {
	// Keyword 搜索关键词，必填（空白会被本地拦下，不发请求）
	Keyword string
	// Page 页码，从 1 开始；<=0 时服务端按 1 处理
	Page int
	// PageSize 每页条数；<=0 时服务端按 20 处理，上限 100（超出服务端返回 400）
	PageSize int
}

// Get 获取题目详情（GET /api/v1/problems/:pid）。
//
// 服务端会从号池选一个账号去洛谷取题；命中失效 cookie 会自动换号重试，
// 号池没有可用账号时返回 *Error（IsPoolExhausted），全部账号登录态失效时
// 返回 *Error（IsUpstreamUnauthorized）。
func (s *ProblemService) Get(ctx context.Context, pid string) (*Problem, error) {
	pid = strings.TrimSpace(pid)
	if pid == "" {
		return nil, newInvalidParam("pid 不能为空")
	}

	var out Problem
	if err := s.client.getData(ctx, "/api/v1/problems/"+url.PathEscape(pid), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Search 搜索题目（GET /api/v1/problems?keyword=&page=&pageSize=）。
//
// 本页题目与总数在 SearchResult 里；分页参数不合法时返回 *Error（IsInvalidParam）。
func (s *ProblemService) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	keyword := strings.TrimSpace(params.Keyword)
	if keyword == "" {
		return nil, newInvalidParam("keyword 不能为空")
	}

	q := url.Values{}
	q.Set("keyword", keyword)
	// page/pageSize 交给服务端取默认值：客户端只在显式传了正数时才发送
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(params.PageSize))
	}

	var out SearchResult
	if err := s.client.getData(ctx, "/api/v1/problems", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

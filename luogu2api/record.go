package luogu2api

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// RecordService 提交记录接口（只读）
type RecordService struct {
	client *Client
}

// RecordListParams 提交记录查询参数
type RecordListParams struct {
	// PID 题目编号过滤（空表示不按题目过滤；大小写由服务端统一成大写）
	PID string
	// Status 状态过滤（0 表示全部，其余见 RecordStatus* 常量；负数会被本地拦下）
	Status RecordStatus
	// Page 页码，从 1 开始；<=0 时服务端按 1 处理
	Page int
}

// ListByUser 查询某个洛谷用户的提交记录（GET /api/v1/users/:uid/records）。
//
// 记录接口需要登录态：服务端会从号池选号、命中失效 cookie 自动换号重试，
// 因此没有可用账号时返回 *Error（IsPoolExhausted），全部账号登录态失效时返回
// *Error（IsUpstreamUnauthorized）。
//
// 返回值里的 totalPages / pageRecordCount 由服务端算好，调用方不必自己数。
func (s *RecordService) ListByUser(ctx context.Context, uid int, params RecordListParams) (*RecordPage, error) {
	if uid <= 0 {
		return nil, newInvalidParam("uid 必须是正整数")
	}
	if params.Status < 0 {
		return nil, newInvalidParam("status 不能为负数")
	}

	q := url.Values{}
	if pid := strings.TrimSpace(params.PID); pid != "" {
		q.Set("pid", pid)
	}
	if params.Status > 0 {
		q.Set("status", strconv.Itoa(int(params.Status)))
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}

	var out RecordPage
	if err := s.client.getData(ctx, "/api/v1/users/"+strconv.Itoa(uid)+"/records", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

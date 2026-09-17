package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/laoin114514/luogu2api/internal/client"
)

// RecordProvider 提交记录读取能力（由 client.Pool 实现）
type RecordProvider interface {
	ListRecords(ctx context.Context, params client.RecordListParams) (*client.RecordList, error)
}

// RecordListDTO 提交记录列表（对外视图）。
//
// 分页信息由总数与页码推算（每页条数见 client.RecordListPageSize）：
// count=178、page=9 时 totalPages=9、pageRecordCount=18。
//
// SDK 的 RecordList 没有 JSON 标签（直接序列化会输出 Records/Count 这样的大写
// 字段名），这里显式定义响应形状，并回显实际生效的 uid/pid/status/page，
// 便于调用方确认这一页是在什么过滤条件下取的。
type RecordListDTO struct {
	UID     int    `json:"uid"`
	PID     string `json:"pid,omitempty"`
	Status  int    `json:"status,omitempty"`
	Page    int    `json:"page"`
	// PageSize 每页条数（洛谷固定值，与 SDK 的 perPage 一致）
	PageSize int `json:"pageSize"`
	// TotalPages 总页数 = ceil(count/pageSize)；count 为 0 时是 0
	TotalPages int `json:"totalPages"`
	// Count 符合条件的记录总数（不是本页条数）
	Count int `json:"count"`
	// PageRecordCount 本页条数：最后一页可能不满，页码超出总页数时为 0
	PageRecordCount int                    `json:"pageRecordCount"`
	Records         []client.RecordSummary `json:"records"`
}

// RecordService 提交记录业务：把号池的上游错误翻译成业务语义
type RecordService struct {
	pool RecordProvider
}

// NewRecordService 创建提交记录服务
func NewRecordService(pool RecordProvider) *RecordService {
	return &RecordService{pool: pool}
}

// ListByUser 获取某个洛谷用户（uid）的提交记录。
//
// pid 为空表示不按题目过滤；status 为 0 表示不按状态过滤；page 从 1 开始。
// 返回值里的 totalPages / pageRecordCount 由 count 与 page 推算，调用方不必自己算。
//
// 记录接口需要登录态：号池会自动选号，cookie 失效时换号重试；没有可用账号
// 返回 ErrPoolUnavailable，所有账号登录态都失效返回 ErrUpstreamUnauthorized。
func (s *RecordService) ListByUser(ctx context.Context, uid int, pid string, status, page int) (*RecordListDTO, error) {
	if uid <= 0 {
		return nil, fmt.Errorf("%w: uid 必须是正整数", ErrInvalidParam)
	}
	if status < 0 {
		return nil, fmt.Errorf("%w: status 不能为负数", ErrInvalidParam)
	}

	pid = strings.ToUpper(strings.TrimSpace(pid))
	if len(pid) > 32 {
		return nil, fmt.Errorf("%w: 题目编号不合法", ErrInvalidParam)
	}
	if page <= 0 {
		page = 1
	}

	list, err := s.pool.ListRecords(ctx, client.RecordListParams{
		User:    uid,
		Problem: pid,
		Status:  client.RecordStatus(status),
		Page:    page,
	})
	if err != nil {
		return nil, translateUpstream(err)
	}

	totalPages, pageRecordCount := recordPage(list.Count, page)

	return &RecordListDTO{
		UID:             uid,
		PID:             pid,
		Status:          status,
		Page:            page,
		PageSize:        client.RecordListPageSize,
		TotalPages:      totalPages,
		Count:           list.Count,
		PageRecordCount: pageRecordCount,
		Records:         list.Records,
	}, nil
}

// recordPage 由记录总数与页码推算分页信息：
// totalPages = ceil(count / 每页条数)，pageRecordCount = 本页条数。
//
// 只有最后一页可能不满：count=178、page=9 时总页数 9、本页 18 条；
// 页码超出总页数（或根本没有记录）时本页条数为 0，count 为 0 时总页数为 0。
func recordPage(count, page int) (totalPages, pageRecordCount int) {
	per := client.RecordListPageSize
	if count <= 0 || per <= 0 {
		return 0, 0
	}

	totalPages = (count + per - 1) / per

	remain := count - (page-1)*per
	switch {
	case remain <= 0:
		return totalPages, 0
	case remain > per:
		return totalPages, per
	default:
		return totalPages, remain
	}
}

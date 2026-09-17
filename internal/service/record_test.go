package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/laoin114514/luogu2api/internal/client"
)

type fakeRecordPool struct {
	list      *client.RecordList
	err       error
	gotParams client.RecordListParams
}

func (p *fakeRecordPool) ListRecords(_ context.Context, params client.RecordListParams) (*client.RecordList, error) {
	p.gotParams = params
	if p.err != nil {
		return nil, p.err
	}
	return p.list, nil
}

func TestRecordListByUserPassesNormalizedFilters(t *testing.T) {
	pool := &fakeRecordPool{list: &client.RecordList{
		Count: 42,
		Records: []client.RecordSummary{{
			ID:      101,
			Status:  client.RecordStatus(12),
			Problem: client.ProblemRef{PID: "P1001", Title: "A+B Problem"},
			User:    client.UserInfo{UID: 1582049, Name: "tester"},
		}},
	}}
	svc := NewRecordService(pool)

	got, err := svc.ListByUser(context.Background(), 1582049, " p1001 ", 12, 2)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}

	want := client.RecordListParams{User: 1582049, Problem: "P1001", Status: client.RecordStatus(12), Page: 2}
	if pool.gotParams != want {
		t.Errorf("传给号池的参数 = %+v, want %+v", pool.gotParams, want)
	}
	if got.UID != 1582049 || got.Page != 2 || got.Count != 42 || len(got.Records) != 1 {
		t.Errorf("dto = %+v", got)
	}
	if got.PID != "P1001" || got.Status != 12 {
		t.Errorf("回显的过滤条件 = pid %q status %d, want P1001/12", got.PID, got.Status)
	}
	// 42 条、每页 20 → 3 页；第 2 页满页
	if got.PageSize != 20 || got.TotalPages != 3 || got.PageRecordCount != 20 {
		t.Errorf("分页字段 = pageSize %d totalPages %d pageRecordCount %d, want 20/3/20",
			got.PageSize, got.TotalPages, got.PageRecordCount)
	}
}

// 不传 pid/status 时就是"不过滤"，不该伪造出过滤条件
func TestRecordListByUserWithoutFilters(t *testing.T) {
	pool := &fakeRecordPool{list: &client.RecordList{}}
	svc := NewRecordService(pool)

	got, err := svc.ListByUser(context.Background(), 42, "   ", 0, 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if pool.gotParams.Problem != "" || pool.gotParams.Status != 0 {
		t.Errorf("params = %+v, want 无过滤", pool.gotParams)
	}
	if pool.gotParams.Page != 1 {
		t.Errorf("page = %d, want 1（默认第一页）", pool.gotParams.Page)
	}
	if got.PID != "" || got.Status != 0 || got.Page != 1 {
		t.Errorf("dto = %+v", got)
	}
	// 一条记录都没有：总页数 0、本页 0 条
	if got.TotalPages != 0 || got.PageRecordCount != 0 {
		t.Errorf("空结果的分页字段 = totalPages %d pageRecordCount %d, want 0/0",
			got.TotalPages, got.PageRecordCount)
	}
}

// 分页推算：总页数 = ceil(count/pageSize)，本页条数在最后一页可能不满
func TestRecordPageMath(t *testing.T) {
	tests := []struct {
		name            string
		count           int
		page            int
		wantTotalPages  int
		wantPageRecords int
	}{
		{"首页满页", 178, 1, 9, 20},
		{"中间页满页", 178, 8, 9, 20},
		{"末页不满", 178, 9, 9, 18},
		{"整除时末页也是满页", 180, 9, 9, 20},
		{"整除再多一条", 181, 10, 10, 1},
		{"不足一页", 7, 1, 1, 7},
		{"恰好一页", 20, 1, 1, 20},
		{"一条记录", 1, 1, 1, 1},
		{"没有记录", 0, 1, 0, 0},
		{"页码超出总页数", 178, 10, 9, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalPages, pageRecords := recordPage(tt.count, tt.page)
			if totalPages != tt.wantTotalPages || pageRecords != tt.wantPageRecords {
				t.Errorf("recordPage(%d, %d) = (%d, %d), want (%d, %d)",
					tt.count, tt.page, totalPages, pageRecords, tt.wantTotalPages, tt.wantPageRecords)
			}
		})
	}
}

func TestRecordListByUserRejectsBadParams(t *testing.T) {
	tests := []struct {
		name   string
		uid    int
		pid    string
		status int
	}{
		{"uid 为 0", 0, "", 0},
		{"uid 为负", -1, "", 0},
		{"status 为负", 42, "", -1},
		{"pid 过长", 42, "P1001P1001P1001P1001P1001P1001P1001P1001P", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewRecordService(&fakeRecordPool{})
			if _, err := svc.ListByUser(context.Background(), tt.uid, tt.pid, tt.status, 1); !errors.Is(err, ErrInvalidParam) {
				t.Errorf("err = %v, want ErrInvalidParam", err)
			}
		})
	}
}

func TestRecordListByUserTranslatesUpstreamErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "号池无可用账号",
			err:  fmt.Errorf("查询用户 42 的提交记录: %w", client.ErrPoolExhausted),
			want: ErrPoolUnavailable,
		},
		{
			name: "洛谷登录态全部失效",
			err:  fmt.Errorf("查询用户 42 的提交记录: %w", &client.UnauthorizedError{StatusCode: 401}),
			want: ErrUpstreamUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewRecordService(&fakeRecordPool{err: tt.err})

			_, err := svc.ListByUser(context.Background(), 42, "", 0, 1)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRecordListByUserPassesThroughUnknownErrors(t *testing.T) {
	boom := errors.New("lentille-context script not found in page")
	svc := NewRecordService(&fakeRecordPool{err: boom})

	_, err := svc.ListByUser(context.Background(), 42, "", 0, 1)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if errors.Is(err, ErrPoolUnavailable) {
		t.Error("未知错误不应被误判为号池不可用")
	}
}

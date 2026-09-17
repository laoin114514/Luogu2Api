package client

import (
	"context"
	"fmt"

	sdk "github.com/laoin114514/luoguClient"
)

// 类型别名：让 service/handler 层无需直接 import SDK 就能消费返回数据。
//
// 与 problem.go 同样的取舍：返回值形状来自 SDK（洛谷页面结构的映射）；
// 若将来要把 SDK 完全隔离，只需在这一层改成自定义 DTO 再映射。
type (
	// RecordListParams 提交记录查询参数
	RecordListParams = sdk.RecordListParams
	// RecordList 提交记录列表
	RecordList = sdk.RecordList
	// RecordSummary 单条提交记录摘要
	RecordSummary = sdk.RecordSummary
	// RecordStatus 提交状态（StatusAccepted / StatusUnaccepted 等）
	RecordStatus = sdk.RecordStatus
	// ProblemRef 记录中的题目引用
	ProblemRef = sdk.ProblemRef
	// UserInfo 用户信息（记录中的提交者 / 题目提供者等共用）
	UserInfo = sdk.UserInfo
)

// RecordListPageSize 洛谷记录列表每页条数。
//
// 洛谷 /record/list 按固定页大小分页（SDK 请求里不带 perPage 参数，服务端用默认值），
// 实测与 SDK 测试夹具里的 perPage 都是 20。
//
// SDK 的 RecordList 只带出 Records/Count，响应里的 perPage 被解析后丢弃，因此
// service 层的分页计算（总页数 / 本页条数）以这个常量作为除数。洛谷若调整默认
// 页大小，这里与 README 的接口说明需要同步更新。
const RecordListPageSize = 20

// ListRecords 查询提交记录（自动从号池选号；命中失效 cookie 会换号重试）
//
// 记录接口需要登录态，未登录时洛谷返回 401，号池会据此换号重试。
func (p *Pool) ListRecords(ctx context.Context, params RecordListParams) (*RecordList, error) {
	var out *RecordList
	err := p.withSession(ctx, recordOp(params), func(c SessionClient) error {
		list, err := c.SDK().Record.GetList(params)
		if err != nil {
			return err
		}
		out = list
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// recordOp 生成日志/错误里的操作描述（User 为 0 表示不按用户过滤）
func recordOp(params RecordListParams) string {
	if params.User > 0 {
		return fmt.Sprintf("查询用户 %d 的提交记录", params.User)
	}
	return "查询提交记录"
}

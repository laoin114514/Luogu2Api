package luogu2api

import "context"

// HealthService 探活接口。
//
// 这两个接口在服务端不需要令牌，SDK 照样会带上 X-Admin-Token（服务端忽略），
// 因此可以用同一个 Client 完成"探活 + 业务调用"。
type HealthService struct {
	client *Client
}

// Check 请求 /healthz（就绪探针）。
//
// 与服务端约定一致：数据库不可用或号池没有在线账号时，服务端返回 HTTP 503，同时 data
// 仍是一份完整报告（Status 为 StatusDegraded）——这不是错误，调用方拿到的就是"不健康"
// 这个事实本身。只有传输失败、响应不合法、业务码非 0 才会返回 error。
func (s *HealthService) Check(ctx context.Context) (*HealthReport, error) {
	var out HealthReport
	if err := s.client.getProbe(ctx, "/healthz", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Live 请求 /livez（存活探针）。
//
// 进程还能处理 HTTP 就返回 StatusOK，不检查数据库与号池；容器 HEALTHCHECK 用它。
func (s *HealthService) Live(ctx context.Context) (*LiveStatus, error) {
	var out LiveStatus
	if err := s.client.getProbe(ctx, "/livez", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

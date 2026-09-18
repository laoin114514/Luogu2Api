package luogu2api

import "context"

// PoolService 号池只读接口
type PoolService struct {
	client *Client
}

// Status 获取号池快照（GET /api/v1/pool/status）。
//
// 服务端返回内存快照（在线/待重登/失败/停用/封禁数与最近一轮扫描统计），
// 不含任何账号凭据，适合做监控与告警。
func (s *PoolService) Status(ctx context.Context) (*PoolStatus, error) {
	var out PoolStatus
	if err := s.client.getData(ctx, "/api/v1/pool/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

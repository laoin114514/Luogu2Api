package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// HealthRepository 提供数据库连通性检查
type HealthRepository struct {
	db *gorm.DB
}

// NewHealthRepository 创建健康检查仓储
func NewHealthRepository(db *gorm.DB) *HealthRepository {
	return &HealthRepository{db: db}
}

// Ping 检查数据库连通性（调用方应带上超时 context）
func (r *HealthRepository) Ping(ctx context.Context) error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return fmt.Errorf("获取底层连接失败: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("数据库 ping 失败: %w", err)
	}
	return nil
}

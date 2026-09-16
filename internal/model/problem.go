// Package model 定义数据库实体（GORM Model）与表结构映射。
//
// 该包只放结构体与表名约定，不写任何业务逻辑与查询。
package model

import "time"

// Problem 洛谷题目缓存。
//
// 示例实体：演示表结构约定（主键、唯一索引、注释、时间戳）。
// 注意：GORM 会把 Go 的 int 映射成 bigint，需要窄类型时用 int32/int16 等明确宽度。
// 目前接口尚未暴露，按业务需要增删即可。
type Problem struct {
	ID         uint      `gorm:"column:id;primaryKey;autoIncrement"`
	PID        string    `gorm:"column:pid;type:varchar(32);not null;uniqueIndex:uk_pid;comment:题目编号"`
	Title      string    `gorm:"column:title;type:varchar(255);not null;default:'';comment:题目标题"`
	Difficulty int32     `gorm:"column:difficulty;not null;default:0;comment:难度（1-7）"`
	FetchedAt  time.Time `gorm:"column:fetched_at;comment:最近一次抓取时间"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

// TableName 指定表名（默认会变成复数 problems，这里显式声明以便阅读）
func (Problem) TableName() string { return "problems" }

// All 返回需要 AutoMigrate 的全部实体，作为迁移的唯一来源
func All() []interface{} {
	return []interface{}{
		&Problem{},
	}
}

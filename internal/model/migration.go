package model

import "time"

// SchemaMigration 库结构变更的审计记录（internal/schema 每次实际执行了变更、
// 或发现需要人工处理的差异时追加一行）。
//
// 它不是"迁移脚本的版本号"：本项目的结构来源始终是 model 包里的模型本身，
// 一致性由"模型 vs 库现状"现场比对得出（见 internal/schema 的包注释）。这张表
// 回答的是另一个问题——"这个库经历过哪些结构变更、什么时候、当时的形态是什么"，
// 用于回溯（例如线上多了个列，能查到是哪次部署带出来的）。
type SchemaMigration struct {
	ID uint `gorm:"column:id;primaryKey;autoIncrement"`

	// CodeHash / DBHash 是 Describe / Inspect 得到的形态指纹：前者来自模型，
	// 后者来自库。两者不同就说明这次检查发现了差异。
	CodeHash string `gorm:"column:code_hash;type:char(64);not null;comment:模型形态指纹"`
	DBHash   string `gorm:"column:db_hash;type:char(64);not null;comment:检查时的库形态指纹"`

	// Applied / Pending 是人类可读的摘要，每行一条（分别为自动执行的变更与
	// 需要人工处理的差异）。刻意存文本而不是结构化 JSON：这两列只用于回溯阅读。
	Applied string `gorm:"column:applied;type:text;comment:自动执行的变更（每行一条）"`
	Pending string `gorm:"column:pending;type:text;comment:需要人工处理的差异（每行一条）"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 指定表名
func (SchemaMigration) TableName() string { return "schema_migrations" }

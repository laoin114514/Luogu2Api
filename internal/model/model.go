package model

// All 返回需要检查/迁移的全部实体，作为库结构的唯一来源。
//
// 它同时被 internal/schema 用于"模型形态 vs 库现状"的比对与自动建表/加列，
// repository 的集成测试也用它建表；新增表时在这里登记一行即可。
func All() []interface{} {
	return []interface{}{
		&Account{},
		&SchemaMigration{},
	}
}

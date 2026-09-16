package model

// All 返回需要 AutoMigrate 的全部实体，作为迁移的唯一来源。
//
// 新增表时在这里登记一行即可（repository.Migrate 直接消费它）。
func All() []interface{} {
	return []interface{}{
		&Account{},
	}
}

// Package schema 负责数据库表结构的版本管理与自动迁移。
//
// 这里刻意没有任何手写的迁移 SQL 文件：**结构的唯一来源是 model 包里的 GORM 模型**。
// 一次检查分三步：
//
//  1. Describe：由模型 + 当前 dialector 推导出"期望形态"（表 / 列 / 索引）；
//  2. Inspect：从 information_schema 读回"实际形态"；
//  3. Diff：两者相减，差异分成两类——
//     · 安全（建表 / 加列 / 建索引）：不会丢数据，Apply 时自动执行；
//     · 需人工确认（库里多出来的列或索引，或列的类型 / 可空性 / 自增 / 默认值不一致）：
//     可能是收窄丢数据，也可能是别人手工加的列，**永不自动执行**，只报告并给出
//     可以直接复制的建议 SQL。
//
// 一致性判定始终是"模型 vs 库现状"，所以手工执行的 DDL 不需要在代码里登记——
// 改完自然就一致了。每次真正执行了变更、或发现了需人工处理的差异，就往
// schema_migrations 追加一行审计记录（模型指纹 + 库指纹 + 摘要）。
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// ColumnShape 一列的形态。
//
// Type 是归一化后的类型（比较用），Definition 是 dialector 渲染出的完整定义
// （只有模型侧有，用于给出建议 SQL）。
type ColumnShape struct {
	Name          string
	Type          string
	Definition    string
	NotNull       bool
	PrimaryKey    bool
	AutoIncrement bool
	HasDefault    bool
}

// IndexShape 一个索引的形态
type IndexShape struct {
	Name    string
	Unique  bool
	Columns []string
}

// TableShape 一张表的形态（列与索引都按名字排序，保证指纹稳定）
type TableShape struct {
	Name    string
	Columns []ColumnShape
	Indexes []IndexShape

	// model 是该表对应的 GORM 模型，自动建表/加列/建索引时需要它
	model interface{}
}

// Shape 一套表结构。模型期望的形态与库里实际的形态用同一个结构表达，
// 于是"比较"就是普通的结构相减。
type Shape struct {
	Hash   string
	Tables []TableShape
}

func (s *Shape) table(name string) *TableShape {
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	return nil
}

func (t *TableShape) column(name string) *ColumnShape {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

func (t *TableShape) index(name string) *IndexShape {
	for i := range t.Indexes {
		if t.Indexes[i].Name == name {
			return &t.Indexes[i]
		}
	}
	return nil
}

// Describe 由模型推导期望的库结构（不连库，只读模型元数据 + dialector 的类型映射）。
func Describe(db *gorm.DB, models ...interface{}) (*Shape, error) {
	shape := &Shape{}

	for _, m := range models {
		stmt, err := parseModel(db, m)
		if err != nil {
			return nil, fmt.Errorf("解析模型 %T 失败: %w", m, err)
		}

		table := TableShape{Name: stmt.Schema.Table, model: m}
		for _, field := range stmt.Schema.Fields {
			// gorm:"-" / -:migration 的字段不参与建表
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			definition := strings.TrimSpace(db.Migrator().FullDataTypeOf(field).SQL)
			typ, notNull, hasDefault := splitDefinition(definition)
			table.Columns = append(table.Columns, ColumnShape{
				Name: field.DBName,
				Type: normalizeType(typ),
				// 主键必然是 NOT NULL（MySQL 建表时强制），而模型上通常没写
				// `not null` 标签——不这样处理，主键会永远被报成"可空性不一致"。
				Definition:    definition,
				NotNull:       notNull || field.PrimaryKey,
				PrimaryKey:    field.PrimaryKey,
				AutoIncrement: field.AutoIncrement,
				HasDefault:    hasDefault,
			})
		}

		for _, idx := range stmt.Schema.ParseIndexes() {
			columns := make([]string, 0, len(idx.Fields))
			for _, f := range idx.Fields {
				if f.Field != nil {
					columns = append(columns, f.Field.DBName)
				}
			}
			table.Indexes = append(table.Indexes, IndexShape{
				Name:    idx.Name,
				Unique:  strings.EqualFold(idx.Class, "UNIQUE"),
				Columns: columns,
			})
		}

		shape.Tables = append(shape.Tables, normalizeTable(table))
	}

	sortTables(shape)
	shape.Hash = fingerprint(shape)
	return shape, nil
}

// Inspect 读回库的实际结构。不存在的表不会出现在结果里——Diff 据此给出建表变更。
func Inspect(db *gorm.DB, models ...interface{}) (*Shape, error) {
	shape := &Shape{}

	for _, m := range models {
		stmt, err := parseModel(db, m)
		if err != nil {
			return nil, fmt.Errorf("解析模型 %T 失败: %w", m, err)
		}

		// 表不存在时**不放进 Shape**：Diff 只有看到"压根没有这张表"才会给出
		// create_table；如果放一个空形态进去，每列都会被判成 add_column，
		// 于是第一次迁移会对着一张不存在的表逐列 ALTER。
		if !db.Migrator().HasTable(m) {
			continue
		}

		table := TableShape{Name: stmt.Schema.Table, model: m}
		{
			columns, err := db.Migrator().ColumnTypes(m)
			if err != nil {
				return nil, fmt.Errorf("读取表 %s 的列失败: %w", table.Name, err)
			}
			for _, c := range columns {
				typ := columnTypeOf(c)
				nullable, _ := c.Nullable()
				primaryKey, _ := c.PrimaryKey()
				autoIncrement, _ := c.AutoIncrement()
				_, hasDefault := c.DefaultValue()
				table.Columns = append(table.Columns, ColumnShape{
					Name:          c.Name(),
					Type:          normalizeType(typ),
					Definition:    typ,
					NotNull:       !nullable,
					PrimaryKey:    primaryKey,
					AutoIncrement: autoIncrement,
					HasDefault:    hasDefault,
				})
			}

			indexes, err := db.Migrator().GetIndexes(m)
			if err != nil {
				return nil, fmt.Errorf("读取表 %s 的索引失败: %w", table.Name, err)
			}
			for _, idx := range indexes {
				// PRIMARY 由主键列本身表达（ColumnShape.PrimaryKey），不当作普通索引比较
				if strings.EqualFold(idx.Name(), "PRIMARY") {
					continue
				}
				unique, _ := idx.Unique()
				table.Indexes = append(table.Indexes, IndexShape{
					Name:    idx.Name(),
					Unique:  unique,
					Columns: idx.Columns(),
				})
			}
		}

		shape.Tables = append(shape.Tables, normalizeTable(table))
	}

	sortTables(shape)
	shape.Hash = fingerprint(shape)
	return shape, nil
}

// ChangeKind 变更类型
type ChangeKind string

// 安全变更（Auto：不会丢数据，可自动执行）
const (
	KindCreateTable ChangeKind = "create_table"
	KindAddColumn   ChangeKind = "add_column"
	KindCreateIndex ChangeKind = "create_index"
)

// 需要人工确认的差异（永不自动执行）
const (
	KindDropColumn  ChangeKind = "drop_column"
	KindDropIndex   ChangeKind = "drop_index"
	KindAlterColumn ChangeKind = "alter_column"
	KindAlterIndex  ChangeKind = "alter_index"
)

// Change 一处差异
type Change struct {
	Kind    ChangeKind
	Table   string
	Subject string // 列名或索引名
	Detail  string // 人类可读的差异说明
	DDL     string // 建议执行的 SQL（自动变更也给出，便于审计与手工复现）

	auto  bool
	model interface{}
}

// Auto 是否可以自动执行（安全子集）
func (c Change) Auto() bool { return c.auto }

func (c Change) String() string {
	target := c.Table
	if c.Subject != "" {
		target += "." + c.Subject
	}
	if c.Detail == "" {
		return fmt.Sprintf("%s %s", c.Kind, target)
	}
	return fmt.Sprintf("%s %s（%s）", c.Kind, target, c.Detail)
}

// Diff 计算"期望形态 - 实际形态"。
//
// 只遍历模型声明过的表：库里多出来的表不在管辖范围内（同一个库可能有别的东西，
// 模型只对自己声明的表负责）。
func (expected *Shape) Diff(actual *Shape) []Change {
	var changes []Change

	for _, et := range expected.Tables {
		at := actual.table(et.Name)
		if at == nil {
			changes = append(changes, Change{
				Kind:   KindCreateTable,
				Table:  et.Name,
				Detail: "表不存在，将按模型创建",
				DDL:    fmt.Sprintf("-- 由模型 %T 生成 CREATE TABLE %s", et.model, et.Name),
				auto:   true,
				model:  et.model,
			})
			continue
		}

		for _, ec := range et.Columns {
			ac := at.column(ec.Name)
			if ac == nil {
				changes = append(changes, Change{
					Kind:    KindAddColumn,
					Table:   et.Name,
					Subject: ec.Name,
					Detail:  ec.Definition,
					DDL:     addColumnDDL(et.Name, ec),
					auto:    true,
					model:   et.model,
				})
				continue
			}
			if diffs := columnDiffs(ec, *ac); len(diffs) > 0 {
				changes = append(changes, Change{
					Kind:    KindAlterColumn,
					Table:   et.Name,
					Subject: ec.Name,
					Detail:  strings.Join(diffs, "；"),
					DDL:     modifyColumnDDL(et.Name, ec),
					model:   et.model,
				})
			}
		}

		for _, ac := range at.Columns {
			if et.column(ac.Name) != nil {
				continue
			}
			changes = append(changes, Change{
				Kind:    KindDropColumn,
				Table:   et.Name,
				Subject: ac.Name,
				Detail:  "库中存在但模型已不再声明: " + renderColumn(ac),
				DDL:     dropColumnDDL(et.Name, ac.Name),
			})
		}

		for _, ei := range et.Indexes {
			ai := at.index(ei.Name)
			if ai == nil {
				changes = append(changes, Change{
					Kind:    KindCreateIndex,
					Table:   et.Name,
					Subject: ei.Name,
					Detail:  fmt.Sprintf("%s(%s)", uniqueText(ei.Unique), strings.Join(ei.Columns, ", ")),
					DDL:     createIndexDDL(et.Name, ei),
					auto:    true,
					model:   et.model,
				})
				continue
			}
			if diffs := indexDiffs(ei, *ai); len(diffs) > 0 {
				changes = append(changes, Change{
					Kind:    KindAlterIndex,
					Table:   et.Name,
					Subject: ei.Name,
					Detail:  strings.Join(diffs, "；"),
					DDL:     dropIndexDDL(et.Name, ei.Name) + " " + createIndexDDL(et.Name, ei),
					model:   et.model,
				})
			}
		}

		for _, ai := range at.Indexes {
			if et.index(ai.Name) != nil {
				continue
			}
			changes = append(changes, Change{
				Kind:    KindDropIndex,
				Table:   et.Name,
				Subject: ai.Name,
				Detail:  "库中存在但模型已不再声明: " + fmt.Sprintf("%s(%s)", uniqueText(ai.Unique), strings.Join(ai.Columns, ", ")),
				DDL:     dropIndexDDL(et.Name, ai.Name),
			})
		}
	}

	return changes
}

// --- 内部工具 ---

// parseModel 解析模型元数据（不连库）。用独立的 Statement，避免污染调用方的 session。
func parseModel(db *gorm.DB, model interface{}) (*gorm.Statement, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return nil, err
	}
	if stmt.Schema == nil {
		return nil, fmt.Errorf("模型没有可迁移的表结构")
	}
	return stmt, nil
}

// splitDefinition 从"类型 + 修饰"的完整定义里拆出类型，并识别 NOT NULL 与 DEFAULT。
//
// dialector 渲染出的定义里修饰词的位置并不统一：类型自带 NULL（可空时间列）与
// AUTO_INCREMENT（整数列），NOT NULL 与 DEFAULT 由 FullDataTypeOf 追加在后面。
func splitDefinition(definition string) (typ string, notNull bool, hasDefault bool) {
	rest := strings.TrimSpace(definition)
	lower := strings.ToLower(rest)

	// 先剥 COMMENT：它排在最后，且注释文本里可能出现 default / not null 之类的词，
	// 不先切掉会污染后面的判断（也会让"类型"里混进整段注释）。
	if idx := strings.Index(lower, " comment "); idx >= 0 {
		rest, lower = strings.TrimSpace(rest[:idx]), lower[:idx]
	}
	if idx := strings.Index(lower, " default "); idx >= 0 {
		hasDefault = true
		rest, lower = strings.TrimSpace(rest[:idx]), lower[:idx]
	}
	if idx := strings.Index(lower, " not null"); idx >= 0 {
		notNull = true
		rest, lower = strings.TrimSpace(rest[:idx]), lower[:idx]
	}
	// 类型自带的修饰：可空时间列会渲染成 "datetime(3) NULL"，自增整数会渲染成
	// "bigint unsigned AUTO_INCREMENT"
	for _, suffix := range []string{" auto_increment", " null"} {
		if strings.HasSuffix(lower, suffix) {
			rest = strings.TrimSpace(rest[:len(rest)-len(suffix)])
			lower = strings.ToLower(rest)
		}
	}
	return rest, notNull, hasDefault
}

// normalizeType 把不同来源的类型文本归一成可比较的形态。
//
// 必须抹平的两个现实差异：
//   - bool：模型侧 dialector 渲染成 boolean，MySQL 的 information_schema 报 tinyint(1)；
//   - 整数显示宽度：MySQL 8.0 的 column_type 不带宽度，5.7 会带（int(11)）。
func normalizeType(typ string) string {
	s := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(typ))), " ")

	unsigned := strings.HasSuffix(s, " unsigned")
	s = strings.TrimSpace(strings.TrimSuffix(s, " unsigned"))

	switch s {
	case "boolean", "bool", "tinyint(1)":
		s = "bool"
	case "integer":
		s = "int"
	default:
		s = stripDisplayWidth(s)
		if strings.HasPrefix(s, "character varying(") {
			s = "varchar(" + strings.TrimPrefix(s, "character varying(")
		}
	}

	if unsigned {
		s += " unsigned"
	}
	return s
}

// stripDisplayWidth 去掉整数类型的显示宽度（tinyint(1) 已在 normalizeType 里归成 bool）
func stripDisplayWidth(s string) string {
	open := strings.Index(s, "(")
	if open < 0 || !strings.HasSuffix(s, ")") {
		return s
	}
	switch s[:open] {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint":
		return s[:open]
	}
	return s
}

func columnTypeOf(c gorm.ColumnType) string {
	if typ, ok := c.ColumnType(); ok && typ != "" {
		return typ
	}
	return c.DatabaseTypeName()
}

func columnDiffs(expected, actual ColumnShape) []string {
	var diffs []string
	if expected.Type != actual.Type {
		diffs = append(diffs, fmt.Sprintf("类型 模型=%s 库=%s", expected.Type, actual.Type))
	}
	if expected.NotNull != actual.NotNull {
		diffs = append(diffs, fmt.Sprintf("可空性 模型=%s 库=%s", nullText(expected.NotNull), nullText(actual.NotNull)))
	}
	if expected.PrimaryKey != actual.PrimaryKey {
		diffs = append(diffs, fmt.Sprintf("主键 模型=%t 库=%t", expected.PrimaryKey, actual.PrimaryKey))
	}
	if expected.AutoIncrement != actual.AutoIncrement {
		diffs = append(diffs, fmt.Sprintf("自增 模型=%t 库=%t", expected.AutoIncrement, actual.AutoIncrement))
	}
	if expected.HasDefault != actual.HasDefault {
		diffs = append(diffs, fmt.Sprintf("默认值 模型=%s 库=%s", hasText(expected.HasDefault), hasText(actual.HasDefault)))
	}
	return diffs
}

func indexDiffs(expected, actual IndexShape) []string {
	var diffs []string
	if expected.Unique != actual.Unique {
		diffs = append(diffs, fmt.Sprintf("唯一性 模型=%t 库=%t", expected.Unique, actual.Unique))
	}
	if strings.Join(expected.Columns, ",") != strings.Join(actual.Columns, ",") {
		diffs = append(diffs, fmt.Sprintf("列 模型=(%s) 库=(%s)",
			strings.Join(expected.Columns, ", "), strings.Join(actual.Columns, ", ")))
	}
	return diffs
}

func normalizeTable(table TableShape) TableShape {
	sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].Name < table.Columns[j].Name })
	sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].Name < table.Indexes[j].Name })
	return table
}

func sortTables(shape *Shape) {
	sort.Slice(shape.Tables, func(i, j int) bool { return shape.Tables[i].Name < shape.Tables[j].Name })
}

// fingerprint 计算形态指纹。只包含参与比较的字段（不含 Definition / 注释），
// 保证同一套模型在任意一次运行里得到同一个哈希。
func fingerprint(shape *Shape) string {
	// 自己排序（而不是要求调用方先 normalize），这样任何来源的 Shape 都能得到稳定
	// 的指纹；排的是副本，不就地改动调用方的切片。
	tables := make([]TableShape, len(shape.Tables))
	copy(tables, shape.Tables)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })

	var b strings.Builder
	for _, t := range tables {
		b.WriteString("table " + t.Name + "\n")

		columns := make([]ColumnShape, len(t.Columns))
		copy(columns, t.Columns)
		sort.Slice(columns, func(i, j int) bool { return columns[i].Name < columns[j].Name })
		for _, c := range columns {
			fmt.Fprintf(&b, "  column %s %s notnull=%t pk=%t auto=%t default=%t\n",
				c.Name, c.Type, c.NotNull, c.PrimaryKey, c.AutoIncrement, c.HasDefault)
		}

		indexes := make([]IndexShape, len(t.Indexes))
		copy(indexes, t.Indexes)
		sort.Slice(indexes, func(i, j int) bool { return indexes[i].Name < indexes[j].Name })
		for _, i := range indexes {
			fmt.Fprintf(&b, "  index %s unique=%t (%s)\n", i.Name, i.Unique, strings.Join(i.Columns, ","))
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func addColumnDDL(table string, c ColumnShape) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", quote(table), quote(c.Name), c.Definition)
}

func modifyColumnDDL(table string, c ColumnShape) string {
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s;", quote(table), quote(c.Name), c.Definition)
}

func dropColumnDDL(table, column string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", quote(table), quote(column))
}

func createIndexDDL(table string, idx IndexShape) string {
	columns := make([]string, 0, len(idx.Columns))
	for _, c := range idx.Columns {
		columns = append(columns, quote(c))
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s);", uniquePrefix(idx.Unique), quote(idx.Name), quote(table), strings.Join(columns, ", "))
}

func dropIndexDDL(table, name string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s;", quote(name), quote(table))
}

func quote(identifier string) string { return "`" + identifier + "`" }

func renderColumn(c ColumnShape) string {
	parts := []string{c.Type, nullText(c.NotNull)}
	if c.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	if c.AutoIncrement {
		parts = append(parts, "AUTO_INCREMENT")
	}
	if c.HasDefault {
		parts = append(parts, "DEFAULT")
	}
	return strings.Join(parts, " ")
}

func nullText(notNull bool) string {
	if notNull {
		return "NOT NULL"
	}
	return "NULL"
}

func hasText(has bool) string {
	if has {
		return "有"
	}
	return "无"
}

func uniquePrefix(unique bool) string {
	if unique {
		return "UNIQUE "
	}
	return ""
}

func uniqueText(unique bool) string {
	if unique {
		return "唯一索引"
	}
	return "普通索引"
}

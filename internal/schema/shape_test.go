package schema

import (
	"strings"
	"testing"
)

func TestSplitDefinition(t *testing.T) {
	cases := []struct {
		definition  string
		wantType    string
		wantNotNull bool
		wantDefault bool
	}{
		{"varchar(64)", "varchar(64)", false, false},
		{"varchar(64) NOT NULL", "varchar(64)", true, false},
		{"varchar(64) NOT NULL DEFAULT ''", "varchar(64)", true, true},
		{"datetime(3) NULL", "datetime(3)", false, false},
		{"bigint unsigned AUTO_INCREMENT", "bigint unsigned", false, false},
		{"boolean NOT NULL DEFAULT false", "boolean", true, true},
		{"text", "text", false, false},
		// COMMENT 排在最后，且注释文本里可能混着关键字，必须先剥掉
		{"datetime(3) NULL COMMENT '到期验证时间'", "datetime(3)", false, false},
		{"text COMMENT 'no default, not null here'", "text", false, false},
		{"varchar(64) NOT NULL DEFAULT '' COMMENT 'default 出现在注释里'", "varchar(64)", true, true},
	}

	for _, tc := range cases {
		typ, notNull, hasDefault := splitDefinition(tc.definition)
		if typ != tc.wantType || notNull != tc.wantNotNull || hasDefault != tc.wantDefault {
			t.Errorf("splitDefinition(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tc.definition, typ, notNull, hasDefault, tc.wantType, tc.wantNotNull, tc.wantDefault)
		}
	}
}

// 归一化必须抹平两侧的真实差异，否则每次启动都会误报"类型不一致"
func TestNormalizeType(t *testing.T) {
	cases := map[string]string{
		"boolean":               "bool",
		"tinyint(1)":            "bool",
		"TINYINT(1)":            "bool",
		"int(11)":               "int",
		"integer":               "int",
		"bigint unsigned":       "bigint unsigned",
		"BIGINT UNSIGNED":       "bigint unsigned",
		"character varying(64)": "varchar(64)",
		"varchar(64)":           "varchar(64)",
		"datetime(3)":           "datetime(3)",
		"longtext":              "longtext",
		"decimal(10, 2)":        "decimal(10, 2)",
	}

	for in, want := range cases {
		if got := normalizeType(in); got != want {
			t.Errorf("normalizeType(%q) = %q, want %q", in, got, want)
		}
	}
}

func shapeOf(table string, columns []ColumnShape, indexes []IndexShape) *Shape {
	shape := &Shape{Tables: []TableShape{{Name: table, Columns: columns, Indexes: indexes}}}
	shape.Hash = fingerprint(shape)
	return shape
}

func TestDiffSplitsSafeAndManual(t *testing.T) {
	expected := shapeOf("t",
		[]ColumnShape{
			{Name: "id", Type: "bigint unsigned", Definition: "bigint unsigned AUTO_INCREMENT", NotNull: true, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: "varchar(64)", Definition: "varchar(64) NOT NULL"},
			{Name: "added", Type: "int", Definition: "int NOT NULL"},
		},
		[]IndexShape{{Name: "idx_name", Columns: []string{"name"}}},
	)
	actual := shapeOf("t",
		[]ColumnShape{
			{Name: "id", Type: "bigint unsigned", NotNull: true, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: "varchar(32)", NotNull: true},
			{Name: "legacy", Type: "text"},
		},
		nil,
	)

	got := map[ChangeKind]Change{}
	for _, c := range expected.Diff(actual) {
		got[c.Kind] = c
	}

	// 安全变更：缺列与缺索引，应可自动执行
	if c, ok := got[KindAddColumn]; !ok || !c.Auto() || c.Subject != "added" {
		t.Errorf("缺少 add_column(added) 或未标为自动: %+v", got)
	}
	if c, ok := got[KindCreateIndex]; !ok || !c.Auto() || c.Subject != "idx_name" {
		t.Errorf("缺少 create_index(idx_name) 或未标为自动: %+v", got)
	}

	// 需人工确认：类型变化与库里多出来的列，绝不自动执行
	if c, ok := got[KindAlterColumn]; !ok || c.Auto() || c.Subject != "name" {
		t.Errorf("缺少 alter_column(name) 或误标为自动: %+v", got)
	} else if !strings.Contains(c.Detail, "varchar(32)") || !strings.Contains(c.Detail, "varchar(64)") {
		t.Errorf("alter_column 的说明应同时给出两侧类型: %q", c.Detail)
	}
	if c, ok := got[KindDropColumn]; !ok || c.Auto() || c.Subject != "legacy" {
		t.Errorf("缺少 drop_column(legacy) 或误标为自动: %+v", got)
	}
}

func TestDiffMissingTableIsCreateTable(t *testing.T) {
	expected := shapeOf("t", []ColumnShape{{Name: "id", Type: "bigint unsigned"}}, nil)
	changes := expected.Diff(&Shape{})
	if len(changes) != 1 || changes[0].Kind != KindCreateTable || !changes[0].Auto() {
		t.Fatalf("缺表应只产生一条可自动执行的 create_table: %+v", changes)
	}
}

func TestDiffIndexChangesNeedManualWork(t *testing.T) {
	expected := shapeOf("t", []ColumnShape{{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true}},
		[]IndexShape{{Name: "uk_a", Unique: true, Columns: []string{"id"}}})
	actual := shapeOf("t", []ColumnShape{{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true}},
		[]IndexShape{{Name: "uk_a", Unique: false, Columns: []string{"id"}}, {Name: "idx_gone", Columns: []string{"id"}}})

	changes := expected.Diff(actual)
	if len(changes) != 2 {
		t.Fatalf("应有改索引与删索引各一条: %+v", changes)
	}
	for _, c := range changes {
		if c.Auto() {
			t.Errorf("索引变化不该自动执行: %+v", c)
		}
	}
}

// 指纹只依赖结构内容：顺序不同不能影响哈希，内容变了必须变
func TestFingerprintIsStableAndOrderIndependent(t *testing.T) {
	a := fingerprint(&Shape{Tables: []TableShape{
		{Name: "b", Columns: []ColumnShape{{Name: "x", Type: "int"}}},
		{Name: "a", Columns: []ColumnShape{{Name: "y", Type: "int"}, {Name: "z", Type: "int"}}},
	}})
	b := fingerprint(&Shape{Tables: []TableShape{
		{Name: "a", Columns: []ColumnShape{{Name: "y", Type: "int"}, {Name: "z", Type: "int"}}},
		{Name: "b", Columns: []ColumnShape{{Name: "x", Type: "int"}}},
	}})
	if a != b {
		t.Errorf("同样的结构应得到同一个指纹: %s vs %s", a, b)
	}

	changed := fingerprint(&Shape{Tables: []TableShape{
		{Name: "a", Columns: []ColumnShape{{Name: "y", Type: "int"}, {Name: "z", Type: "bigint"}}},
	}})
	if changed == a {
		t.Error("列类型变化后指纹必须变")
	}
}

func TestSuggestionsQuoteIdentifiers(t *testing.T) {
	column := ColumnShape{Name: "note", Definition: "varchar(64) NULL"}
	if got := addColumnDDL("accounts", column); !strings.Contains(got, `accounts`) || !strings.Contains(got, `note`) {
		t.Errorf("建议 SQL 应给标识符加反引号: %q", got)
	}
	if got := createIndexDDL("accounts", IndexShape{Name: "idx_a", Unique: true, Columns: []string{"a", "b"}}); got != "CREATE UNIQUE INDEX `idx_a` ON `accounts` (`a`, `b`);" {
		t.Errorf("唯一索引建议 SQL = %q", got)
	}
}

func TestReportConsistent(t *testing.T) {
	if !(Report{}).Consistent() {
		t.Error("没有差异时应认为一致")
	}
	if (Report{Deferred: []Change{{Kind: KindAddColumn}}}).Consistent() {
		t.Error("有未执行的变更时不该认为一致")
	}
	if (Report{Pending: []Change{{Kind: KindDropColumn}}}).Consistent() {
		t.Error("有需人工处理的差异时不该认为一致")
	}
}

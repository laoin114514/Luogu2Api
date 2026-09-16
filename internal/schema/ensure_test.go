package schema

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/laoin114514/luogu2api/internal/model"
)

// 集成测试：库结构检查必须拿到 information_schema 的真实回报才算验证过
// （类型归一化做错的表现就是"每次都报差异"，只有真库能测出来）。
//
//	$env:TEST_DB_DSN="root:密码@tcp(127.0.0.1:3306)/luogu2api_test?charset=utf8mb4&parseTime=True&loc=Local"
//	go test ./internal/schema/ -v
//
// 未设置 TEST_DB_DSN 时全部跳过。
func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DB_DSN"))
	if dsn == "" {
		t.Skip("未设置 TEST_DB_DSN，跳过 schema 集成测试")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	return db
}

// 测试模型：刻意覆盖真实模型里那些"两侧文本不一致"的类型——
// bool ↔ tinyint(1)、整数显示宽度、可空时间列、字符串默认值、唯一索引与普通索引。
type shapeV1 struct {
	ID        uint       `gorm:"column:id;primaryKey;autoIncrement"`
	Name      string     `gorm:"column:name;type:varchar(32);not null;default:'';uniqueIndex:uk_shape_name"`
	Enabled   bool       `gorm:"column:enabled;not null;default:false"`
	Count     int32      `gorm:"column:count;not null"`
	Note      string     `gorm:"column:note;type:text"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	ExpiredAt *time.Time `gorm:"column:expired_at;index:idx_shape_expired"`
}

func (shapeV1) TableName() string { return "itest_schema_shape" }

// shapeV2 在 V1 的基础上多两列一索引（对应"给模型加字段"的日常）
type shapeV2 struct {
	ID        uint       `gorm:"column:id;primaryKey;autoIncrement"`
	Name      string     `gorm:"column:name;type:varchar(32);not null;default:'';uniqueIndex:uk_shape_name"`
	Enabled   bool       `gorm:"column:enabled;not null;default:false"`
	Count     int32      `gorm:"column:count;not null"`
	Note      string     `gorm:"column:note;type:text"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	ExpiredAt *time.Time `gorm:"column:expired_at;index:idx_shape_expired"`
	Score     int32      `gorm:"column:score;not null;default:0"`
	Memo      string     `gorm:"column:memo;type:varchar(64);index:idx_shape_memo"`
}

func (shapeV2) TableName() string { return "itest_schema_shape" }

func resetShapeTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("DROP TABLE IF EXISTS itest_schema_shape").Error; err != nil {
		t.Fatalf("清理测试表失败: %v", err)
	}
}

func kindsOf(changes []Change) map[ChangeKind]Change {
	out := make(map[ChangeKind]Change, len(changes))
	for _, c := range changes {
		out[c.Kind] = c
	}
	return out
}

// 最关键的回归点：由模型建出来的表，紧接着的检查必须是"零差异"。
// 类型归一化（bool/tinyint(1)、显示宽度、可空时间列）写错就会在这里暴露。
func TestEnsureCreatesTableAndThenReportsNoDiff(t *testing.T) {
	db := integrationDB(t)
	resetShapeTable(t, db)
	ctx := context.Background()

	report, err := Status(ctx, db, shapeV1{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if report.Consistent() {
		t.Fatal("表还不存在时不该报告一致")
	}
	if len(report.Deferred) != 1 || report.Deferred[0].Kind != KindCreateTable {
		t.Fatalf("缺表时应给出一条待执行的 create_table: %v", report.Lines())
	}

	report, err = Ensure(ctx, db, Options{Apply: true, Strict: true}, shapeV1{})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(report.Applied) != 1 || report.Applied[0].Kind != KindCreateTable {
		t.Fatalf("应自动建表: %v", report.Lines())
	}

	report, err = Status(ctx, db, shapeV1{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !report.Consistent() {
		t.Fatalf("刚建出来的表与模型仍有差异（归一化有误？）: %v", report.Lines())
	}
}

func TestEnsureAddsColumnsAndIndex(t *testing.T) {
	db := integrationDB(t)
	resetShapeTable(t, db)
	ctx := context.Background()

	if _, err := Ensure(ctx, db, Options{Apply: true}, shapeV1{}); err != nil {
		t.Fatalf("建表: %v", err)
	}

	report, err := Ensure(ctx, db, Options{Apply: true, Strict: true}, shapeV2{})
	if err != nil {
		t.Fatalf("加列: %v", err)
	}
	if len(report.Applied) != 3 {
		t.Fatalf("应自动补两列一索引: %v", report.Lines())
	}

	report, err = Status(ctx, db, shapeV2{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !report.Consistent() {
		t.Fatalf("补完之后应零差异: %v", report.Lines())
	}
	if !db.Migrator().HasColumn(shapeV2{}, "score") || !db.Migrator().HasIndex(shapeV2{}, "idx_shape_memo") {
		t.Error("自动变更没有真正落到库里")
	}
}

// 模型不再声明的列与索引：只报告、给 SQL，绝不自动删
func TestEnsureNeverDropsExtraColumns(t *testing.T) {
	db := integrationDB(t)
	resetShapeTable(t, db)
	ctx := context.Background()

	if _, err := Ensure(ctx, db, Options{Apply: true}, shapeV2{}); err != nil {
		t.Fatalf("建表: %v", err)
	}

	report, err := Ensure(ctx, db, Options{Apply: true, Strict: true}, shapeV1{})
	var pendingErr *PendingError
	if !errors.As(err, &pendingErr) {
		t.Fatalf("模型退回 V1 时应报需人工确认的差异，实际: %v", err)
	}
	got := kindsOf(report.Pending)
	if _, ok := got[KindDropColumn]; !ok {
		t.Fatalf("应报告多余的列: %v", report.Lines())
	}
	if db.Migrator().HasColumn(shapeV2{}, "score") == false {
		t.Error("多余列被自动删掉了——这是不能接受的")
	}
	for _, c := range report.Pending {
		if c.DDL == "" {
			t.Errorf("需人工确认的差异必须给出建议 SQL: %+v", c)
		}
	}

	// Strict=false 时同样的差异只报告不报错（给运维一个"我知道，先放着"的开关）
	report, err = Ensure(ctx, db, Options{Apply: true}, shapeV1{})
	if err != nil {
		t.Fatalf("Strict=false 时不该报错: %v", err)
	}
	if len(report.Pending) == 0 {
		t.Error("差异仍应出现在报告里")
	}
}

func TestEnsureIsIdempotentAndWritesAudit(t *testing.T) {
	db := integrationDB(t)
	resetShapeTable(t, db)
	ctx := context.Background()

	// 审计表本身也可能还没建出来，先确保它存在（这一步可能会写一条自己的审计，
	// 所以计数放在它之后）
	if _, err := Ensure(ctx, db, Options{Apply: true}, &model.SchemaMigration{}); err != nil {
		t.Fatalf("准备审计表: %v", err)
	}

	var before int64
	if err := db.Model(&model.SchemaMigration{}).Count(&before).Error; err != nil {
		t.Fatalf("统计审计记录失败: %v", err)
	}

	models := []interface{}{shapeV1{}, &model.SchemaMigration{}}
	report, err := Ensure(ctx, db, Options{Apply: true}, models...)
	if err != nil {
		t.Fatalf("首次 Ensure: %v", err)
	}
	if len(report.Applied) == 0 {
		t.Fatal("首次应执行建表")
	}

	var afterFirst int64
	if err := db.Model(&model.SchemaMigration{}).Count(&afterFirst).Error; err != nil {
		t.Fatalf("统计审计记录失败: %v", err)
	}
	if afterFirst != before+1 {
		t.Errorf("真正改了结构应写一条审计: before=%d after=%d", before, afterFirst)
	}
	var row model.SchemaMigration
	if err := db.Order("id DESC").First(&row).Error; err != nil {
		t.Fatalf("读取审计记录失败: %v", err)
	}
	if row.CodeHash != report.Expected.Hash || row.DBHash != report.Actual.Hash {
		t.Error("审计记录里的指纹应为本次检查的模型/库指纹")
	}

	// 第二次：什么都不缺，既不该有变更，也不该再多写审计
	report, err = Ensure(ctx, db, Options{Apply: true}, models...)
	if err != nil {
		t.Fatalf("第二次 Ensure: %v", err)
	}
	if len(report.Applied) != 0 || len(report.Pending) != 0 {
		t.Fatalf("第二次不该有任何变更: %v", report.Lines())
	}
	var afterSecond int64
	if err := db.Model(&model.SchemaMigration{}).Count(&afterSecond).Error; err != nil {
		t.Fatalf("统计审计记录失败: %v", err)
	}
	if afterSecond != afterFirst {
		t.Errorf("没有变更就不该写审计: %d -> %d", afterFirst, afterSecond)
	}
}

// 只读入口不能在库里留下任何痕迹
func TestStatusWritesNothing(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	if _, err := Ensure(ctx, db, Options{Apply: true}, &model.SchemaMigration{}); err != nil {
		t.Fatalf("准备审计表: %v", err)
	}
	var before int64
	if err := db.Model(&model.SchemaMigration{}).Count(&before).Error; err != nil {
		t.Fatalf("统计审计记录失败: %v", err)
	}

	if _, err := Status(ctx, db, model.All()...); err != nil {
		t.Fatalf("Status: %v", err)
	}

	var after int64
	if err := db.Model(&model.SchemaMigration{}).Count(&after).Error; err != nil {
		t.Fatalf("统计审计记录失败: %v", err)
	}
	if after != before {
		t.Errorf("Status 是只读的，不该写审计: %d -> %d", before, after)
	}
}

// 真实模型也必须能被自动补齐（缺表就建、缺列就加），且不会留下"待执行"的变更。
//
// 测试库里可能存在早期 AutoMigrate 建出来的 accounts 表（那时 enabled 带 DEFAULT），
// 那种历史差异会以 Pending 出现——这里只记录不判失败，避免测试依赖库的历史。
func TestEnsureRealModels(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	// 测试库是空的时候（CI / 新环境），这是对真实模型类型归一化的强断言；
	// 库里已经有早期 AutoMigrate 建出来的表时，历史差异会以 Pending 出现，
	// 那种情况只记录不判失败，避免测试依赖某个库的历史状态。
	freshAccounts := !db.Migrator().HasTable(&model.Account{})

	report, err := Ensure(ctx, db, Options{Apply: true}, model.All()...)
	if err != nil {
		t.Fatalf("Ensure(model.All()): %v", err)
	}
	if freshAccounts && len(report.Pending) > 0 {
		t.Fatalf("新建的真实表不该有差异（归一化有误？）: %v", report.Lines())
	}
	if len(report.Deferred) > 0 {
		t.Fatalf("自动补齐后仍有未执行的变更: %v", report.Lines())
	}
	for _, c := range report.Applied {
		t.Logf("已自动执行: %s", c.String())
	}
	for _, c := range report.Pending {
		t.Logf("测试库中的历史差异（需人工确认）: %s -> %s", c.String(), c.DDL)
	}
}

// 模拟"有人手工动过库"：只报告、给 SQL，绝不自动改（这是 Strict 模式真正的价值）
func TestEnsureDetectsManualDdl(t *testing.T) {
	db := integrationDB(t)
	resetShapeTable(t, db)
	ctx := context.Background()

	if _, err := Ensure(ctx, db, Options{Apply: true}, shapeV1{}); err != nil {
		t.Fatalf("建表: %v", err)
	}

	// 手工给 count 加了模型里没有的默认值，又加了一列、改窄了 name
	for _, ddl := range []string{
		"ALTER TABLE itest_schema_shape MODIFY COLUMN count int NOT NULL DEFAULT 5",
		"ALTER TABLE itest_schema_shape ADD COLUMN hand_added varchar(8) NULL",
		"ALTER TABLE itest_schema_shape MODIFY COLUMN name varchar(16) NOT NULL DEFAULT ''",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("制造手工差异失败（%s）: %v", ddl, err)
		}
	}

	report, err := Status(ctx, db, shapeV1{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(report.Pending) == 0 {
		t.Fatal("手工改动应被识别出来")
	}

	got := kindsOf(report.Pending)
	alter, ok := got[KindAlterColumn]
	if !ok {
		t.Fatalf("应报告列差异: %v", report.Lines())
	}
	if _, ok := got[KindDropColumn]; !ok {
		t.Fatalf("应报告多出来的列: %v", report.Lines())
	}
	if alter.DDL == "" || (!strings.Contains(alter.Detail, "默认值") && !strings.Contains(alter.Detail, "类型")) {
		t.Errorf("列差异要有说明与建议 SQL: %+v", alter)
	}

	// 只读检查不能改动库
	if !db.Migrator().HasColumn(shapeV1{}, "hand_added") {
		t.Error("Status 不该删掉手工加的列")
	}
}

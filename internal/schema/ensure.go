package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/laoin114514/luogu2api/internal/model"
)

// lockName 结构变更期间的 MySQL 命名锁。
//
// MySQL 的 DDL 是隐式提交的，多条变更无法放进一个事务回滚，因此"同时只让一个
// 执行者改结构"是唯一能提供的保护。单实例部署时它只是一次额外的往返；
// 多实例（或 CI 与人工同时动手）时它避免了两边同时 ALTER 的乱局。
const lockName = "luogu2api.schema"

// lockTimeout 拿锁的等待秒数
const lockTimeoutSeconds = 10

// logger 只用到日志的最小子集（*slog.Logger 天然满足），便于测试注入空实现
type logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// Options 一次结构检查 / 变更的行为
type Options struct {
	// Apply 执行安全变更（建表 / 加列 / 建索引）。false 时只检查、只报告，
	// 一行 DDL 也不会发出去。
	Apply bool
	// Strict 存在需人工确认的差异时返回 *PendingError。生产应为 true：
	// 库结构与模型不一致时宁可不启动，也不要带着未知的列跑。
	Strict bool
	// Logger 可为 nil
	Logger logger
}

// Report 一次检查的结果
type Report struct {
	Expected *Shape // 模型期望的形态
	Actual   *Shape // 库实际的形态（Apply 时是变更之后的）
	Applied  []Change
	Deferred []Change // 安全但未执行（Apply=false）
	Pending  []Change // 需人工确认，永不自动执行
}

// Consistent 库结构与模型完全一致（没有待执行的、也没有需人工处理的差异）
func (r Report) Consistent() bool {
	return len(r.Deferred) == 0 && len(r.Pending) == 0
}

// Lines 逐行描述本次结果，便于日志与命令行打印
func (r Report) Lines() []string {
	lines := make([]string, 0, len(r.Applied)+len(r.Deferred)+len(r.Pending))
	for _, c := range r.Applied {
		lines = append(lines, "已执行 "+c.String())
	}
	for _, c := range r.Deferred {
		lines = append(lines, "待执行 "+c.String())
	}
	for _, c := range r.Pending {
		lines = append(lines, "需人工确认 "+c.String())
	}
	return lines
}

// OutOfDateError 库结构落后于模型：有安全变更还没执行。
//
// 这是"启动即报错"的那一类：缺列的服务能起来，但业务查询会在第一次碰到该列时
// 才失败——不如现在就说清楚。
type OutOfDateError struct{ Deferred []Change }

func (e *OutOfDateError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "数据库结构落后于代码：%d 处变更尚未执行（执行 api -migrate，或设置 DB_MIGRATE_ON_START=true 让服务启动时自动补齐）", len(e.Deferred))
	for _, c := range e.Deferred {
		fmt.Fprintf(&b, "\n  - %s", c.String())
		if c.DDL != "" {
			fmt.Fprintf(&b, "\n    %s", c.DDL)
		}
	}
	return b.String()
}

// PendingError 存在需要人工确认的差异（删列/改类型/索引变化）
type PendingError struct{ Pending []Change }

func (e *PendingError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "数据库结构与模型有 %d 处需要人工确认的差异（可能是收窄丢数据，或别人手工加的列，因此不会自动执行）：", len(e.Pending))
	for _, c := range e.Pending {
		fmt.Fprintf(&b, "\n  - %s", c.String())
		if c.DDL != "" {
			fmt.Fprintf(&b, "\n    %s", c.DDL)
		}
	}
	b.WriteString("\n按上面的 SQL 手工处理后重启即可；确认这些差异无需处理时，设置 DB_SCHEMA_STRICT=false")
	return b.String()
}

// ErrSchemaBusy 另一个实例正在执行结构变更（命名锁没拿到）
var ErrSchemaBusy = errors.New("schema: 另一个实例正在执行结构变更，请稍后重试")

// Ensure 检查库结构，并在 Apply 时执行安全变更。
//
// 流程：Describe（模型期望）→ Inspect（库现状）→ Diff → 执行安全子集 → 记审计。
// 返回值：
//   - 只有 Apply=false 且库落后时才有 Deferred，此时返回 *OutOfDateError；
//   - 有需人工确认的差异且 Strict 时返回 *PendingError；
//   - 两个都不是（例如 Strict=false 时只有 Pending）时返回 nil，由调用方决定容忍。
func Ensure(ctx context.Context, db *gorm.DB, opts Options, models ...interface{}) (Report, error) {
	report, err := check(ctx, db, opts, models...)
	if err != nil {
		return report, err
	}
	return report, decide(report, opts)
}

// Status 只检查不修改，等价于 Ensure(Apply=false, Strict=false)，但**不写审计**、
// 也不因为发现差异而返回错误：调用方拿到 Report 自己决定怎么说。
func Status(ctx context.Context, db *gorm.DB, models ...interface{}) (Report, error) {
	return check(ctx, db, Options{}, models...)
}

func check(ctx context.Context, db *gorm.DB, opts Options, models ...interface{}) (Report, error) {
	log := opts.Logger
	if log == nil {
		log = nopLogger{}
	}

	expected, err := Describe(db, models...)
	if err != nil {
		return Report{}, err
	}
	actual, err := Inspect(db, models...)
	if err != nil {
		return Report{}, err
	}

	report := Report{Expected: expected, Actual: actual}
	var safe []Change
	for _, c := range expected.Diff(actual) {
		if c.auto {
			safe = append(safe, c)
		} else {
			report.Pending = append(report.Pending, c)
		}
	}

	if !opts.Apply {
		report.Deferred = safe
		return report, nil
	}

	if len(safe) > 0 {
		applied, applyErr := applyWithLock(ctx, db, safe, log)
		report.Applied = applied

		// 审计里记的库指纹必须是"变更之后"的样子
		if refreshed, err := Inspect(db, models...); err == nil {
			report.Actual = refreshed
		} else {
			log.Error("变更后重新读取库结构失败", "err", err)
		}
		recordHistory(ctx, db, report, log)
		if applyErr != nil {
			return report, applyErr
		}
		return report, nil
	}

	// 没有安全变更要执行：只有"发现需人工处理的差异"值得留一条审计
	recordHistory(ctx, db, report, log)
	return report, nil
}

func decide(report Report, opts Options) error {
	if len(report.Deferred) > 0 {
		return &OutOfDateError{Deferred: report.Deferred}
	}
	if len(report.Pending) > 0 && opts.Strict {
		return &PendingError{Pending: report.Pending}
	}
	return nil
}

// applyWithLock 在命名锁保护下执行安全变更。
//
// 用 Connection 把整轮钉在同一条连接上：GET_LOCK / RELEASE_LOCK 是连接级的，
// 通过连接池发出去的两条语句可能落在不同连接上，那样锁就形同虚设。
func applyWithLock(ctx context.Context, db *gorm.DB, changes []Change, log logger) ([]Change, error) {
	var applied []Change

	err := db.WithContext(ctx).Connection(func(tx *gorm.DB) error {
		if err := acquireLock(tx); err != nil {
			return err
		}
		defer releaseLock(tx, log)

		for _, c := range changes {
			if err := applyChange(tx, c); err != nil {
				return fmt.Errorf("执行结构变更失败（%s）: %w", c.String(), err)
			}
			applied = append(applied, c)
			log.Info("数据库结构已变更",
				"kind", string(c.Kind), "table", c.Table, "subject", c.Subject, "ddl", c.DDL)
		}
		return nil
	})
	if err != nil {
		return applied, err
	}
	return applied, nil
}

func applyChange(tx *gorm.DB, c Change) error {
	switch c.Kind {
	case KindCreateTable:
		return tx.Migrator().CreateTable(c.model)
	case KindAddColumn:
		return tx.Migrator().AddColumn(c.model, c.Subject)
	case KindCreateIndex:
		return tx.Migrator().CreateIndex(c.model, c.Subject)
	default:
		// Diff 只会把安全变更标成 auto，走到这里说明分类逻辑被改坏了
		return fmt.Errorf("不支持的自动变更类型 %s", c.Kind)
	}
}

func acquireLock(tx *gorm.DB) error {
	row := tx.Raw("SELECT GET_LOCK(?, ?)", lockName, lockTimeoutSeconds).Row()

	var got sql.NullInt64
	if err := row.Scan(&got); err != nil {
		return fmt.Errorf("获取结构变更锁失败: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return ErrSchemaBusy
	}
	return nil
}

func releaseLock(tx *gorm.DB, log logger) {
	// DO 只求值不返回结果集，适合这种"只为副作用"的语句
	if err := tx.Exec("DO RELEASE_LOCK(?)", lockName).Error; err != nil {
		log.Warn("释放结构变更锁失败（连接归还后会自然释放）", "err", err)
	}
}

// recordHistory 追加一行审计。
//
// 只在 Apply（真的动过手，或发现需人工处理的差异）时写：
// 启动校验与 -schema-status 是只读动作，不该往库里写东西。
// 写失败只记日志：结构本身已经检查/修正过了，审计丢一条不该挡住启动。
func recordHistory(ctx context.Context, db *gorm.DB, report Report, log logger) {
	if len(report.Applied) == 0 && len(report.Pending) == 0 {
		return
	}
	if !db.Migrator().HasTable(&model.SchemaMigration{}) {
		// 首次检查且没开自动迁移：历史表还没建，没什么可记的
		return
	}

	row := model.SchemaMigration{
		CodeHash: report.Expected.Hash,
		DBHash:   report.Actual.Hash,
		Applied:  joinLines(report.Applied),
		Pending:  joinLines(report.Pending),
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		log.Error("写入 schema_migrations 审计记录失败", "err", err)
	}
}

func joinLines(changes []Change) string {
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, c.String())
	}
	return strings.Join(lines, "\n")
}

package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/secret"
)

// 仓储集成测试：需要一台真实 MySQL（本包是唯一接触 SQL 的地方，mock 意义有限）。
//
//	$env:TEST_DB_DSN="root:@tcp(127.0.0.1:3306)/luogu2api_test?charset=utf8mb4&parseTime=True&loc=Local"
//	go test ./internal/repository/ -v
//
// 未设置 TEST_DB_DSN 时全部跳过，默认 CI 不依赖数据库。
func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DB_DSN"))
	if dsn == "" {
		t.Skip("未设置 TEST_DB_DSN，跳过仓储集成测试")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.Account{}); err != nil {
		t.Fatalf("迁移 accounts 表失败: %v", err)
	}
	return db
}

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()

	c, err := secret.NewCipher(strings.Repeat("ab", 32)) // hex 形式的 32 字节密钥
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// testRepo 给仓储加上本次测试专用的用户名前缀，便于隔离与清理
type testRepo struct {
	*AccountRepository
	prefix string
}

func (r *testRepo) username(name string) string { return r.prefix + name }

func newRepo(t *testing.T) (*testRepo, *gorm.DB) {
	t.Helper()

	db := integrationDB(t)
	prefix := fmt.Sprintf("itest_%d_", time.Now().UnixNano())

	t.Cleanup(func() {
		err := db.Unscoped().Where("username LIKE ?", prefix+"%").Delete(&model.Account{}).Error
		if err != nil {
			t.Errorf("清理测试数据失败: %v", err)
		}
	})

	return &testRepo{AccountRepository: NewAccountRepository(db, testCipher(t)), prefix: prefix}, db
}

func TestAccountCreateEncryptsCredentials(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{
		Username: repo.username("enc"),
		Password: "p@ssw0rd",
		Cookie:   `[{"name":"_uid","value":"1965145"}]`,
		Enabled:  true,
	}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acc.ID == 0 {
		t.Fatal("Create 应回填自增主键")
	}
	if acc.Status != model.AccountStatusNew {
		t.Errorf("Status = %q, want %q", acc.Status, model.AccountStatusNew)
	}

	// 落库的是密文
	if acc.PasswordSecret == acc.Password {
		t.Error("密码列不应是明文")
	}
	if strings.Contains(acc.CookieSecret, "_uid") {
		t.Errorf("cookie 列不应是明文: %q", acc.CookieSecret)
	}
	if !strings.HasPrefix(acc.PasswordSecret, "v1:") {
		t.Errorf("密文应带版本前缀: %q", acc.PasswordSecret)
	}

	// 读出来能还原
	got, err := repo.GetByUsername(ctx, acc.Username)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if got.Password != "p@ssw0rd" || got.Cookie != acc.Cookie {
		t.Errorf("解密结果 = %q / %q", got.Password, got.Cookie)
	}
}

func TestAccountCreateRejectsDuplicateUsername(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("dup"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &model.Account{Username: acc.Username, Password: "pwd2", Enabled: true}
	if _, err := repo.Create(ctx, dup); !errors.Is(err, model.ErrAccountExists) {
		t.Errorf("err = %v, want ErrAccountExists", err)
	}
}

// 同名账号软删除后重建：应复活原行（沿用主键）并重置成"刚导入"的状态
func TestAccountCreateRevivesSoftDeletedUsername(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	first := &model.Account{Username: repo.username("revive"), Password: "old-pwd", Enabled: true}
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 先让它像"用过一阵子"：有登录态、平台档案、失败计数
	now := time.Now().Truncate(time.Second)
	if err := repo.SaveSession(ctx, first.ID, `[{"name":"_uid","value":"1965145"}]`, 1965145,
		model.LuoguProfile{Name: "老昵称", RawJSON: `{"uid":1965145}`}, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := repo.UpdatePoolState(ctx, first.ID, model.PoolState{
		Online:       false,
		Status:       model.AccountStatusBanned,
		FailureCount: 2,
		LastError:    "登录成功但所有接口均返回 403",
	}); err != nil {
		t.Fatalf("UpdatePoolState: %v", err)
	}
	if err := repo.SoftDelete(ctx, first.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// 同名重建
	second := &model.Account{Username: first.Username, Password: "new-pwd", Nickname: "新昵称", Enabled: true}
	revived, err := repo.Create(ctx, second)
	if err != nil {
		t.Fatalf("Create(revive): %v", err)
	}
	if !revived {
		t.Fatal("同名软删除账号应走复活分支")
	}
	if second.ID != first.ID {
		t.Errorf("复活应沿用旧主键 %d，实际 %d", first.ID, second.ID)
	}

	got, err := repo.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DeletedAt.Valid {
		t.Error("deleted_at 应被清空")
	}
	if got.Password != "new-pwd" {
		t.Errorf("密码应换成新的，实际 %q", got.Password)
	}
	if got.Cookie != "" || got.CookieSecret != "" {
		t.Errorf("复活应清空 cookie: %q / %q", got.Cookie, got.CookieSecret)
	}
	if got.UIDValue() != 0 || got.Nickname != "新昵称" || got.Name != "" || got.ProfileJSON != "" {
		t.Errorf("平台档案应重置: %+v", got)
	}
	if got.Status != model.AccountStatusNew || got.Online || got.FailureCount != 0 || got.NextVerifyAt != nil {
		t.Errorf("运行状态应重置: status=%q online=%v failures=%d next=%v",
			got.Status, got.Online, got.FailureCount, got.NextVerifyAt)
	}
	if got.LastLoginAt != nil || got.LastVerifiedAt != nil {
		t.Errorf("时间戳应清空: %+v", got)
	}
	if !got.Enabled {
		t.Error("复活后应处于启用状态")
	}
	if got.OpenSourceJoined || got.OpenSourceJoinedAt != nil {
		t.Errorf("复活应把代码公开计划状态重置为未确认: joined=%v at=%v",
			got.OpenSourceJoined, got.OpenSourceJoinedAt)
	}

	// 复活后能按用户名查到（软删除已撤销）
	if _, err := repo.GetByUsername(ctx, first.Username); err != nil {
		t.Errorf("复活后应能按用户名查到: %v", err)
	}

	// 再建一次同名：这次是"存在且未删除"，必须是真冲突
	again := &model.Account{Username: first.Username, Password: "x", Enabled: true}
	if _, err := repo.Create(ctx, again); !errors.Is(err, model.ErrAccountExists) {
		t.Errorf("同名且未删除应报 ErrAccountExists，得到 %v", err)
	}
}

// MarkOpenSourceJoined 幂等：重复调用不报错，且不会把加入时间改成新值
func TestAccountMarkOpenSourceJoinedIsIdempotent(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("opensource"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	joinedAt := time.Now().Truncate(time.Second)
	if err := repo.MarkOpenSourceJoined(ctx, acc.ID, joinedAt); err != nil {
		t.Fatalf("MarkOpenSourceJoined: %v", err)
	}

	got, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.OpenSourceJoined {
		t.Error("open_source_joined 应为 true")
	}
	if got.OpenSourceJoinedAt == nil || !got.OpenSourceJoinedAt.Equal(joinedAt) {
		t.Errorf("open_source_joined_at = %v, want %v", got.OpenSourceJoinedAt, joinedAt)
	}

	// 重复调用（并发下可能发生）：不能报错，也不能覆盖首次的加入时间
	later := joinedAt.Add(time.Hour)
	if err := repo.MarkOpenSourceJoined(ctx, acc.ID, later); err != nil {
		t.Fatalf("重复 MarkOpenSourceJoined 不应报错: %v", err)
	}
	got, err = repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.OpenSourceJoinedAt == nil || !got.OpenSourceJoinedAt.Equal(joinedAt) {
		t.Errorf("加入时间被覆盖为 %v, want %v", got.OpenSourceJoinedAt, joinedAt)
	}
}

// 只碰这两列：不能顺手改动号池状态或凭据
func TestAccountMarkOpenSourceJoinedTouchesOnlyItsColumns(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("opensource-cols"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	if err := repo.SaveSession(ctx, acc.ID, `[{"name":"_uid","value":"1965145"}]`, 1965145,
		model.LuoguProfile{Name: "昵称", RawJSON: `{"uid":1965145}`}, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	if err := repo.MarkOpenSourceJoined(ctx, acc.ID, now); err != nil {
		t.Fatalf("MarkOpenSourceJoined: %v", err)
	}

	got, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.AccountStatusActive || !got.Online {
		t.Errorf("号池状态被改动: status=%q online=%v", got.Status, got.Online)
	}
	if got.UIDValue() != 1965145 || got.Cookie == "" || got.Password != "pwd" {
		t.Errorf("凭据/档案被改动: uid=%d cookie=%q", got.UIDValue(), got.Cookie)
	}
	if got.NextVerifyAt == nil {
		t.Error("next_verify_at 被改动")
	}
}

func TestAccountGetByIDMissing(t *testing.T) {
	repo, _ := newRepo(t)

	if _, err := repo.GetByID(context.Background(), 1<<40); !errors.Is(err, model.ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}

func TestAccountListDueForVerifyFilters(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	now := time.Now()

	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	names := map[uint]string{}
	mk := func(name, status string, enabled bool, next *time.Time) uint {
		t.Helper()
		acc := &model.Account{
			Username:     repo.username(name),
			Password:     "pwd",
			Enabled:      enabled,
			Status:       status,
			NextVerifyAt: next,
		}
		if _, err := repo.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		names[acc.ID] = name
		return acc.ID
	}

	wantDue := []uint{
		mk("new", model.AccountStatusNew, true, nil),                 // 从未验证
		mk("active-expired", model.AccountStatusActive, true, &past), // 已过期
		mk("pending-expired", model.AccountStatusReloginPending, true, &past),
		mk("failed-expired", model.AccountStatusReloginFailed, true, &past), // 慢速重试到期
	}
	wantSkipped := []uint{
		mk("active-future", model.AccountStatusActive, true, &future), // 还没到期
		mk("disabled", model.AccountStatusDisabled, true, nil),        // 只能人工恢复
		mk("banned", model.AccountStatusBanned, true, nil),            // 封禁不自动重试
		mk("disabled-flag", model.AccountStatusActive, false, nil),    // enabled=false
	}

	rows, err := repo.ListDueForVerify(ctx, now, 100)
	if err != nil {
		t.Fatalf("ListDueForVerify: %v", err)
	}

	got := map[uint]bool{}
	for _, row := range rows {
		if strings.HasPrefix(row.Username, repo.prefix) {
			got[row.ID] = true
		}
	}

	for _, id := range wantDue {
		if !got[id] {
			t.Errorf("到期的账号 %s(id=%d) 未被捞出", names[id], id)
		}
	}
	for _, id := range wantSkipped {
		if got[id] {
			t.Errorf("不该捞出的账号 %s(id=%d) 出现在结果里 status=%s enabled=%v next=%v",
				names[id], id, rowStatus(repo, id), rowEnabled(repo, id), rowNext(repo, id))
		}
	}

	limited, err := repo.ListDueForVerify(ctx, now, 1)
	if err != nil {
		t.Fatalf("ListDueForVerify(limit=1): %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limit=1 时返回 %d 条", len(limited))
	}
}

// rowStatus / rowEnabled / rowNext 仅在用例失败时用来打印被误捞账号的真实字段
func rowStatus(repo *testRepo, id uint) string {
	acc, err := repo.GetByID(context.Background(), id)
	if err != nil {
		return "?"
	}
	return acc.Status
}

func rowEnabled(repo *testRepo, id uint) bool {
	acc, err := repo.GetByID(context.Background(), id)
	return err == nil && acc.Enabled
}

func rowNext(repo *testRepo, id uint) string {
	acc, err := repo.GetByID(context.Background(), id)
	if err != nil || acc.NextVerifyAt == nil {
		return "<nil>"
	}
	return acc.NextVerifyAt.Format(time.RFC3339)
}

// disabled / banned 账号不参与启动预热（否则封禁号会一直被重登）
func TestAccountListActiveSkipsInactiveStatuses(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	names := map[uint]string{}
	mk := func(name, status string, enabled bool) uint {
		t.Helper()
		acc := &model.Account{Username: repo.username(name), Password: "pwd", Enabled: enabled, Status: status}
		if _, err := repo.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		names[acc.ID] = name
		return acc.ID
	}

	wantActive := []uint{
		mk("active", model.AccountStatusActive, true),
		mk("new", model.AccountStatusNew, true),
		mk("pending", model.AccountStatusReloginPending, true),
		mk("failed", model.AccountStatusReloginFailed, true),
	}
	wantSkipped := []uint{
		mk("disabled", model.AccountStatusDisabled, true),
		mk("banned", model.AccountStatusBanned, true),
		mk("disabled-flag", model.AccountStatusActive, false),
	}

	rows, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}

	got := map[uint]bool{}
	for _, row := range rows {
		if strings.HasPrefix(row.Username, repo.prefix) {
			got[row.ID] = true
		}
	}

	for _, id := range wantActive {
		if !got[id] {
			t.Errorf("账号 %s(id=%d) 应参与号池", names[id], id)
		}
	}
	for _, id := range wantSkipped {
		if got[id] {
			t.Errorf("账号 %s(id=%d) 不应参与号池 status=%s enabled=%v",
				names[id], id, rowStatus(repo, id), rowEnabled(repo, id))
		}
	}
}

func TestAccountSaveSessionThenSelectiveUpdates(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("session"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	next := now.Add(30 * time.Minute)
	profile := model.LuoguProfile{
		Name:       "昵称",
		Avatar:     "https://cdn.luogu.com.cn/avatar.png",
		IsAdmin:    true,
		CCFLevel:   7,
		Background: "bg.png",
		RawJSON:    `{"uid":1965145,"name":"昵称"}`,
	}

	if err := repo.SaveSession(ctx, acc.ID, `[{"name":"_uid","value":"1965145"}]`, 1965145, profile, now, next); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	got, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.Online || got.Status != model.AccountStatusActive {
		t.Errorf("登录成功后应在线且 active: online=%v status=%q", got.Online, got.Status)
	}
	if got.UIDValue() != 1965145 {
		t.Errorf("LuoguUID = %d", got.UIDValue())
	}
	if got.Name != "昵称" || got.CCFLevel != 7 || !got.IsAdmin || got.Background != "bg.png" {
		t.Errorf("平台字段未回填: %+v", got)
	}
	if got.ProfileJSON != profile.RawJSON {
		t.Errorf("profile_json = %q", got.ProfileJSON)
	}
	if got.LastLoginAt == nil || got.NextVerifyAt == nil {
		t.Errorf("时间列未写入: %+v", got)
	}
	if !strings.Contains(got.Cookie, "1965145") {
		t.Errorf("cookie 未更新: %q", got.Cookie)
	}

	cookieSecretBefore := got.CookieSecret

	// 环境类失败：只改 last_error 与 next_verify_at，不能碰 cookie/status/online
	if err := repo.MarkTransientFailure(ctx, acc.ID, "网络超时", now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkTransientFailure: %v", err)
	}

	after, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if after.CookieSecret != cookieSecretBefore {
		t.Error("MarkTransientFailure 不应改动 cookie")
	}
	if !after.Online || after.Status != model.AccountStatusActive {
		t.Errorf("MarkTransientFailure 不应改动可用状态: online=%v status=%q", after.Online, after.Status)
	}
	if after.LastError != "网络超时" {
		t.Errorf("LastError = %q", after.LastError)
	}

	// 验证通过：复位状态并推迟下次验证，同样不碰 cookie
	if err := repo.MarkVerified(ctx, acc.ID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	verified, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if verified.CookieSecret != cookieSecretBefore {
		t.Error("MarkVerified 不应改动 cookie")
	}
	if verified.LastError != "" {
		t.Errorf("验证通过应清空 last_error，实际 %q", verified.LastError)
	}
	if verified.LastVerifiedAt == nil {
		t.Error("LastVerifiedAt 未写入")
	}
}

func TestAccountUpdatePoolStateAndSoftDelete(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("state"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	next := time.Now().Add(time.Hour)
	if err := repo.UpdatePoolState(ctx, acc.ID, model.PoolState{
		Online:       false,
		Status:       model.AccountStatusReloginFailed,
		FailureCount: 3,
		LastError:    "图形验证码错误",
		NextVerifyAt: &next,
	}); err != nil {
		t.Fatalf("UpdatePoolState: %v", err)
	}

	got, err := repo.GetByID(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Online || got.Status != model.AccountStatusReloginFailed || got.FailureCount != 3 {
		t.Errorf("状态 = %+v", got)
	}

	if err := repo.SetEnabled(ctx, acc.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if again, err := repo.GetByID(ctx, acc.ID); err != nil || again.Enabled {
		t.Errorf("SetEnabled 未生效: enabled=%v err=%v", again.Enabled, err)
	}

	if err := repo.SoftDelete(ctx, acc.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := repo.GetByID(ctx, acc.ID); !errors.Is(err, model.ErrAccountNotFound) {
		t.Errorf("软删除后应查不到: %v", err)
	}
	if err := repo.SoftDelete(ctx, acc.ID); !errors.Is(err, model.ErrAccountNotFound) {
		t.Errorf("重复删除应报 ErrAccountNotFound: %v", err)
	}
}

func TestAccountWrongKeyFailsLoudly(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("key"), Password: "pwd", Enabled: true}
	if _, err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	other, err := secret.NewCipher(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	wrong := NewAccountRepository(db, other)

	if _, err := wrong.GetByID(ctx, acc.ID); err == nil {
		t.Error("换密钥后读取应报错，而不是静默返回空凭据")
	} else if !strings.Contains(err.Error(), "解密") {
		t.Errorf("错误信息应指出解密失败: %v", err)
	}
}

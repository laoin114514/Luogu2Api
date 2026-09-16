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
	if err := repo.Create(ctx, acc); err != nil {
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
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &model.Account{Username: acc.Username, Password: "pwd2", Enabled: true}
	if err := repo.Create(ctx, dup); !errors.Is(err, model.ErrAccountExists) {
		t.Errorf("err = %v, want ErrAccountExists", err)
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

	mk := func(name, status string, enabled bool, next *time.Time) uint {
		t.Helper()
		acc := &model.Account{
			Username:     repo.username(name),
			Password:     "pwd",
			Enabled:      enabled,
			Status:       status,
			NextVerifyAt: next,
		}
		if err := repo.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
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
			t.Errorf("到期账号 %d 未被捞出", id)
		}
	}
	for _, id := range wantSkipped {
		if got[id] {
			t.Errorf("不该捞出的账号 %d 出现在结果里", id)
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

func TestAccountSaveSessionThenSelectiveUpdates(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	acc := &model.Account{Username: repo.username("session"), Password: "pwd", Enabled: true}
	if err := repo.Create(ctx, acc); err != nil {
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
	if err := repo.Create(ctx, acc); err != nil {
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
	if err := repo.Create(ctx, acc); err != nil {
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

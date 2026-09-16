package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/laoin114514/luogu2api/internal/model"
)

type fakeAccountStore struct {
	accounts map[uint]*model.Account
	nextID   uint

	createdErr error
	deleted    []uint
	enabledSet map[uint]bool
	states     []model.PoolState
}

func newFakeAccountStore(accounts ...*model.Account) *fakeAccountStore {
	s := &fakeAccountStore{accounts: map[uint]*model.Account{}, enabledSet: map[uint]bool{}}
	for _, acc := range accounts {
		s.accounts[acc.ID] = acc
		if acc.ID > s.nextID {
			s.nextID = acc.ID
		}
	}
	return s
}

func (s *fakeAccountStore) Create(_ context.Context, acc *model.Account) error {
	if s.createdErr != nil {
		return s.createdErr
	}
	s.nextID++
	acc.ID = s.nextID
	s.accounts[acc.ID] = acc
	return nil
}

func (s *fakeAccountStore) GetByID(_ context.Context, id uint) (*model.Account, error) {
	acc, ok := s.accounts[id]
	if !ok {
		return nil, model.ErrAccountNotFound
	}
	return acc, nil
}

func (s *fakeAccountStore) ListAll(context.Context) ([]*model.Account, error) {
	out := make([]*model.Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		out = append(out, acc)
	}
	return out, nil
}

func (s *fakeAccountStore) SetEnabled(_ context.Context, id uint, enabled bool) error {
	acc, ok := s.accounts[id]
	if !ok {
		return model.ErrAccountNotFound
	}
	acc.Enabled = enabled
	s.enabledSet[id] = enabled
	return nil
}

func (s *fakeAccountStore) SoftDelete(_ context.Context, id uint) error {
	if _, ok := s.accounts[id]; !ok {
		return model.ErrAccountNotFound
	}
	s.deleted = append(s.deleted, id)
	delete(s.accounts, id)
	return nil
}

func (s *fakeAccountStore) UpdatePoolState(_ context.Context, id uint, state model.PoolState) error {
	acc, ok := s.accounts[id]
	if !ok {
		return model.ErrAccountNotFound
	}
	s.states = append(s.states, state)
	acc.Online = state.Online
	acc.Status = state.Status
	acc.FailureCount = state.FailureCount
	acc.LastError = state.LastError
	acc.NextVerifyAt = state.NextVerifyAt
	return nil
}

type fakeAccountPool struct {
	loaded    []uint
	removed   []uint
	relogined []uint
	reloginEr error
	loadErr   error
}

func (p *fakeAccountPool) Load(_ context.Context, id uint) error {
	if p.loadErr != nil {
		return p.loadErr
	}
	p.loaded = append(p.loaded, id)
	return nil
}

func (p *fakeAccountPool) Remove(id uint) { p.removed = append(p.removed, id) }

func (p *fakeAccountPool) ForceRelogin(_ context.Context, id uint) error {
	p.relogined = append(p.relogined, id)
	return p.reloginEr
}

func newTestAccountService(store AccountStore, pool AccountPool) *AccountService {
	return NewAccountService(store, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func sampleAccount(id uint) *model.Account {
	uid := int64(1965145)
	return &model.Account{
		ID:          id,
		Username:    "user1",
		Password:    "p@ssw0rd",
		Cookie:      `[{"name":"_uid","value":"1965145"}]`,
		LuoguUID:    &uid,
		Nickname:    "昵称",
		Online:      true,
		Status:      model.AccountStatusActive,
		Enabled:     true,
		Weight:      1,
		Name:        "昵称",
		CCFLevel:    7,
		CreatedAt:   time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		LastLoginAt: nil,
	}
}

func TestCreateValidatesInput(t *testing.T) {
	svc := newTestAccountService(newFakeAccountStore(), &fakeAccountPool{})

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"空用户名", "  ", "pwd"},
		{"空密码", "user", ""},
		{"用户名过长", strings.Repeat("u", 65), "pwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), tt.username, tt.password, ""); !errors.Is(err, ErrInvalidParam) {
				t.Errorf("err = %v, want ErrInvalidParam", err)
			}
		})
	}
}

func TestCreateLogsInAndReturnsAccount(t *testing.T) {
	store := newFakeAccountStore()
	pool := &fakeAccountPool{}
	svc := newTestAccountService(store, pool)

	dto, err := svc.Create(context.Background(), "user1", "p@ssw0rd", "昵称")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if dto.ID == 0 || dto.Username != "user1" || dto.Nickname != "昵称" {
		t.Errorf("dto = %+v", dto)
	}
	if len(pool.loaded) != 1 || pool.loaded[0] != dto.ID {
		t.Errorf("应把新账号载入号池，实际 %v", pool.loaded)
	}
	if len(pool.relogined) != 1 || pool.relogined[0] != dto.ID {
		t.Errorf("应触发首次登录，实际 %v", pool.relogined)
	}
	if got := store.accounts[dto.ID]; got == nil || got.Password != "p@ssw0rd" {
		t.Errorf("密码应交给仓储加密存储，实际 %+v", got)
	}
}

// 首次登录失败不应让接口失败：账号留在待重登，交给扫描器
func TestCreateSucceedsEvenIfFirstLoginFails(t *testing.T) {
	store := newFakeAccountStore()
	pool := &fakeAccountPool{reloginEr: errors.New("OCR 不可用")}
	svc := newTestAccountService(store, pool)

	dto, err := svc.Create(context.Background(), "user1", "p@ssw0rd", "")
	if err != nil {
		t.Fatalf("Create 不应因首次登录失败而报错: %v", err)
	}
	if dto.Username != "user1" {
		t.Errorf("dto = %+v", dto)
	}
}

func TestCreatePropagatesDuplicate(t *testing.T) {
	store := newFakeAccountStore()
	store.createdErr = model.ErrAccountExists
	svc := newTestAccountService(store, &fakeAccountPool{})

	if _, err := svc.Create(context.Background(), "user1", "pwd", ""); !errors.Is(err, model.ErrAccountExists) {
		t.Errorf("err = %v, want ErrAccountExists", err)
	}
}

func TestSetEnabledTogglesPoolMembership(t *testing.T) {
	acc := sampleAccount(1)
	store := newFakeAccountStore(acc)
	pool := &fakeAccountPool{}
	svc := newTestAccountService(store, pool)

	disabled, err := svc.SetEnabled(context.Background(), 1, false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if disabled.Enabled {
		t.Error("应返回停用状态")
	}
	if len(pool.removed) != 1 || pool.removed[0] != 1 {
		t.Errorf("停用应把账号移出号池，实际 %v", pool.removed)
	}

	enabled, err := svc.SetEnabled(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !enabled.Enabled {
		t.Error("应返回启用状态")
	}
	if len(pool.loaded) != 1 || pool.loaded[0] != 1 {
		t.Errorf("启用应把账号载入号池，实际 %v", pool.loaded)
	}
}

// disabled 账号人工重新启用后，必须能重新进入验证/重登流程
// （否则运维修好密码，账号也永远回不了池子）
func TestSetEnabledRevivesDisabledAccount(t *testing.T) {
	acc := sampleAccount(1)
	acc.Status = model.AccountStatusDisabled
	acc.Online = false
	acc.FailureCount = 3
	acc.LastError = "密码错误"

	store := newFakeAccountStore(acc)
	pool := &fakeAccountPool{}
	svc := newTestAccountService(store, pool)

	dto, err := svc.SetEnabled(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	if dto.Status != model.AccountStatusNew {
		t.Errorf("Status = %q, want %q", dto.Status, model.AccountStatusNew)
	}
	if dto.FailureCount != 0 || dto.LastError != "" {
		t.Errorf("应清空失败计数与错误: %+v", dto)
	}
	if len(store.states) != 1 {
		t.Fatalf("应复位一次状态，实际 %d 次", len(store.states))
	}
	if store.states[0].NextVerifyAt != nil {
		t.Error("复位后应立即到期（next_verify_at 为空），让扫描器马上处理")
	}
	if len(pool.loaded) != 1 || pool.loaded[0] != 1 {
		t.Errorf("重新启用应载入号池，实际 %v", pool.loaded)
	}
}

// 本来就正常的账号被启用时不应复位状态（否则会被误判成待重登）
func TestSetEnabledKeepsActiveStatus(t *testing.T) {
	store := newFakeAccountStore(sampleAccount(1))
	pool := &fakeAccountPool{}
	svc := newTestAccountService(store, pool)

	dto, err := svc.SetEnabled(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if dto.Status != model.AccountStatusActive {
		t.Errorf("Status = %q, want %q", dto.Status, model.AccountStatusActive)
	}
	if len(store.states) != 0 {
		t.Errorf("正常账号不应被复位: %+v", store.states)
	}
}

func TestSetEnabledMissingAccount(t *testing.T) {
	svc := newTestAccountService(newFakeAccountStore(), &fakeAccountPool{})

	if _, err := svc.SetEnabled(context.Background(), 99, false); !errors.Is(err, model.ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}

func TestDeleteRemovesFromStoreAndPool(t *testing.T) {
	store := newFakeAccountStore(sampleAccount(1))
	pool := &fakeAccountPool{}
	svc := newTestAccountService(store, pool)

	if err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.deleted) != 1 || len(pool.removed) != 1 {
		t.Errorf("deleted=%v removed=%v", store.deleted, pool.removed)
	}
}

func TestReloginDelegatesToPool(t *testing.T) {
	store := newFakeAccountStore(sampleAccount(1))
	pool := &fakeAccountPool{reloginEr: errors.New("重登失败")}
	svc := newTestAccountService(store, pool)

	dto, err := svc.Relogin(context.Background(), 1)
	if err != nil {
		t.Fatalf("Relogin 不应把失败直接抛出（状态已在返回值里）: %v", err)
	}
	if dto.ID != 1 {
		t.Errorf("dto = %+v", dto)
	}
	if len(pool.relogined) != 1 {
		t.Errorf("应调用一次强制重登，实际 %v", pool.relogined)
	}
}

func TestListReturnsDTOs(t *testing.T) {
	svc := newTestAccountService(newFakeAccountStore(sampleAccount(1), sampleAccount(2)), &fakeAccountPool{})

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
}

// 凭据绝不能出现在 HTTP 响应里：序列化 DTO 后全文搜索密码与 cookie
func TestAccountDTOLeaksNoCredentials(t *testing.T) {
	acc := sampleAccount(1)

	raw, err := json.Marshal(NewAccountDTO(acc))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, secret := range []string{acc.Password, acc.Cookie, "password", "cookie"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("DTO 里出现了敏感内容 %q: %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), "user1") {
		t.Errorf("DTO 应包含用户名: %s", raw)
	}
}

func TestGetMissingAccount(t *testing.T) {
	svc := newTestAccountService(newFakeAccountStore(), &fakeAccountPool{})

	if _, err := svc.Get(context.Background(), 42); !errors.Is(err, model.ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}

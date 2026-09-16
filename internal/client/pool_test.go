package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/laoin114514/luoguClient"

	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/model"
)

// ---------- 测试替身 ----------

type savedSession struct {
	id      uint
	cookie  string
	uid     int
	profile model.LuoguProfile
}

type stateUpdate struct {
	id    uint
	state model.PoolState
}

type transientUpdate struct {
	id   uint
	msg  string
	next time.Time
}

// fakeStore 内存版 AccountStore，同时记录所有写入供断言
type fakeStore struct {
	mu         sync.Mutex
	accounts   map[uint]*model.Account
	due        []*model.Account
	saved      []savedSession
	states     []stateUpdate
	transients []transientUpdate
	verified   []uint
	failWrite  error
}

func newFakeStore(accounts ...*model.Account) *fakeStore {
	s := &fakeStore{accounts: make(map[uint]*model.Account, len(accounts))}
	for _, acc := range accounts {
		s.accounts[acc.ID] = acc
	}
	return s
}

func (s *fakeStore) ListActive(context.Context) ([]*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*model.Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		if acc.Enabled && acc.Status != model.AccountStatusDisabled && acc.Status != model.AccountStatusBanned {
			out = append(out, acc)
		}
	}
	return out, nil
}

func (s *fakeStore) ListDueForVerify(_ context.Context, _ time.Time, limit int) ([]*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*model.Account, 0, len(s.due))
	for i, acc := range s.due {
		if i >= limit {
			break
		}
		out = append(out, acc)
	}
	return out, nil
}

func (s *fakeStore) GetByID(_ context.Context, id uint) (*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return nil, model.ErrAccountNotFound
	}
	return acc, nil
}

func (s *fakeStore) SaveSession(_ context.Context, id uint, cookie string, uid int, profile model.LuoguProfile, _, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWrite != nil {
		return s.failWrite
	}
	s.saved = append(s.saved, savedSession{id: id, cookie: cookie, uid: uid, profile: profile})
	if acc, ok := s.accounts[id]; ok {
		acc.Cookie = cookie
		acc.Status = model.AccountStatusActive
		acc.Online = true
	}
	return nil
}

func (s *fakeStore) MarkVerified(_ context.Context, id uint, _, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.verified = append(s.verified, id)
	return s.failWrite
}

func (s *fakeStore) UpdatePoolState(_ context.Context, id uint, state model.PoolState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states = append(s.states, stateUpdate{id: id, state: state})
	if acc, ok := s.accounts[id]; ok {
		acc.Online = state.Online
		acc.Status = state.Status
		acc.FailureCount = state.FailureCount
	}
	return s.failWrite
}

func (s *fakeStore) MarkTransientFailure(_ context.Context, id uint, msg string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.transients = append(s.transients, transientUpdate{id: id, msg: msg, next: next})
	return s.failWrite
}

func (s *fakeStore) lastState() (stateUpdate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.states) == 0 {
		return stateUpdate{}, false
	}
	return s.states[len(s.states)-1], true
}

func (s *fakeStore) stateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.states)
}

func (s *fakeStore) savedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saved)
}

// offlineStatesFor 返回指定账号被标记为离线的次数
func (s *fakeStore) offlineStatesFor(id uint) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	for _, u := range s.states {
		if u.id == id && !u.state.Online {
			n++
		}
	}
	return n
}

// waitFor 等待异步动作（例如请求路径触发的后台重登）落地，避免测试与 goroutine 抢时序
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// fakeSession 实现 SessionClient，行为由字段控制
type fakeSession struct {
	cookie string
	uid    int

	verifyErr  error
	verifyErrs []error // 按调用顺序返回；用完后回落到 verifyErr（重登成功后还会复核一次）
	csrfErr    error
	captchaErr error
	captcha    []byte
	exportErr  error
	profile    model.LuoguProfile
	profileErr error

	loginErrs  []error
	loginCalls int
	verifyHits int

	mu sync.Mutex
}

func (f *fakeSession) UID() int { return f.uid }

func (f *fakeSession) ExportCookies() ([]byte, error) {
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	return []byte(f.cookie), nil
}

func (f *fakeSession) ImportCookies([]byte) error { return nil }
func (f *fakeSession) ClearCookies() error        { return nil }

func (f *fakeSession) RefreshCSRF() error { return f.csrfErr }

func (f *fakeSession) GetCaptcha() ([]byte, error) {
	if f.captchaErr != nil {
		return nil, f.captchaErr
	}
	if f.captcha == nil {
		return []byte("jpeg"), nil
	}
	return f.captcha, nil
}

func (f *fakeSession) Login(_, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.loginCalls++
	if len(f.loginErrs) == 0 {
		return nil
	}
	err := f.loginErrs[0]
	f.loginErrs = f.loginErrs[1:]
	return err
}

func (f *fakeSession) LoginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCalls
}

func (f *fakeSession) Verify() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.verifyHits++
	if len(f.verifyErrs) > 0 {
		err := f.verifyErrs[0]
		f.verifyErrs = f.verifyErrs[1:]
		return err
	}
	return f.verifyErr
}

func (f *fakeSession) UserProfile() (model.LuoguProfile, error) {
	if f.profileErr != nil {
		return model.LuoguProfile{}, f.profileErr
	}
	return f.profile, nil
}

func (f *fakeSession) SDK() *sdk.Client { return nil }

// fakeSolver 固定返回验证码或错误
type fakeSolver struct {
	code string
	err  error

	mu    sync.Mutex
	calls int
}

func (s *fakeSolver) Solve(context.Context, []byte) (string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	if s.code == "" {
		return "abcd", nil
	}
	return s.code, nil
}

func (s *fakeSolver) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// ---------- 测试脚手架 ----------

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func testPoolConfig() config.Config {
	return config.Config{
		Luogu: config.Luogu{Timeout: 5 * time.Second, Retry: 0},
		Account: config.Account{
			SweepInterval:     time.Minute,
			VerifyInterval:    10 * time.Minute,
			VerifyJitter:      0,
			VerifyConcurrency: 1,
			LoginMaxAttempts:  3,
			LoginBackoff:      time.Minute,
			FailedRetry:       time.Hour,
			RequestMaxTry:     2,
			SweepBatchLimit:   100,
		},
	}
}

type poolFixture struct {
	pool    *Pool
	store   *fakeStore
	solver  *fakeSolver
	byID    map[uint]*fakeSession
	factory ClientFactory
}

// newFixture 用"一个账号一个 fakeSession"的方式装配号池
func newFixture(t *testing.T, withDue bool, accounts []*model.Account, sessions map[uint]*fakeSession) *poolFixture {
	t.Helper()

	store := newFakeStore(accounts...)
	if withDue {
		store.due = accounts
	}

	solver := &fakeSolver{}
	fixture := &poolFixture{store: store, solver: solver, byID: sessions}

	// 用"这次 factory 被第几号账号调用"来绑定会话：
	// buildSession 传入的 cookie 与我们注册的账号一一对应
	byCookie := make(map[string]*fakeSession, len(accounts))
	for _, acc := range accounts {
		if s, ok := sessions[acc.ID]; ok {
			byCookie[acc.Cookie] = s
		}
	}
	var mu sync.Mutex
	fixture.factory = func(cookie []byte) (SessionClient, error) {
		mu.Lock()
		defer mu.Unlock()
		if s, ok := byCookie[string(cookie)]; ok {
			return s, nil
		}
		// 空 cookie（重登场景）：按顺序取第一个未使用的会话
		for _, acc := range accounts {
			if s, ok := sessions[acc.ID]; ok && s.cookie == "" {
				return s, nil
			}
		}
		return nil, fmt.Errorf("测试工厂：没有匹配 cookie=%q 的会话", string(cookie))
	}

	fixture.pool = NewPool(
		context.Background(),
		testPoolConfig(),
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithClientFactory(fixture.factory),
		WithSolver(solver),
		WithClock(func() time.Time { return testNow }),
	)
	return fixture
}

func activeAccount(id uint, username, cookie string) *model.Account {
	return &model.Account{
		ID:       id,
		Username: username,
		Password: "pwd-" + username,
		Cookie:   cookie,
		Enabled:  true,
		Online:   true,
		Status:   model.AccountStatusActive,
		Weight:   1,
	}
}

func captchaErr() error {
	return &sdk.AuthError{
		Code:    400,
		Type:    `LuoguWeb\Spilopelia\Exception\CaptchaNotMatchException`,
		Message: "图形验证码错误",
	}
}

func unauthorizedErr() error {
	return &sdk.UnauthorizedError{StatusCode: 401, Message: "verify auth"}
}

// 洛谷对封禁账号的表现：登录能成功，但一切请求返回 403
func forbiddenErr() error {
	return &sdk.UnauthorizedError{StatusCode: 403, Message: "verify auth"}
}

// ---------- 预热 ----------

func TestWarmupSeparatesServingAndPending(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "")
	acc2.Online = false
	acc2.Status = model.AccountStatusNew

	f := newFixture(t, false, []*model.Account{acc1, acc2}, map[uint]*fakeSession{
		1: {cookie: "cookie-1", uid: 101},
		2: {},
	})

	warm, err := f.pool.Warmup(context.Background())
	if err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	if warm.Total != 2 || warm.Serving != 1 || warm.Pending != 1 {
		t.Errorf("Warmup = %+v", warm)
	}

	stats := f.pool.Stats()
	if stats.Total != 2 || stats.Online != 1 || stats.ReloginPending != 1 {
		t.Errorf("Stats = %+v", stats)
	}
}

func TestWarmupEmptyPoolIsNotAnError(t *testing.T) {
	f := newFixture(t, false, nil, nil)

	warm, err := f.pool.Warmup(context.Background())
	if err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if warm.Total != 0 {
		t.Errorf("Warmup = %+v", warm)
	}
	if _, err := f.pool.pick(map[uint]bool{}); !errors.Is(err, ErrPoolExhausted) {
		t.Errorf("空池 pick 应返回 ErrPoolExhausted，得到 %v", err)
	}
}

func TestWarmupSkipsDisabledAccounts(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "cookie-2")
	acc2.Status = model.AccountStatusDisabled

	f := newFixture(t, false, []*model.Account{acc1, acc2}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
		2: {cookie: "cookie-2"},
	})

	warm, err := f.pool.Warmup(context.Background())
	if err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if warm.Total != 1 {
		t.Errorf("disabled 账号不应进入号池: %+v", warm)
	}
}

// ---------- 扫描：验证成功 ----------

func TestSweepVerifyOKPostponesNextVerify(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{cookie: "cookie-1", uid: 1}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Checked != 1 || res.OK != 1 || res.Relogged != 0 {
		t.Errorf("SweepResult = %+v", res)
	}
	if len(f.store.verified) != 1 {
		t.Errorf("应调用 MarkVerified，实际 %v", f.store.verified)
	}
	if f.store.stateCount() != 0 {
		t.Errorf("验证成功不应改状态，实际 %+v", f.store.states)
	}
	if !f.pool.Stats().LastSweepAt.Equal(testNow) {
		t.Errorf("LastSweepAt = %v", f.pool.Stats().LastSweepAt)
	}
}

// ---------- 扫描：cookie 失效 → 重登成功 ----------

func TestSweepUnauthorizedReloginsAndReturnsToPool(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie: "cookie-1",
		uid:    1965145,
		// 第一次验证 401（cookie 失效），重登成功后的复核正常——真实场景就是这样
		verifyErrs: []error{unauthorizedErr()},
		profile:    model.LuoguProfile{Name: "昵称", RawJSON: `{"uid":1965145}`},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	// 登录成功后导出的是新 cookie
	session.cookie = "cookie-2"

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Relogged != 1 {
		t.Errorf("SweepResult = %+v", res)
	}
	if session.LoginCount() != 1 || f.solver.Calls() != 1 {
		t.Errorf("登录次数=%d OCR 调用=%d，都应为 1", session.LoginCount(), f.solver.Calls())
	}
	if f.store.savedCount() != 1 {
		t.Fatalf("应保存新登录态，实际 %+v", f.store.saved)
	}
	if got := f.store.saved[0]; got.cookie != "cookie-2" || got.uid != 1965145 || got.profile.Name != "昵称" {
		t.Errorf("保存的会话 = %+v", got)
	}

	stats := f.pool.Stats()
	if stats.Online != 1 {
		t.Errorf("重登成功后应回到在线: %+v", stats)
	}
	if !f.pool.getSession(1).isServing() {
		t.Error("重登成功后应重新参与选号")
	}
}

// ---------- 扫描：尝试次数用尽 → 标记离线 ----------

func TestSweepReloginExhaustsAttemptsMarksOffline(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie:    "cookie-1",
		verifyErr: unauthorizedErr(),
		loginErrs: []error{captchaErr(), captchaErr(), captchaErr()},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Declared != 1 {
		t.Errorf("SweepResult = %+v", res)
	}
	// 每次尝试都要换一张验证码
	if session.LoginCount() != 3 || f.solver.Calls() != 3 {
		t.Errorf("登录次数=%d OCR 调用=%d，都应为 3", session.LoginCount(), f.solver.Calls())
	}

	last, ok := f.store.lastState()
	if !ok {
		t.Fatal("应写入离线状态")
	}
	if last.state.Online || last.state.Status != model.AccountStatusReloginFailed {
		t.Errorf("最终状态 = %+v", last.state)
	}
	if last.state.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", last.state.FailureCount)
	}
	// 退避：第一次失败 = 1 × LoginBackoff
	if last.state.NextVerifyAt == nil || !last.state.NextVerifyAt.Equal(testNow.Add(time.Minute)) {
		t.Errorf("NextVerifyAt = %v", last.state.NextVerifyAt)
	}
	if f.pool.Stats().Online != 0 {
		t.Errorf("标记离线的账号不应在线: %+v", f.pool.Stats())
	}
}

// ---------- 扫描：网络错误绝不改账号状态（关键回归） ----------

func TestSweepNetworkErrorLeavesAccountStateUntouched(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie:    "cookie-1",
		verifyErr: &sdk.NetworkError{Err: errors.New("dial tcp 1.2.3.4:443: i/o timeout")},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Transient != 1 {
		t.Errorf("SweepResult = %+v", res)
	}
	if f.store.stateCount() != 0 {
		t.Errorf("网络错误不应改动 online/status，实际 %+v", f.store.states)
	}
	if f.store.savedCount() != 0 {
		t.Error("网络错误不应触发重登")
	}
	if session.LoginCount() != 0 || f.solver.Calls() != 0 {
		t.Error("网络错误不应调用登录/OCR")
	}
	if len(f.store.transients) != 1 {
		t.Fatalf("应记录临时失败，实际 %+v", f.store.transients)
	}
	if f.pool.Stats().Online != 1 {
		t.Errorf("账号应保持在线: %+v", f.pool.Stats())
	}
}

// ---------- 扫描：OCR 不可用 → 待重登，而不是失败 ----------

func TestSweepOCRDownKeepsPendingNotFailed(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{cookie: "cookie-1", verifyErr: unauthorizedErr()}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	f.solver.err = fmt.Errorf("%w: 调用 OCR 服务失败", ErrSolver)
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Pending != 1 || res.Declared != 0 || res.Disabled != 0 {
		t.Errorf("SweepResult = %+v", res)
	}
	// OCR 失败不消耗账号侧的尝试次数：只试一次就退回待重登
	if session.LoginCount() != 0 {
		t.Errorf("OCR 失败时不应调用登录接口，实际 %d 次", session.LoginCount())
	}

	last, _ := f.store.lastState()
	if last.state.Status != model.AccountStatusReloginPending {
		t.Errorf("状态 = %q, want %q", last.state.Status, model.AccountStatusReloginPending)
	}
	if last.state.FailureCount != 0 {
		t.Errorf("OCR 故障不应累加账号失败次数，实际 %d", last.state.FailureCount)
	}
	if last.state.Online {
		t.Error("cookie 已确认失效，应离线等待重登")
	}
}

// ---------- 扫描：凭据/账号级错误 → 立即停用，不消耗尝试次数 ----------

func TestSweepCredentialErrorDisablesImmediately(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie:    "cookie-1",
		verifyErr: unauthorizedErr(),
		loginErrs: []error{&sdk.AuthError{
			Code:    400,
			Type:    `LuoguWeb\Spilopelia\Exception\InvalidPasswordException`,
			Message: "用户名或密码错误",
		}},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Disabled != 1 {
		t.Errorf("SweepResult = %+v", res)
	}
	if session.LoginCount() != 1 {
		t.Errorf("凭据错误应立即终止，实际尝试 %d 次", session.LoginCount())
	}

	last, _ := f.store.lastState()
	if last.state.Status != model.AccountStatusDisabled || last.state.Online {
		t.Errorf("状态 = %+v", last.state)
	}
	if last.state.NextVerifyAt != nil {
		t.Errorf("停用后不应再排验证时间: %v", last.state.NextVerifyAt)
	}
}

// ---------- 扫描：已有重登任务时跳过，不写库 ----------

func TestSweepSkipsBusyAccount(t *testing.T) {
	// 没有 cookie 的账号会直接走重登分支，正好用来验证"同一账号只允许一个重登任务"
	acc := activeAccount(1, "u1", "")
	acc.Online = false
	acc.Status = model.AccountStatusNew
	session := &fakeSession{}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	// 模拟请求路径已经触发了重登
	f.pool.getSession(1).verifying.Store(true)

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Busy != 1 || res.Relogged != 0 {
		t.Errorf("SweepResult = %+v", res)
	}
	if f.store.stateCount() != 0 || f.store.savedCount() != 0 {
		t.Errorf("busy 时不应写库: states=%+v saved=%+v", f.store.states, f.store.saved)
	}
	if session.LoginCount() != 0 {
		t.Error("busy 时不应再次登录")
	}
}

// ---------- 请求路径：轮询与换号重试 ----------

func TestPickRoundRobinsAcrossAccounts(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "cookie-2")

	f := newFixture(t, false, []*model.Account{acc1, acc2}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
		2: {cookie: "cookie-2"},
	})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	first, err := f.pool.pick(map[uint]bool{})
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	second, err := f.pool.pick(map[uint]bool{})
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if first.id == second.id {
		t.Errorf("两次选号应轮询到不同账号，都是 %d", first.id)
	}
}

func TestWithSessionFailsOverOnUnauthorized(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "cookie-2")

	f := newFixture(t, false, []*model.Account{acc1, acc2}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
		2: {cookie: "cookie-2"},
	})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	var served []uint
	err := f.pool.withSession(context.Background(), "测试请求", func(c SessionClient) error {
		s := c.(*fakeSession)
		if s.cookie == "cookie-1" {
			served = append(served, 1)
			return unauthorizedErr() // 第一个账号登录态失效
		}
		served = append(served, 2)
		return nil
	})
	if err != nil {
		t.Fatalf("withSession 应换号成功: %v", err)
	}
	if len(served) != 2 || served[0] != 1 || served[1] != 2 {
		t.Errorf("调用顺序 = %v, want [1 2]", served)
	}

	// 失效账号会被立刻摘出号池（摘池可能发生在后台恢复流程里，稍等它落地）
	waitFor(t, "账号 1 被标记离线", func() bool {
		return f.store.offlineStatesFor(1) > 0
	})
	if f.pool.getSession(2) == nil || !f.pool.getSession(2).isServing() {
		t.Error("正常账号应继续参与选号")
	}
}

func TestWithSessionReturnsPoolExhaustedWhenAllAccountsFail(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")

	f := newFixture(t, false, []*model.Account{acc1}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
	})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	err := f.pool.withSession(context.Background(), "测试请求", func(SessionClient) error {
		return unauthorizedErr()
	})
	if !IsPoolExhausted(err) {
		t.Errorf("应返回 ErrPoolExhausted，得到 %v", err)
	}
}

func TestWithSessionPropagatesBusinessErrors(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")

	f := newFixture(t, false, []*model.Account{acc1}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
	})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	businessErr := errors.New("题目不存在")
	err := f.pool.withSession(context.Background(), "测试请求", func(SessionClient) error {
		return businessErr
	})
	if !errors.Is(err, businessErr) {
		t.Errorf("业务错误应原样透出，得到 %v", err)
	}
	if !f.pool.getSession(1).isServing() {
		t.Error("业务错误不应导致账号被摘出号池")
	}
}

// ---------- 错误分类 ----------

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errClass
	}{
		{"nil", nil, clsOK},
		{"unauthorized", unauthorizedErr(), clsUnauthorized},
		{"unauthorized-wrapped", fmt.Errorf("获取题目 P1001: %w", unauthorizedErr()), clsUnauthorized},
		{"forbidden", forbiddenErr(), clsForbidden},
		{"forbidden-wrapped", fmt.Errorf("获取题目 P1001: %w", forbiddenErr()), clsForbidden},
		{"captcha", captchaErr(), clsCaptcha},
		{"password", &sdk.AuthError{Type: "InvalidPasswordException"}, clsFatal},
		{"network", &sdk.NetworkError{Err: errors.New("timeout")}, clsTransient},
		{"solver", fmt.Errorf("%w: 服务不可用", ErrSolver), clsTransient},
		{"unknown", errors.New("lentille-context script not found in page"), clsTransient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}

	// 日志可读性：401 与 403 要能一眼区分
	if clsUnauthorized.String() == clsForbidden.String() {
		t.Error("401 与 403 的类别名不应相同")
	}
	if clsForbidden.String() != "forbidden_403" {
		t.Errorf("clsForbidden.String() = %q", clsForbidden.String())
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	f := newFixture(t, false, nil, nil)

	cfg := testPoolConfig()
	f.pool.cfg = cfg

	if got := f.pool.backoff(1); got != time.Minute {
		t.Errorf("backoff(1) = %v, want 1m", got)
	}
	if got := f.pool.backoff(2); got != 2*time.Minute {
		t.Errorf("backoff(2) = %v, want 2m", got)
	}
	if got := f.pool.backoff(3); got != 4*time.Minute {
		t.Errorf("backoff(3) = %v, want 4m", got)
	}
	// 上限为 FailedRetry
	if got := f.pool.backoff(20); got != time.Hour {
		t.Errorf("backoff(20) = %v, want 1h", got)
	}
}

func TestSummarizeKeepsSingleLineAndLimitsLength(t *testing.T) {
	msg := summarize(errors.New("line1\nline2\twith   spaces"))
	if msg != "line1 line2 with spaces" {
		t.Errorf("summarize = %q", msg)
	}

	long := summarize(errors.New(strings.Repeat("x", 600)))
	if len(long) > 490 {
		t.Errorf("摘要过长: %d", len(long))
	}
	if summarize(nil) != "" {
		t.Error("nil 错误应得到空摘要")
	}
}

// ---------- 扫描：封禁账号（登录能成功但一切 403）----------

// 洛谷封禁账号的实测表现：登录返回成功、cookie 正常签发，但 /user/setting 与
// /user/{uid} 全部 403。这类账号必须判为 banned 并退出自动重试，
// 否则会陷入"验证 403 → 重登成功 → 再验证 403"的死循环。
func TestSweepForbiddenMarksAccountBanned(t *testing.T) {
	acc := activeAccount(1, "laoyin", "cookie-banned")
	session := &fakeSession{
		cookie:    "cookie-banned",
		uid:       1851093,
		verifyErr: forbiddenErr(), // 登录前后都是 403
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Banned != 1 || res.Relogged != 0 || res.OK != 0 {
		t.Errorf("SweepResult = %+v", res)
	}
	// 登录确实被调过一次（用凭据复核），但只此一次
	if session.LoginCount() != 1 {
		t.Errorf("登录次数 = %d, want 1", session.LoginCount())
	}

	last, ok := f.store.lastState()
	if !ok {
		t.Fatal("应写入状态")
	}
	if last.state.Status != model.AccountStatusBanned || last.state.Online {
		t.Errorf("最终状态 = %+v", last.state)
	}
	// 封禁不是重试能解决的问题：不排下次验证，也不再累加失败次数
	if last.state.NextVerifyAt != nil {
		t.Errorf("banned 不应排下次验证: %v", last.state.NextVerifyAt)
	}
	if last.state.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", last.state.FailureCount)
	}
	if f.store.savedCount() != 0 {
		t.Error("封禁账号不该把 cookie 当作有效登录态保存")
	}
	if f.pool.getSession(1).isServing() {
		t.Error("封禁账号必须退出服务集")
	}
	if stats := f.pool.Stats(); stats.Banned != 1 || stats.Online != 0 {
		t.Errorf("Stats = %+v", stats)
	}
}

// 封禁后不再被扫描（否则每轮都会重登一次）
func TestBannedAccountIsNotSweptAgain(t *testing.T) {
	acc := activeAccount(1, "laoyin", "cookie-banned")
	session := &fakeSession{cookie: "cookie-banned", verifyErr: forbiddenErr()}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if _, err := f.pool.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// 第二轮：仓储层按状态过滤，banned 不在可扫描状态里
	f.store.due = nil
	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Checked != 0 {
		t.Errorf("banned 账号不应再被扫描: %+v", res)
	}
	if session.LoginCount() != 1 {
		t.Errorf("不应重复登录，实际 %d 次", session.LoginCount())
	}

	// 仓储层的过滤条件也必须是"排除 banned"
	for _, status := range model.AccountSweepableStatuses {
		if status == model.AccountStatusBanned {
			t.Error("banned 不应出现在可扫描状态集合里")
		}
	}
}

// 登录成功但复核遇到网络抖动：不能丢掉这次成功的登录
func TestReloginTransientVerifyKeepsAccountAlive(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie:     "cookie-1",
		uid:        1965145,
		verifyErrs: []error{unauthorizedErr(), &sdk.NetworkError{Err: errors.New("i/o timeout")}},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if res.Relogged != 1 {
		t.Errorf("SweepResult = %+v", res)
	}
	if got := f.pool.Stats().Online; got != 1 {
		t.Errorf("Online = %d, want 1（登录成功不该因为复核失败而作废）", got)
	}
}

// 登录成功后复核仍是 401：不改状态定性，留给下一轮
func TestReloginUnauthorizedVerifyGoesPending(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{
		cookie:     "cookie-1",
		verifyErrs: []error{unauthorizedErr(), unauthorizedErr()},
	}

	f := newFixture(t, true, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	res, err := f.pool.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Pending != 1 || res.Relogged != 0 {
		t.Errorf("SweepResult = %+v", res)
	}

	last, _ := f.store.lastState()
	if last.state.Status != model.AccountStatusReloginPending {
		t.Errorf("状态 = %q, want %q", last.state.Status, model.AccountStatusReloginPending)
	}
}

// 请求路径遇到 403：换号继续服务，不把请求打回去
func TestWithSessionFailsOverOnForbidden(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "cookie-2")

	f := newFixture(t, false, []*model.Account{acc1, acc2}, map[uint]*fakeSession{
		1: {cookie: "cookie-1", verifyErr: forbiddenErr()},
		2: {cookie: "cookie-2"},
	})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	var served []uint
	err := f.pool.withSession(context.Background(), "测试请求", func(c SessionClient) error {
		s := c.(*fakeSession)
		served = append(served, uint(len(s.cookie)))
		if s.cookie == "cookie-1" {
			return forbiddenErr()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("403 应换号重试并成功: %v", err)
	}
	if len(served) != 2 {
		t.Errorf("应尝试两个账号，实际 %v", served)
	}
	if f.pool.getSession(1).isServing() {
		t.Error("403 的账号应立刻摘出服务集")
	}
}

// ---------- 手动重登 ----------

// 本来可用的账号，手动重登遇到环境问题时应保持在线
func TestForceReloginKeepsHealthyAccountOnlineOnTransientFailure(t *testing.T) {
	acc := activeAccount(1, "u1", "cookie-1")
	session := &fakeSession{cookie: "cookie-1", captchaErr: errors.New("captcha endpoint 500")}

	f := newFixture(t, false, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if !f.pool.getSession(1).isServing() {
		t.Fatal("前置条件：账号应在线")
	}

	if err := f.pool.ForceRelogin(context.Background(), 1); err == nil {
		t.Error("环境原因导致的重登失败应返回错误")
	}

	if !f.pool.getSession(1).isServing() {
		t.Error("本来可用的账号不该因为环境问题被摘出号池")
	}
	if stats := f.pool.Stats(); stats.Online != 1 {
		t.Errorf("Stats = %+v, want Online=1", stats)
	}
	if len(f.store.verified) == 0 {
		t.Error("应把账号恢复为已验证状态")
	}
}

// 离线账号手动重登失败，保持离线（不会因为一次失败反而被"复活"）
func TestForceReloginKeepsOfflineAccountOffline(t *testing.T) {
	acc := activeAccount(1, "u1", "")
	acc.Online = false
	acc.Status = model.AccountStatusReloginPending
	session := &fakeSession{loginErrs: []error{captchaErr(), captchaErr(), captchaErr()}}

	f := newFixture(t, false, []*model.Account{acc}, map[uint]*fakeSession{1: session})
	if _, err := f.pool.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	if err := f.pool.ForceRelogin(context.Background(), 1); err == nil {
		t.Error("重登失败应返回错误")
	}

	if f.pool.getSession(1).isServing() {
		t.Error("重登失败的账号不应进入服务集")
	}
	last, ok := f.store.lastState()
	if !ok || last.state.Status != model.AccountStatusReloginFailed {
		t.Errorf("状态 = %+v", last.state)
	}
	if session.LoginCount() != 3 {
		t.Errorf("登录次数 = %d, want 3", session.LoginCount())
	}
}

// ---------- 号池快照 ----------

func TestStatsCountsDisabledAndPending(t *testing.T) {
	acc1 := activeAccount(1, "u1", "cookie-1")
	acc2 := activeAccount(2, "u2", "cookie-2")
	acc2.Online = false
	acc2.Status = model.AccountStatusReloginFailed
	acc3 := activeAccount(3, "u3", "cookie-3")
	acc3.Status = model.AccountStatusDisabled
	acc4 := activeAccount(4, "u4", "cookie-4")
	acc4.Online = false
	acc4.Status = model.AccountStatusBanned

	f := newFixture(t, false, []*model.Account{acc1, acc2, acc3, acc4}, map[uint]*fakeSession{
		1: {cookie: "cookie-1"},
		2: {cookie: "cookie-2"},
		3: {cookie: "cookie-3"},
		4: {cookie: "cookie-4"},
	})

	// disabled/banned 账号会被 ListActive 过滤掉，用 buildSession 直接塞进池来验证计数
	for _, acc := range []*model.Account{acc1, acc2, acc3, acc4} {
		s, err := f.pool.buildSession(acc)
		if err != nil {
			t.Fatalf("buildSession: %v", err)
		}
		f.pool.putSession(s)
	}

	stats := f.pool.Stats()
	if stats.Total != 4 || stats.Online != 1 || stats.ReloginFailed != 1 || stats.Disabled != 1 || stats.Banned != 1 {
		t.Errorf("Stats = %+v", stats)
	}
}

package client

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/laoin114514/luoguClient"

	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/internal/model"
)

// 号池对外的哨兵错误
var (
	// ErrPoolExhausted 没有任何可用账号（全部离线/待重登/停用）
	ErrPoolExhausted = errors.New("号池中没有可用账号")
	// ErrAccountNotInPool 账号不在池中
	ErrAccountNotInPool = errors.New("账号不在号池中")
)

// AccountStore 号池需要的数据访问能力（由 repository 实现，便于测试替换）
type AccountStore interface {
	ListActive(ctx context.Context) ([]*model.Account, error)
	ListDueForVerify(ctx context.Context, now time.Time, limit int) ([]*model.Account, error)
	GetByID(ctx context.Context, id uint) (*model.Account, error)
	SaveSession(ctx context.Context, id uint, cookie string, uid int, profile model.LuoguProfile, now, nextVerifyAt time.Time) error
	MarkVerified(ctx context.Context, id uint, now, nextVerifyAt time.Time) error
	UpdatePoolState(ctx context.Context, id uint, state model.PoolState) error
	MarkTransientFailure(ctx context.Context, id uint, errMsg string, nextVerifyAt time.Time) error
}

// logger 只用到日志的最小子集（*slog.Logger 天然满足），便于测试注入空实现
type logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// accountOutcome 单个账号一次处理的结果（决定落库状态）
type accountOutcome int

const (
	outcomeOK            accountOutcome = iota // cookie 有效
	outcomeRelogged                            // 重登成功
	outcomeReloginFailed                       // 尝试次数用尽 → relogin_failed + 离线
	outcomePending                             // 环境原因（OCR/网络）→ relogin_pending + 离线
	outcomeDisabled                            // 凭据/账号级问题 → disabled
	outcomeBanned                              // 登录能用但被 403 拒绝 → banned
	outcomeTransient                           // 连 cookie 有效性都无法确认 → 不改 online/status
	outcomeBusy                                // 已有重登在进行 → 本轮跳过，不写库
)

// errClass 错误分类：决定"能不能据此判定账号不可用"
type errClass int

const (
	clsOK           errClass = iota
	clsUnauthorized          // 401：cookie 失效
	clsForbidden             // 403：凭据没问题但拒绝访问（洛谷封禁/限制账号的典型表现）
	clsCaptcha               // 验证码识别错（可换一张重试）
	clsFatal                 // 凭据/账号级问题
	clsTransient             // 网络/5xx/OCR/未知：与 cookie 无关，绝不动账号状态
)

// String 便于日志里直接说明错误类别（401 还是 403 会决定账号的处置方式）
func (c errClass) String() string {
	switch c {
	case clsOK:
		return "ok"
	case clsUnauthorized:
		return "unauthorized_401"
	case clsForbidden:
		return "forbidden_403"
	case clsCaptcha:
		return "captcha"
	case clsFatal:
		return "fatal"
	default:
		return "transient"
	}
}

// SweepResult 一轮扫描的统计
type SweepResult struct {
	At        time.Time `json:"at"`
	Checked   int       `json:"checked"`
	OK        int       `json:"ok"`
	Relogged  int       `json:"relogged"`
	Declared  int       `json:"declared"`
	Pending   int       `json:"pending"`
	Disabled  int       `json:"disabled"`
	Banned    int       `json:"banned"`
	Transient int       `json:"transient"`
	Busy      int       `json:"busy"`
	// Duration 以纳秒输出（time.Duration 的 JSON 形式），字段名随之写实
	Duration time.Duration `json:"durationNs"`
}

// Stats 号池快照（健康检查与管理接口使用）
type Stats struct {
	Total          int          `json:"total"`
	Online         int          `json:"online"`
	ReloginPending int          `json:"reloginPending"`
	ReloginFailed  int          `json:"reloginFailed"`
	Disabled       int          `json:"disabled"`
	Banned         int          `json:"banned"`
	LastSweepAt    time.Time    `json:"lastSweepAt"`
	LastSweep      *SweepResult `json:"lastSweep,omitempty"`
}

// WarmupResult 预热统计
type WarmupResult struct {
	Total   int
	Serving int
	Pending int
	Failed  int
}

// session 池内单个账号：一个 client + 一份运行状态
type session struct {
	id       uint
	username string

	mu      sync.RWMutex
	account model.Account
	client  SessionClient

	serving   atomic.Bool // 是否可被请求选中
	verifying atomic.Bool // 重登进行中（每个账号同时只允许一个重登任务）
	lastOK    atomic.Int64
}

func (s *session) isServing() bool { return s.serving.Load() }

func (s *session) snapshot() model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.account
}

// Pool 洛谷账号池：一号一 client，负责选号、验证、重登与业务请求分发
type Pool struct {
	cfg     config.Config
	store   AccountStore
	factory ClientFactory
	solver  CaptchaSolver
	logger  logger
	now     func() time.Time

	mu       sync.RWMutex
	sessions map[uint]*session
	rr       atomic.Uint64

	lastSweepAt atomic.Int64
	lastSweep   atomic.Pointer[SweepResult]

	// bg 跟踪请求路径触发的后台恢复流程，供关服时等待
	bg sync.WaitGroup
}

// PoolOption 号池可选项（测试注入用）
type PoolOption func(*Pool)

// WithClientFactory 替换会话工厂（测试用假实现）
func WithClientFactory(f ClientFactory) PoolOption {
	return func(p *Pool) { p.factory = f }
}

// WithSolver 替换验证码识别实现
func WithSolver(s CaptchaSolver) PoolOption {
	return func(p *Pool) { p.solver = s }
}

// WithClock 替换时钟（测试用）
func WithClock(now func() time.Time) PoolOption {
	return func(p *Pool) { p.now = now }
}

// NewPool 创建号池（尚未加载账号，需调用 Warmup）
func NewPool(ctx context.Context, cfg config.Config, store AccountStore, log logger, opts ...PoolOption) *Pool {
	p := &Pool{
		cfg:      cfg,
		store:    store,
		logger:   log,
		now:      time.Now,
		factory:  newSDKFactory(ctx, cfg.Luogu),
		solver:   NewOCRClient(cfg.OCR),
		sessions: make(map[uint]*session),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Warmup 从数据库加载账号并建立会话。
//
// 空表不是错误（新部署先起服务、再用管理接口导入账号）；cookie 为空或无法
// 导入的账号仍会建立会话，只是不参与选号，等扫描器重登成功后再入池。
func (p *Pool) Warmup(ctx context.Context) (WarmupResult, error) {
	accounts, err := p.store.ListActive(ctx)
	if err != nil {
		return WarmupResult{}, err
	}

	var res WarmupResult
	for _, acc := range accounts {
		res.Total++

		s, err := p.buildSession(acc)
		if err != nil {
			res.Failed++
			p.logger.Error("账号会话初始化失败，无法参与号池",
				"account_id", acc.ID, "username", acc.Username, "err", err.Error())
			continue
		}

		p.putSession(s)
		if s.isServing() {
			res.Serving++
		} else {
			res.Pending++
		}
	}
	return res, nil
}

// Stats 返回号池快照
func (p *Pool) Stats() Stats {
	p.mu.RLock()
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.mu.RUnlock()

	stats := Stats{Total: len(sessions)}
	for _, s := range sessions {
		acc := s.snapshot()
		switch {
		case acc.Status == model.AccountStatusBanned:
			stats.Banned++
		case acc.Status == model.AccountStatusDisabled || !acc.Enabled:
			stats.Disabled++
		case acc.Online && acc.Status == model.AccountStatusActive:
			stats.Online++
		case acc.Status == model.AccountStatusReloginFailed:
			stats.ReloginFailed++
		default:
			stats.ReloginPending++
		}
	}

	if ns := p.lastSweepAt.Load(); ns > 0 {
		stats.LastSweepAt = time.Unix(0, ns)
	}
	stats.LastSweep = p.lastSweep.Load()
	return stats
}

// Sweep 执行一轮扫描：取出到期账号，逐个验证并在必要时重登。
//
// 严格区分"cookie 失效"与"环境故障"：只有确认 cookie 失效才会把账号摘出号池；
// 网络抖动 / OCR 不可用绝不会让整池被标记离线（那会把一次抖动放大成全站不可用）。
func (p *Pool) Sweep(ctx context.Context) (SweepResult, error) {
	start := p.now()

	accounts, err := p.store.ListDueForVerify(ctx, start, p.cfg.Account.SweepBatchLimit)
	if err != nil {
		return SweepResult{}, err
	}

	res := SweepResult{At: start, Checked: len(accounts)}

	concurrency := p.cfg.Account.VerifyConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, acc := range accounts {
		acc := acc
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			outcome := p.handleAccount(ctx, acc)

			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case outcomeOK:
				res.OK++
			case outcomeRelogged:
				res.Relogged++
			case outcomeReloginFailed:
				res.Declared++
			case outcomePending:
				res.Pending++
			case outcomeDisabled:
				res.Disabled++
			case outcomeBanned:
				res.Banned++
			case outcomeBusy:
				res.Busy++
			default:
				res.Transient++
			}
		}()
	}
	wg.Wait()

	res.Duration = p.now().Sub(start)
	p.lastSweepAt.Store(start.UnixNano())
	p.lastSweep.Store(&res)
	return res, nil
}

// handleAccount 处理单个到期账号：先验证 cookie，失效则摘池并重登
func (p *Pool) handleAccount(ctx context.Context, acc *model.Account) accountOutcome {
	s, err := p.sessionFor(acc)
	if err != nil {
		p.logger.Error("账号会话不可用", "account_id", acc.ID, "err", err.Error())
		return p.recordTransient(ctx, acc, fmt.Errorf("会话初始化失败: %w", err))
	}

	// 没有 cookie（刚导入 / 上次重登失败）直接重登，省一次注定 401 的请求
	if s.snapshot().Cookie == "" {
		outcome, _, _ := p.tryRelogin(ctx, s)
		return outcome
	}

	s.mu.RLock()
	verifyErr := s.client.Verify()
	s.mu.RUnlock()

	switch classify(verifyErr) {
	case clsOK:
		now := p.now()
		if err := p.store.MarkVerified(ctx, acc.ID, now, p.nextVerifyAt(now)); err != nil {
			p.logger.Error("更新验证时间失败", "account_id", acc.ID, "err", err.Error())
		}
		s.markAlive(now)
		return outcomeOK

	case clsUnauthorized, clsForbidden:
		// 401：cookie 确认失效；403：凭据没错但被拒（封禁账号的典型表现）。
		// 两者都先落库摘池（crash-safe），再同步重登——403 的具体定性交给
		// 重登成功后的复核（那里才会看到"登录成功但依然 403"）。
		p.logger.Warn("账号登录态不可用，已摘出号池",
			"account_id", s.id, "username", s.username,
			"class", classify(verifyErr).String(), "err", summarize(verifyErr))
		p.markOffline(ctx, s, model.AccountStatusReloginPending, acc.FailureCount, verifyErr,
			timePtr(p.now().Add(p.cfg.Account.LoginBackoff)))

		outcome, _, _ := p.tryRelogin(ctx, s)
		return outcome

	default:
		// 没能确认 cookie 是否有效：保持 online/status 不变，只记错误并短退避
		return p.recordTransient(ctx, acc, verifyErr)
	}
}

// sessionFor 取（或补建）账号在池中的会话，并把账号快照刷新为最新行
func (p *Pool) sessionFor(acc *model.Account) (*session, error) {
	if s := p.getSession(acc.ID); s != nil {
		s.mu.Lock()
		s.account = *acc
		s.mu.Unlock()
		return s, nil
	}

	s, err := p.buildSession(acc)
	if err != nil {
		return nil, err
	}
	p.putSession(s)
	return s, nil
}

func (p *Pool) buildSession(acc *model.Account) (*session, error) {
	c, err := p.factory([]byte(acc.Cookie))
	if err != nil && acc.Cookie != "" {
		// cookie 损坏不该让账号变得不可恢复：退回空会话，靠重登重建
		p.logger.Warn("cookie 无法导入，将改用空会话重登",
			"account_id", acc.ID, "username", acc.Username, "err", err.Error())
		c, err = p.factory(nil)
	}
	if err != nil {
		return nil, err
	}

	return &session{
		id:       acc.ID,
		username: acc.Username,
		account:  *acc,
		client:   c,
	}, nil
}

// tryRelogin 带互斥的重登：同一账号同时只允许一个重登任务
func (p *Pool) tryRelogin(ctx context.Context, s *session) (accountOutcome, error, bool) {
	if !s.verifying.CompareAndSwap(false, true) {
		return outcomeBusy, nil, false
	}
	defer s.verifying.Store(false)

	outcome, err := p.relogin(ctx, s)
	return outcome, err, true
}

// relogin 执行重登：单次任务内最多 LoginMaxAttempts 次尝试（每次换新验证码）
func (p *Pool) relogin(ctx context.Context, s *session) (accountOutcome, error) {
	acc := s.snapshot()
	attempts := p.cfg.Account.LoginMaxAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 1; i <= attempts; i++ {
		if err := ctx.Err(); err != nil {
			return p.recordPending(ctx, s, acc, fmt.Errorf("重登被取消: %w", err))
		}

		err := p.loginOnce(ctx, s)
		if err == nil {
			return p.completeLogin(ctx, s, acc)
		}
		lastErr = err

		switch classify(err) {
		case clsCaptcha:
			// 验证码识别错：换一张继续，计入尝试次数
			p.logger.Debug("验证码识别错误，换一张重试", "account_id", s.id, "attempt", i)
			continue
		case clsUnauthorized, clsFatal:
			// 凭据/账号级问题：重试没有意义
			return p.recordDisabled(ctx, s, acc, err)
		default:
			// 环境问题：本轮无法完成，保持待重登并短退避
			return p.recordPending(ctx, s, acc, err)
		}
	}

	p.logger.Warn("账号重登尝试次数用尽",
		"account_id", s.id, "username", s.username, "attempts", attempts, "err", summarize(lastErr))
	return p.recordReloginFailed(ctx, s, acc, lastErr)
}

// loginOnce 走一遍完整登录流程：CSRF → 验证码 → OCR → 登录
func (p *Pool) loginOnce(ctx context.Context, s *session) error {
	acc := s.snapshot()

	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()

	if err := c.RefreshCSRF(); err != nil {
		return fmt.Errorf("刷新 CSRF 失败: %w", err)
	}

	image, err := c.GetCaptcha()
	if err != nil {
		return fmt.Errorf("获取验证码失败: %w", err)
	}

	code, err := p.solver.Solve(ctx, image)
	if err != nil {
		return err
	}

	if err := c.Login(acc.Username, acc.Password, code); err != nil {
		return err
	}
	return nil
}

// completeLogin 登录成功：导出 cookie、拉档案、落库并重新入池
func (p *Pool) completeLogin(ctx context.Context, s *session, acc model.Account) (accountOutcome, error) {
	now := p.now()

	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()

	data, err := c.ExportCookies()
	if err != nil {
		return p.recordPending(ctx, s, acc, fmt.Errorf("登录成功但导出 cookie 失败: %w", err))
	}
	cookie := string(data)
	uid := c.UID()

	// 登录成功 ≠ 账号可用：洛谷对封禁账号照样签发会话，但随后所有接口都返回 403。
	// 这里立刻验一次，避免把封禁账号当成在线账号放进池子（也避免"重登成功→403→再重登"
	// 的死循环）。网络抖动导致的验证失败不算数——登录本身已经成功，状态照常推进。
	s.mu.RLock()
	verifyErr := c.Verify()
	s.mu.RUnlock()

	switch classify(verifyErr) {
	case clsForbidden:
		return p.recordBanned(ctx, s, acc,
			fmt.Errorf("登录成功但所有接口均返回 403（账号被洛谷封禁或限制）: %w", verifyErr))
	case clsUnauthorized:
		return p.recordPending(ctx, s, acc,
			fmt.Errorf("登录成功后仍然未授权，稍后重试: %w", verifyErr))
	case clsTransient:
		p.logger.Warn("登录成功但登录态复核未完成（环境原因），按在线处理",
			"account_id", s.id, "username", s.username, "err", summarize(verifyErr))
	}

	profile, err := c.UserProfile()
	if err != nil {
		// 资料拉取失败不影响登录态本身
		p.logger.Warn("登录成功但拉取用户资料失败",
			"account_id", s.id, "username", s.username, "err", summarize(err))
	}
	if profile.IsBanned {
		return p.recordBanned(ctx, s, acc, errors.New("洛谷用户资料显示该账号已被封禁（isBanned=true）"))
	}

	if err := p.store.SaveSession(ctx, acc.ID, cookie, uid, profile, now, p.nextVerifyAt(now)); err != nil {
		p.logger.Error("保存登录态失败（内存中仍可用，重启后会退化）",
			"account_id", s.id, "username", s.username, "err", err.Error())
	}

	s.mu.Lock()
	s.account.Cookie = cookie
	if uid > 0 {
		v := int64(uid)
		s.account.LuoguUID = &v
	}
	if profile.Name != "" {
		s.account.Nickname = profile.Name
		s.account.Name = profile.Name
	}
	s.mu.Unlock()

	s.markAlive(now)
	p.logger.Info("账号重登成功",
		"account_id", s.id, "username", s.username, "uid", uid, "nickname", profile.Name)
	return outcomeRelogged, nil
}

// suspend 请求路径命中 401 时的处置：立刻摘池，并异步重登恢复。
//
// 异步是刻意的：业务请求不该等一次重登（可能几十秒），换号重试即可。
func (p *Pool) suspend(s *session, cause error) {
	s.serving.Store(false)
	p.logger.Warn("账号登录态失效，已摘出号池",
		"account_id", s.id, "username", s.username, "err", summarize(cause))

	if !s.verifying.CompareAndSwap(false, true) {
		return // 已有重登任务在进行
	}

	p.bg.Add(1)
	go func() {
		defer p.bg.Done()
		defer s.verifying.Store(false)

		acc := s.snapshot()
		p.markOffline(context.Background(), s, model.AccountStatusReloginPending,
			acc.FailureCount, cause, timePtr(p.now().Add(p.cfg.Account.LoginBackoff)))

		ctx, cancel := context.WithTimeout(context.Background(), p.loginWindow())
		defer cancel()
		if _, err := p.relogin(ctx, s); err != nil {
			p.logger.Warn("账号自动恢复失败",
				"account_id", s.id, "username", s.username, "err", summarize(err))
		}
	}()
}

// recordTransient 环境类失败：不动 online/status/failure_count
func (p *Pool) recordTransient(ctx context.Context, acc *model.Account, cause error) accountOutcome {
	msg := summarize(cause)
	next := p.now().Add(p.cfg.Account.LoginBackoff)

	if err := p.store.MarkTransientFailure(ctx, acc.ID, msg, next); err != nil {
		p.logger.Error("记录临时失败出错", "account_id", acc.ID, "err", err.Error())
	}
	p.logger.Warn("账号验证未能完成（不改变账号可用状态）",
		"account_id", acc.ID, "username", acc.Username, "err", msg)
	return outcomeTransient
}

// recordPending 环境原因导致本轮重登没做成：保持待重登，短退避重试
func (p *Pool) recordPending(ctx context.Context, s *session, acc model.Account, cause error) (accountOutcome, error) {
	p.markOffline(ctx, s, model.AccountStatusReloginPending, acc.FailureCount, cause,
		timePtr(p.now().Add(p.cfg.Account.LoginBackoff)))
	p.logger.Warn("账号重登未完成（环境原因，稍后重试）",
		"account_id", s.id, "username", s.username, "err", summarize(cause))
	return outcomePending, cause
}

// recordReloginFailed 尝试次数用尽：标记离线，并按退避慢速重试
func (p *Pool) recordReloginFailed(ctx context.Context, s *session, acc model.Account, cause error) (accountOutcome, error) {
	failures := acc.FailureCount + 1

	p.markOffline(ctx, s, model.AccountStatusReloginFailed, failures, cause,
		timePtr(p.now().Add(p.backoff(failures))))

	p.logger.Error("账号重登失败，已标记离线",
		"account_id", s.id, "username", s.username,
		"failure_count", failures, "err", summarize(cause))
	return outcomeReloginFailed, cause
}

// recordDisabled 凭据/账号级问题：停止自动重试，等人工处理
func (p *Pool) recordDisabled(ctx context.Context, s *session, acc model.Account, cause error) (accountOutcome, error) {
	failures := acc.FailureCount + 1

	p.markOffline(ctx, s, model.AccountStatusDisabled, failures, cause, nil)

	p.logger.Error("账号不可用，已停用（需人工处理：密码错误/账号锁定/二次验证）",
		"account_id", s.id, "username", s.username, "err", summarize(cause))
	return outcomeDisabled, cause
}

// recordBanned 账号被洛谷封禁/限制：登录能成功但所有接口 403。
//
// 不累加 failure_count、不排下次验证：重登对封禁账号没有意义，只会白白打洛谷。
// 解封后由运维用 PATCH enabled=true 复位状态重新入池。
func (p *Pool) recordBanned(ctx context.Context, s *session, acc model.Account, cause error) (accountOutcome, error) {
	p.markOffline(ctx, s, model.AccountStatusBanned, acc.FailureCount, cause, nil)

	p.logger.Error("账号已被洛谷封禁/限制，移出号池（需人工处理：解封后重新启用）",
		"account_id", s.id, "username", s.username, "uid", acc.UIDValue(), "err", summarize(cause))
	return outcomeBanned, cause
}

// markOffline 把账号标记为离线并落库（cookie 已确认失效时使用）
func (p *Pool) markOffline(
	ctx context.Context,
	s *session,
	status string,
	failureCount int32,
	cause error,
	nextVerifyAt *time.Time,
) {
	msg := summarize(cause)

	s.serving.Store(false)
	if err := p.store.UpdatePoolState(ctx, s.id, model.PoolState{
		Online:       false,
		Status:       status,
		FailureCount: failureCount,
		LastError:    msg,
		NextVerifyAt: nextVerifyAt,
	}); err != nil {
		p.logger.Error("更新账号状态失败", "account_id", s.id, "err", err.Error())
	}

	s.mu.Lock()
	s.account.Online = false
	s.account.Status = status
	s.account.FailureCount = failureCount
	s.account.LastError = msg
	s.mu.Unlock()
}

// markAlive 标记账号可用并同步内存快照
func (s *session) markAlive(now time.Time) {
	s.mu.Lock()
	s.account.Online = true
	s.account.Status = model.AccountStatusActive
	s.account.FailureCount = 0
	s.account.LastError = ""
	s.mu.Unlock()

	s.serving.Store(true)
	s.lastOK.Store(now.UnixNano())
}

// nextVerifyAt 计算下次验证时间（带抖动，避免整池同一时刻一起打洛谷）
func (p *Pool) nextVerifyAt(now time.Time) time.Time {
	d := p.cfg.Account.VerifyInterval
	if jitter := p.cfg.Account.VerifyJitter; jitter > 0 {
		factor := 1 + (rand.Float64()*2-1)*jitter
		d = time.Duration(float64(d) * factor)
	}
	if d < time.Second {
		d = time.Second
	}
	return now.Add(d)
}

// backoff 重登失败的指数退避，上限为 FailedRetry
func (p *Pool) backoff(failures int32) time.Duration {
	base := p.cfg.Account.LoginBackoff
	if failures < 1 {
		failures = 1
	}
	if failures > 16 {
		failures = 16
	}

	d := base * time.Duration(int64(1)<<(failures-1))
	if max := p.cfg.Account.FailedRetry; max > 0 && d > max {
		d = max
	}
	if d <= 0 {
		d = base
	}
	return d
}

// loginWindow 单个重登任务的整体超时上限
func (p *Pool) loginWindow() time.Duration {
	w := time.Duration(p.cfg.Account.LoginMaxAttempts) * p.cfg.Luogu.Timeout
	if w < 10*time.Second {
		w = 10 * time.Second
	}
	return w
}

func (p *Pool) getSession(id uint) *session {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sessions[id]
}

func (p *Pool) putSession(s *session) {
	s.serving.Store(s.snapshot().Serving())
	p.mu.Lock()
	p.sessions[s.id] = s
	p.mu.Unlock()
}

// pick 按轮询挑一个可用账号；excluded 用于同一请求内的换号重试
func (p *Pool) pick(excluded map[uint]bool) (*session, error) {
	p.mu.RLock()
	candidates := make([]*session, 0, len(p.sessions))
	for id, s := range p.sessions {
		if excluded[id] || !s.isServing() {
			continue
		}
		candidates = append(candidates, s)
	}
	p.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, ErrPoolExhausted
	}

	// 排序保证轮询顺序稳定（map 遍历本身是随机的）
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].id < candidates[j].id })

	n := p.rr.Add(1) - 1
	return candidates[int(n%uint64(len(candidates)))], nil
}

// withSession 选号执行 fn，命中未授权时自动换号重试。
//
// 闭包在整个调用期间持有会话读锁：重登会等它结束再改 cookie。
func (p *Pool) withSession(ctx context.Context, op string, fn func(SessionClient) error) error {
	maxTry := p.cfg.Account.RequestMaxTry
	if maxTry < 1 {
		maxTry = 1
	}

	tried := make(map[uint]bool, maxTry)
	var lastErr error

	for i := 0; i < maxTry; i++ {
		s, err := p.pick(tried)
		if err != nil {
			if lastErr != nil {
				return fmt.Errorf("%s: %w（已尝试 %d 个账号，最后错误: %v）",
					op, ErrPoolExhausted, len(tried), summarize(lastErr))
			}
			return fmt.Errorf("%s: %w", op, ErrPoolExhausted)
		}
		tried[s.id] = true
		p.logger.Debug("选号执行请求", "op", op, "account_id", s.id, "username", s.username)

		s.mu.RLock()
		callErr := fn(s.client)
		s.mu.RUnlock()

		if callErr == nil {
			s.lastOK.Store(p.now().UnixNano())
			return nil
		}
		lastErr = callErr

		switch classify(callErr) {
		case clsUnauthorized, clsForbidden:
			// 401：cookie 失效；403：账号被封禁/限制。两种都换号重试，
			// 具体状态交给后台核实（403 会在重登后的复核里被判为 banned）。
			p.suspend(s, callErr)
			continue
		default:
			return fmt.Errorf("%s: %w", op, callErr)
		}
	}

	return fmt.Errorf("%s: %w（已尝试 %d 个账号，最后错误: %v）",
		op, ErrPoolExhausted, len(tried), summarize(lastErr))
}

// Load 从数据库加载（或重载）单个账号到号池
func (p *Pool) Load(ctx context.Context, id uint) error {
	acc, err := p.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !acc.Enabled || acc.Status == model.AccountStatusDisabled || acc.Status == model.AccountStatusBanned {
		p.Remove(id)
		return nil
	}

	s, err := p.buildSession(acc)
	if err != nil {
		return err
	}
	p.putSession(s)
	return nil
}

// Remove 把账号移出号池（停用/删除时调用）
func (p *Pool) Remove(id uint) {
	p.mu.Lock()
	delete(p.sessions, id)
	p.mu.Unlock()
}

// ForceRelogin 立即重登指定账号（管理接口用）。
//
// 若账号本来可用、只是这次重登遇到环境问题（OCR/网络），则保持它在线：
// 手动重登不该把一个健康账号摘下线。
func (p *Pool) ForceRelogin(ctx context.Context, id uint) error {
	acc, err := p.store.GetByID(ctx, id)
	if err != nil {
		return err
	}

	s, err := p.sessionFor(acc)
	if err != nil {
		return err
	}
	wasServing := s.isServing()

	outcome, cause, started := p.tryRelogin(ctx, s)
	if !started {
		return errors.New("该账号已有重登任务在进行")
	}
	switch outcome {
	case outcomeRelogged, outcomeOK:
		return nil
	}

	if wasServing && outcome == outcomePending {
		now := p.now()
		if err := p.store.MarkVerified(ctx, s.id, now, p.nextVerifyAt(now)); err != nil {
			p.logger.Error("恢复账号在线状态失败", "account_id", s.id, "err", err.Error())
		}
		s.markAlive(now)
		p.logger.Warn("手动重登未完成（环境原因），账号保持在线",
			"account_id", s.id, "username", s.username, "err", summarize(cause))
	}

	if cause != nil {
		return cause
	}
	return fmt.Errorf("重登未成功（outcome=%d）", int(outcome))
}

// WaitBackground 等待请求路径触发的后台恢复流程结束。
//
// 关服时调用：先停 HTTP、再停扫描器、再等后台恢复，最后才关数据库，
// 避免出现"库已关、后台任务还在写"的报错。
func (p *Pool) WaitBackground(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		p.bg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// classify 把 SDK 错误映射成处置类别。
//
// 除"明确未授权 / 明确凭据错误 / 明确验证码错误"外一律归入 clsTransient：
// 宁可下一轮重试，也不能因为一次网络抖动把账号标记为不可用。
//
// 401 与 403 必须分开：洛谷对"cookie 失效"回 401，对"账号被封禁/限制"回 403
// （实测：封禁账号能正常登录，但 /user/setting、/user/{uid} 全部 403）。
func classify(err error) errClass {
	if err == nil {
		return clsOK
	}

	if errors.Is(err, ErrSolver) {
		return clsTransient
	}

	var unauthorized *sdk.UnauthorizedError
	if errors.As(err, &unauthorized) {
		if unauthorized.StatusCode == http.StatusForbidden {
			return clsForbidden
		}
		return clsUnauthorized
	}

	var authErr *sdk.AuthError
	if errors.As(err, &authErr) {
		if strings.Contains(strings.ToLower(authErr.Type), "captcha") {
			return clsCaptcha
		}
		return clsFatal
	}

	return clsTransient
}

// IsUnauthorized 判断错误是否源自洛谷登录态失效（供 service 层映射响应）
func IsUnauthorized(err error) bool { return classify(err) == clsUnauthorized }

// IsPoolExhausted 判断错误是否为"号池无可用账号"
func IsPoolExhausted(err error) bool { return errors.Is(err, ErrPoolExhausted) }

// summarize 生成可落库的错误摘要（单行、限长，且不含凭据）
func summarize(err error) string {
	if err == nil {
		return ""
	}

	msg := strings.Join(strings.Fields(err.Error()), " ")
	const limit = 480
	if len(msg) > limit {
		msg = msg[:limit] + "..."
	}
	return msg
}

func timePtr(t time.Time) *time.Time { return &t }

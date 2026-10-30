package authorization

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Device 是适配器报告的用户确认入口，不包含内部设备凭据或访问令牌。
type Device struct {
	// ChallengeID 是桥接签发的唯一挑战。
	ChallengeID string
	// Actions 是桥接明确提供的交互。
	Actions []Action
	// Notice 是本次挑战声明的本地化提醒。
	Notice *Notice
	// DeadlineAt 是本机等待上限。
	DeadlineAt int64
	// VerificationURL 是官方原始确认地址。
	VerificationURL string
	// UserCode 是需用户输入的一次性验证码。
	UserCode string
	// ExpiresAt 是本机授权等待期限。
	ExpiresAt int64
}

// Driver 解释包内桥接的共享身份契约。
type Driver interface {
	Logout(context.Context) error
	Status(context.Context, bool) (Account, error)
	Login(context.Context, []string, func(Device) error) error
}

// Config 装配实际协议边界和不包含凭据的变更通知。
type Config struct {
	// Driver 是包内共享身份协议适配器。
	Driver Driver
	// Scopes 是该提供者的最小权限。
	Scopes []string
	// Changed 只发布授权会话变化，不得把会话验证码放入事件载荷。
	Changed func(Session)
	// UsageChanged 发布原生消费者占用或当前执行引用变化，不代表授权或插件配置变化。
	UsageChanged func()
}

// Authorizer 拥有唯一共享 profile 的等待和工作占用，不持久化第二份账号。
type Authorizer struct {
	// summaryMu 保护最近核验摘要，不与授权会话持锁期间的真实核验互锁。
	summaryMu sync.Mutex
	// summary 是当前 Core 内最近一次核验的公开身份，不参与执行准入。
	summary Account
	// summaryGeneration 隔离并发核验与身份重置，拒绝过期查询覆盖新摘要。
	summaryGeneration uint64
	// driverMu 保护固定桥接版本的安全切换。
	driverMu sync.RWMutex
	// config 是提供者协议与最小权限。
	config Config
	// mu 保护会话和身份占用。
	mu sync.Mutex
	// root 是 Core 生命周期。
	root context.Context
	// stop 结束 Core 生命周期。
	stop context.CancelFunc
	// cancel 取消当前真实设备流程。
	cancel context.CancelCauseFunc
	// session 是可交付当前界面的安全摘要。
	session Session
	// usages 是当前工作对共享身份的引用。
	usages map[string]Usage
	// active 是共享身份的实际占用数。
	active int
	// deviceRunning 在真实进程退出前阻止身份变更与新工作。
	deviceRunning bool
	// deviceDone 在真实授权进程和完成通知全部退出后关闭。
	deviceDone <-chan struct{}
	// resetting 阻止最后引用清理期间接纳新的身份使用或授权。
	resetting bool
	// workers 等待授权进程完整结束。
	workers sync.WaitGroup
}

// New 不启动后台协程，生命周期由 Open/Close 管理。
func New(config Config) *Authorizer {
	config.Scopes = slices.Clone(config.Scopes)
	return &Authorizer{config: config, usages: map[string]Usage{}, session: Session{Account: Account{Scopes: []string{}}}}
}
func (a *Authorizer) Open(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.root == nil {
		a.root, a.stop = context.WithCancel(ctx)
	}
}
func (a *Authorizer) Close() {
	a.mu.Lock()
	if a.stop != nil {
		a.stop()
	}
	if a.cancel != nil {
		a.cancel(context.Canceled)
	}
	a.mu.Unlock()
	a.workers.Wait()
}

// RequiredScopes 合并提供者及插件权限，保持稳定顺序且不跨提供者追加权限。
func (a *Authorizer) RequiredScopes(requested ...string) []string {
	result := []string{}
	for _, scope := range append(slices.Clone(a.config.Scopes), requested...) {
		if !slices.Contains(result, scope) {
			result = append(result, scope)
		}
	}
	return result
}

// ScopeGranted 委托提供者解释权限包含关系，默认使用精确匹配。
func (a *Authorizer) ScopeGranted(granted []string, required string) bool {
	if driver, ok := a.driver().(interface{ ScopeGranted([]string, string) bool }); ok {
		return driver.ScopeGranted(granted, required)
	}
	return slices.Contains(granted, required)
}

func (a *Authorizer) Status(ctx context.Context, verify bool) (Account, error) {
	a.summaryMu.Lock()
	a.summaryGeneration++
	generation := a.summaryGeneration
	a.summaryMu.Unlock()
	result, err := a.driver().Status(ctx, verify)
	for _, scope := range a.config.Scopes {
		if !a.ScopeGranted(result.Scopes, scope) {
			result.Available = false
		}
	}
	if err == nil {
		result.CheckedAt = time.Now().UnixMilli()
	} else {
		result = Account{Scopes: []string{}}
	}
	a.summaryMu.Lock()
	if generation == a.summaryGeneration {
		a.summary = result
		a.summary.Scopes = slices.Clone(result.Scopes)
	}
	a.summaryMu.Unlock()
	return result, err
}

// CachedAccount 只返回当前 Core 的最近核验摘要，不启动驱动或读取身份文件。
func (a *Authorizer) CachedAccount() Account {
	a.summaryMu.Lock()
	defer a.summaryMu.Unlock()
	result := a.summary
	result.Scopes = append([]string{}, result.Scopes...)
	return result
}

func (a *Authorizer) clearSummary(checked bool) {
	a.summaryMu.Lock()
	defer a.summaryMu.Unlock()
	a.summaryGeneration++
	a.summary = Account{Scopes: []string{}}
	if checked {
		a.summary.CheckedAt = time.Now().UnixMilli()
	}
}

func (a *Authorizer) Acquire(uses ...Usage) (func(), error) {
	_, release, err := a.AcquireUsage(uses...)
	return release, err
}

// AcquireUsage 将原生消费者的身份占用与可随回合更新的执行引用分开维护。
func (a *Authorizer) AcquireUsage(uses ...Usage) (func(...Usage), func(), error) {
	a.mu.Lock()
	if a.deviceRunning || a.resetting {
		a.mu.Unlock()
		return nil, nil, status_error.PluginBusy
	}
	a.active++
	id := util.New()
	if len(uses) > 0 {
		a.usages[id] = uses[0]
	}
	released := false
	a.mu.Unlock()
	a.publishUsage()
	update := func(current ...Usage) {
		a.mu.Lock()
		if released {
			a.mu.Unlock()
			return
		}
		delete(a.usages, id)
		if len(current) > 0 {
			a.usages[id] = current[0]
		}
		a.mu.Unlock()
		a.publishUsage()
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			a.mu.Lock()
			released = true
			a.active--
			delete(a.usages, id)
			a.mu.Unlock()
			a.publishUsage()
		})
	}
	return update, release, nil
}
func (a *Authorizer) Usages() []Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Usage, 0, len(a.usages))
	for _, u := range a.usages {
		result = append(result, u)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExecutionID < result[j].ExecutionID })
	return result
}
func (a *Authorizer) Logout(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active > 0 || a.deviceRunning || a.resetting {
		return status_error.PluginBusy
	}
	if err := a.driver().Logout(ctx); err != nil {
		return err
	}
	a.clearSummary(true)
	a.session = Session{Account: Account{Scopes: []string{}}}
	return nil
}
func (a *Authorizer) Snapshot() Session { a.mu.Lock(); defer a.mu.Unlock(); return a.snapshotLocked() }
func (a *Authorizer) snapshotLocked() Session {
	s := a.session
	s.Account.Scopes = append([]string{}, s.Account.Scopes...)
	s.Actions = slices.Clone(s.Actions)
	s.Notice = cloneNotice(s.Notice)
	return s
}

// cloneNotice 隔离驱动、会话及观察者持有的提示映射。
func cloneNotice(notice *Notice) *Notice {
	if notice == nil {
		return nil
	}
	return &Notice{Messages: maps.Clone(notice.Messages)}
}

// Login 的 HTTP 请求只等待确认入口；真实授权由 Core 根生命周期继续拥有。
func (a *Authorizer) Login(ctx context.Context, requested ...string) (Session, error) {
	a.mu.Lock()
	if a.resetting {
		a.mu.Unlock()
		return Session{}, status_error.PluginBusy
	}
	if a.session.State == constants.PluginAuthorizationStateWaiting {
		s := a.snapshotLocked()
		a.mu.Unlock()
		return s, nil
	}
	if a.active > 0 || a.deviceRunning {
		a.mu.Unlock()
		return Session{}, status_error.PluginBusy
	}
	if a.root == nil || a.root.Err() != nil {
		a.mu.Unlock()
		return Session{}, status_error.PluginNotReady
	}
	account, err := a.Status(ctx, false)
	_, canInitialize := a.driver().(interface {
		Initialize(context.Context, func(Device) error) error
	})
	if err != nil || (!account.Configured && !canInitialize) {
		a.mu.Unlock()
		if err != nil {
			return Session{}, err
		}
		return Session{}, status_error.PluginNotReady
	}
	work, cancel := context.WithCancelCause(a.root)
	a.cancel = cancel
	a.deviceRunning = true
	a.session = Session{ID: util.New(), State: constants.PluginAuthorizationStateWaiting, Account: account}
	id := a.session.ID
	scopes := a.RequiredScopes(requested...)
	initialized := make(chan struct{}, 1)
	finished := make(chan struct{})
	a.deviceDone = finished
	a.workers.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.workers.Done()
		defer close(finished)
		defer cancel(context.Canceled)
		timeout := time.AfterFunc(constants.PluginAuthPreparationTimeoutSeconds*time.Second, func() { cancel(context.DeadlineExceeded) })
		defer timeout.Stop()
		reportDevice := func(device Device) error {
			expiry := device.ExpiresAt
			if expiry == 0 {
				expiry = device.DeadlineAt
			}
			duration := time.Until(time.UnixMilli(expiry))
			if len(device.Actions) == 0 {
				parsed, err := url.Parse(device.VerificationURL)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
					return status_error.PluginAuthorizationFailed
				}
				device.Actions = []Action{{Type: "open-url", URL: device.VerificationURL}, {Type: "show-qr", Value: device.VerificationURL}}
				if device.UserCode != "" {
					device.Actions = append(device.Actions, Action{Type: "copy-code", Value: device.UserCode})
				}
			}
			if duration <= 0 || duration > 30*time.Minute {
				return status_error.PluginAuthorizationFailed
			}
			a.mu.Lock()
			if a.session.ID != id || a.session.State != constants.PluginAuthorizationStateWaiting {
				a.mu.Unlock()
				return context.Canceled
			}
			a.session.ChallengeID = device.ChallengeID
			if a.session.ChallengeID == "" {
				a.session.ChallengeID = util.New()
			}
			a.session.Actions = slices.Clone(device.Actions)
			a.session.Notice = cloneNotice(device.Notice)
			a.session.DeadlineAt = device.DeadlineAt
			a.session.VerificationURL = device.VerificationURL
			a.session.UserCode = device.UserCode
			a.session.ExpiresAt = device.ExpiresAt
			snapshot := a.snapshotLocked()
			a.mu.Unlock()
			timeout.Reset(duration)
			a.publish(snapshot)
			select {
			case initialized <- struct{}{}:
			default:
			}
			return nil
		}
		var err error
		if !account.Configured {
			// 只有声明初始化能力的官方适配器才能在同一独占会话内先完成应用扫码。
			err = a.driver().(interface {
				Initialize(context.Context, func(Device) error) error
			}).Initialize(work, reportDevice)
		}
		if err == nil {
			if driver, ok := a.driver().(interface {
				LoginSession(context.Context, string, []string, func(Device) error) error
			}); ok {
				err = driver.LoginSession(work, id, scopes, reportDevice)
			} else {
				err = a.driver().Login(work, scopes, reportDevice)
			}
		}
		result := Account{Scopes: []string{}}
		if err == nil {
			result, err = a.Status(work, true)
			for _, scope := range scopes {
				if !a.ScopeGranted(result.Scopes, scope) {
					result.Available = false
				}
			}
			if !result.Available {
				err = status_error.PluginAuthorizationFailed
			}
		}
		if err == nil {
			if complete, ok := a.driver().(interface {
				CompleteLogin(context.Context, Account) error
			}); ok {
				err = complete.CompleteLogin(work, result)
			}
		}
		a.mu.Lock()
		a.deviceRunning = false
		a.cancel = nil
		if a.session.ID == id && a.session.State == constants.PluginAuthorizationStateWaiting {
			a.session.State = constants.PluginAuthorizationStateReady
			if err != nil {
				a.session.State = constants.PluginAuthorizationStateFailed
				a.session.ErrorCode = "PluginAuthorizationFailed"
				if context.Cause(work) == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
					a.session.State = constants.PluginAuthorizationStateExpired
				} else if work.Err() != nil || errors.Is(err, context.Canceled) {
					a.session.State = constants.PluginAuthorizationStateCancelled
				}
			}
			a.session.Actions = nil
			a.session.Notice = nil
			a.session.ChallengeID = ""
			a.session.VerificationURL = ""
			a.session.UserCode = ""
			a.session.Account = result
		}
		snapshot := a.snapshotLocked()
		a.mu.Unlock()
		a.publish(snapshot)
	}()
	select {
	case <-initialized:
		return a.Snapshot(), nil
	case <-finished:
		snapshot := a.Snapshot()
		if snapshot.State == constants.PluginAuthorizationStateReady {
			return snapshot, nil
		}
		return Session{}, status_error.PluginAuthorizationFailed
	case <-ctx.Done():
		cancel(ctx.Err())
		return Session{}, ctx.Err()
	}
}

// Cancel 立即清理短时交互信息，但占用必须留到真实进程结束。
func (a *Authorizer) Cancel(id string) error {
	_, err := a.cancelSession(id)
	return err
}

// CancelAndWait 在刷新二维码前等待旧进程退出，避免新会话撞上仍在清理的身份占用。
func (a *Authorizer) CancelAndWait(ctx context.Context, id string) error {
	done, err := a.cancelSession(id)
	if err != nil {
		return err
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return status_error.OperationCanceled
		}
	}
	return nil
}

func (a *Authorizer) cancelSession(id string) (<-chan struct{}, error) {
	a.mu.Lock()
	if a.session.ID != id {
		a.mu.Unlock()
		return nil, status_error.PluginNotFound
	}
	if a.session.State == constants.PluginAuthorizationStateWaiting {
		a.session.State = constants.PluginAuthorizationStateCancelled
		a.session.Actions = nil
		a.session.Notice = nil
		a.session.ChallengeID = ""
		a.session.VerificationURL = ""
		a.session.UserCode = ""
		if a.cancel != nil {
			a.cancel(context.Canceled)
		}
	}
	done := a.deviceDone
	snapshot := a.snapshotLocked()
	a.mu.Unlock()
	a.publish(snapshot)
	return done, nil
}
func (a *Authorizer) publish(session Session) {
	if a.config.Changed != nil {
		a.config.Changed(session)
	}
}

func (a *Authorizer) publishUsage() {
	if a.config.UsageChanged != nil {
		a.config.UsageChanged()
	}
}

// ReferenceGuard 让安装引用变更与身份清理共享原有占用边界。
type ReferenceGuard struct {
	// authorizer 是唯一受保护的共享身份。
	authorizer *Authorizer
	// once 保证清理边界只释放一次。
	once sync.Once
}

// GuardReferences 阻止新的身份使用，已有工作继续持有原占用。
func (a *Authorizer) GuardReferences() (*ReferenceGuard, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.resetting {
		return nil, status_error.PluginBusy
	}
	a.resetting = true
	return &ReferenceGuard{authorizer: a}, nil
}

// CanReset 表示当前没有仍在执行或安装的身份使用。
func (g *ReferenceGuard) CanReset() bool {
	g.authorizer.mu.Lock()
	defer g.authorizer.mu.Unlock()
	return g.authorizer.active == 0
}

// Reset 取消授权等待，等真实进程退出后通过原驱动清理官方本机授权。
func (g *ReferenceGuard) Reset(ctx context.Context) error {
	a := g.authorizer
	a.mu.Lock()
	if a.active > 0 {
		a.mu.Unlock()
		return status_error.PluginBusy
	}
	if a.session.State == constants.PluginAuthorizationStateWaiting {
		a.session.State = constants.PluginAuthorizationStateCancelled
		a.session.VerificationURL, a.session.UserCode = "", ""
		a.session.Actions = nil
		a.session.Notice = nil
		a.session.ChallengeID = ""
	}
	if a.cancel != nil {
		a.cancel(context.Canceled)
	}
	done := a.deviceDone
	snapshot := a.snapshotLocked()
	a.mu.Unlock()
	a.publish(snapshot)
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return status_error.OperationCanceled
		}
	}
	if err := a.driver().Logout(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	a.clearSummary(true)
	a.session = Session{Account: Account{Scopes: []string{}}}
	snapshot = a.snapshotLocked()
	a.mu.Unlock()
	a.publish(snapshot)
	return nil
}

// Close 释放短时清理边界；未退出的授权进程仍受 deviceRunning 保护。
func (g *ReferenceGuard) Close() {
	g.once.Do(func() {
		g.authorizer.mu.Lock()
		g.authorizer.resetting = false
		g.authorizer.mu.Unlock()
	})
}

// ReplaceDriver 仅在授权与执行均空闲时切换固定入口，保留同一共享身份租约。
func (a *Authorizer) ReplaceDriver(driver Driver) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active > 0 || a.deviceRunning || a.resetting {
		return status_error.PluginBusy
	}
	a.driverMu.Lock()
	a.config.Driver = driver
	a.driverMu.Unlock()
	a.clearSummary(false)
	a.session = Session{Account: Account{Scopes: []string{}}}
	return nil
}
func (a *Authorizer) driver() Driver {
	a.driverMu.RLock()
	defer a.driverMu.RUnlock()
	return a.config.Driver
}

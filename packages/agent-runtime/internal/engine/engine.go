// Package engine 拥有调用、热会话与租约的唯一技术生命周期。
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/agent-runtime/internal/plan"
	"strconv"
	"sync"
	"time"
)

// Config 是显式依赖与工程保护值。
type Config struct {
	Drivers              []c.Driver           // Drivers 是构造前准备好的协议驱动。
	Resources            c.ResourceProvider   // Resources 是可选会话资源端口。
	Credentials          c.CredentialProvider // Credentials 是可选账号租约端口。
	Grants               c.Grants             // Grants 是宿主授权上界。
	Limits               c.Limits             // Limits 是默认调用预算。
	MaxActiveInvocations int                  // MaxActiveInvocations 是活动调用和待用调用身份各自的上限。
	MaxOpenSessions      int                  // MaxOpenSessions 是包含准备中会话的上限。
	CleanupTimeout       time.Duration        // CleanupTimeout 是一次清理的总期限。
}

// Engine 在明确 Start 后接纳调用，构造不启动后台工作。
type Engine struct {
	mu            sync.Mutex                      // mu 保护接纳、取消和会话所有权。
	config        Config                          // config 是已冻结的模块配置。
	drivers       map[string]c.Driver             // drivers 是构造后只读的驱动集合。
	capabilities  map[string]c.DriverCapabilities // capabilities 是构造时冻结的能力声明。
	id            string                          // id 是本引擎随机身份。
	counter       uint64                          // counter 在引擎中单调分配调用与会话身份。
	pending       map[string]bool                 // pending 保存有界的未消费预分配身份。
	active        map[string]*invocation          // active 保存清理完成前的活动调用。
	sessions      map[string]*session             // sessions 保存本引擎的热会话与准备占位。
	started       bool                            // started 表示已开放接纳。
	closing       bool                            // closing 表示已永久停止接纳。
	closeAttempt  *cleanupAttempt                 // closeAttempt 是当前或最近一次显式关闭尝试。
	closeRunning  bool                            // closeRunning 表示关闭尝试正在独占闲置资源。
	closeComplete bool                            // closeComplete 表示所有会话与驱动已确认释放。
	closedDrivers map[string]bool                 // closedDrivers 避免重试时重复关闭已经成功的驱动。
	work          sync.WaitGroup                  // work 等待活动调用和已开始的显式会话清理。
}

type invocation struct {
	ref          c.InvocationRef         // ref 是有效调用及会话修订。
	ctx          context.Context         // ctx 是本轮生命周期。
	cancel       context.CancelCauseFunc // cancel 保留第一个有效取消根因。
	done         chan struct{}           // done 在终态和资源收敛后关闭。
	session      *session                // session 是本次唯一占用的会话。
	finished     bool                    // finished 是取消与完成的线性化边界。
	cleanupFault *c.Fault                // cleanupFault 在 done 关闭前写入，供 CloseSession 读取。
}

type session struct {
	id            string                        // id 是引擎分配的会话身份。
	driverID      string                        // driverID 是唯一驱动选择。
	contextDigest string                        // contextDigest 是静态配置兼容摘要。
	securityKey   string                        // securityKey 是账号与权限兼容摘要。
	revision      uint64                        // revision 每次接纳回合递增。
	driver        c.DriverSession               // driver 是唯一拥有的原生会话。
	closers       []func(context.Context) error // closers 按取得顺序保存会话资源释放函数。
	call          *invocation                   // call 是当前唯一调用，空值表示闲置。
	closing       bool                          // closing 禁止显式关闭期间再次接纳。
	cleanupBusy   bool                          // cleanupBusy 表示某次清理独占会话释放栈。
	cleanupDone   chan struct{}                 // cleanupDone 在当前清理尝试完成时关闭。
	cleanupFault  *c.Fault                      // cleanupFault 保存最近一次清理失败，不丢失资源所有权。
}

type cleanupAttempt struct {
	done chan struct{} // done 在本次关闭尝试完成时关闭。
	err  error         // err 是本次关闭尝试的准确结果，发布后不再改变。
}

// New 校验并冻结配置，不创建 goroutine 或原生资源。
func New(cfg Config) (*Engine, error) {
	if len(cfg.Drivers) == 0 {
		return nil, fault(c.FaultInvalidSpec, "configure", "at least one driver is required", nil)
	}
	if cfg.MaxActiveInvocations < 0 || cfg.MaxOpenSessions < 0 || cfg.CleanupTimeout < 0 {
		return nil, fault(c.FaultInvalidSpec, "configure", "negative lifecycle budget", nil)
	}
	if cfg.MaxActiveInvocations == 0 {
		cfg.MaxActiveInvocations = 32
	}
	if cfg.MaxOpenSessions == 0 {
		cfg.MaxOpenSessions = 64
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = 5 * time.Second
	}
	var err error
	cfg.Limits, err = plan.NormalizeLimits(cfg.Limits, plan.DefaultLimits())
	if err != nil {
		return nil, err
	}
	cfg.Drivers = append([]c.Driver(nil), cfg.Drivers...)
	cfg.Grants.Directories = append([]string(nil), cfg.Grants.Directories...)
	cfg.Grants.Tools = append([]string(nil), cfg.Grants.Tools...)
	e := &Engine{config: cfg, drivers: make(map[string]c.Driver), capabilities: make(map[string]c.DriverCapabilities), pending: make(map[string]bool), active: make(map[string]*invocation), sessions: make(map[string]*session), closedDrivers: make(map[string]bool)}
	var identity [16]byte
	if _, err = rand.Read(identity[:]); err != nil {
		return nil, fault(c.FaultPreparation, "configure", "engine identity unavailable", err)
	}
	e.id = hex.EncodeToString(identity[:])
	for _, d := range cfg.Drivers {
		if d == nil || d.ID() == "" {
			return nil, fault(c.FaultInvalidSpec, "configure", "driver identity missing", nil)
		}
		if _, ok := e.drivers[d.ID()]; ok {
			return nil, fault(c.FaultInvalidSpec, "configure", "duplicate driver identity", nil)
		}
		e.drivers[d.ID()] = d
		e.capabilities[d.ID()] = d.Capabilities()
	}
	return e, nil
}

// Start 显式打开接纳，无探测、预热或后台监听器。
func (e *Engine) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fault(c.FaultCancelled, "start", "start cancelled", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return fault(c.FaultClosed, "start", "runtime is closed", nil)
	}
	e.started = true
	return nil
}

// NewInvocationID 预留可在运行前取消的唯一调用身份；待用身份有界。
func (e *Engine) NewInvocationID() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return "", fault(c.FaultClosed, "admission", "runtime is closed", nil)
	}
	if len(e.pending) >= e.config.MaxActiveInvocations {
		return "", fault(c.FaultCapacity, "admission", "pending invocation identity budget exceeded", nil)
	}
	id := e.nextID("i")
	e.pending[id] = true
	return id, nil
}
func (e *Engine) nextID(kind string) string {
	e.counter++
	return e.id + "/" + kind + "/" + strconv.FormatUint(e.counter, 10)
}

// Execute 为 Invoke 和 Run 共用同一接纳与执行机制。
func (e *Engine) Execute(ctx context.Context, spec c.ExecutionSpec, options c.RunOptions, temporary bool) c.Result {
	if temporary {
		if spec.Session.Mode == c.SessionResume || spec.Session.Ref != "" || spec.Session.Checkpoint != nil || len(spec.Session.Seed) > 0 {
			return failed(spec.InvocationID, fault(c.FaultInvalidSpec, "plan", "invoke requires a temporary new session without seed", nil))
		}
		spec.Session.Mode = c.SessionNew
		spec.Policies.Interaction = c.InteractionUnattended
		options = c.RunOptions{}
	}
	driver, ok := e.drivers[spec.Profile.DriverID]
	if !ok {
		return failed(spec.InvocationID, fault(c.FaultInvalidSpec, "plan", "requested driver is not registered", nil))
	}
	compiled, err := plan.Compile(spec, e.capabilities[spec.Profile.DriverID], e.config.Grants, e.config.Limits)
	if err != nil {
		return failed(spec.InvocationID, asFault(err, c.FaultInvalidSpec, "plan", "execution description rejected"))
	}
	frozen := compiled.Spec()
	call, err := e.admit(ctx, frozen, compiled)
	if err != nil {
		return failed(spec.InvocationID, asFault(err, c.FaultInvalidSpec, "admission", "invocation rejected"))
	}
	result := c.Result{Invocation: call.ref, Status: c.StatusFailed, StopReason: c.StopFailed, Output: c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}}
	// 调用占位在任何外部准备前建立；关闭等待包含准备与释放全过程。
	defer func() {
		e.mu.Lock()
		delete(e.active, call.ref.ID)
		close(call.done)
		e.mu.Unlock()
		call.cancel(nil)
		e.work.Done()
	}()
	sess := call.session
	if sess.driver == nil {
		err = e.prepare(call, driver, frozen, compiled)
		if err != nil {
			result.Fault = asFault(err, c.FaultPreparation, "prepare", "session preparation failed")
			e.settle(call, &result)
			result.CleanupFault = e.releaseInvocationSession(call)
			return result
		}
	}
	sink := newSink(call, frozen, options, e.config.CleanupTimeout)
	raw := sess.driver.Prompt(call.ctx, c.TurnRequest{Invocation: call.ref, Input: frozen.Input, Limits: frozen.Limits}, sink)
	delivery, effects, streamFault := sink.finish()
	result.Status = raw.Status
	result.StopReason = raw.StopReason
	result.Fault = raw.Fault
	result.Usage = raw.Usage
	result.Configuration = raw.Configuration
	result.DeliveryFault = delivery
	result.Effects = effects
	if streamFault != nil && result.Fault == nil {
		result.Fault = streamFault
	}
	if result.Status == "" || result.StopReason == "" {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		if result.Fault == nil {
			result.Fault = fault(c.FaultProtocol, "prompt", "driver returned no execution terminal state", nil)
		}
	}
	if result.Status == c.StatusCompleted && !result.Effects.PromptAccepted && result.Fault == nil {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		result.Fault = fault(c.FaultProtocol, "prompt", "driver completed without prompt acceptance", nil)
	}
	e.settle(call, &result)
	result.Output = sink.collector.Finish(result.StopReason)
	keep := false
	e.mu.Lock()
	keep = !temporary && e.capabilities[sess.driverID].HotResume && !e.closing && !sess.closing && result.Status == c.StatusCompleted && result.Fault == nil && result.DeliveryFault == nil
	if keep {
		sess.call = nil
	} else {
		sess.closing = true
	}
	e.mu.Unlock()
	if !keep {
		result.CleanupFault = e.releaseInvocationSession(call)
	}
	return result
}

func (e *Engine) admit(ctx context.Context, spec c.ExecutionSpec, p plan.Plan) (*invocation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return nil, fault(c.FaultClosed, "admission", "runtime is closed", nil)
	}
	if !e.started {
		return nil, fault(c.FaultNotStarted, "admission", "runtime has not started", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, fault(c.FaultCancelled, "admission", "invocation context cancelled", err)
	}
	if len(e.active) >= e.config.MaxActiveInvocations {
		return nil, fault(c.FaultCapacity, "admission", "active invocation budget exceeded", nil)
	}
	id := spec.InvocationID
	if id != "" {
		if !e.pending[id] {
			return nil, fault(c.FaultDuplicateInvocation, "admission", "invocation identity is not pending in this runtime", nil)
		}
		delete(e.pending, id)
	} else {
		id = e.nextID("i")
	}
	var sess *session
	if spec.Session.Mode == c.SessionResume {
		sess = e.sessions[spec.Session.Ref]
		if sess == nil {
			return nil, fault(c.FaultSessionMissing, "admission", "session is not available in this runtime", nil)
		}
		if sess.call != nil || sess.closing {
			return nil, fault(c.FaultSessionBusy, "admission", "session already has an active owner", nil)
		}
		if sess.driverID != spec.Profile.DriverID || sess.contextDigest != p.ContextDigest() || sess.securityKey != p.SecurityScopeKey() {
			return nil, fault(c.FaultSessionMismatch, "admission", "session static context or security scope changed", nil)
		}
	} else {
		if len(e.sessions) >= e.config.MaxOpenSessions {
			return nil, fault(c.FaultCapacity, "admission", "open session budget exceeded", nil)
		}
		sess = &session{id: e.nextID("s"), driverID: spec.Profile.DriverID, contextDigest: p.ContextDigest(), securityKey: p.SecurityScopeKey()}
		e.sessions[sess.id] = sess
	}
	sess.revision++
	callCtx, cancel := context.WithCancelCause(ctx)
	call := &invocation{ref: c.InvocationRef{ID: id, SessionID: sess.id, Revision: sess.revision}, ctx: callCtx, cancel: cancel, done: make(chan struct{}), session: sess}
	sess.call = call
	e.active[id] = call
	e.work.Add(1)
	return call, nil
}

func (e *Engine) prepare(call *invocation, driver c.Driver, spec c.ExecutionSpec, p plan.Plan) error {
	sess := call.session
	credential := c.Credential{}
	if spec.Profile.CredentialRef != "" {
		if e.config.Credentials == nil {
			return fault(c.FaultPreparation, "credential", "credential provider is required", nil)
		}
		lease, err := e.config.Credentials.Acquire(call.ctx, c.CredentialRequest{Ref: spec.Profile.CredentialRef, Subject: spec.Profile.Subject, Generation: spec.Profile.CredentialGeneration})
		if lease != nil {
			sess.closers = append(sess.closers, lease.Close)
		}
		if err != nil {
			return fault(c.FaultPreparation, "credential", "credential acquisition failed", err)
		}
		if lease == nil {
			return fault(c.FaultProtocol, "credential", "credential provider returned no lease", nil)
		}
		credential = lease.Credential()
		copyEnvironment := make(map[string]string, len(credential.Environment))
		for key, value := range credential.Environment {
			copyEnvironment[key] = value
		}
		credential.Environment = copyEnvironment
	}
	var resources []c.ResourceBinding
	if e.config.Resources != nil {
		resourceSpec := p.Spec()
		lease, err := e.config.Resources.Acquire(call.ctx, c.ResourceRequest{Invocation: call.ref, Workspace: resourceSpec.Workspace, Contributions: resourceSpec.Contributions})
		if lease != nil {
			sess.closers = append(sess.closers, lease.Close)
		}
		if err != nil {
			return fault(c.FaultPreparation, "resource", "resource materialization failed", err)
		}
		if lease == nil {
			return fault(c.FaultProtocol, "resource", "resource provider returned no lease", nil)
		}
		resources = append([]c.ResourceBinding(nil), lease.Resources()...)
	}
	if err := validateResources(spec.Contributions, resources); err != nil {
		return err
	}
	if err := call.ctx.Err(); err != nil {
		return fault(c.FaultCancelled, "prepare", "preparation cancelled", err)
	}
	openSpec := p.Spec()
	native, err := driver.Open(call.ctx, c.OpenRequest{Invocation: call.ref, Profile: openSpec.Profile, Session: openSpec.Session, Workspace: openSpec.Workspace, Contributions: openSpec.Contributions, Policies: openSpec.Policies, Resources: resources, Credential: credential, ContextDigest: p.ContextDigest(), SecurityScopeKey: p.SecurityScopeKey()})
	if native != nil {
		sess.driver = native
		sess.closers = append(sess.closers, native.Close)
	}
	if err != nil {
		return asFault(err, c.FaultPreparation, "open", "native session preparation failed")
	}
	if native == nil {
		return fault(c.FaultProtocol, "open", "driver returned no session", nil)
	}
	if err = call.ctx.Err(); err != nil {
		return fault(c.FaultCancelled, "open", "session open cancelled", err)
	}
	return nil
}

func validateResources(contributions c.Contributions, resources []c.ResourceBinding) error {
	bindings := make(map[string]string, len(resources))
	for _, binding := range resources {
		if binding.ID == "" || binding.Path == "" {
			return fault(c.FaultProtocol, "resource", "invalid resource binding", nil)
		}
		if path, ok := bindings[binding.ID]; ok && path != binding.Path {
			return fault(c.FaultPreparation, "resource", "conflicting resource binding", nil)
		}
		bindings[binding.ID] = binding.Path
	}
	for _, skill := range contributions.Skills {
		if bindings[skill.ResourceID] == "" && !skill.Optional {
			return fault(c.FaultPreparation, "resource", "required skill resource missing", nil)
		}
	}
	for _, mcp := range contributions.MCP {
		if bindings[mcp.ResourceID] == "" && !mcp.Optional {
			return fault(c.FaultPreparation, "resource", "required mcp resource missing", nil)
		}
	}
	return nil
}

func (e *Engine) settle(call *invocation, result *c.Result) {
	e.mu.Lock()
	defer e.mu.Unlock()
	call.finished = true
	if cause := context.Cause(call.ctx); cause != nil {
		first := asFault(cause, c.FaultCancelled, "execute", "invocation cancelled")
		result.Fault = first
		result.Status = c.StatusCancelled
		result.StopReason = c.StopCancelled
		if first.Code != c.FaultCancelled {
			result.Status = c.StatusFailed
			result.StopReason = c.StopFailed
		}
		if first.Code == c.FaultInteractionRequired {
			result.StopReason = c.StopInteractionRequired
		}
	}
	if result.Status != c.StatusCompleted && result.Status != c.StatusCancelled && result.Status != c.StatusFailed {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		result.Fault = fault(c.FaultProtocol, "prompt", "invalid driver execution status", nil)
	}
	if (result.Effects.PromptAccepted || result.Effects.ToolActivity) && result.Status != c.StatusCompleted {
		result.Effects.OutcomeUnknown = true
	}
}

func (e *Engine) releaseInvocationSession(call *invocation) *c.Fault {
	sess := call.session
	e.mu.Lock()
	sess.closing = true
	e.beginSessionCleanup(sess)
	e.mu.Unlock()
	cleanup := e.releaseSession(sess)
	e.mu.Lock()
	call.cleanupFault = cleanup
	e.finishSessionCleanup(sess, cleanup)
	e.mu.Unlock()
	return cleanup
}

// beginSessionCleanup 与 finishSessionCleanup 均由接纳锁保护。
func (e *Engine) beginSessionCleanup(sess *session) {
	sess.cleanupBusy = true
	sess.cleanupDone = make(chan struct{})
}
func (e *Engine) finishSessionCleanup(sess *session, cleanup *c.Fault) {
	sess.cleanupBusy = false
	sess.cleanupFault = cleanup
	sess.call = nil
	if cleanup == nil {
		delete(e.sessions, sess.id)
	}
	close(sess.cleanupDone)
}
func (e *Engine) releaseSession(sess *session) *c.Fault {
	ctx, cancel := context.WithTimeout(context.Background(), e.config.CleanupTimeout)
	defer cancel()
	return e.releaseSessionWith(ctx, sess)
}
func (e *Engine) releaseSessionWith(ctx context.Context, sess *session) *c.Fault {
	for len(sess.closers) > 0 {
		last := len(sess.closers) - 1
		// 只有成功才证明此使用者已停止；失败保留栈顶及其依赖，供显式重试。
		if err := sess.closers[last](ctx); err != nil {
			return fault(c.FaultCleanup, "cleanup", "session resource release incomplete; dependencies retained", err)
		}
		sess.closers[last] = nil
		sess.closers = sess.closers[:last]
	}
	return nil
}

// Cancel 消耗待用身份或取消其当前调用；完成后身份永不重新接纳。
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending[id] {
		delete(e.pending, id)
		return true
	}
	call := e.active[id]
	if call == nil || call.finished {
		return false
	}
	call.cancel(fault(c.FaultCancelled, "cancel", "invocation cancelled explicitly", nil))
	return true
}

// CloseSession 阻止会话重入，取消活动回合后独立有界释放。
func (e *Engine) CloseSession(ctx context.Context, id string) error {
	e.mu.Lock()
	sess := e.sessions[id]
	if sess == nil {
		e.mu.Unlock()
		return fault(c.FaultSessionMissing, "close_session", "session is not available", nil)
	}
	sess.closing = true
	if sess.call != nil {
		call := sess.call
		if !call.finished {
			call.cancel(fault(c.FaultCancelled, "close_session", "session closed explicitly", nil))
		}
		done := call.done
		e.mu.Unlock()
		select {
		case <-done:
			if call.cleanupFault != nil {
				return call.cleanupFault
			}
			return nil
		case <-ctx.Done():
			return fault(c.FaultCleanup, "close_session", "waiting for active session close timed out", ctx.Err())
		}
	}
	if sess.cleanupBusy {
		done := sess.cleanupDone
		e.mu.Unlock()
		select {
		case <-done:
			e.mu.Lock()
			cleanup := sess.cleanupFault
			e.mu.Unlock()
			if cleanup != nil {
				return cleanup
			}
			return nil
		case <-ctx.Done():
			return fault(c.FaultCleanup, "close_session", "waiting for session cleanup timed out", ctx.Err())
		}
	}
	if e.closeRunning {
		e.mu.Unlock()
		return fault(c.FaultSessionBusy, "close_session", "runtime cleanup currently owns session", nil)
	}
	e.beginSessionCleanup(sess)
	e.work.Add(1)
	e.mu.Unlock()
	// 等待 Context 不拥有释放 Context；等待结束后 Runtime 仍持有未释放资源。
	done := make(chan *c.Fault, 1)
	go func() {
		defer e.work.Done()
		cleanup := e.releaseSession(sess)
		e.mu.Lock()
		e.finishSessionCleanup(sess, cleanup)
		e.mu.Unlock()
		done <- cleanup
	}()
	select {
	case cleanup := <-done:
		if cleanup != nil {
			return cleanup
		}
		return nil
	case <-ctx.Done():
		return fault(c.FaultCleanup, "close_session", "waiting for session cleanup timed out", ctx.Err())
	}
}

// Close 永久停止接纳并启动唯一清理；调用者期限不取消已经开始的释放。
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	if !e.closing {
		e.closing = true
		clear(e.pending)
		for _, call := range e.active {
			if !call.finished {
				call.cancel(fault(c.FaultCancelled, "close", "runtime closed", nil))
			}
		}
	}
	if e.closeComplete {
		e.mu.Unlock()
		return nil
	}
	if !e.closeRunning {
		e.closeRunning = true
		e.closeAttempt = &cleanupAttempt{done: make(chan struct{})}
		go e.shutdown(e.closeAttempt)
	}
	attempt := e.closeAttempt
	e.mu.Unlock()
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return fault(c.FaultCleanup, "close", "runtime cleanup deadline exceeded", ctx.Err())
	}
}
func (e *Engine) shutdown(attempt *cleanupAttempt) {
	e.work.Wait()
	e.mu.Lock()
	sessions := make([]*session, 0, len(e.sessions))
	for _, sess := range e.sessions {
		e.beginSessionCleanup(sess)
		sessions = append(sessions, sess)
	}
	e.mu.Unlock()
	// 所有闲置会话与 Driver 共用一次关闭预算，不按会话数乘以超时。
	ctx, cancel := context.WithTimeout(context.Background(), e.config.CleanupTimeout)
	defer cancel()
	var first error
	for _, sess := range sessions {
		cleanup := e.releaseSessionWith(ctx, sess)
		e.mu.Lock()
		e.finishSessionCleanup(sess, cleanup)
		e.mu.Unlock()
		if cleanup != nil && first == nil {
			first = cleanup
		}
	}
	e.mu.Lock()
	remaining := len(e.sessions)
	e.mu.Unlock()
	// 未确认关闭的 Session 仍依赖 Driver，因此 Driver 的释放也必须延后。
	if remaining == 0 {
		for i := len(e.config.Drivers) - 1; i >= 0; i-- {
			d := e.config.Drivers[i]
			if e.closedDrivers[d.ID()] {
				continue
			}
			if err := d.Close(ctx); err != nil {
				if first == nil {
					first = fault(c.FaultCleanup, "close", "driver release incomplete", err)
				}
			} else {
				e.closedDrivers[d.ID()] = true
			}
		}
	}
	e.mu.Lock()
	e.closeComplete = remaining == 0 && len(e.closedDrivers) == len(e.config.Drivers)
	e.closeRunning = false
	attempt.err = first
	close(attempt.done)
	e.mu.Unlock()
}

func failed(id string, f *c.Fault) c.Result {
	return c.Result{Invocation: c.InvocationRef{ID: id}, Status: c.StatusFailed, StopReason: c.StopFailed, Fault: f, Output: c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}}
}
func fault(code c.FaultCode, stage, message string, cause error) *c.Fault {
	return &c.Fault{Code: code, Stage: stage, Message: message, Cause: cause}
}
func asFault(err error, code c.FaultCode, stage, message string) *c.Fault {
	var technical *c.Fault
	if errors.As(err, &technical) {
		return technical
	}
	return fault(code, stage, message, err)
}

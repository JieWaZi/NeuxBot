package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
)

// preparedRun 引用唯一调用；冷资源交接前取消不能遗弃可能迟到的取得结果。
type preparedRun struct {
	runtime        *Runtime          // runtime 是技术会话及释放栈的唯一所有者。
	call           *invocation       // call 是取得资源、交接与执行共用的占位。
	cold           bool              // cold 标记本次是否必须取得并交接资源。
	requestContext context.Context   // requestContext 只约束冷资源取得，Bind完成后由Run接管取消。
	mode           c.PreparationMode // mode 保留普通执行或只读交接的约束。
}

func (p *preparedRun) Context() context.Context {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	return p.call.ctx
}
func (p *preparedRun) Invocation() c.InvocationRef { return p.call.ref }
func (p *preparedRun) NeedsResources() bool {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	return p.cold && !p.call.bound && !p.call.claimed
}
func (p *preparedRun) Plan() c.SessionPlan {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	return plan.CloneSession(p.call.session.declared)
}
func (p *preparedRun) Session() c.SessionDirective {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	return p.call.session.directive
}
func (p *preparedRun) Resources() c.ResourceLease {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	return p.call.session.resources
}

// Prepare 只占用唯一会话，回收不兼容消费者；不取得资源或打开原生连接。
func (e *Runtime) Prepare(ctx context.Context, request c.PrepareRequest) (c.PreparedRun, error) {
	if ctx == nil {
		return nil, fault(c.FaultInvalidSpec, "prepare", "preparation context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, fault(c.FaultCancelled, "prepare", "preparation cancelled", err)
	}
	request.Plan = plan.CloneSession(request.Plan)
	if request.Session.Mode == "" {
		request.Session.Mode = c.SessionNew
	}
	if request.Plan.Profile.DriverID == "" || (request.Session.Mode != c.SessionNew && request.Session.Mode != c.SessionResume) {
		return nil, fault(c.FaultInvalidSpec, "prepare", "driver identity and supported session mode are required", nil)
	}
	if request.Lifetime != c.SessionPersistent && request.Lifetime != c.SessionTemporary {
		return nil, fault(c.FaultInvalidSpec, "prepare", "unknown session lifetime", nil)
	}
	handoff := request.Mode == c.PreparationReadOnlyHandoff
	if request.Mode != c.PreparationContinue && !handoff {
		return nil, fault(c.FaultInvalidSpec, "prepare", "unknown preparation mode", nil)
	}
	if handoff && (!request.Plan.Workspace.ReadOnly || request.Session.Mode != c.SessionResume || request.Session.NativeHandle == "") {
		return nil, fault(c.FaultInvalidSpec, "prepare", "read-only handoff requires a native resume identity and read-only workspace", nil)
	}
	key, err := preparationKey(request.Plan)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.closing || !e.started {
		e.mu.Unlock()
		return nil, fault(c.FaultClosed, "prepare", "runtime is not accepting preparations", nil)
	}
	var previous *session
	for _, candidate := range e.sessions {
		if candidate.call != nil || candidate.cleanupBusy {
			e.mu.Unlock()
			return nil, fault(c.FaultSessionBusy, "prepare", "session already has an active owner", nil)
		}
		if candidate.managed {
			previous = candidate
		}
	}
	if handoff && previous != nil && previous.nativeHandle != "" {
		prior := previous.declared
		if previous.nativeHandle != request.Session.NativeHandle || prior.Profile.DriverID != request.Plan.Profile.DriverID || prior.Profile.Subject != request.Plan.Profile.Subject {
			e.mu.Unlock()
			return nil, fault(c.FaultSessionMismatch, "prepare", "read-only handoff does not match the owned native account and identity", nil)
		}
	}
	reuse := !handoff && previous != nil && !previous.closing && !previous.released && request.Session.Mode == c.SessionResume && previous.requestKey == key && previous.nativeHandle == request.Session.NativeHandle
	forceNew := previous != nil && (request.Session.Mode != c.SessionResume || previous.requestKey != key || previous.nativeHandle != request.Session.NativeHandle)
	if handoff && previous != nil && previous.nativeHandle == request.Session.NativeHandle {
		forceNew = false
		key = previous.requestKey
	}
	id := e.nextID("i")
	sess := previous
	if !reuse {
		if previous != nil {
			previous.closing = true
			previous.managed = false
			e.beginSessionCleanup(previous)
		}
		directive := request.Session
		if forceNew || request.Session.Mode != c.SessionResume || request.Session.NativeHandle == "" {
			directive = c.SessionDirective{Mode: c.SessionNew}
		}
		sess = &session{id: e.nextID("s"), managed: true, requestKey: key, tags: slices.Clone(request.Plan.Tags), declared: request.Plan, grants: request.Plan.Grants, directive: directive, closeAfterRun: handoff || request.Lifetime == c.SessionTemporary}
		if !forceNew && request.Session.Mode == c.SessionResume && request.Session.NativeHandle != "" {
			sess.nativeHandle = request.Session.NativeHandle
		}
		e.sessions[sess.id] = sess
	}
	sess.revision++
	callCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	call := &invocation{ref: c.InvocationRef{ID: id, SessionID: sess.id, Revision: sess.revision}, ctx: callCtx, cancel: cancel, session: sess, bound: reuse}
	call.stopPreparation = context.AfterFunc(ctx, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if !call.bound && !call.claimed {
			call.cancel(fault(c.FaultCancelled, "prepare", "resource acquisition cancelled", ctx.Err()))
		}
	})
	sess.call = call
	e.active[id] = call
	e.work.Add(1)
	e.mu.Unlock()
	p := &preparedRun{runtime: e, call: call, cold: !reuse, mode: request.Mode, requestContext: ctx}
	if !reuse && previous != nil {
		cleanup := e.releaseSession(previous)
		e.mu.Lock()
		e.finishSessionCleanup(previous, cleanup)
		if cleanup != nil {
			previous.managed = true
			sess.managed = false
		}
		e.mu.Unlock()
		if cleanup != nil {
			e.releaseInvocationSession(call)
			e.finishInvocation(call)
			return nil, cleanup
		}
	}
	if reuse {
		call.stopPreparation()
		e.mu.Lock()
		sess.directive = c.SessionDirective{Mode: c.SessionResume, NativeHandle: sess.nativeHandle}
		sess.closeAfterRun = sess.closeAfterRun || request.Lifetime == c.SessionTemporary
		cancelled := e.closing || call.ctx.Err() != nil
		e.mu.Unlock()
		if cancelled {
			_ = p.Close(context.Background())
			return nil, fault(c.FaultCancelled, "prepare", "preparation cancelled before handoff", context.Cause(call.ctx))
		}
	}
	return p, nil
}

// Bind 先接管部分资源，再校验实际配置；失败由同一句柄 Close 释放，不在宿主回滚。
func (p *preparedRun) Bind(ctx context.Context, actual c.SessionResources) error {
	e, call := p.runtime, p.call
	e.mu.Lock()
	defer e.mu.Unlock()
	if !p.cold || call.bound || call.claimed {
		return fault(c.FaultInvalidSpec, "bind", "prepared resources already bound or invocation consumed", nil)
	}
	call.bound = true
	sess := call.session
	sess.resources = actual.Resources
	if actual.Resources != nil {
		sess.closers = append(sess.closers, actual.Resources.Close)
	}
	sess.openDriver = actual.Driver
	if actual.Driver != nil {
		sess.closers = append(sess.closers, actual.Driver.Close)
		sess.caps = actual.Driver.Capabilities()
	}
	sess.credential = cloneCredential(actual.Credential)
	sess.closeAfterRun = sess.closeAfterRun || actual.CloseAfterRun
	reject := func(err error) error { call.bindFault = err; return err }
	if actual.ActualPlan != nil {
		sess.declared = plan.CloneSession(*actual.ActualPlan)
		// 实际资源可以改变配置，但权限上界仍以取得资源前的宿主声明为准。
		sess.declared.Grants = sess.grants
		sess.tags = slices.Clone(sess.declared.Tags)
	}
	if ctx == nil {
		return reject(fault(c.FaultInvalidSpec, "bind", "binding context is required", nil))
	}
	if err := p.requestContext.Err(); err != nil {
		return reject(fault(c.FaultCancelled, "bind", "resource acquisition cancelled", err))
	}
	if err := ctx.Err(); err != nil {
		return reject(fault(c.FaultCancelled, "bind", "resource binding cancelled", err))
	}
	if err := call.ctx.Err(); err != nil {
		return reject(asFault(context.Cause(call.ctx), c.FaultCancelled, "bind", "resource binding cancelled"))
	}
	if actual.Driver == nil {
		return reject(fault(c.FaultProtocol, "bind", "resource binding returned no driver", nil))
	}
	if sess.declared.Profile.DriverID != actual.Driver.ID() {
		return reject(fault(c.FaultInvalidSpec, "bind", "prepared driver does not match session profile", nil))
	}
	if p.mode == c.PreparationReadOnlyHandoff && !sess.declared.Workspace.ReadOnly {
		return reject(fault(c.FaultInvalidSpec, "bind", "handoff resources must retain read-only workspace policy", nil))
	}
	if actual.Session != nil {
		if actual.Session.NativeHandle != "" || (actual.Session.Mode != "" && actual.Session.Mode != c.SessionNew && actual.Session.Mode != c.SessionResume) {
			return reject(fault(c.FaultInvalidSpec, "bind", "resources may only request a supported session mode", nil))
		}
		if actual.Session.Mode == c.SessionNew {
			sess.directive = c.SessionDirective{Mode: c.SessionNew}
		}
	}
	if p.mode == c.PreparationReadOnlyHandoff && sess.directive.Mode != c.SessionResume {
		return reject(fault(c.FaultInvalidSpec, "bind", "handoff resources must retain native resume policy", nil))
	}
	if sess.directive.Mode == c.SessionResume && !sess.caps.ColdResume {
		return reject(fault(c.FaultUnsupported, "bind", "driver native resume is not implemented", nil))
	}
	compiled, err := compileSession(sess.declared, sess.directive, sess.caps, e.config.Limits)
	if err != nil {
		return reject(err)
	}
	call.stopPreparation()
	if call.ctx.Err() != nil {
		return reject(asFault(context.Cause(call.ctx), c.FaultCancelled, "bind", "resource binding cancelled"))
	}
	sess.plan = compiled
	return nil
}

func compileSession(value c.SessionPlan, directive c.SessionDirective, caps c.DriverCapabilities, defaults c.Limits) (plan.Plan, error) {
	return plan.Compile(plan.Spec{Profile: value.Profile, Workspace: value.Workspace, Contributions: value.Contributions, Session: directive, Policies: plan.Policies{Tools: value.Policies.Tools, NativeConfig: value.Policies.NativeConfig}}, caps, value.Grants, defaults)
}
func preparationKey(value c.SessionPlan) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fault(c.FaultInvalidSpec, "prepare", "static configuration cannot be encoded", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func cloneCredential(value c.Credential) c.Credential {
	result := c.Credential{}
	if value.Environment != nil {
		result.Environment = make(map[string]string, len(value.Environment))
		for key, item := range value.Environment {
			result.Environment[key] = item
		}
	}
	return result
}
func (p *preparedRun) claim() bool {
	p.runtime.mu.Lock()
	defer p.runtime.mu.Unlock()
	if p.call.claimed {
		return false
	}
	p.call.claimed = true
	return true
}
func (p *preparedRun) Run(ctx context.Context, turn c.TurnInput) c.Result {
	if ctx == nil || !p.claim() {
		return failed(p.call.ref.ID, fault(c.FaultInvalidSpec, "run", "prepared invocation already consumed or context missing", nil))
	}
	e, call := p.runtime, p.call
	defer e.finishInvocation(call)
	e.mu.Lock()
	previousContext, previousCancel := call.ctx, call.cancel
	runContext, runCancel := context.WithCancelCause(ctx)
	if cause := context.Cause(previousContext); cause != nil {
		runCancel(cause)
	}
	call.ctx = runContext
	call.cancel = func(cause error) { previousCancel(cause); runCancel(cause) }
	bound, bindFault := call.bound, call.bindFault
	sess := call.session
	frozen := sess.plan.Spec()
	directive := sess.directive
	e.mu.Unlock()
	var err error
	if !bound {
		err = fault(c.FaultInvalidSpec, "run", "cold session resources must be bound before run", nil)
	} else if bindFault != nil {
		err = bindFault
	}
	if err == nil {
		frozen.Input = turn.Input
		frozen.Limits = turn.Limits
		frozen.Policies.Interaction = turn.Interaction
		frozen.Policies.Output = turn.Output
		frozen.Session = directive
		if directive.Mode == c.SessionNew {
			frozen.Seed = turn.Seed
		}
	}
	var compiled plan.Plan
	if err == nil {
		compiled, err = plan.Compile(frozen, sess.caps, sess.grants, e.config.Limits)
	}
	if err == nil {
		reused := sess.driver != nil
		var elapsed time.Duration
		if !reused {
			started := time.Now()
			err = e.openPrepared(call, compiled)
			elapsed = time.Since(started)
		}
		logStage(e.config.Logger, call, "cli_initialization", elapsed, err == nil, reused)
	}
	if err != nil {
		result := failed(call.ref.ID, asFault(err, c.FaultPreparation, "prepare", "session preparation failed"))
		result.Invocation = call.ref
		e.settle(call, &result)
		result.CleanupFault = e.releaseInvocationSession(call)
		return result
	}
	return e.executeCall(call, compiled.Spec(), turn.Options)
}
func (e *Runtime) openPrepared(call *invocation, compiled plan.Plan) error {
	sess := call.session
	frozen := compiled.Spec()
	var bindings []c.ResourceBinding
	if sess.resources != nil {
		bindings = sess.resources.Resources()
	}
	if err := validateResources(frozen.Contributions, bindings); err != nil {
		return err
	}
	if call.ctx.Err() != nil {
		return asFault(context.Cause(call.ctx), c.FaultCancelled, "open", "session open cancelled")
	}
	native, err := sess.openDriver.Open(call.ctx, c.OpenRequest{Invocation: call.ref, Profile: frozen.Profile, Session: frozen.Session, Seed: frozen.Seed, Workspace: frozen.Workspace, Contributions: frozen.Contributions, Policies: c.SessionPolicies{Tools: frozen.Policies.Tools, NativeConfig: frozen.Policies.NativeConfig}, Resources: bindings, Credential: cloneCredential(sess.credential)})
	if native != nil {
		sess.driver = native
		sess.closers = append(sess.closers, native.Close)
	}
	if err != nil {
		return err
	}
	if native == nil {
		return fault(c.FaultProtocol, "open", "driver returned no session", nil)
	}
	if call.ctx.Err() != nil {
		return asFault(context.Cause(call.ctx), c.FaultCancelled, "open", "session open cancelled")
	}
	return nil
}
func (p *preparedRun) Close(ctx context.Context) error {
	if ctx == nil {
		return fault(c.FaultInvalidSpec, "close_prepared", "close context is required", nil)
	}
	if !p.claim() {
		return nil
	}
	e, call := p.runtime, p.call
	e.mu.Lock()
	keep := !p.cold && !e.closing && !call.session.closing
	if keep {
		call.finished = true
		call.session.call = nil
	}
	e.mu.Unlock()
	if keep {
		e.finishInvocation(call)
		return nil
	}
	done := make(chan *c.Fault, 1)
	go func() { cleanup := e.releaseInvocationSession(call); e.finishInvocation(call); done <- cleanup }()
	select {
	case cleanup := <-done:
		if cleanup != nil {
			return cleanup
		}
		return nil
	case <-ctx.Done():
		return fault(c.FaultCleanup, "close_prepared", "waiting for prepared resource cleanup timed out", ctx.Err())
	}
}
func (e *Runtime) finishInvocation(call *invocation) {
	e.mu.Lock()
	delete(e.active, call.ref.ID)
	call.finished = true
	e.mu.Unlock()
	call.stopPreparation()
	call.cancel(nil)
	e.work.Done()
}

// abandonPreparedLocked 只自动放弃已有资源的热准备；冷准备等待取得资源的调用方 Close。
func (e *Runtime) abandonPreparedLocked(call *invocation) {
	if call.claimed || !call.bound {
		return
	}
	// Bind 调用本身仍由调用方拥有，取消不抢先消费交接失败的句柄。
	if call.bindFault != nil {
		return
	}
	call.claimed = true
	go func() { e.releaseInvocationSession(call); e.finishInvocation(call) }()
}

// CloseIdle 在唯一接纳锁下检查匹配资源；有活动回合时不释放资源。
func (e *Runtime) CloseIdle(ctx context.Context, tag string) error {
	if ctx == nil {
		return fault(c.FaultInvalidSpec, "close_idle", "close context is required", nil)
	}
	e.mu.Lock()
	var sessions []*session
	for _, sess := range e.sessions {
		if !slices.Contains(sess.tags, tag) || (sess.call == nil && len(sess.closers) == 0) {
			continue
		}
		if sess.call != nil || sess.cleanupBusy || e.closeRunning {
			e.mu.Unlock()
			return fault(c.FaultSessionBusy, "close_idle", "matching session has an active owner", nil)
		}
		sessions = append(sessions, sess)
	}
	for _, sess := range sessions {
		sess.closing = true
		e.beginSessionCleanup(sess)
	}
	if len(sessions) > 0 {
		e.work.Add(1)
	}
	e.mu.Unlock()
	if len(sessions) == 0 {
		return nil
	}
	done := make(chan error, 1)
	go func() {
		defer e.work.Done()
		var first error
		for _, sess := range sessions {
			cleanup := e.releaseSession(sess)
			e.mu.Lock()
			e.finishSessionCleanup(sess, cleanup)
			e.mu.Unlock()
			if first == nil && cleanup != nil {
				first = cleanup
			}
		}
		done <- first
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fault(c.FaultCleanup, "close_idle", "waiting for idle cleanup timed out", ctx.Err())
	}
}

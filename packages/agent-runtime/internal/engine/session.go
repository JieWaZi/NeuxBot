package engine

import (
	"context"
	"strings"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/agent-runtime/internal/plan"
)

// 会话接纳、原生准备和资源释放共用 Engine 的唯一接纳锁与所有权集合。
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

// NativeCheckpoint 固定宿主真实原生身份的恢复期望，不读取历史或取得原生资源。
// 能力和当前配置仍须在后续冷恢复的 Driver.Open 中由原生回执确认。
func (e *Engine) NativeCheckpoint(spec c.ExecutionSpec, nativeHandle string) (*c.SessionCheckpoint, error) {
	if strings.TrimSpace(nativeHandle) == "" {
		return nil, fault(c.FaultInvalidSpec, "checkpoint", "native session handle is required", nil)
	}
	capabilities, ok := e.capabilities[spec.Profile.DriverID]
	if !ok || !capabilities.ColdResume {
		return nil, fault(c.FaultUnsupported, "checkpoint", "driver native resume is not implemented", nil)
	}
	spec.Session = c.SessionDirective{Mode: c.SessionNew}
	compiled, err := plan.Compile(spec, capabilities, e.config.Grants, e.config.Limits)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return nil, fault(c.FaultClosed, "checkpoint", "runtime is closed", nil)
	}
	if !e.started {
		return nil, fault(c.FaultNotStarted, "checkpoint", "runtime has not started", nil)
	}
	return &c.SessionCheckpoint{Version: 1, EngineID: e.id, SessionID: e.nextID("s"), DriverID: spec.Profile.DriverID, DriverVersion: capabilities.Version, NativeHandle: nativeHandle, ContextDigest: compiled.ContextDigest(), SecurityScopeKey: compiled.SecurityScopeKey()}, nil
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
	if checkpoint := spec.Session.Checkpoint; checkpoint != nil {
		if e.sessions[checkpoint.SessionID] != nil {
			return nil, fault(c.FaultSessionBusy, "admission", "checkpoint session already has a runtime owner", nil)
		}
		if len(e.sessions) >= e.config.MaxOpenSessions {
			return nil, fault(c.FaultCapacity, "admission", "open session budget exceeded", nil)
		}
		sess = &session{id: checkpoint.SessionID, driverID: spec.Profile.DriverID, contextDigest: p.ContextDigest(), securityKey: p.SecurityScopeKey(), revision: checkpoint.Revision}
		e.sessions[sess.id] = sess
	} else if spec.Session.Mode == c.SessionResume {
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
	bindings := make(map[string]c.ResourceBinding, len(resources))
	for _, binding := range resources {
		if binding.ID == "" || (binding.Path == "") == (binding.MCP == nil) {
			return fault(c.FaultProtocol, "resource", "invalid resource binding", nil)
		}
		if _, ok := bindings[binding.ID]; ok {
			return fault(c.FaultPreparation, "resource", "conflicting resource binding", nil)
		}
		bindings[binding.ID] = binding
	}
	for _, skill := range contributions.Skills {
		if bindings[skill.ResourceID].Path == "" && !skill.Optional {
			return fault(c.FaultPreparation, "resource", "required skill resource missing", nil)
		}
	}
	for _, mcp := range contributions.MCP {
		if bindings[mcp.ResourceID].MCP == nil && !mcp.Optional {
			return fault(c.FaultPreparation, "resource", "required mcp resource missing", nil)
		}
	}
	return nil
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

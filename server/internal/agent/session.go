package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Session 唯一拥有一个产品工作区的稳定计划、当前回合、原生连接和释放进度。
type Session struct {
	// mu 保护本会话的接纳、取消和完成线性化。
	mu sync.Mutex
	// manager 提供唯一产品与原生边界配置。
	manager *Manager
	// id 是本所有者固定且不重用的身份。
	id string
	// workspace 是 Manager 的工作区索引键。
	workspace string
	// temporary 标记独立任务所有者，聊天不占临时容量。
	temporary bool
	// ended 表示独立任务的执行入口已经结束，可以后台清理。
	ended bool
	// declared 保存唯一的实际静态产品配置。
	declared SessionPlan
	// requestKey 是接纳声明的无秘密兼容摘要。
	requestKey string
	// initialized 表示此前已接纳过静态配置。
	initialized bool
	// revision 是本 Session 单调增加的回合身份。
	revision uint64
	// current 是唯一活动准备或执行，不存在管理其他 Session 的集合。
	current *Turn
	// directive 保存当前实际新建或恢复动作。
	directive SessionDirective
	// nativeHandle 是原生回执提供的真实句柄。
	nativeHandle string
	// connection 是唯一原生消费者，关闭失败时不能替换。
	connection NativeConnection
	// resources 是实际资源唯一所有者。
	resources *sessionResources
	// connectionConfig 是本消费者已经装配的唯一连接参数。
	connectionConfig native.ConnectionConfig
	// idleCleanup 表示可恢复的闲置清理占用，不代表永久关闭。
	idleCleanup bool
	// closing 阻止关闭期间接纳新回合。
	closing bool
	// closed 表示显式关闭已经完成。
	closed bool
	// retiring 表示失败或强制关闭的消费者必须先清理，不能热复用。
	retiring bool
	// released 标记当前消费者已经全部释放，配置事实仍可用于下一次兼容判断。
	released bool
	// closeAfterRun 保留临时、只读与实际资源的强制关闭约束。
	closeAfterRun bool
	// closeAttempt 是当前共享的显式关闭尝试，调用方超时不丢弃释放责任。
	closeAttempt *sessionCleanup
	// cleanupGate 串行化清理尝试，成功步骤不重复。
	cleanupGate chan struct{}
}

// Turn 是准备和执行共用的唯一回合，不复制会话所有权。
type Turn struct {
	// pendingAccount 是已接纳准备的目标账号，替换时与尚未退出的旧资源分别保护。
	pendingAccount string
	// pendingPlugins 是本轮准备已经占用的插件身份。
	pendingPlugins []string
	// ref 是不可重用的调用与会话修订身份。
	ref native.InvocationRef
	// ctx 是当前准备或执行 Context。
	ctx context.Context
	// cancel 保留首个有效撤销原因。
	cancel context.CancelCauseFunc
	// requestContext 只约束冷资源获取，成功交接后解除。
	requestContext context.Context
	// stopPreparation 解除短接纳 Context 的取消转发。
	stopPreparation func() bool
	// handedOff 表示冷装配已成功，短接纳 Context 已交给执行阶段。
	handedOff bool
	// binding 表示首次资源装配仍在进行，阻止并发消费资源。
	binding bool
	// bindingDone 在当前装配方法退出时关闭。
	bindingDone chan struct{}
	// bound 表示首次冷资源已经交接，校验失败也不能退还所有权。
	bound bool
	// bindFault 保存首次资源交接失败。
	bindFault error
	// claimed 防止 Run 与 Close 重复消费同一准备句柄。
	claimed bool
	// finished 是取消和完成的唯一线性化点。
	finished bool
	// done 在回合及已开始的清理完成后关闭。
	done chan struct{}
	// cold 表示本轮需要新消费者资源。
	cold bool
	// handoff 强制只读原生恢复且执行后关闭。
	handoff bool
}

// PreparedTurn 直接关联同一 Session 和 Turn，不通过另一套内核准备句柄转发。
type PreparedTurn struct {
	// session 是计划、连接与资源唯一所有者。
	session *Session
	// turn 是当前唯一回合占位。
	turn *Turn
}

func newSession(m *Manager, workspace string, temporary bool) (*Session, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return nil, fault(native.FaultPreparation, "prepare", "session identity unavailable", err)
	}
	return &Session{manager: m, id: hex.EncodeToString(bytes[:]), workspace: workspace, temporary: temporary, cleanupGate: make(chan struct{}, 1)}, nil
}

// Context 返回可取消取得资源、但不随已交接接纳请求结束的准备 Context。
func (p *PreparedTurn) Context() context.Context {
	p.session.mu.Lock()
	defer p.session.mu.Unlock()
	return p.turn.ctx
}

// NeedsResources 只在尚未绑定且未消费的冷准备中返回真。
func (p *PreparedTurn) NeedsResources() bool {
	p.session.mu.Lock()
	defer p.session.mu.Unlock()
	return p.turn.cold && !p.turn.bound && !p.turn.claimed
}

// Session 返回唯一所有者确定的实际原生动作。
func (p *PreparedTurn) Session() SessionDirective {
	p.session.mu.Lock()
	defer p.session.mu.Unlock()
	return p.session.directive
}

// Resources 回读唯一所有者中已固定的具名产品资源。
func (p *PreparedTurn) Resources() SessionResources {
	p.session.mu.Lock()
	defer p.session.mu.Unlock()
	if p.session.resources == nil {
		return SessionResources{}
	}
	return p.session.resources.snapshot
}

func (s *Session) prepare(ctx context.Context, plan SessionPlan, directive SessionDirective) (*PreparedTurn, error) {
	if ctx == nil {
		return nil, fault(native.FaultInvalidSpec, "prepare", "preparation context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, fault(native.FaultCancelled, "prepare", "preparation cancelled", err)
	}
	plan = cloneSessionPlan(plan)
	if directive.Mode == "" {
		directive.Mode = SessionModeNew
	}
	if plan.Target.RuntimeID == "" || (directive.Mode != SessionModeNew && directive.Mode != SessionModeResume) {
		return nil, fault(native.FaultInvalidSpec, "prepare", "runtime identity and supported session mode are required", nil)
	}
	if plan.ReadOnlyHandoff && (!plan.Policy.Execution.WorkspaceReadOnly || directive.Mode != SessionModeResume || directive.ID == "") {
		return nil, fault(native.FaultInvalidSpec, "prepare", "read-only handoff requires a native resume identity and read-only workspace", nil)
	}
	if _, err := nativeSessionRequest(plan); err != nil {
		return nil, err
	}
	key, err := preparationKey(plan)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		return nil, fault(native.FaultClosed, "prepare", "session is closed", nil)
	}
	if s.current != nil || s.idleCleanup {
		s.mu.Unlock()
		return nil, fault(native.FaultSessionBusy, "prepare", "session already has an active turn", nil)
	}
	if plan.ReadOnlyHandoff && s.nativeHandle != "" && (s.nativeHandle != directive.ID || s.declared.Target.RuntimeID != plan.Target.RuntimeID || s.declared.Target.AccountID != plan.Target.AccountID) {
		s.mu.Unlock()
		return nil, fault(native.FaultSessionMismatch, "prepare", "handoff does not match owned native identity and account", nil)
	}
	reuse := !plan.ReadOnlyHandoff && !s.retiring && !s.closeAfterRun && s.initialized && !s.released && s.connection != nil && directive.Mode == SessionModeResume && s.requestKey == key && s.nativeHandle == directive.ID
	forceNew := s.initialized && (directive.Mode != SessionModeResume || s.requestKey != key || s.nativeHandle != directive.ID)
	if plan.ReadOnlyHandoff && s.nativeHandle == directive.ID {
		forceNew = false
		key = s.requestKey
	}
	s.revision++
	callCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	turn := &Turn{pendingAccount: plan.Target.AccountID, pendingPlugins: slices.Clone(plan.PluginIDs), ref: native.InvocationRef{ID: s.id + "/" + fmtRevision(s.revision), SessionID: s.id, Revision: s.revision}, ctx: callCtx, cancel: cancel, requestContext: ctx, bound: reuse, handedOff: reuse, cold: !reuse, handoff: plan.ReadOnlyHandoff, done: make(chan struct{})}
	turn.stopPreparation = context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !turn.handedOff && !turn.claimed {
			turn.cancel(fault(native.FaultCancelled, "prepare", "resource acquisition cancelled", ctx.Err()))
		}
	})
	s.current = turn
	s.mu.Unlock()
	p := &PreparedTurn{session: s, turn: turn}
	if !reuse {
		s.mu.Lock()
		s.retiring = true
		s.mu.Unlock()
		if cleanup := s.releaseWithBudget(); cleanup != nil {
			s.finishTurn(turn)
			return nil, cleanup
		}
		s.mu.Lock()
		s.declared = plan
		s.requestKey = key
		s.initialized = true
		s.released = false
		s.closeAfterRun = s.temporary || plan.ReadOnlyHandoff
		s.directive = directive
		if forceNew || directive.Mode != SessionModeResume || directive.ID == "" {
			s.directive = SessionDirective{Mode: SessionModeNew}
			s.nativeHandle = ""
		} else {
			s.nativeHandle = directive.ID
		}
		s.mu.Unlock()
	} else {
		turn.stopPreparation()
		s.mu.Lock()
		s.directive = SessionDirective{Mode: SessionModeResume, ID: s.nativeHandle}
		cancelled := s.closing || turn.ctx.Err() != nil
		s.mu.Unlock()
		if cancelled {
			_ = p.Close(context.Background())
			return nil, fault(native.FaultCancelled, "prepare", "preparation cancelled before handoff", context.Cause(turn.ctx))
		}
	}
	return p, nil
}
func fmtRevision(v uint64) string {
	const digits = "0123456789"
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = digits[v%10]
		v /= 10
		if v == 0 {
			return string(b[i:])
		}
	}
}

// Bind 的首次冷交接先接管实际和部分资源，之后的任何错误都由同一 Session 清理。
func (p *PreparedTurn) Bind(ctx context.Context, resources SessionResources) error {
	return p.bind(ctx, resources, resourcePreparation{})
}
func (p *PreparedTurn) bind(ctx context.Context, snapshot SessionResources, options resourcePreparation) error {
	s, t := p.session, p.turn
	s.mu.Lock()
	if !t.cold || t.bound || t.claimed {
		s.mu.Unlock()
		return mapChatPreparationFault(fault(native.FaultInvalidSpec, "bind", "resources already bound or turn consumed", nil))
	}
	t.bound = true
	t.binding = true
	t.bindingDone = make(chan struct{})
	owned := &sessionResources{snapshot: snapshot, taskLease: options.taskLease, releaseWorkspace: options.releaseWorkspace, taskResources: options.taskResources, contributions: options.contributions}
	s.resources = owned
	reject := func(err error) error {
		s.mu.Lock()
		t.bindFault = err
		t.binding = false
		close(t.bindingDone)
		s.mu.Unlock()
		return mapChatPreparationFault(err)
	}
	plan := cloneSessionPlan(s.declared)
	s.mu.Unlock()
	if ctx == nil {
		return reject(fault(native.FaultInvalidSpec, "bind", "binding context is required", nil))
	}
	if err := t.requestContext.Err(); err != nil {
		return reject(fault(native.FaultCancelled, "bind", "resource acquisition cancelled", err))
	}
	if err := ctx.Err(); err != nil {
		return reject(fault(native.FaultCancelled, "bind", "resource binding cancelled", err))
	}
	if err := t.ctx.Err(); err != nil {
		return reject(asFault(context.Cause(t.ctx), native.FaultCancelled, "bind", "resource binding cancelled"))
	}
	if options.acquisitionError != nil {
		return reject(options.acquisitionError)
	}
	actual := plan
	actual.Target = DeclareTarget(snapshot.Target)
	actual.Skills = snapshot.Skills
	actual.Connectors = DeclareConnectors(snapshot.Connectors)
	actual.PluginIDs = nil
	for _, plugin := range snapshot.PluginResources {
		actual.PluginIDs = append(actual.PluginIDs, plugin.ID)
	}
	s.mu.Lock()
	s.declared = cloneSessionPlan(actual)
	s.mu.Unlock()

	if actual.Target.RuntimeID == "" {
		return reject(fault(native.FaultInvalidSpec, "bind", "actual runtime identity is required", nil))
	}
	if actual.Target.RuntimeID != plan.Target.RuntimeID {
		return reject(fault(native.FaultInvalidSpec, "bind", "actual runtime does not match declared identity", nil))
	}
	if snapshot.Session != nil {
		if snapshot.Session.ID != "" || (snapshot.Session.Mode != "" && snapshot.Session.Mode != SessionModeNew && snapshot.Session.Mode != SessionModeResume) {
			return reject(fault(native.FaultInvalidSpec, "bind", "resources may only request a supported session mode", nil))
		}
		if snapshot.Session.Mode == SessionModeNew {
			s.mu.Lock()
			s.directive = SessionDirective{Mode: SessionModeNew}
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	directive := s.directive
	s.mu.Unlock()
	if t.handoff && (!actual.Policy.Execution.WorkspaceReadOnly || directive.Mode != SessionModeResume) {
		return reject(fault(native.FaultInvalidSpec, "bind", "handoff resources must retain read-only resume policy", nil))
	}
	request, err := nativeSessionRequest(actual)
	if err != nil {
		return reject(err)
	}
	request.Session = native.SessionDirective{Mode: native.SessionMode(directive.Mode), NativeHandle: directive.ID}
	request.Contributions = appendContributions(request.Contributions, options.contributions)
	if _, err = compileExecution(executionSpec{OpenRequest: request, Policies: executionPolicies{Tools: request.Policies.Tools, NativeConfig: request.Policies.NativeConfig}}, nativeCapabilities{HotResume: true, ColdResume: true}, sessionGrants(plan), s.manager.config.Limits); err != nil {
		return reject(err)
	}
	bindingContext, cancelBinding := context.WithCancel(t.ctx)
	stopBinding := context.AfterFunc(ctx, cancelBinding)
	defer func() { stopBinding(); cancelBinding() }()
	if err = owned.prepare(bindingContext, actual, options, s.manager.config); err != nil {
		return reject(err)
	}
	if ctx.Err() != nil {
		return reject(fault(native.FaultCancelled, "bind", "resource binding cancelled", ctx.Err()))
	}
	s.mu.Lock()
	s.declared = cloneSessionPlan(actual)
	s.connectionConfig = owned.connectionConfig
	s.closeAfterRun = s.closeAfterRun || snapshot.Target.CloseAfterRun
	if requestErr := t.requestContext.Err(); requestErr != nil {
		t.cancel(fault(native.FaultCancelled, "bind", "resource acquisition cancelled", requestErr))
	}
	t.stopPreparation()
	if err = t.ctx.Err(); err != nil {
		t.bindFault = asFault(context.Cause(t.ctx), native.FaultCancelled, "bind", "resource binding cancelled")
		err = t.bindFault
	}
	t.handedOff = err == nil
	t.binding = false
	close(t.bindingDone)
	s.mu.Unlock()
	if err != nil {
		return mapChatPreparationFault(err)
	}
	return nil
}

// Run 提交本轮新增输入，每个准备最多消费一次。
func (p *PreparedTurn) Run(ctx context.Context, input TurnInput, sink EventSink) Result {
	if ctx == nil || sink == nil {
		return chatFailed(p.session.declared, p.Session(), status_error.BadRequest)
	}
	s := p.session
	s.mu.Lock()
	plan, directive := cloneSessionPlan(s.declared), s.directive
	s.mu.Unlock()
	bridge := &chatObserver{sink: sink}
	result := p.run(ctx, input, bridge, false)
	for _, f := range []*native.Fault{result.Fault, result.DeliveryFault, result.CleanupFault} {
		if f != nil {
			logx.Errorf("agent session failed: invocation_id=%s code=%s stage=%s detail=%s", result.Invocation.ID, f.Code, f.Stage, f.Message)
		}
	}
	return chatResult(plan, directive, result, bridge)
}
func (p *PreparedTurn) claim() bool {
	s := p.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.turn.claimed || p.turn.binding {
		return false
	}
	p.turn.claimed = true
	return true
}
func (p *PreparedTurn) run(ctx context.Context, input TurnInput, observer turnObserver, unattended bool) (result turnResult) {
	if ctx == nil || !p.claim() {
		return failed(p.turn.ref.ID, fault(native.FaultInvalidSpec, "run", "prepared turn consumed or context missing", nil))
	}
	s, t := p.session, p.turn
	defer s.finishTurn(t)
	// 最终用量也是当前回合的交付；完成之前不能让下一回合替换计划或连接。
	defer func() {
		if unattended || observer == nil || (!result.Usage.Known && !result.Usage.ContextKnown) {
			return
		}
		delivery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := observer.Accept(delivery, observedEvent{Invocation: t.ref, NativeEvent: native.Event{Kind: native.EventUsage, Usage: &result.Usage}})
		cancel()
		if err != nil && result.DeliveryFault == nil {
			result.DeliveryFault = fault(native.FaultDelivery, "usage", "final usage delivery failed", err)
			s.mu.Lock()
			s.retiring = true
			s.mu.Unlock()
			if cleanup := s.releaseWithBudget(); result.CleanupFault == nil {
				result.CleanupFault = cleanup
			}
		}
	}()
	s.mu.Lock()
	prior, priorCancel := t.ctx, t.cancel
	runCtx, runCancel := context.WithCancelCause(ctx)
	if cause := context.Cause(prior); cause != nil {
		runCancel(cause)
	}
	t.ctx = runCtx
	t.cancel = func(cause error) { priorCancel(cause); runCancel(cause) }
	bound, bindErr := t.bound, t.bindFault
	plan := cloneSessionPlan(s.declared)
	directive := s.directive
	owned := s.resources
	s.mu.Unlock()
	var err error
	if !bound {
		err = fault(native.FaultInvalidSpec, "run", "cold session resources must be bound", nil)
	} else if bindErr != nil {
		err = bindErr
	}
	var frozen executionSpec
	if err == nil {
		var request native.OpenRequest
		request, err = nativeSessionRequest(plan)
		request.Invocation = t.ref
		request.Session = native.SessionDirective{Mode: native.SessionMode(directive.Mode), NativeHandle: directive.ID}
		if directive.Mode == SessionModeNew {
			request.Seed = chatSeed(input.History)
		}
		if owned != nil {
			request.Contributions = appendContributions(request.Contributions, owned.contributions)
			request.Resources = owned.bindings
			request.Credential = owned.credential
			if owned.taskResources != nil {
				request.Credential.Environment = owned.taskResources.LaunchEnvironment(maps.Clone(request.Credential.Environment))
			}
		}
		frozen = executionSpec{OpenRequest: request, Input: technicalTurnInput(plan.Workspace.Path, p.Resources(), input), Policies: executionPolicies{Tools: request.Policies.Tools, NativeConfig: request.Policies.NativeConfig, Interaction: InteractionInteractive, Output: OutputBestEffort}}
		if unattended {
			frozen.Policies.Interaction = InteractionUnattended
			frozen.Policies.Output = OutputStrict
		}
	}
	if err == nil {
		var compiled compiledExecution
		compiled, err = compileExecution(frozen, nativeCapabilities{HotResume: true, ColdResume: true}, sessionGrants(plan), s.manager.config.Limits)
		frozen = compiled.Spec()
	}
	if err == nil {
		err = validateResources(frozen.Contributions, frozen.Resources)
	}
	if err == nil {
		reused := s.connection != nil
		var elapsed time.Duration
		if !reused {
			started := time.Now()
			open := s.manager.config.Open
			if open == nil {
				open = func(ctx context.Context, c native.ConnectionConfig, r native.OpenRequest) (NativeConnection, error) {
					return native.Open(ctx, c, r)
				}
			}
			request := cloneExecution(frozen).OpenRequest
			connection, openErr := open(t.ctx, s.connectionConfig, request)
			s.mu.Lock()
			if connection != nil {
				s.connection = connection
			}
			s.mu.Unlock()
			err = openErr
			if err == nil && connection == nil {
				err = fault(native.FaultProtocol, "open", "native returned no connection", nil)
			}
			if err == nil && t.ctx.Err() != nil {
				err = asFault(context.Cause(t.ctx), native.FaultCancelled, "open", "session open cancelled")
			}
			elapsed = time.Since(started)
		}
		logStage(s.manager.config.Logger, t, s, "cli_initialization", elapsed, err == nil, reused)
	}
	if err != nil {
		if owned != nil && owned.taskResources != nil {
			owned.taskResources.Revoke()
		}
		result := failed(t.ref.ID, asFault(err, native.FaultPreparation, "prepare", "session preparation failed"))
		result.Invocation = t.ref
		s.settle(t, &result)
		s.mu.Lock()
		s.retiring = true
		s.mu.Unlock()
		result.CleanupFault = s.releaseWithBudget()
		return result
	}
	options := turnOptions{Observer: observer, UserInputState: input.UserInputState, InteractionHandler: chatInteraction{approval: input.ApprovalHandler, input: input.ElicitationHandler, readOnly: plan.Policy.Execution.WorkspaceReadOnly}}
	return s.executeTurn(t, frozen, options)
}
func (s *Session) executeTurn(t *Turn, frozen executionSpec, options turnOptions) turnResult {
	result := turnResult{Invocation: t.ref, Status: native.StatusFailed, StopReason: native.StopFailed, Output: Output{Status: OutputUnavailable, Reliability: ReliabilityUnverified}}
	sink := newSink(t, s, frozen, options, s.manager.config.CleanupTimeout, s.manager.config.Logger)
	raw := s.connection.Prompt(t.ctx, native.TurnRequest{Invocation: t.ref, Input: frozen.Input, Limits: frozen.Limits, UserInputState: options.UserInputState}, sink)
	if s.resources != nil && s.resources.taskResources != nil {
		s.resources.taskResources.Revoke()
	}
	delivery, effects, streamFault := sink.finish()
	if !effects.PromptAccepted {
		logStage(s.manager.config.Logger, t, s, "input_acceptance", time.Since(sink.promptStarted), false, false)
	}
	result.Status = raw.Status
	result.StopReason = raw.StopReason
	result.Fault = raw.Fault
	result.Usage = raw.Usage
	result.Configuration = raw.Configuration
	result.NativeSession = raw.NativeSession
	result.DeliveryFault = delivery
	result.Effects = effects
	if streamFault != nil && result.Fault == nil {
		result.Fault = streamFault
	}
	if result.Status == "" || result.StopReason == "" {
		result.Status = native.StatusFailed
		result.StopReason = native.StopFailed
		if result.Fault == nil {
			result.Fault = fault(native.FaultProtocol, "prompt", "native returned no execution terminal state", nil)
		}
	}
	if result.Status == native.StatusCompleted && !effects.PromptAccepted && result.Fault == nil {
		result.Status = native.StatusFailed
		result.StopReason = native.StopFailed
		result.Fault = fault(native.FaultProtocol, "prompt", "native completed without prompt acceptance", nil)
	}
	s.settle(t, &result)
	result.Output = sink.collector.Finish(result.StopReason)
	s.mu.Lock()
	s.nativeHandle = raw.NativeSession.Handle
	keep := !s.closeAfterRun && !s.closing && result.Status == native.StatusCompleted && result.Fault == nil && result.DeliveryFault == nil
	if !keep {
		s.retiring = true
	}
	s.mu.Unlock()
	if !keep {
		result.CleanupFault = s.releaseWithBudget()
	}
	return result
}
func (s *Session) settle(t *Turn, result *turnResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.finished = true
	if cause := context.Cause(t.ctx); cause != nil {
		first := asFault(cause, native.FaultCancelled, "execute", "turn cancelled")
		result.Fault = first
		result.Status = native.StatusCancelled
		result.StopReason = native.StopCancelled
		if first.Code != native.FaultCancelled {
			result.Status = native.StatusFailed
			result.StopReason = native.StopFailed
		}
		if first.Code == native.FaultInteractionRequired {
			result.StopReason = native.StopInteractionRequired
		}
	}
	if result.Status != native.StatusCompleted && result.Status != native.StatusCancelled && result.Status != native.StatusFailed {
		result.Status = native.StatusFailed
		result.StopReason = native.StopFailed
		result.Fault = fault(native.FaultProtocol, "prompt", "invalid native execution status", nil)
	}
	if (result.Effects.PromptAccepted || result.Effects.ToolActivity) && result.Status != native.StatusCompleted {
		result.Effects.OutcomeUnknown = true
	}
}
func (s *Session) finishTurn(t *Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == t {
		s.current = nil
	}
	t.finished = true
	t.stopPreparation()
	t.cancel(nil)
	select {
	case <-t.done:
	default:
		close(t.done)
	}
}

// Close 放弃未提交回合；成功热准备继续留给同一 Session。
func (p *PreparedTurn) Close(ctx context.Context) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	s, t := p.session, p.turn
	s.mu.Lock()
	binding := t.binding
	bindingDone := t.bindingDone
	if binding {
		t.cancel(fault(native.FaultCancelled, "close_prepared", "prepared turn closed", nil))
	}
	s.mu.Unlock()
	if binding {
		select {
		case <-bindingDone:
		case <-ctx.Done():
			return status_error.RuntimeShutdownFailed
		}
	}
	if !p.claim() {
		return nil
	}
	s.mu.Lock()
	keep := !t.cold && !s.closing
	s.mu.Unlock()
	if keep {
		s.finishTurn(t)
		return nil
	}
	done := make(chan *native.Fault, 1)
	go func() { f := s.releaseWithBudget(); s.finishTurn(t); done <- f }()
	select {
	case f := <-done:
		if f != nil {
			return mapFault(f)
		}
		return nil
	case <-ctx.Done():
		return status_error.RuntimeShutdownFailed
	}
}
func (s *Session) requestClose() {
	s.mu.Lock()
	s.closing = true
	t := s.current
	if t != nil && !t.finished {
		t.cancel(fault(native.FaultCancelled, "close", "session closed", nil))
		if !t.claimed && t.bound && !t.binding && t.bindFault == nil {
			t.claimed = true
			go func() { s.releaseWithBudget(); s.finishTurn(t) }()
		}
	}
	s.mu.Unlock()
}

// sessionCleanup 发布同一次显式关闭尝试的完成结果。
type sessionCleanup struct {
	done chan struct{} // done 在当前尝试结束时关闭。
	err  error         // err 发布后不再修改。
}

// Close 停止本 Session 的接纳，等待当前回合后按依赖顺序清理，失败可重试。
func (s *Session) Close(ctx context.Context) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	s.requestClose()
	s.mu.Lock()
	t := s.current
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil
	}
	if t != nil {
		select {
		case <-t.done:
		case <-ctx.Done():
			return fault(native.FaultCleanup, "close", "waiting for session turn timed out", ctx.Err())
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	attempt := s.closeAttempt
	if attempt != nil {
		select {
		case <-attempt.done:
			attempt = nil
		default:
		}
	}
	if attempt == nil {
		attempt = &sessionCleanup{done: make(chan struct{})}
		s.closeAttempt = attempt
		go func() {
			f := s.releaseWithBudget()
			s.mu.Lock()
			if f != nil {
				attempt.err = f
			} else {
				s.closed = true
			}
			close(attempt.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-attempt.done:
		if ctx.Err() != nil {
			return fault(native.FaultCleanup, "close", "session cleanup deadline exceeded", ctx.Err())
		}
		return attempt.err
	case <-ctx.Done():
		return fault(native.FaultCleanup, "close", "session cleanup deadline exceeded", ctx.Err())
	}
}
func (s *Session) closeIdle(ctx context.Context, tag string) error {
	s.mu.Lock()
	match := tag == "" || tag == "account:"+s.declared.Target.AccountID
	if s.current != nil {
		match = match || tag == "account:"+s.current.pendingAccount
		for _, id := range s.current.pendingPlugins {
			match = match || tag == "plugin:"+id
		}
	}
	if s.resources != nil {
		match = match || tag == "account:"+s.resources.snapshot.Target.AccountID
		for _, plugin := range s.resources.snapshot.PluginResources {
			match = match || tag == "plugin:"+plugin.ID
		}
	}
	for _, id := range s.declared.PluginIDs {
		match = match || tag == "plugin:"+id
	}
	if !match {
		s.mu.Unlock()
		return nil
	}
	if s.current != nil {
		s.mu.Unlock()
		return fault(native.FaultSessionBusy, "close_idle", "matching session has an active turn", nil)
	}
	if s.closing || s.idleCleanup {
		s.mu.Unlock()
		return fault(native.FaultSessionBusy, "close_idle", "session cleanup has an active owner", nil)
	}
	s.idleCleanup = true
	s.retiring = true
	s.mu.Unlock()
	f := s.release(ctx)
	s.mu.Lock()
	s.idleCleanup = false
	s.mu.Unlock()
	if f != nil {
		return f
	}
	return nil
}
func (s *Session) releaseWithBudget() *native.Fault {
	ctx, cancel := context.WithTimeout(context.Background(), s.manager.config.CleanupTimeout)
	defer cancel()
	return s.release(ctx)
}
func (s *Session) release(ctx context.Context) *native.Fault {
	select {
	case s.cleanupGate <- struct{}{}:
		defer func() { <-s.cleanupGate }()
	case <-ctx.Done():
		return fault(native.FaultCleanup, "cleanup", "waiting for cleanup owner timed out", ctx.Err())
	}
	s.mu.Lock()
	connection, resources := s.connection, s.resources
	s.mu.Unlock()
	if connection != nil {
		if err := connection.Close(ctx); err != nil {
			return fault(native.FaultCleanup, "cleanup", "native connection still owns dependencies", err)
		}
		s.mu.Lock()
		s.connection = nil
		s.mu.Unlock()
	}
	if resources != nil {
		if err := resources.Close(ctx); err != nil {
			return fault(native.FaultCleanup, "cleanup", "session resource release incomplete", err)
		}
		s.mu.Lock()
		s.resources = nil
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.released = true
	s.connectionConfig = native.ConnectionConfig{}
	s.retiring = false
	s.mu.Unlock()
	return nil
}
func logStage(logger *slog.Logger, t *Turn, s *Session, stage string, elapsed time.Duration, success, reused bool) {
	if logger == nil {
		return
	}
	fields := []any{"stage", stage, "elapsed_ms", float64(elapsed) / float64(time.Millisecond), "success", success, "driver_id", s.declared.Target.RuntimeID, "invocation_id", t.ref.ID, "session_id", t.ref.SessionID, "workspace", s.workspace}
	if stage == "cli_initialization" {
		fields = append(fields, "reused", reused)
	}
	logger.InfoContext(t.ctx, "agent runtime stage", fields...)
}
func fault(code native.FaultCode, stage, message string, cause error) *native.Fault {
	return &native.Fault{Code: code, Stage: stage, Message: message, Cause: cause}
}
func asFault(err error, code native.FaultCode, stage, message string) *native.Fault {
	var f *native.Fault
	if errors.As(err, &f) {
		return f
	}
	return fault(code, stage, message, err)
}
func failed(id string, f *native.Fault) turnResult {
	return turnResult{Invocation: native.InvocationRef{ID: id}, Status: native.StatusFailed, StopReason: native.StopFailed, Fault: f, Output: Output{Status: OutputUnavailable, Reliability: ReliabilityUnverified}}
}
func preparationKey(value SessionPlan) (string, error) {
	base := sessionBase(value)
	value.Detection = DetectionResult{}
	value.ReadOnlyHandoff = false
	data, err := json.Marshal(struct {
		Plan          SessionPlan // Plan 是无秘密的固定产品配置。
		BaseRuntimeID string      // BaseRuntimeID 是唯一原生协议选择。
	}{Plan: value, BaseRuntimeID: base})
	if err != nil {
		return "", fault(native.FaultInvalidSpec, "prepare", "static configuration cannot be encoded", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func cloneSessionPlan(value SessionPlan) SessionPlan {
	value.Target = DeclareTarget(value.Target.LaunchTarget())
	value.Detection = cloneDetectionResult(value.Detection)
	value.Workspace.AdditionalWritableDirectories = slices.Clone(value.Workspace.AdditionalWritableDirectories)
	value.Workspace.ProjectDirectories = slices.Clone(value.Workspace.ProjectDirectories)
	value.Instructions = slices.Clone(value.Instructions)
	value.Skills = slices.Clone(value.Skills)
	value.Connectors = slices.Clone(value.Connectors)
	for i := range value.Connectors {
		value.Connectors[i].Arguments = slices.Clone(value.Connectors[i].Arguments)
	}
	value.PluginIDs = slices.Clone(value.PluginIDs)
	if value.Policy.Thinking != nil {
		v := *value.Policy.Thinking
		value.Policy.Thinking = &v
	}
	return value
}

// nativeSessionRequest 一次性投影产品静态事实，不创建第二份技术 SessionPlan。
func nativeSessionRequest(r SessionPlan) (native.OpenRequest, error) {
	base := sessionBase(r)
	mode, err := chatNativeMode(base, r.Policy.Execution)
	if err != nil {
		return native.OpenRequest{}, err
	}
	plan := native.OpenRequest{Profile: native.Profile{DriverID: r.Target.RuntimeID, Subject: r.Target.AccountID, Model: nativeModelID(base, r.Target.AccountID, r.Policy.Model), Reasoning: r.Policy.Reasoning, Thinking: r.Policy.Thinking, NativeMode: mode}, Workspace: native.WorkspaceScope{Root: r.Workspace.Path, Directories: slices.Clone(r.Workspace.AdditionalWritableDirectories), ReadOnly: r.Policy.Execution.WorkspaceReadOnly}, Policies: native.SessionPolicies{Tools: native.ToolPolicy{Mode: native.ToolsAllowAll}, NativeConfig: native.NativeConfigInherit}}
	for _, instruction := range r.Instructions {
		plan.Contributions.Instructions = append(plan.Contributions.Instructions, native.Instruction{Name: instruction.Name, Text: instruction.Content})
	}
	for _, skill := range r.Skills {
		id := "skill:" + skill.Name
		plan.Contributions.Skills = append(plan.Contributions.Skills, native.Skill{Name: skill.Name, ResourceID: id, Version: skill.Path})
	}
	for _, connector := range r.Connectors {
		name := connectorName(connector.ID, connector.Name, connector.RuntimeKey)
		id := "mcp:" + connector.ID
		if connector.ID == "" {
			id = "mcp:" + name
		}
		// 临时凭据不进入公开连接版本；兼容性只由固定连接配置确定。
		public, _ := json.Marshal([]any{connector.Transport, connector.Command, connector.Arguments, connector.URL})
		version := sha256.Sum256(public)
		plan.Contributions.MCP = append(plan.Contributions.MCP, native.MCP{Name: name, ResourceID: id, Version: hex.EncodeToString(version[:])})
	}
	return plan, nil
}

// technicalTurnInput 只转换本轮新增内容与明确选择，不重建会话静态描述。
func technicalTurnInput(workspace string, resources SessionResources, turn TurnInput) native.Input {
	input := native.Input{Text: turn.Message.Text, SelectedSkills: append([]string(nil), turn.SelectedSkills...)}
	for _, attachment := range turn.Message.Attachments {
		path := attachment.RuntimePath
		if path == "" {
			path = filepath.Join(workspace, attachment.Path)
		}
		input.Attachments = append(input.Attachments, native.Attachment{Name: attachment.Name, Path: path, DisplayPath: attachment.Path, MIMEType: attachment.MediaType})
	}
	for _, reference := range turn.Message.CodeReferences {
		input.CodeReferences = append(input.CodeReferences, native.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
	}
	selected := make(map[string]bool, len(turn.SelectedConnectors))
	for _, id := range turn.SelectedConnectors {
		selected[id] = true
	}
	for _, connector := range resources.Connectors {
		if selected[connector.ID] {
			input.SelectedMCP = append(input.SelectedMCP, connectorName(connector.ID, connector.Name, connector.RuntimeKey))
		}
	}
	return input
}

func connectorName(id, name, runtimeKey string) string {
	if runtimeKey != "" {
		return runtimeKey
	}
	if name != "" {
		return name
	}
	return id
}
func sessionBase(plan SessionPlan) string {
	if plan.Detection.Runtime.BaseRuntimeID != "" {
		return plan.Detection.Runtime.BaseRuntimeID
	}
	if plan.Detection.Runtime.ID != "" {
		return plan.Detection.Runtime.ID
	}
	return plan.Target.RuntimeID
}
func chatNativeMode(base string, policy ExecutionPolicy) (string, error) {
	descriptor, ok := native.Lookup(base)
	if !ok {
		return "", status_error.RuntimeUnavailable
	}
	modes := chatModeCandidates(base, policy)
	if policy.WorkspaceReadOnly && len(modes) == 0 {
		return "", status_error.RuntimePolicyUnsupported
	}
	if !policy.WorkspaceReadOnly && descriptor.Modes.Negotiate {
		return "", nil
	}
	if len(modes) == 0 {
		return "", nil
	}
	return modes[0], nil
}
func chatModeCandidates(base string, policy ExecutionPolicy) []string {
	descriptor, ok := native.Lookup(base)
	if !ok {
		return nil
	}
	switch {
	case policy.WorkspaceReadOnly:
		return descriptor.Modes.ReadOnly
	case policy.PermissionLevel == PermissionLevelFullAccess:
		return descriptor.Modes.Unrestricted
	case policy.PermissionLevel == PermissionLevelAuto:
		return descriptor.Modes.Automatic
	default:
		return descriptor.Modes.Default
	}
}

// chatObserver 把同一技术调用的规范事实交付给既有实时与持久消费者。
type chatObserver struct {
	// sink 是业务执行唯一的有序事件入口。
	sink EventSink
	// effects 保留失败重试判断需要的已观察副作用。
	effects SideEffectSummary
	// thinkingStarted 保证每轮只产生一次已有思考开始事实。
	thinkingStarted bool
}

func (o *chatObserver) emit(ctx context.Context, kind EventType, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return o.sink.Accept(ctx, Event{Type: kind, Data: data})
}
func (o *chatObserver) Accept(ctx context.Context, event observedEvent) error {
	fact := event.NativeEvent
	switch fact.Kind {
	case native.EventPromptAccepted:
		return o.emit(ctx, EventTypeInvocationStarted, InvocationStartedData{InvocationID: event.Invocation.ID})
	case native.EventAssistantMessage:
		if fact.Message == nil || fact.Message.Delta == "" {
			return nil
		}
		o.effects.VisibleOutput = true
		kind := EventTypeTextDelta
		if fact.Message.Phase == native.PhaseCommentary {
			kind = EventTypeCommentaryDelta
		}
		return o.emit(ctx, kind, map[string]any{"text": fact.Message.Delta})
	case native.EventThinking:
		o.effects.ReasoningOutput = true
		if !o.thinkingStarted {
			o.thinkingStarted = true
			if err := o.emit(ctx, EventTypeThinkingStart, map[string]any{}); err != nil {
				return err
			}
		}
		if fact.Text != "" {
			return o.emit(ctx, EventTypeThinkingDelta, map[string]any{"text": fact.Text})
		}
	case native.EventTool:
		if fact.Tool == nil {
			return nil
		}
		tool := fact.Tool
		o.effects.ToolUse = true
		if tool.Kind == "edit" || tool.Kind == "delete" || tool.Kind == "move" {
			o.effects.FileWrite = true
		}
		payload := map[string]any{"toolCallId": tool.ID}
		if tool.Name != "" {
			payload["name"] = tool.Name
		}
		if tool.Title != "" {
			payload["title"] = tool.Title
		}
		if tool.Kind != "" {
			payload["kind"] = tool.Kind
		}
		if tool.Status != "" {
			payload["status"] = tool.Status
		}
		if tool.Input != nil {
			payload["rawInput"] = tool.Input
			payload["input"] = tool.Input
		}
		if tool.Output != nil {
			payload["rawOutput"] = tool.Output
			payload["output"] = tool.Output
		}
		if tool.Locations != nil {
			locations := make([]map[string]any, 0, len(tool.Locations))
			for _, location := range tool.Locations {
				item := map[string]any{"path": location.Path}
				if location.Line != nil {
					item["line"] = *location.Line
				}
				locations = append(locations, item)
			}
			payload["locations"] = locations
		}
		if tool.Content != nil {
			content := make([]map[string]any, 0, len(tool.Content))
			for _, block := range tool.Content {
				item := map[string]any{"type": block.Type}
				if block.Diff != nil {
					item["path"] = block.Diff.Path
					item["oldText"] = block.Diff.OldText
					item["newText"] = block.Diff.NewText
					o.effects.FileWrite = true
				}
				if block.Text != "" {
					item["text"] = block.Text
				}
				if block.URI != "" {
					item["uri"] = block.URI
				}
				if block.Data != "" {
					item["data"] = block.Data
				}
				if block.MIMEType != "" {
					item["mimeType"] = block.MIMEType
				}
				if block.Name != "" {
					item["name"] = block.Name
				}
				if block.TerminalID != "" {
					item["terminalId"] = block.TerminalID
				}
				if block.Type == "resource" {
					resource := map[string]any{"uri": block.URI}
					if block.MIMEType != "" {
						resource["mimeType"] = block.MIMEType
					}
					if block.Data != "" {
						resource["blob"] = block.Data
					} else {
						resource["text"] = block.Text
					}
					item = map[string]any{"type": "resource", "resource": resource}
				}
				if block.Type != "diff" && block.Type != "terminal" {
					item = map[string]any{"type": "content", "content": item}
				}
				content = append(content, item)
			}
			payload["content"] = content
		}
		terminal := tool.Status == "completed" || tool.Status == "failed"
		kind := EventTypeToolUse
		if tool.Update {
			kind = EventTypeToolUpdate
			if terminal {
				kind = EventTypeToolResult
			} else if tool.Input != nil && tool.Content == nil && tool.Output == nil {
				kind = EventTypeToolInputSnapshot
			}
		}
		if err := o.emit(ctx, kind, payload); err != nil {
			return err
		}
		if !tool.Update && terminal {
			return o.emit(ctx, EventTypeToolResult, payload)
		}
	case native.EventUsage:
		if fact.Usage != nil {
			return o.emit(ctx, EventTypeUsage, chatUsage(*fact.Usage))
		}
	}
	return nil
}
func chatUsage(usage native.Usage) UsageData {
	data := UsageData{}
	if usage.Known {
		data.Turn = &TurnUsageData{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CachedReadTokens, CacheCreationTokens: usage.CachedWriteTokens, ReasoningOutputTokens: usage.ThoughtTokens}
	}
	if usage.ContextKnown {
		data.Context = &ContextUsageData{UsedTokens: usage.ContextTokens, WindowTokens: usage.ContextWindowTokens}
	}
	return data
}

func chatResult(request SessionPlan, session SessionDirective, result turnResult, observer *chatObserver) Result {
	output := Result{ExitCode: 0, StopReason: string(result.StopReason), ProtocolHealthy: result.Effects.PromptAccepted, SideEffects: observer.effects, Diagnostics: request.Detection.Diagnostics, Session: SessionResult{Mode: session.Mode, InputID: session.ID, OutputID: result.NativeSession.Handle, ForkMode: result.NativeSession.ForkMode, ForkPosition: result.NativeSession.ForkPosition, State: SessionStateUnknown}, Facts: InvocationFacts{InvocationID: result.Invocation.ID, RuntimeID: request.Target.RuntimeID, BaseRuntimeID: request.Detection.Runtime.BaseRuntimeID, Binary: request.Detection.ResolvedBinary, Version: request.Detection.Version, Model: result.Configuration.ActualModel, Reasoning: result.Configuration.ActualReasoning, Policy: request.Policy.Execution, PrefixArgs: append([]string(nil), request.Target.PrefixArgs...), ConfigHome: request.Target.ConfigHome, Capabilities: request.Detection.Capabilities}}
	if output.Session.OutputID != "" {
		output.Session.State = SessionStateCreated
		if session.Mode == SessionModeResume {
			output.Session.State = SessionStateResumed
		}
	}
	switch result.StopReason {
	case native.StopEndTurn:
		output.TerminationReason = constants.RuntimeTerminationReasonNormal
	case native.StopCancelled:
		output.TerminationReason = constants.RuntimeTerminationReasonCancelled
	case native.StopRefused:
		output.TerminationReason = constants.RuntimeTerminationReasonRefusal
	case native.StopMaxTokens:
		output.TerminationReason = constants.RuntimeTerminationReasonTruncated
	default:
		output.TerminationReason = constants.RuntimeTerminationReasonUnknown
	}
	fault := result.Fault
	if fault == nil {
		fault = result.DeliveryFault
	}
	if fault != nil {
		output.ExitCode = -1
		code := ClassifyFailure(request.Detection.Runtime.BaseRuntimeID, fault)
		stage := constants.RuntimeACPFailureStagePrompt
		switch fault.Code {
		case native.FaultSessionMissing:
			code = FailureCodeSessionNotFound
			output.Session.State = SessionStateMissing
		case native.FaultInvalidSpec:
			code = FailureCodeInvalidRequest
		case native.FaultUnsupported:
			code = FailureCodeUnsupportedCapability
		case native.FaultDelivery:
			code = FailureCodeEventDelivery
		case native.FaultInactivityTimeout:
			code = FailureCodeInactivityTimeout
		case native.FaultCancelled:
			if !errors.Is(fault, context.DeadlineExceeded) {
				code = FailureCodeContextCanceled
			}
		}
		if !result.Effects.PromptAccepted {
			stage = constants.RuntimeRunFailureStagePrepare
		}
		transience := FailureTransiencePermanent
		if code == FailureCodeRateLimited || code == FailureCodeUpstreamUnavailable {
			transience = FailureTransienceTransient
		}
		output.Failure = &Failure{Code: code, Stage: stage, Message: fault.Error(), DisplayMessage: FailureDisplayMessage(fault), SessionState: output.Session.State, SideEffects: output.SideEffects, Transience: transience}
		if errors.Is(fault, context.Canceled) {
			output.TerminationReason = constants.RuntimeTerminationReasonCancelled
		}
	} else if result.Status == native.StatusCancelled || result.StopReason == native.StopCancelled {
		output.ExitCode = -1
		output.TerminationReason = constants.RuntimeTerminationReasonCancelled
		output.Failure = &Failure{Code: FailureCodeContextCanceled, Stage: constants.RuntimeACPFailureStagePrompt, Message: context.Canceled.Error(), DisplayMessage: FailureDisplayMessage(context.Canceled), SessionState: output.Session.State, SideEffects: output.SideEffects, Transience: FailureTransiencePermanent}
	}
	return output
}

// chatInteraction 复用Execution的审批与输入协调器，准确保留原生选择身份。
type chatInteraction struct {
	// approval 保存本轮工具审批入口。
	approval ApprovalHandler
	// input 保存本轮结构化输入入口。
	input ElicitationHandler
	// readOnly 禁止只读工作越过原生审批写权限。
	readOnly bool
}

func (h chatInteraction) Handle(ctx context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
	response := native.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: native.InteractionCancel}
	switch request.Kind {
	case native.InteractionApproval:
		if h.approval == nil || request.Approval == nil || h.readOnly {
			return response, nil
		}
		approval := request.Approval
		product := ApprovalRequest{InvocationID: request.Invocation.ID, ToolCallID: approval.ToolCallID, Title: approval.Title, Kind: approval.Kind, Summary: approval.Summary, Command: approval.Command}
		for _, option := range approval.Options {
			product.Options = append(product.Options, ApprovalOption{ID: option.ID, Name: option.Name, Kind: option.Kind})
		}
		for _, location := range approval.Locations {
			product.Locations = append(product.Locations, ApprovalLocation{Path: location.Path, Line: location.Line})
		}
		decision, err := h.approval.Request(ctx, product)
		if err != nil {
			return response, err
		}
		if !decision.Cancelled {
			response.Action = native.InteractionAccept
			response.OptionID = decision.OptionID
		}
	case native.InteractionInput:
		if h.input == nil || request.Input == nil {
			return response, nil
		}
		input := request.Input
		decision, err := h.input.Request(ctx, ElicitationRequest{InvocationID: request.Invocation.ID, Mode: input.Mode, Message: request.Prompt, RequestedSchema: input.RequestedSchema, URL: input.URL})
		if err != nil {
			return response, err
		}
		if decision.Accepted {
			response.Action = native.InteractionAccept
			response.Content = decision.Content
		} else if decision.Declined {
			response.Action = native.InteractionDecline
		}
	}
	return response, nil
}

func mapChatPreparationFault(err error) error {
	var technical *native.Fault
	if errors.As(err, &technical) {
		if technical.Code == native.FaultSessionBusy {
			return status_error.AIAccountBusy
		}
		if technical.Cause != nil && technical.Code == native.FaultCancelled {
			return technical.Cause
		}
	}
	return mapFault(err)
}
func chatSeed(history []HistoryMessage) []native.SeedMessage {
	seed := make([]native.SeedMessage, 0, len(history))
	for _, message := range history {
		references := make([]native.CodeReference, 0, len(message.CodeReferences))
		for _, reference := range message.CodeReferences {
			references = append(references, native.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
		}
		seed = append(seed, native.SeedMessage{Role: message.Role, Text: message.Content, CodeReferences: references})
	}
	return seed
}
func chatFailed(plan SessionPlan, session SessionDirective, err error) Result {
	code := ClassifyFailure(sessionBase(plan), err)
	return Result{ExitCode: -1, Session: SessionResult{Mode: session.Mode, InputID: session.ID, State: SessionStateUnknown}, Failure: &Failure{Code: code, Stage: constants.RuntimeRunFailureStagePrepare, Message: err.Error(), DisplayMessage: FailureDisplayMessage(err), Transience: FailureTransiencePermanent}}
}

// InteractionPolicy 表达交互端口的使用方式。
type InteractionPolicy string

const (
	InteractionUnattended  InteractionPolicy = "unattended"  // InteractionUnattended 不等待人工输入。
	InteractionInteractive InteractionPolicy = "interactive" // InteractionInteractive 使用本次交互端口。
)

// OutputPolicy 表达最终回答的证据要求。
type OutputPolicy string

const (
	OutputStrict     OutputPolicy = "strict"      // OutputStrict 拒绝无最终证明的正文。
	OutputBestEffort OutputPolicy = "best_effort" // OutputBestEffort 允许明确标注未验证的单条正文。
)

// Grants 是宿主授予的权限上界。
type Grants struct {
	Directories   []string // Directories 是可访问目录上界。
	Tools         []string // Tools 是可调用工具上界。
	AllowAllTools bool     // AllowAllTools 明确授予任意工具。
}

// OutputStatus 表达最终答案可用性。
type OutputStatus string

const (
	OutputAvailable     OutputStatus = "available"          // OutputAvailable 表示存在满足策略的答案。
	OutputUnavailable   OutputStatus = "output_unavailable" // OutputUnavailable 表示缺少最终答案证明。
	OutputEmpty         OutputStatus = "empty"              // OutputEmpty 表示已验证但为空。
	OutputRefused       OutputStatus = "refused"            // OutputRefused 表示拒绝回答。
	OutputTruncated     OutputStatus = "truncated"          // OutputTruncated 表示答案被截断。
	OutputLimitExceeded OutputStatus = "limit_exceeded"     // OutputLimitExceeded 表示答案超过预算。
)

// Reliability 表达答案所依据的证明。
type Reliability string

const (
	ReliabilityVerifiedPhase      Reliability = "verified_phase"             // ReliabilityVerifiedPhase 依据明确 final 阶段。
	ReliabilityVerifiedCompletion Reliability = "verified_driver_completion" // ReliabilityVerifiedCompletion 依据驱动明确等价语义。
	ReliabilityUnverified         Reliability = "unverified"                 // ReliabilityUnverified 没有最终证明。
)

// Output 保存有界最终正文。
type Output struct {
	Status      OutputStatus // Status 是正文可用性。
	Text        string       // Text 是满足预算的单条最终消息正文。
	Reliability Reliability  // Reliability 是最终答案证明。
}

// Effects 保留已发生的事实，不能用于隐式重试。
type Effects struct {
	PromptAccepted bool // PromptAccepted 表示输入已经交付。
	ToolActivity   bool // ToolActivity 表示观察到工具调用。
	OutcomeUnknown bool // OutcomeUnknown 表示取消或故障后副作用未知。
}

// turnResult 分别保存执行、最终证据、交付与清理结果。
type turnResult struct {
	Invocation    native.InvocationRef     // Invocation 是本次唯一调用身份。
	Status        native.ExecutionStatus   // Status 是原生执行状态。
	StopReason    native.StopReason        // StopReason 是真实停止原因。
	Output        Output                   // Output 是有界最终答案。
	Fault         *native.Fault            // Fault 是执行失败。
	DeliveryFault *native.Fault            // DeliveryFault 独立保存交付失败。
	CleanupFault  *native.Fault            // CleanupFault 独立保存清理失败。
	Usage         native.Usage             // Usage 是原生用量。
	Configuration native.Configuration     // Configuration 是原生回执确认的配置。
	Effects       Effects                  // Effects 是已发生的真实事实。
	NativeSession native.NativeSessionInfo // NativeSession 是真实原生句柄及定位。
}

// nativeCapabilities 只用于检查有效策略，生产 ACP 不声称未经验证的工具隔离。
type nativeCapabilities struct {
	Version        string // Version 是能力实现版本。
	ExplicitConfig bool   // ExplicitConfig 证明可以限制配置来源。
	DenyAllTools   bool   // DenyAllTools 证明能禁止全部工具。
	ToolAllowList  bool   // ToolAllowList 证明能限制明确工具集合。
	HotResume      bool   // HotResume 证明能复用当前原生消费者。
	ColdResume     bool   // ColdResume 证明能按原生握手恢复。
}

// observedEvent 为协议事实附加当前回合身份和顺序。
type observedEvent struct {
	Invocation  native.InvocationRef // Invocation 是事实归属。
	Sequence    uint64               // Sequence 是本轮同步交付序号。
	NativeEvent native.Event         // NativeEvent 是原生公开事实。
}
type turnObserver interface {
	Accept(context.Context, observedEvent) error
}

// turnInteractionHandler 的成功选择代表宿主已在期限内取得唯一接纳权；持久化与发布完成后的返回可晚于等待期限。
type turnInteractionHandler interface {
	Handle(context.Context, native.InteractionRequest) (native.InteractionResponse, error)
}
type turnOptions struct {
	Observer           turnObserver           // Observer 同步消费本轮有序事实。
	InteractionHandler turnInteractionHandler // InteractionHandler 处理当前交互。
	UserInputState     UserInputState         // UserInputState 观察宿主能力的人工等待。
}

// executionPolicies 是编译阶段的有效策略视图，公共合同分别属于会话与本轮输入。
type executionPolicies struct {
	Tools        native.ToolPolicy         // Tools 是工具策略。
	NativeConfig native.NativeConfigPolicy // NativeConfig 是原生配置策略。
	Interaction  InteractionPolicy         // Interaction 是交互策略。
	Output       OutputPolicy              // Output 是结果证据策略。
}

// executionSpec 是仅供内核编译的冻结视图，不能作为公共执行入口。
type executionSpec struct {
	native.OpenRequest                   // OpenRequest 是当前一次原生边界投影，不是第二套会话计划。
	Input              native.Input      // Input 是本轮新增消息。
	Policies           executionPolicies // Policies 是本轮有效策略。
	Limits             native.Limits     // Limits 是已规范化预算。
}

// compiledExecution 隐藏所有可变切片，读取时复制。
type compiledExecution struct {
	spec executionSpec // spec 是深冻结后的有效描述。
}

// executionSpec 返回不可影响计划的深复制描述。
func (p compiledExecution) Spec() executionSpec { return cloneExecution(p.spec) }

// DefaultLimits 返回首批工程保护值，累计预算不等同于 ACP 排队预算。
func defaultLimits() native.Limits {
	return native.Limits{MaxOutputBytes: 1 << 20, MaxEventBytes: 1 << 20, MaxEvents: 16384, MaxTotalEventBytes: 64 << 20, DeliveryTimeout: 5 * time.Second, InteractionTimeout: 5 * time.Minute, InactivityTimeout: 10 * time.Minute}
}

// NormalizeLimits 补全零值并拒绝负值。
func normalizeLimits(v, defaults native.Limits) (native.Limits, error) {
	if v.MaxOutputBytes < 0 || v.MaxEventBytes < 0 || v.MaxEvents < 0 || v.MaxTotalEventBytes < 0 || v.DeliveryTimeout < 0 || v.InteractionTimeout < 0 || v.InactivityTimeout < 0 {
		return v, bad("negative execution budget")
	}
	if v.MaxOutputBytes == 0 {
		v.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if v.MaxEventBytes == 0 {
		v.MaxEventBytes = defaults.MaxEventBytes
	}
	if v.MaxEvents == 0 {
		v.MaxEvents = defaults.MaxEvents
	}
	if v.MaxTotalEventBytes == 0 {
		v.MaxTotalEventBytes = defaults.MaxTotalEventBytes
	}
	if v.DeliveryTimeout == 0 {
		v.DeliveryTimeout = defaults.DeliveryTimeout
	}
	if v.InteractionTimeout == 0 {
		v.InteractionTimeout = defaults.InteractionTimeout
	}
	if v.InactivityTimeout == 0 {
		v.InactivityTimeout = defaults.InactivityTimeout
	}
	if v.MaxOutputBytes > v.MaxTotalEventBytes || v.MaxEventBytes > v.MaxTotalEventBytes {
		return v, bad("event and output budgets exceed total event budget")
	}
	return v, nil
}

// Compile 校验能力、收敛权限并产生不含秘密的稳定计划。
func compileExecution(input executionSpec, caps nativeCapabilities, grants Grants, defaults native.Limits) (compiledExecution, error) {
	// 在深复制前核算历史预算，避免超限输入先造成无界复制。
	if len(input.Seed) > 40 {
		return compiledExecution{}, bad("seed message count exceeds twenty complete turns")
	}
	seedBytes := 0
	for _, seed := range input.Seed {
		if len(seed.Text) > (64<<10)-seedBytes {
			return compiledExecution{}, bad("seed text budget exceeded")
		}
		seedBytes += len(seed.Text)
		for _, reference := range seed.CodeReferences {
			for _, text := range []string{reference.Path, reference.Content} {
				if len(text) > (64<<10)-seedBytes {
					return compiledExecution{}, bad("seed text budget exceeded")
				}
				seedBytes += len(text)
			}
		}
	}
	s := cloneExecution(input)
	var err error
	if s.Profile.DriverID == "" {
		return compiledExecution{}, bad("driver identity missing")
	}
	if s.Session.Mode == "" {
		s.Session.Mode = native.SessionNew
	}
	switch s.Session.Mode {
	case native.SessionNew:
		if s.Session.NativeHandle != "" {
			return compiledExecution{}, bad("new session cannot include a native identity")
		}
	case native.SessionResume:
		if len(s.Seed) > 0 {
			return compiledExecution{}, bad("resume cannot replay seed history")
		}
		// 这里不拥有连接状态；冷恢复的能力由 Runtime 在 Bind 时检查。
		if !caps.HotResume && !caps.ColdResume {
			return compiledExecution{}, unsupported("driver session resume is not verified")
		}
	default:
		return compiledExecution{}, bad("unknown session mode")
	}
	for _, seed := range s.Seed {
		if seed.Role != "user" && seed.Role != "assistant" {
			return compiledExecution{}, bad("unsupported seed role")
		}
	}
	if s.Policies.Output == "" {
		s.Policies.Output = OutputStrict
	}
	if s.Policies.Output != OutputStrict && s.Policies.Output != OutputBestEffort {
		return compiledExecution{}, bad("unknown output policy")
	}
	if s.Policies.Interaction == "" {
		s.Policies.Interaction = InteractionUnattended
	}
	if s.Policies.Interaction != InteractionUnattended && s.Policies.Interaction != InteractionInteractive {
		return compiledExecution{}, bad("unknown interaction policy")
	}
	if s.Policies.NativeConfig == "" {
		s.Policies.NativeConfig = native.NativeConfigExplicitOnly
	}
	switch s.Policies.NativeConfig {
	case native.NativeConfigExplicitOnly:
		if !caps.ExplicitConfig {
			return compiledExecution{}, unsupported("explicit native configuration is not verified")
		}
	case native.NativeConfigInherit:
	default:
		return compiledExecution{}, bad("unknown native configuration policy")
	}
	if s.Policies.Tools.Mode == "" {
		s.Policies.Tools.Mode = native.ToolsDenyAll
	}
	switch s.Policies.Tools.Mode {
	case native.ToolsDenyAll:
		s.Policies.Tools.Allow = nil
	case native.ToolsAllowAll:
		if !grants.AllowAllTools {
			s.Policies.Tools.Mode = native.ToolsAllowList
			s.Policies.Tools.Allow = slices.Clone(grants.Tools)
		} else {
			s.Policies.Tools.Allow = nil
		}
	case native.ToolsAllowList:
		allow := []string{}
		for _, name := range s.Policies.Tools.Allow {
			if name != "" && (grants.AllowAllTools || slices.Contains(grants.Tools, name)) {
				allow = append(allow, name)
			}
		}
		s.Policies.Tools.Allow = allow
	default:
		return compiledExecution{}, bad("unknown tool policy")
	}
	if s.Policies.Tools.Mode == native.ToolsAllowList {
		slices.Sort(s.Policies.Tools.Allow)
		s.Policies.Tools.Allow = slices.Compact(s.Policies.Tools.Allow)
		if len(s.Policies.Tools.Allow) == 0 {
			s.Policies.Tools.Mode = native.ToolsDenyAll
		}
	}
	if s.Policies.Tools.Mode == native.ToolsDenyAll && !caps.DenyAllTools {
		return compiledExecution{}, unsupported("deny all tools is not verified")
	}
	if s.Policies.Tools.Mode == native.ToolsAllowList && !caps.ToolAllowList {
		return compiledExecution{}, unsupported("tool allow list is not verified")
	}
	if s.Workspace.Root != "" {
		s.Workspace.Root, err = directory(s.Workspace.Root, grants.Directories)
		if err != nil {
			return compiledExecution{}, err
		}
	}
	for i, path := range s.Workspace.Directories {
		s.Workspace.Directories[i], err = directory(path, grants.Directories)
		if err != nil {
			return compiledExecution{}, err
		}
	}
	slices.Sort(s.Workspace.Directories)
	s.Workspace.Directories = slices.Compact(s.Workspace.Directories)
	effective := slices.Clone(s.Workspace.Directories)
	if s.Workspace.Root != "" {
		effective = append(effective, s.Workspace.Root)
	}
	for _, a := range s.Input.Attachments {
		if _, err = directory(a.Path, effective); err != nil {
			return compiledExecution{}, bad("attachment is outside effective workspace")
		}
	}
	if err = contributions(&s.Contributions); err != nil {
		return compiledExecution{}, err
	}
	defaults, err = normalizeLimits(defaults, defaultLimits())
	if err != nil {
		return compiledExecution{}, err
	}
	s.Limits, err = normalizeLimits(s.Limits, defaults)
	if err != nil {
		return compiledExecution{}, err
	}
	return compiledExecution{spec: s}, nil
}

func contributions(v *native.Contributions) error {
	instructions := make(map[string]native.Instruction)
	out := []native.Instruction{}
	for _, x := range v.Instructions {
		if x.Name == "" {
			return bad("instruction identity missing")
		}
		if old, ok := instructions[x.Name]; ok {
			if old != x {
				return bad("conflicting instruction contribution")
			}
			continue
		}
		instructions[x.Name] = x
		out = append(out, x)
	}
	v.Instructions = out
	names := make(map[string]string)
	skills := []native.Skill{}
	for _, x := range v.Skills {
		if x.Name == "" || x.ResourceID == "" {
			return bad("skill identity missing")
		}
		key := "skill:" + x.Name
		value := digest(x)
		if old, ok := names[key]; ok {
			if old != value {
				return bad("conflicting skill contribution")
			}
			continue
		}
		names[key] = value
		skills = append(skills, x)
	}
	v.Skills = skills
	mcp := []native.MCP{}
	for _, x := range v.MCP {
		if x.Name == "" || x.ResourceID == "" {
			return bad("mcp identity missing")
		}
		key := "mcp:" + x.Name
		value := digest(x)
		if old, ok := names[key]; ok {
			if old != value {
				return bad("conflicting mcp contribution")
			}
			continue
		}
		names[key] = value
		mcp = append(mcp, x)
	}
	v.MCP = mcp
	return nil
}
func directory(path string, grants []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", bad("workspace path must be absolute")
	}
	resolved, err := canonicalPath(path)
	if err != nil {
		return "", bad("workspace path cannot be resolved")
	}
	for _, root := range grants {
		if !filepath.IsAbs(root) {
			continue
		}
		resolvedRoot, err := canonicalPath(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", bad("workspace path is not granted")
}

// canonicalPath 对尚不存在的叶节点解析最近已存在祖先，拒绝当前符号链接逃逸。
// 文件系统后续变化仍由宿主快照租约和 Driver 原生隔离边界控制。
func canonicalPath(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		parent := filepath.Dir(current)
		if parent == current {
			return "", bad("path has no resolvable ancestor")
		}
		current = parent
	}
}

func cloneExecution(v executionSpec) executionSpec {
	if v.Profile.Thinking != nil {
		thinking := *v.Profile.Thinking
		v.Profile.Thinking = &thinking
	}
	v.Input.Attachments = slices.Clone(v.Input.Attachments)
	v.Input.CodeReferences = slices.Clone(v.Input.CodeReferences)
	v.Input.SelectedSkills = slices.Clone(v.Input.SelectedSkills)
	v.Input.SelectedMCP = slices.Clone(v.Input.SelectedMCP)
	v.Seed = slices.Clone(v.Seed)
	for index := range v.Seed {
		v.Seed[index].CodeReferences = slices.Clone(v.Seed[index].CodeReferences)
	}
	v.Workspace.Directories = slices.Clone(v.Workspace.Directories)
	v.Contributions.Instructions = slices.Clone(v.Contributions.Instructions)
	v.Contributions.Skills = slices.Clone(v.Contributions.Skills)
	v.Contributions.MCP = slices.Clone(v.Contributions.MCP)
	v.Policies.Tools.Allow = slices.Clone(v.Policies.Tools.Allow)
	return v
}

func digest(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func bad(message string) *native.Fault {
	return &native.Fault{Code: native.FaultInvalidSpec, Stage: "plan", Message: message}
}
func unsupported(message string) *native.Fault {
	return &native.Fault{Code: native.FaultUnsupported, Stage: "plan", Message: message}
}

// outputCollector 只保存当前消息和最近最终候选；已见身份集合受事件预算限制。
type outputCollector struct {
	policy      OutputPolicy        // policy 是最终证据要求。
	maxBytes    int                 // maxBytes 是每个候选正文上限。
	maxMessages int                 // maxMessages 限制消息身份数量。
	currentID   string              // currentID 是当前连续消息身份。
	phase       native.MessagePhase // phase 是当前消息已知阶段。
	verified    bool                // verified 是驱动等价完成证明。
	complete    bool                // complete 是消息完成边界。
	text        strings.Builder     // text 有界保留当前正文，允许完成边界重新分类。
	seen        map[string]bool     // seen 防止交错分片被误当作完整答案。
	final       Output              // final 是前序消息中最近的明确最终候选。
	fallback    Output              // fallback 是前序消息中最近的未验证候选。
	discarded   bool                // discarded 表示当前非最终正文超过保留预算，无法再恢复完整答案。
	exceeded    bool                // exceeded 表示正文预算耗尽。
	invalid     bool                // invalid 表示消息身份契约损坏。
}

// New 建立无后台工作的有界收集器。
func newCollector(policy OutputPolicy, limits native.Limits) *outputCollector {
	return &outputCollector{policy: policy, maxBytes: limits.MaxOutputBytes, maxMessages: limits.MaxEvents, seen: make(map[string]bool)}
}

// Accept 接收同一消息的连续增量，拒绝会造成伪完整正文的交错消息。
func (co *outputCollector) Accept(m native.AssistantMessage) error {
	if co.exceeded {
		return &native.Fault{Code: native.FaultLimit, Stage: "output", Message: "final answer budget exceeded"}
	}
	if m.MessageID == "" {
		// 缺失身份只说明无法提取，不证明原生执行失败。
		return nil
	}
	if m.MessageID != co.currentID {
		co.snapshot()
		if co.seen[m.MessageID] || len(co.seen) >= co.maxMessages {
			co.invalid = true
			return &native.Fault{Code: native.FaultProtocol, Stage: "output", Message: "assistant message identity interleaved or over budget"}
		}
		co.seen[m.MessageID] = true
		co.currentID = m.MessageID
		co.phase = native.PhaseUnknown
		co.verified = false
		co.complete = false
		co.discarded = false
		co.text.Reset()
	}
	if co.complete && m.Delta != "" {
		co.invalid = true
		return &native.Fault{Code: native.FaultProtocol, Stage: "output", Message: "text arrived after message completion"}
	}
	if m.Phase != "" && m.Phase != native.PhaseUnknown {
		if co.phase != native.PhaseUnknown && co.phase != m.Phase && !m.Complete {
			co.invalid = true
			return &native.Fault{Code: native.FaultProtocol, Stage: "output", Message: "assistant phase changed incompatibly"}
		}
		co.phase = m.Phase
	}
	co.verified = co.verified || m.CompletionVerified
	co.complete = co.complete || m.Complete
	if co.discarded || len(m.Delta) > co.maxBytes-co.text.Len() {
		co.text.Reset()
		if co.phase == native.PhaseCommentary {
			// 过程正文不能耗尽最终输出预算；只记录此消息已无法完整恢复。
			co.discarded = true
			return nil
		}
		co.exceeded = true
		co.final = Output{}
		co.fallback = Output{}
		return &native.Fault{Code: native.FaultLimit, Stage: "output", Message: "final answer budget exceeded"}
	}
	co.text.WriteString(m.Delta)
	return nil
}

func (co *outputCollector) snapshot() {
	value := co.candidate()
	if value.Reliability == "" {
		return
	}
	co.fallback = Output{Text: value.Text, Reliability: ReliabilityUnverified}
	if value.Reliability != ReliabilityUnverified {
		co.final = value
	}
}

func (co *outputCollector) candidate() Output {
	if co.currentID == "" || co.phase == native.PhaseCommentary || co.discarded {
		return Output{}
	}
	value := Output{Text: co.text.String(), Reliability: ReliabilityUnverified}
	if co.phase == native.PhaseFinal {
		value.Reliability = ReliabilityVerifiedPhase
	} else if co.verified && co.complete {
		value.Reliability = ReliabilityVerifiedCompletion
	}
	return value
}

// Finish 仅在原生终态允许时提供最终答案，执行状态由 Runtime 独立保留。
func (co *outputCollector) Finish(stop native.StopReason) Output {
	if co.exceeded {
		return Output{Status: OutputLimitExceeded, Reliability: ReliabilityUnverified}
	}
	if co.invalid {
		return Output{Status: OutputUnavailable, Reliability: ReliabilityUnverified}
	}
	if stop == native.StopRefused {
		return Output{Status: OutputRefused, Reliability: ReliabilityUnverified}
	}
	if stop != native.StopEndTurn && stop != native.StopMaxTokens {
		return Output{Status: OutputUnavailable, Reliability: ReliabilityUnverified}
	}
	// 当前消息在完成边界仍可撤销，不能覆盖前序消息的候选。
	current := co.candidate()
	result := co.final
	if current.Reliability != "" && current.Reliability != ReliabilityUnverified {
		result = current
	}
	if result.Reliability == "" && co.policy == OutputBestEffort {
		result = current
		if result.Reliability == "" {
			result = co.fallback
		}
	}
	if result.Reliability == "" {
		return Output{Status: OutputUnavailable, Reliability: ReliabilityUnverified}
	}
	if stop == native.StopMaxTokens {
		result.Status = OutputTruncated
		return result
	}
	if strings.TrimSpace(result.Text) == "" {
		result.Status = OutputEmpty
	} else {
		result.Status = OutputAvailable
	}
	return result
}

type eventSink struct {
	session            *Session                      // session 提供日志所需的唯一配置事实。
	mu                 sync.Mutex                    // mu 串行处理事件与交互，避免重复交付。
	call               *Turn                         // call 是唯一所属调用。
	spec               executionSpec                 // spec 是本轮私有冻结描述。
	options            turnOptions                   // options 是本轮观察与交互端口。
	collector          *outputCollector              // collector 仅收集有界最终候选。
	sequence           uint64                        // sequence 是本轮交付顺序。
	bytes              int                           // bytes 累计参数快照新增字节及其他事件的完整字节。
	events             int                           // events 是离散事实与空/重复无进展通知的累计数量。
	closed             bool                          // closed 拒绝 Prompt 返回后的迟到生产。
	effects            Effects                       // effects 保存真实输入与工具事实。
	firstFault         *native.Fault                 // firstFault 保存同步消费的首个故障。
	deliveryFault      *native.Fault                 // deliveryFault 独立保存观察交付故障。
	interactions       map[string]bool               // interactions 保存有界已回答请求身份。
	tools              map[string]toolEventState     // tools 保存授权与预算共用的最近工具事实，身份和字节均有界。
	messageState       messageEventState             // messageState 保存当前消息的计数边界，不保留正文。
	activeInteractions map[string]context.CancelFunc // activeInteractions 允许 finish 撤销并等待已接纳的交互。
	interactionsDone   chan struct{}                 // interactionsDone 在当前活动交互全部退出时关闭。
	cleanupTimeout     time.Duration                 // cleanupTimeout 限制 Prompt 返回后的交互收敛等待。
	logger             *slog.Logger                  // logger 只接收技术诊断，不交付业务事实。
	promptStarted      time.Time                     // promptStarted 是提交当前输入的阶段起点，不包含 CLI 初始化。
}

func newSink(call *Turn, session *Session, spec executionSpec, options turnOptions, cleanupTimeout time.Duration, logger *slog.Logger) *eventSink {
	return &eventSink{session: session, call: call, spec: spec, options: options, collector: newCollector(spec.Policies.Output, spec.Limits), interactions: make(map[string]bool), tools: make(map[string]toolEventState), activeInteractions: make(map[string]context.CancelFunc), cleanupTimeout: cleanupTimeout, logger: logger, promptStarted: time.Now()}
}
func (s *eventSink) fail(f *native.Fault) error {
	if s.firstFault == nil {
		s.firstFault = f
		s.call.cancel(f)
	}
	return s.firstFault
}

// Accept 在同步消费链中核算预算；事实交付始终由本轮 Context 拥有。
// 来源 Context 不抹掉已发生的 Prompt/工具事实，Observer 使用本轮 Context 和独立交付期限。
func (s *eventSink) Accept(_ context.Context, event native.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fault(native.FaultProtocol, "event", "event arrived after prompt returned", nil)
	}
	// 排空仍属于已接纳回合：先登记收到的真实证据，再决定是否继续消费/交付。
	duplicateAcceptance := event.Kind == native.EventPromptAccepted && s.effects.PromptAccepted
	if event.Kind == native.EventPromptAccepted {
		if !duplicateAcceptance {
			logStage(s.logger, s.call, s.session, "input_acceptance", time.Since(s.promptStarted), true, false)
		}
		s.effects.PromptAccepted = true
	}
	if event.Kind == native.EventTool && event.Tool != nil {
		s.effects.ToolActivity = true
	}
	if s.firstFault != nil {
		return s.firstFault
	}
	if err := s.call.ctx.Err(); err != nil {
		return err
	}
	if duplicateAcceptance {
		return s.fail(fault(native.FaultProtocol, "event", "prompt acceptance emitted twice", nil))
	}
	// 按规范载荷的字符串实际字节与固定标量开销计费；事件热路径不做 JSON 编码。
	// 32 字节是每个事件的固定预算开销，消息完成标记另计 2 字节，用量的七个整数与两个标记另计 58 字节。
	size := 32 + len(event.Text) + len(event.Kind)
	if event.Message != nil {
		m := *event.Message
		event.Message = &m
		var contentBytes int
		m.Content, contentBytes = cloneContent(m.Content)
		size += len(m.MessageID) + len(m.Phase) + len(m.Delta) + contentBytes + 2
	}
	if event.Tool != nil {
		tool := *event.Tool
		event.Tool = &tool
		var contentBytes int
		tool.Content, contentBytes = cloneContent(tool.Content)
		tool.Input = slices.Clone(tool.Input)
		tool.Output = slices.Clone(tool.Output)
		tool.Locations = slices.Clone(tool.Locations)
		for i, location := range tool.Locations {
			size += len(location.Path) + 8
			if location.Line != nil {
				line := *location.Line
				tool.Locations[i].Line = &line
			}
		}
		size += len(tool.ID) + len(tool.Name) + len(tool.Title) + len(tool.Kind) + len(tool.Status) + len(tool.Text) + len(tool.Input) + len(tool.Output) + contentBytes
	}
	if event.Usage != nil {
		usage := *event.Usage
		event.Usage = &usage
		size += 58
	}
	// 参数是同一工具的替换快照，不是每次都新增一份业务正文。
	// 只抵扣与紧邻上一快照相同的前缀；独立工具、改写、清空后重建仍计新增字节，预算不退款。
	reusedInputBytes := 0
	if event.Kind == native.EventTool && event.Tool != nil && event.Tool.Input != nil {
		previous := s.tools[event.Tool.ID].input
		for reusedInputBytes < len(previous) && reusedInputBytes < len(event.Tool.Input) && previous[reusedInputBytes] == event.Tool.Input[reusedInputBytes] {
			reusedInputBytes++
		}
	}
	chargedBytes := size - reusedInputBytes
	if size > s.spec.Limits.MaxEventBytes {
		return s.fail(eventLimit(native.LimitSingleEventBytes, "event", "event delivery budget exceeded: single event bytes"))
	}
	discrete := s.discreteEvent(event)
	if discrete && s.events >= s.spec.Limits.MaxEvents {
		return s.fail(eventLimit(native.LimitEventCount, "event", "event delivery budget exceeded: discrete event count"))
	}
	if chargedBytes > s.spec.Limits.MaxTotalEventBytes-s.bytes {
		return s.fail(eventLimit(native.LimitTotalEventBytes, "event", "event delivery budget exceeded: cumulative event bytes"))
	}
	var err error
	if discrete {
		s.events++
	}
	s.bytes += chargedBytes
	switch event.Kind {
	case native.EventPromptAccepted:
	case native.EventAssistantMessage:
		if !s.effects.PromptAccepted || event.Message == nil {
			return s.fail(fault(native.FaultProtocol, "event", "assistant message before prompt acceptance or without payload", nil))
		}
		if err = s.collector.Accept(*event.Message); err != nil {
			return s.fail(asFault(err, native.FaultProtocol, "output", "invalid assistant message"))
		}
		s.rememberMessage(*event.Message)
	case native.EventThinking:
		if !s.effects.PromptAccepted {
			return s.fail(fault(native.FaultProtocol, "event", "thinking event before prompt acceptance", nil))
		}
	case native.EventTool:
		if !s.effects.PromptAccepted || event.Tool == nil {
			return s.fail(fault(native.FaultProtocol, "event", "tool event before prompt acceptance or without payload", nil))
		}
		s.effects.ToolActivity = true
		if event.Tool.ID == "" {
			return s.fail(fault(native.FaultProtocol, "event", "tool activity identity missing", nil))
		}
		name := event.Tool.Name
		if previous, known := s.tools[event.Tool.ID]; known {
			if name == "" {
				name = previous.name
			} else if previous.name != "" && previous.name != name {
				return s.fail(fault(native.FaultProtocol, "event", "tool activity identity changed tool name", nil))
			}
		}
		if s.spec.Policies.Tools.Mode != native.ToolsAllowAll && !s.toolAllowed(name) {
			return s.fail(fault(native.FaultProtocol, "event", "driver emitted unauthorized tool activity", nil))
		}
		s.rememberTool(*event.Tool, name)
	case native.EventUsage:
		if !s.effects.PromptAccepted || event.Usage == nil {
			return s.fail(fault(native.FaultProtocol, "event", "usage event before prompt acceptance or without payload", nil))
		}
	default:
		return s.fail(fault(native.FaultProtocol, "event", "unknown driver event kind", nil))
	}
	s.sequence++
	if s.options.Observer != nil {
		ctx, cancel := context.WithTimeout(s.call.ctx, s.spec.Limits.DeliveryTimeout)
		defer cancel()
		err = s.options.Observer.Accept(ctx, observedEvent{Invocation: s.call.ref, Sequence: s.sequence, NativeEvent: event})
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			if s.call.ctx.Err() != nil {
				return s.call.ctx.Err()
			}
			s.deliveryFault = fault(native.FaultDelivery, "observer", "observer delivery failed", err)
			return s.fail(s.deliveryFault)
		}
	}
	return nil
}
func (s *eventSink) toolAllowed(name string) bool {
	return s.spec.Policies.Tools.Mode == native.ToolsAllowAll || name != "" && s.spec.Policies.Tools.Mode == native.ToolsAllowList && slices.Contains(s.spec.Policies.Tools.Allow, name)
}

// Interact 验证调用修订和一次性请求身份；无人值守输入立即结束调用。
func (s *eventSink) Interact(requestCtx context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
	empty := native.InteractionResponse{}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return empty, fault(native.FaultProtocol, "interaction", "interaction arrived after prompt returned", nil)
	}
	if s.firstFault != nil {
		err := s.firstFault
		s.mu.Unlock()
		return empty, err
	}
	if err := s.call.ctx.Err(); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	if !s.effects.PromptAccepted || request.ID == "" {
		err := s.fail(fault(native.FaultProtocol, "interaction", "invalid interaction identity or ordering", nil))
		s.mu.Unlock()
		return empty, err
	}
	if request.Invocation != (native.InvocationRef{}) && request.Invocation != s.call.ref {
		err := s.fail(fault(native.FaultProtocol, "interaction", "interaction invocation identity mismatch", nil))
		s.mu.Unlock()
		return empty, err
	}
	request.Invocation = s.call.ref
	if s.interactions[request.ID] {
		err := s.fail(fault(native.FaultProtocol, "interaction", "duplicate interaction identity", nil))
		s.mu.Unlock()
		return empty, err
	}
	encoded, encodeErr := json.Marshal(request)
	if encodeErr != nil {
		err := s.fail(fault(native.FaultProtocol, "interaction", "interaction request is not JSON", encodeErr))
		s.mu.Unlock()
		return empty, err
	}
	size := len(encoded)
	if err := s.reserveInteractionBudget(size); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	s.interactions[request.ID] = true
	// 即便撤销请求先于接纳到达，其身份也已失效，不能用新 Context 重用。
	if err := requestCtx.Err(); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	response := native.InteractionResponse{Invocation: s.call.ref, ID: request.ID, Action: native.InteractionCancel}
	if request.Kind != native.InteractionApproval && request.Kind != native.InteractionInput {
		err := s.fail(fault(native.FaultProtocol, "interaction", "unknown interaction kind", nil))
		s.mu.Unlock()
		return empty, err
	}
	if request.Kind == native.InteractionApproval && !s.toolAllowed(request.ToolName) {
		s.mu.Unlock()
		return response, nil
	}
	if s.spec.Policies.Interaction == InteractionUnattended {
		if request.Kind == native.InteractionApproval {
			if request.Approval != nil {
				for _, option := range request.Approval.Options {
					if option.Kind == "allow_once" || option.Kind == "allow_always" {
						response.Action = native.InteractionAccept
						response.OptionID = option.ID
						break
					}
				}
			}
			s.mu.Unlock()
			return response, nil
		}
		err := s.fail(fault(native.FaultInteractionRequired, "interaction", "user input is required", nil))
		s.mu.Unlock()
		return empty, err
	}
	if s.options.InteractionHandler == nil {
		err := s.fail(fault(native.FaultInteractionRequired, "interaction", "interaction handler is not available", nil))
		s.mu.Unlock()
		return empty, err
	}
	// 本次等待拥有独立期限；用结构化原因区分外层调用和模型超时。
	waitTimeout := fault(native.FaultInputTimeout, "interaction", "waiting for user input timed out", context.DeadlineExceeded)
	if request.Kind == native.InteractionApproval {
		waitTimeout = fault(native.FaultApprovalTimeout, "interaction", "waiting for approval timed out", context.DeadlineExceeded)
	}
	handlerCtx, cancel := context.WithTimeoutCause(s.call.ctx, s.spec.Limits.InteractionTimeout, waitTimeout)
	stopRequestCancellation := context.AfterFunc(requestCtx, cancel)
	if len(s.activeInteractions) == 0 {
		s.interactionsDone = make(chan struct{})
	}
	s.activeInteractions[request.ID] = cancel
	handler := s.options.InteractionHandler
	s.mu.Unlock()
	// 交互端口不占用事件消费锁；单请求撤销只使本问题失效。
	response, err := handler.Handle(handlerCtx, request)
	waitExpired := context.Cause(handlerCtx) == waitTimeout
	// 产品协调器拥有决定的唯一接纳点；接纳后的持久化/发布可跨过等待期限。
	decided := err == nil && (response.Action == native.InteractionAccept || response.Action == native.InteractionDecline)
	if err == nil && !decided {
		err = handlerCtx.Err()
	}
	stopRequestCancellation()
	cancel()
	s.mu.Lock()
	defer func() {
		delete(s.activeInteractions, request.ID)
		if len(s.activeInteractions) == 0 {
			close(s.interactionsDone)
		}
		s.mu.Unlock()
	}()
	if requestErr := requestCtx.Err(); requestErr != nil {
		return empty, requestErr
	}
	if callErr := s.call.ctx.Err(); callErr != nil {
		return empty, callErr
	}
	if s.closed {
		return empty, fault(native.FaultCancelled, "interaction", "interaction no longer belongs to an active turn", nil)
	}
	if s.firstFault != nil {
		return empty, s.firstFault
	}

	if waitExpired && !decided {
		return empty, s.fail(waitTimeout)
	}

	if err != nil {
		return empty, s.fail(fault(native.FaultInteractionRequired, "interaction", "interaction delivery failed", err))
	}
	if response.Invocation != s.call.ref || response.ID != request.ID {
		return empty, s.fail(fault(native.FaultProtocol, "interaction", "interaction response identity mismatch", nil))
	}
	if response.Action != native.InteractionAccept && response.Action != native.InteractionDecline && response.Action != native.InteractionCancel {
		return empty, s.fail(fault(native.FaultProtocol, "interaction", "unknown interaction response action", nil))
	}
	encoded, encodeErr = json.Marshal(response)
	if encodeErr != nil {
		return empty, s.fail(fault(native.FaultProtocol, "interaction", "interaction response is not JSON", encodeErr))
	}
	if err := s.reserveInteractionBudget(len(encoded)); err != nil {
		return empty, err
	}
	return response, nil
}

// reserveInteractionBudget 与普通事件共用本轮总量，不把交互身份集合变成另一条无界缓存。
func (s *eventSink) reserveInteractionBudget(size int) error {
	if size > s.spec.Limits.MaxEventBytes {
		return s.fail(eventLimit(native.LimitSingleEventBytes, "interaction", "interaction delivery budget exceeded: single event bytes"))
	}
	if s.events >= s.spec.Limits.MaxEvents {
		return s.fail(eventLimit(native.LimitEventCount, "interaction", "interaction delivery budget exceeded: discrete event count"))
	}
	if size > s.spec.Limits.MaxTotalEventBytes-s.bytes {
		return s.fail(eventLimit(native.LimitTotalEventBytes, "interaction", "interaction delivery budget exceeded: cumulative event bytes"))
	}
	s.events++
	s.bytes += size
	return nil
}

func (s *eventSink) finish() (*native.Fault, Effects, *native.Fault) {
	s.mu.Lock()
	s.closed = true
	var done <-chan struct{}
	if len(s.activeInteractions) > 0 {
		done = s.interactionsDone
		for _, cancel := range s.activeInteractions {
			cancel()
		}
	}
	s.mu.Unlock()
	if done != nil {
		timer := time.NewTimer(s.cleanupTimeout)
		select {
		case <-done:
		case <-timer.C:
			s.mu.Lock()
			s.fail(fault(native.FaultProtocol, "interaction", "driver returned while interaction handler did not stop", nil))
			s.mu.Unlock()
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryFault, s.effects, s.firstFault
}

// cloneContent 将规范内容与观察者分离，并按实际字符串字节计入本轮预算。
func cloneContent(content []native.ContentBlock) ([]native.ContentBlock, int) {
	result := slices.Clone(content)
	size := 0
	for i, block := range result {
		size += 32 + len(block.Type) + len(block.Text) + len(block.MIMEType) + len(block.Data) + len(block.URI) + len(block.Name) + len(block.TerminalID)
		if block.Diff != nil {
			diff := *block.Diff
			result[i].Diff = &diff
			size += len(diff.Path) + len(diff.OldText) + len(diff.NewText)
		}
	}
	return result, size
}

func sessionGrants(plan SessionPlan) Grants {
	return Grants{Directories: append([]string{plan.Workspace.Path}, plan.Workspace.AdditionalWritableDirectories...), AllowAllTools: true}
}

func validateResources(contributions native.Contributions, resources []native.ResourceBinding) error {
	bindings := make(map[string]native.ResourceBinding, len(resources))
	for _, binding := range resources {
		if binding.ID == "" || (binding.Path == "") == (binding.MCP == nil) {
			return fault(native.FaultProtocol, "resource", "invalid resource binding", nil)
		}
		if _, ok := bindings[binding.ID]; ok {
			return fault(native.FaultPreparation, "resource", "conflicting resource binding", nil)
		}
		bindings[binding.ID] = binding
	}
	for _, skill := range contributions.Skills {
		if bindings[skill.ResourceID].Path == "" && !skill.Optional {
			return fault(native.FaultPreparation, "resource", "required skill resource missing", nil)
		}
	}
	for _, mcp := range contributions.MCP {
		if bindings[mcp.ResourceID].MCP == nil && !mcp.Optional {
			return fault(native.FaultPreparation, "resource", "required mcp resource missing", nil)
		}
	}
	return nil
}

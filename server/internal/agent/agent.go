// Package agent 拥有产品会话、执行与资源生命周期。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// NativeConnection 是一次原生会话的窄连接边界。
type NativeConnection interface {
	Prompt(context.Context, native.TurnRequest, native.EventSink) native.TurnResult
	Close(context.Context) error
}

// ManagerConfig 注入产品权威端口和唯一原生连接边界，构造时不启动进程。
type ManagerConfig struct {
	// PrepareWorkspace 物化受管原生指令与技能，不提交输入。
	PrepareWorkspace func(SessionPlan) error
	// Launch 从已有探测与账号快照生成启动事实。
	Launch func(RuntimeTarget, DetectionResult) (LaunchFacts, error)
	// Runtimes 返回已经注册的目录声明。
	Runtimes func() []RuntimeSummary
	// Fork 独立执行无 Prompt 的原生分叉。
	Fork func(context.Context, ForkSessionRequest) (ForkSessionResult, error)
	// Resolve 解析独立文本与工具任务的明确环境路由。
	Resolve func(context.Context, TextRequest) (Environment, error)
	// Prepare 取得独立任务账号租约，部分失败也必须返回释放能力。
	Prepare func(context.Context, TextRequest, Environment, string) (RuntimeTarget, func(context.Context) error, error)
	// Open 连接一次原生会话；为空时使用正式 ACP 连接。
	Open func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error)
	// Limits 是所有回合的默认工程预算。
	Limits native.Limits
	// CleanupTimeout 是一次清理的总期限。
	CleanupTimeout time.Duration
	// Logger 只接收技术阶段诊断，不记录输入正文与秘密。
	Logger *slog.Logger
}

// Manager 登记工作区与临时 Session，不复制 Session 的活动、计划或资源状态。
type Manager struct {
	// config 是冻结的产品装配端口。
	config ManagerConfig
	// mutex 只保护索引、登记与关闭接纳，不覆盖原生 I/O。
	mutex sync.Mutex
	// workspaces 将产品工作区索引到唯一所有者。
	workspaces map[string]*Session
	// owners 保存所有尚未完成清理的会话，包括临时接纳占位。
	owners map[*Session]struct{}
	// temporary 只登记计入独立任务容量的所有者。
	temporary map[*Session]struct{}
	// root 统一取消已接纳的调用与后台清理。
	root context.Context
	// cancel 撤销根生命周期。
	cancel context.CancelFunc
	// closing 阻止新的接纳。
	closing bool
	// started 防止重复启动后台清理。
	started bool
	// workers 等待后台清理停止。
	workers sync.WaitGroup
	// admissions 等待尚在解析、准备独立任务的调用退出。
	admissions sync.WaitGroup
}

// NewManager 构造没有后台线程和原生进程的共享会话管理器。
func NewManager(config ManagerConfig) *Manager {
	if config.CleanupTimeout == 0 {
		config.CleanupTimeout = 5 * time.Second
	}
	if config.Logger == nil {
		config.Logger = AdapterLogger(context.Background())
	}
	root, cancel := context.WithCancel(context.Background())
	return &Manager{config: config, workspaces: make(map[string]*Session), owners: make(map[*Session]struct{}), temporary: make(map[*Session]struct{}), root: root, cancel: cancel}
}

// Start 显式启动每十秒一次、只面向已结束临时任务的清理重试。
func (m *Manager) Start(ctx context.Context) {
	if ctx == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.started || m.closing {
		return
	}
	m.started = true
	stop := context.AfterFunc(ctx, m.cancel)
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer stop()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.root.Done():
				return
			case <-ticker.C:
				m.retryTemporary()
			}
		}
	}()
}
func (m *Manager) retryTemporary() {
	m.mutex.Lock()
	sessions := make([]*Session, 0, len(m.temporary))
	for s := range m.temporary {
		sessions = append(sessions, s)
	}
	m.mutex.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		ready := s.ended && s.current == nil
		s.mu.Unlock()
		if ready {
			m.closeOwner(s)
		}
	}
}
func (m *Manager) closeOwner(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.CleanupTimeout)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		logx.Errorf("close agent session pending: error=%v", err)
		return
	}
	m.forget(s)
}
func (m *Manager) forget(s *Session) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.owners, s)
	delete(m.temporary, s)
	if m.workspaces[s.workspace] == s {
		delete(m.workspaces, s.workspace)
	}
}

// Runtimes 返回注册目录的当前声明。
func (m *Manager) Runtimes() []RuntimeSummary {
	if m.config.Runtimes == nil {
		return nil
	}
	return m.config.Runtimes()
}

// ForkSession 使用独立原生管理连接，不提交模型输入。
func (m *Manager) ForkSession(ctx context.Context, r ForkSessionRequest) (ForkSessionResult, error) {
	if m.config.Fork == nil {
		return ForkSessionResult{}, status_error.RuntimePolicyUnsupported
	}
	return m.config.Fork(ctx, r)
}

// PrepareSession 只占用工作区的唯一回合，冷资源由调用方显式取得并交接。
func (m *Manager) PrepareSession(ctx context.Context, plan SessionPlan, directive SessionDirective) (PreparedChatRun, error) {
	if ctx == nil || m.config.Launch == nil {
		return nil, status_error.BadRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, mapFault(err)
	}
	m.mutex.Lock()
	if m.closing {
		m.mutex.Unlock()
		return nil, status_error.RuntimeUnavailable
	}
	s := m.workspaces[plan.Workspace.Path]
	if s == nil {
		var err error
		s, err = newSession(m, plan.Workspace.Path, false)
		if err != nil {
			m.mutex.Unlock()
			return nil, mapFault(err)
		}
		m.workspaces[plan.Workspace.Path] = s
		m.owners[s] = struct{}{}
	}
	m.mutex.Unlock()
	p, err := s.prepare(ctx, plan, directive)
	if err != nil {
		return nil, mapChatPreparationFault(err)
	}
	return p, nil
}

// CloseWorkspace 取消并等待该工作区的回合，成功后移除同一所有者。
func (m *Manager) CloseWorkspace(ctx context.Context, workspace string) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	m.mutex.Lock()
	s := m.workspaces[workspace]
	m.mutex.Unlock()
	if s == nil {
		return nil
	}
	if err := s.Close(ctx); err != nil {
		return status_error.RuntimeShutdownFailed
	}
	m.forget(s)
	return nil
}

// CloseIdleAccount 在账号配置改变前关闭其闲置消费者。
func (m *Manager) CloseIdleAccount(ctx context.Context, id string) error {
	return m.closeIdle(ctx, "account:"+id, status_error.AIAccountBusy)
}

// CloseIdlePlugin 在插件改变前关闭其闲置消费者。
func (m *Manager) CloseIdlePlugin(ctx context.Context, id string) error {
	return m.closeIdle(ctx, "plugin:"+id, status_error.PluginBusy)
}
func (m *Manager) closeIdle(ctx context.Context, tag string, busy error) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	m.mutex.Lock()
	sessions := make([]*Session, 0, len(m.owners))
	for s := range m.owners {
		sessions = append(sessions, s)
	}
	m.mutex.Unlock()
	for _, s := range sessions {
		if err := s.closeIdle(ctx, tag); err != nil {
			var f *native.Fault
			if errors.As(err, &f) && f.Code == native.FaultSessionBusy {
				return busy
			}
			return status_error.RuntimeShutdownFailed
		}
	}
	return nil
}

// Close 停止接纳，取消所有准备及执行，关闭失败时保留同一所有者供重试。
func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	m.mutex.Lock()
	m.closing = true
	m.cancel()
	sessions := make([]*Session, 0, len(m.owners))
	for s := range m.owners {
		sessions = append(sessions, s)
	}
	m.mutex.Unlock()
	for _, s := range sessions {
		s.requestClose()
	}
	done := make(chan struct{})
	go func() { m.admissions.Wait(); m.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return status_error.RuntimeShutdownFailed
	}
	var first error
	for _, s := range sessions {
		if err := s.Close(ctx); err != nil {
			if first == nil {
				first = status_error.RuntimeShutdownFailed
			}
		} else {
			m.forget(s)
		}
	}
	return first
}

// ResourceRequest 描述独立任务已获授权且需要物化的资源。
type ResourceRequest struct {
	// Invocation 是本次唯一调用身份。
	Invocation native.InvocationRef
	// Workspace 是接纳时冻结的目录边界。
	Workspace native.WorkspaceScope
	// Contributions 是固定指令、技能与工具声明。
	Contributions native.Contributions
}

// ResourceLease 保护任务文件至原生消费者已确认退出。
type ResourceLease interface {
	Resources() []native.ResourceBinding
	Close(context.Context) error
}

// TaskResources 将调用期精确授权与会话文件资源的释放分开。
type TaskResources interface {
	Acquire(context.Context, ResourceRequest) (ResourceLease, error)
	LaunchEnvironment(map[string]string) map[string]string
	Revoke()
}

// TaskRequest 是全新独立工具任务，不接受聊天或恢复身份。
type TaskRequest struct {
	// TextRequest 保存模型、账号和本次完整消息。
	TextRequest
	// Contributions 保存必须实际交付的固定指令与资源。
	Contributions native.Contributions
	// Resources 物化任务文件并提供精确授权撤销。
	Resources TaskResources
}

// GenerateText 执行一次严格最终证据的独立文本任务。
func (m *Manager) GenerateText(ctx context.Context, input TextRequest) (TextResult, error) {
	return m.invoke(ctx, input, native.Contributions{}, nil)
}

// ExecuteTask 通过同一 Session 准备与执行机制执行独立工具任务。
func (m *Manager) ExecuteTask(ctx context.Context, input TaskRequest) (TextResult, error) {
	return m.invoke(ctx, input.TextRequest, input.Contributions, input.Resources)
}
func (m *Manager) invoke(ctx context.Context, input TextRequest, contributions native.Contributions, resources TaskResources) (TextResult, error) {
	if resources != nil {
		defer resources.Revoke()
	}
	if ctx == nil {
		return TextResult{}, status_error.BadRequest
	}
	if m == nil || m.config.Resolve == nil || m.config.Prepare == nil || m.config.Launch == nil {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	m.mutex.Lock()
	if m.closing || len(m.temporary) >= 64 {
		m.mutex.Unlock()
		return TextResult{}, status_error.RuntimeUnavailable
	}
	s, identityErr := newSession(m, "", true)
	if identityErr != nil {
		m.mutex.Unlock()
		return TextResult{}, mapFault(identityErr)
	}
	m.owners[s] = struct{}{}
	m.temporary[s] = struct{}{}
	m.admissions.Add(1)
	m.mutex.Unlock()
	defer m.admissions.Done()
	defer func() { s.mu.Lock(); s.ended = true; s.mu.Unlock(); m.closeOwner(s) }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.root, cancel)
	defer func() { stop(); cancel() }()
	if resources != nil {
		stopRevoke := context.AfterFunc(ctx, resources.Revoke)
		defer func() { stopRevoke(); resources.Revoke() }()
	}
	env, err := m.config.Resolve(ctx, input)
	if err != nil {
		return TextResult{}, err
	}
	if _, ok := native.Lookup(env.BaseRuntimeID); !ok {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	workspace, err := os.MkdirTemp("", "neux-runtime-model-")
	if err != nil {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, status_error.RuntimeUnavailable
	}
	s.workspace = resolved
	return m.execute(ctx, s, input, env, resolved, contributions, resources)
}
func (m *Manager) execute(ctx context.Context, s *Session, input TextRequest, env Environment, workspace string, contributions native.Contributions, resources TaskResources) (TextResult, error) {
	messages := make([]native.Message, 0, len(input.Messages))
	for _, v := range input.Messages {
		messages = append(messages, native.Message{Role: v.Role, Content: v.Content})
	}
	compiled, err := native.CompileTaskMessages(env.BaseRuntimeID, messages)
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	plan := SessionPlan{Target: DeclareTarget(env.Target), Detection: env.Detection, Workspace: Workspace{Path: workspace}, Policy: RunPolicy{Model: input.Model, Reasoning: input.Reasoning, Thinking: &input.Thinking, Execution: ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}}}
	if plan.Target.RuntimeID == "" {
		plan.Target.RuntimeID = input.RuntimeID
	}
	plan.Target.AccountID = input.AccountID
	if plan.Detection.Runtime.BaseRuntimeID == "" {
		plan.Detection.Runtime.BaseRuntimeID = env.BaseRuntimeID
	}
	for i, v := range compiled.Instructions {
		plan.Instructions = append(plan.Instructions, Instruction{Name: fmt.Sprintf("system-%d", i+1), Content: v})
	}
	p, err := s.prepare(ctx, plan, SessionDirective{Mode: SessionModeNew})
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	defer p.Close(context.WithoutCancel(ctx))
	target, release, acquireErr := m.config.Prepare(p.Context(), input, env, workspace)
	if target.RuntimeID == "" {
		target.RuntimeID = input.RuntimeID
	}
	if target.AccountID == "" {
		target.AccountID = input.AccountID
	}
	var lease ResourceLease
	request, planErr := nativeSessionRequest(plan)
	if acquireErr == nil && planErr != nil {
		acquireErr = planErr
	}
	request.Contributions = appendContributions(request.Contributions, contributions)
	if acquireErr == nil && resources != nil {
		lease, acquireErr = resources.Acquire(p.Context(), ResourceRequest{Invocation: p.turn.ref, Workspace: request.Workspace, Contributions: request.Contributions})
	}
	cleanup := func(context.Context) error {
		if err := os.RemoveAll(workspace); err != nil {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}
	bindErr := p.bind(p.Context(), SessionResources{Target: target, Close: release}, resourcePreparation{taskResources: resources, taskLease: lease, releaseWorkspace: cleanup, contributions: contributions, unattended: true, nativeInstructions: compiled.Instructions, acquisitionError: acquireErr})
	if acquireErr != nil {
		return TextResult{}, mapFault(acquireErr)
	}
	if bindErr != nil {
		return TextResult{}, mapFault(bindErr)
	}
	result := p.run(ctx, TurnInput{Message: Message{Text: compiled.Text}}, nil, true)
	output := TextResult{StopReason: string(result.StopReason), ActualReasoning: result.Configuration.ActualReasoning, ThinkingDisabled: result.Configuration.ThinkingDisabled, ReasoningFallback: result.Configuration.ReasoningFallback, Limitations: compiled.Limitations}
	if env.BaseRuntimeID != RuntimeIDCodex && !target.Isolated && resources == nil {
		output.Limitations = append(output.Limitations, "system_account_native_config_retained: global instructions and configured MCP may load; host does not mount external resources")
	}
	if env.BaseRuntimeID != RuntimeIDClaude && env.BaseRuntimeID != RuntimeIDWorkBuddy {
		output.Limitations = append(output.Limitations, "native_session_persistence_not_disabled: session is fresh and closed, CLI may retain its own logs")
	}
	if input.MaxTokens != 0 {
		output.Limitations = append(output.Limitations, "max_tokens_not_configurable_via_acp")
	}
	if input.Temperature != 0 {
		output.Limitations = append(output.Limitations, "temperature_not_configurable_via_acp")
	}
	if result.Usage.Known {
		output.Usage = &Usage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CacheReadTokens: result.Usage.CachedReadTokens, CacheCreationTokens: result.Usage.CachedWriteTokens}
	}
	if result.Status == native.StatusCompleted && (result.Output.Status == OutputAvailable || result.Output.Status == OutputTruncated || result.Output.Status == OutputEmpty || result.Output.Status == OutputRefused) {
		output.Text = result.Output.Text
		return output, nil
	}
	err = mapFault(result.Fault)
	if result.Fault == nil {
		err = status_error.RuntimeProtocolIncompatible
	}
	if result.Effects.PromptAccepted || result.Effects.ToolActivity || result.Effects.OutcomeUnknown || result.Status == native.StatusCompleted {
		err = status_error.WithoutRetry(err)
	}
	return output, err
}

func mapFault(err error) error {
	if err == nil {
		return status_error.RuntimeExecutionFailed
	}
	if errors.Is(err, context.Canceled) {
		return status_error.OperationCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	var business statuserror.CommonError
	if errors.As(err, &business) {
		return business
	}
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		metadata, _ := requestError.Data.(map[string]any)
		switch metadata[constants.RuntimeACPMetaKeyErrorKind] {
		case constants.RuntimeACPProviderErrorKindAuthenticationFailed, constants.RuntimeACPProviderErrorKindOAuthOrgNotAllowed:
			return status_error.RuntimeAuthRequired
		case constants.RuntimeACPProviderErrorKindTimeout:
			return status_error.OperationTimeout
		}
		switch requestError.Code {
		case -32800:
			return status_error.OperationCanceled
		case -32000:
			return status_error.RuntimeAuthRequired
		case -32601, -32602:
			return status_error.RuntimeProtocolIncompatible
		}
	}
	var fault *native.Fault
	if errors.As(err, &fault) {
		logx.Errorf("model runtime failure: code=%s stage=%s error=%v", fault.Code, fault.Stage, fault)
		switch fault.Code {
		case native.FaultAuthenticationRequired:
			return status_error.RuntimeAuthRequired
		case native.FaultConfigurationInvalid:
			return status_error.RuntimeConfigurationInvalid
		case native.FaultInvalidSpec:
			return status_error.BadRequest
		case native.FaultCancelled:
			return status_error.OperationCanceled
		case native.FaultInactivityTimeout:
			return status_error.RuntimeInactivityTimeout
		case native.FaultUnsupported, native.FaultInteractionRequired:
			return status_error.RuntimePolicyUnsupported
		case native.FaultProtocol:
			return status_error.RuntimeProtocolIncompatible
		case native.FaultClosed, native.FaultCapacity, native.FaultNotStarted, native.FaultPreparation:
			return status_error.RuntimeUnavailable
		}
	}
	return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": err.Error()})
}

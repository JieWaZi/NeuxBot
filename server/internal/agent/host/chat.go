package runtimehost

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// ChatConfig 注入聊天与编排共用的启动事实、目录资源及独立原生分叉能力。
type ChatConfig struct {
	// PrepareWorkspace 只物化受管原生指令与技能，不提交输入或创建进程。
	PrepareWorkspace func(runtimecontract.SessionPlan) error
	// Launch 从已有探测与账号快照生成启动事实。
	Launch func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error)
	// Runtimes 返回当前已注册的智能体目录声明。
	Runtimes func() []runtimeconfig.RuntimeSummary
	// Fork 是独立原生分叉端口。
	Fork func(context.Context, runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error)
	// Driver 允许测试替换公开原生协议边界。
	Driver func(Environment, acpdriver.Factory) (c.Driver, error)
}

// ChatHost 只关联业务工作区与技术 Runtime，技术会话与资源唯一归内核所有。
type ChatHost struct {
	config   ChatConfig                       // config 保存应用注入的静态端口。
	mutex    sync.Mutex                       // mutex 保护业务关联集合与宿主关闭接纳。
	runtimes map[string]*agentruntime.Runtime // runtimes 只保存工作区与 Runtime 的业务关联。
	closing  bool                             // closing 表示宿主不再接纳新的业务关联。
}

// NewChat 创建不启动进程与后台线程的共享聊天执行宿主。
func NewChat(config ChatConfig) *ChatHost {
	return &ChatHost{config: config, runtimes: make(map[string]*agentruntime.Runtime)}
}

// Runtimes 返回宿主已经注册的目录声明。
func (h *ChatHost) Runtimes() []runtimeconfig.RuntimeSummary {
	if h.config.Runtimes == nil {
		return nil
	}
	return h.config.Runtimes()
}

// ForkSession 调用独立原生分叉端口，不提交模型输入。
func (h *ChatHost) ForkSession(ctx context.Context, request runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error) {
	if h.config.Fork == nil {
		return runtimecontract.ForkSessionResult{}, status_error.RuntimePolicyUnsupported
	}
	return h.config.Fork(ctx, request)
}

// PrepareSession 只占用会话并检查静态兼容性，资源由业务层显式取得后调用 Bind。
func (h *ChatHost) PrepareSession(ctx context.Context, plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective) (runtimecontract.PreparedChatRun, error) {
	if ctx == nil || h.config.Launch == nil {
		return nil, status_error.BadRequest
	}
	technical, err := technicalSessionPlan(plan)
	if err != nil {
		return nil, err
	}
	engine, err := h.workspaceRuntime(ctx, plan.Workspace.Path)
	if err != nil {
		return nil, err
	}
	request := c.PrepareRequest{Plan: technical, Session: c.SessionDirective{Mode: c.SessionMode(session.Mode), NativeHandle: session.ID}}
	if plan.ReadOnlyHandoff {
		request.Mode = c.PreparationReadOnlyHandoff
	}
	prepared, err := engine.Prepare(ctx, request)
	if err != nil {
		return nil, mapChatPreparationFault(err)
	}
	return &preparedChat{host: h, plan: plan, session: session, prepared: prepared}, nil
}

// preparedChat 引用内核唯一占位，不复制实际资源和技术会话所有权。
type preparedChat struct {
	host     *ChatHost                        // host 提供原生装配和业务资源回收端口。
	plan     runtimecontract.SessionPlan      // plan 是接纳时冻结的静态业务声明。
	session  runtimecontract.SessionDirective // session 保留调用方的原生恢复身份。
	prepared c.PreparedRun                    // prepared 是内核唯一活动回合。
}

// Context 把内核取消边界交给顺序资源准备。
func (p *preparedChat) Context() context.Context { return p.prepared.Context() }

// NeedsResources 直接读取内核冷准备状态，不建立宿主镜像。
func (p *preparedChat) NeedsResources() bool { return p.prepared.NeedsResources() }

// Close 放弃本轮占位，并在产品边界转换内核清理故障。
func (p *preparedChat) Close(ctx context.Context) error {
	if err := p.prepared.Close(ctx); err != nil {
		return mapFault(err)
	}
	return nil
}

// Session 按内核实际动作返回新建或原生恢复身份。
func (p *preparedChat) Session() runtimecontract.SessionDirective {
	session := p.session
	if p.prepared.Session().Mode == c.SessionNew {
		session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}
	}
	return session
}

// Resources 读取内核唯一持有的宿主具名快照，不要求业务层解释私有类型。
func (p *preparedChat) Resources() runtimecontract.SessionResources {
	if resources, ok := p.prepared.Resources().(*nativeResources); ok && resources != nil {
		return resources.snapshot
	}
	return runtimecontract.SessionResources{}
}

// Bind 在首次冷绑定中统一装配原生资源，再交给内核接管释放责任。
func (p *preparedChat) Bind(ctx context.Context, resources runtimecontract.SessionResources) error {
	if !p.prepared.NeedsResources() {
		return mapChatPreparationFault(p.prepared.Bind(ctx, c.SessionResources{}))
	}
	actual, err := prepareNativeResources(ctx, p.plan, resources, nativePreparationConfig{launch: p.host.config.Launch, driver: p.host.config.Driver, prepareWorkspace: p.host.config.PrepareWorkspace})
	bindErr := p.prepared.Bind(ctx, actual)
	if err != nil {
		return mapFault(err)
	}
	if bindErr != nil {
		return mapChatPreparationFault(bindErr)
	}
	return nil
}

// Run 只转换本轮输入与观察端口，再提交内核已经占用的调用。
func (p *preparedChat) Run(ctx context.Context, input runtimecontract.TurnInput, sink runtimecontract.EventSink) runtimecontract.Result {
	if ctx == nil || sink == nil {
		return chatFailed(p.plan, p.Session(), status_error.BadRequest)
	}
	resources := p.Resources()
	bridge := &chatObserver{sink: sink}
	result := p.prepared.Run(ctx, c.TurnInput{Input: technicalTurnInput(p.plan.Workspace.Path, resources, input), Seed: chatSeed(input.History), Interaction: c.InteractionInteractive, Output: c.OutputBestEffort, Options: c.RunOptions{Observer: bridge, UserInputState: input.UserInputState, InteractionHandler: chatInteraction{approval: input.ApprovalHandler, input: input.ElicitationHandler, readOnly: p.plan.Policy.Execution.WorkspaceReadOnly}}})
	for _, fault := range []*c.Fault{result.Fault, result.DeliveryFault, result.CleanupFault} {
		if fault != nil {
			logx.Errorf("chat runtime failed: runtime_id=%s invocation_id=%s code=%s stage=%s detail=%s", p.plan.Target.RuntimeID, result.Invocation.ID, fault.Code, fault.Stage, fault.Message)
		}
	}
	if result.Usage.Known || result.Usage.ContextKnown {
		delivery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := bridge.emit(delivery, runtimecontract.EventTypeUsage, chatUsage(result.Usage))
		cancel()
		if err != nil && result.DeliveryFault == nil {
			result.DeliveryFault = &c.Fault{Code: c.FaultDelivery, Stage: "usage", Message: "final usage delivery failed", Cause: err}
			_ = p.host.CloseWorkspace(context.WithoutCancel(ctx), p.plan.Workspace.Path)
		}
	}
	actualPlan := p.plan
	actualPlan.Target = runtimecontract.DeclareTarget(resources.Target)
	return chatResult(actualPlan, p.Session(), result, bridge)
}
func (h *ChatHost) workspaceRuntime(ctx context.Context, workspace string) (*agentruntime.Runtime, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.closing {
		return nil, status_error.RuntimeUnavailable
	}
	if engine := h.runtimes[workspace]; engine != nil {
		return engine, nil
	}
	engine, err := agentruntime.New(agentruntime.Config{Logger: catalog.AdapterLogger(ctx).With("workspace", workspace)})
	if err != nil {
		return nil, mapFault(err)
	}
	if err = engine.Start(ctx); err != nil {
		return nil, mapFault(err)
	}
	h.runtimes[workspace] = engine
	return engine, nil
}

// CloseWorkspace 在重写共享认证或工作区文件前释放该业务关联下的全部技术资源。
func (h *ChatHost) CloseWorkspace(ctx context.Context, workspace string) error {
	h.mutex.Lock()
	engine := h.runtimes[workspace]
	h.mutex.Unlock()
	if engine == nil {
		return nil
	}
	if err := engine.Close(ctx); err != nil {
		logx.Errorf("close chat owner failed: error=%v", err)
		return status_error.RuntimeShutdownFailed
	}
	h.mutex.Lock()
	if h.runtimes[workspace] == engine {
		delete(h.runtimes, workspace)
	}
	h.mutex.Unlock()
	return nil
}

// CloseIdleAccount 在账号配置变更前回收其闲置消费者。
func (h *ChatHost) CloseIdleAccount(ctx context.Context, accountID string) error {
	return h.closeIdleResources(ctx, "account:"+accountID, status_error.AIAccountBusy)
}

// CloseIdlePlugin 在插件授权或最后卸载前回收其闲置消费者。
func (h *ChatHost) CloseIdlePlugin(ctx context.Context, pluginID string) error {
	return h.closeIdleResources(ctx, "plugin:"+pluginID, status_error.PluginBusy)
}
func (h *ChatHost) closeIdleResources(ctx context.Context, tag string, busy error) error {
	h.mutex.Lock()
	engines := make([]*agentruntime.Runtime, 0, len(h.runtimes))
	for _, engine := range h.runtimes {
		engines = append(engines, engine)
	}
	h.mutex.Unlock()
	for _, engine := range engines {
		if err := engine.CloseIdle(ctx, tag); err != nil {
			var technical *c.Fault
			if errors.As(err, &technical) && technical.Code == c.FaultSessionBusy {
				return busy
			}
			logx.Errorf("close idle chat resources failed: error=%v", err)
			return status_error.RuntimeShutdownFailed
		}
	}
	return nil
}

// Close 在业务入口停止接纳后关闭剩余工作区关联；失败保留同一Runtime供重试。
func (h *ChatHost) Close(ctx context.Context) error {
	h.mutex.Lock()
	h.closing = true
	workspaces := make([]string, 0, len(h.runtimes))
	for workspace := range h.runtimes {
		workspaces = append(workspaces, workspace)
	}
	h.mutex.Unlock()
	for _, workspace := range workspaces {
		if err := h.CloseWorkspace(ctx, workspace); err != nil {
			return err
		}
	}
	return nil
}

func mapChatPreparationFault(err error) error {
	var technical *c.Fault
	if errors.As(err, &technical) {
		if technical.Code == c.FaultSessionBusy {
			return status_error.AIAccountBusy
		}
		if technical.Cause != nil {
			return technical.Cause
		}
	}
	return mapFault(err)
}
func chatSeed(history []runtimecontract.HistoryMessage) []c.SeedMessage {
	seed := make([]c.SeedMessage, 0, len(history))
	for _, message := range history {
		references := make([]c.CodeReference, 0, len(message.CodeReferences))
		for _, reference := range message.CodeReferences {
			references = append(references, c.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
		}
		seed = append(seed, c.SeedMessage{Role: message.Role, Text: message.Content, CodeReferences: references})
	}
	return seed
}
func chatFailed(plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective, err error) runtimecontract.Result {
	code := runtimediagnostic.ClassifyFailure(sessionBase(plan), err)
	return runtimecontract.Result{ExitCode: -1, Session: runtimecontract.SessionResult{Mode: session.Mode, InputID: session.ID, State: runtimecontract.SessionStateUnknown}, Failure: &runtimecontract.Failure{Code: code, Stage: constants.RuntimeRunFailureStagePrepare, Message: err.Error(), DisplayMessage: runtimediagnostic.FailureDisplayMessage(err), Transience: runtimediagnostic.FailureTransiencePermanent}}
}

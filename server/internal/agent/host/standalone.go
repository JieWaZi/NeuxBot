// Package runtimehost 将产品选择、账号租约与稳定错误接入独立执行内核。
package runtimehost

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	agentruntime "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Message 是一次完整模型请求的显式消息。
type Message struct {
	// Role 是 system、user 或 assistant。
	Role string
	// Content 保留调用方原文。
	Content string
}

// TextRequest 只包含冻结的模型选择和完整消息，不接受聊天身份。
type TextRequest struct {
	// RuntimeID 是内置智能体或用户 Profile 标识。
	RuntimeID string
	// AccountID 是冻结的账号选择。
	AccountID string
	// Model 是本次实际模型选择。
	Model string
	// Reasoning 是明确推理强度。
	Reasoning string
	// Messages 是本次完整消息序列。
	Messages []Message
	// Thinking 表达本次推理开关期望。
	Thinking bool
	// MaxTokens 是协议可能不支持的输出上限。
	MaxTokens int
	// Temperature 是协议可能不支持的采样温度。
	Temperature float64
}

// TextResult 是知识核心所需的正文、用量和实际配置诊断。
type TextResult struct {
	// Text 只来自可信最终回答。
	Text string
	// StopReason 保留原生结束原因。
	StopReason string
	// Usage 在原生协议未提供回合用量时为空。
	Usage *Usage
	// ActualReasoning 是原生回执确认的推理强度。
	ActualReasoning string
	// ThinkingDisabled 表示原生确实确认关闭推理。
	ThinkingDisabled bool
	// ReasoningFallback 说明实际低档回退。
	ReasoningFallback string
	// Limitations 保存无法表达的控制和消息序列化边界。
	Limitations []string
}

// Usage 保存可选的原生回合用量；输入和缓存分量只在知识边界合并一次。
type Usage struct {
	// InputTokens 是不含缓存的输入用量。
	InputTokens int64
	// OutputTokens 是输出用量。
	OutputTokens int64
	// CacheReadTokens 是缓存读取用量。
	CacheReadTokens int64
	// CacheCreationTokens 是缓存写入用量。
	CacheCreationTokens int64
}

// Environment 是接纳前读取的启动事实；BaseRuntimeID 确定唯一 CLI 协议实现。
type Environment struct {
	// BaseRuntimeID 是经过注册验证的内置智能体身份。
	BaseRuntimeID string
	// Target 保留实际二进制、参数和配置目录。
	Target runtimeconfig.RuntimeTarget
	// Detection 是当前可运行探测快照。
	Detection runtimeconfig.DetectionResult
}

// Config 注入产品权威端口，构造时不启动进程。
type Config struct {
	// Resolve 在执行前解析明确的环境路由。
	Resolve func(context.Context, TextRequest) (Environment, error)
	// Prepare 取得共享账号租约，返回的释放端口必须支持关闭重试。
	Prepare func(context.Context, TextRequest, Environment, string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error)
	// Launch 复用 Runtime Registry、Profile、账号隔离与工具路径规则生成启动事实。
	Launch func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error)
	// Driver 允许行为测试替换原生协议边界。
	Driver func(Environment, acpdriver.Factory) (c.Driver, error)
}

// Host 保留清理失败的执行内核，直到同一所有者成功释放资源。
type Host struct {
	// config 是冻结的宿主端口。
	config Config
	// mutex 保护关闭接纳和待清理所有者。
	mutex sync.Mutex
	// owners 保存仍需 Close 的内核，禁止重放调用。
	owners map[*agentruntime.Runtime]struct{}
	// closing 禁止关闭后接纳新请求。
	closing bool
	// active 等待已经接纳的 Generate 退出。
	active sync.WaitGroup
	// activeCount 与保留所有者共同限制宿主总容量，避免失败清理无界增长。
	activeCount int
	// root 统一撤销已经接纳的模型调用和清理工作。
	root context.Context
	// cancel 停止清理重试协程。
	cancel context.CancelFunc
	// started 防止重复启动清理协程。
	started bool
	// workers 等待清理协程结束。
	workers sync.WaitGroup
}

// New 构造没有后台线程和原生进程的结果宿主。
func New(config Config) *Host {
	root, cancel := context.WithCancel(context.Background())
	return &Host{config: config, owners: make(map[*agentruntime.Runtime]struct{}), root: root, cancel: cancel}
}

// Start 绑定宿主生命周期，仅重试已经结束执行的资源所有者。
func (h *Host) Start(ctx context.Context) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.started || h.closing {
		return
	}
	h.started = true
	root := h.root
	stopParent := context.AfterFunc(ctx, h.cancel)
	h.workers.Add(1)
	go func() {
		defer h.workers.Done()
		defer stopParent()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-root.Done():
				return
			case <-ticker.C:
				h.mutex.Lock()
				owners := make([]*agentruntime.Runtime, 0, len(h.owners))
				for owner := range h.owners {
					owners = append(owners, owner)
				}
				h.mutex.Unlock()
				for _, owner := range owners {
					h.closeOwner(owner)
				}
			}
		}
	}()
}

// GenerateText 按 BaseRuntimeID 选择一次实现；任何已执行故障均不会切换或重放。
func (h *Host) GenerateText(ctx context.Context, input TextRequest) (TextResult, error) {
	return h.invoke(ctx, input, c.Contributions{}, nil)
}

// invoke 为文本生成和显式工具任务共用接纳、账号、原生执行与资源所有权。
func (h *Host) invoke(ctx context.Context, input TextRequest, contributions c.Contributions, resources TaskResources) (TextResult, error) {
	if resources != nil {
		defer resources.Revoke()
	}
	if ctx == nil {
		return TextResult{}, status_error.BadRequest
	}
	if h == nil || h.config.Resolve == nil || h.config.Prepare == nil || h.config.Launch == nil {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	h.mutex.Lock()
	if h.closing || h.activeCount+len(h.owners) >= 64 {
		h.mutex.Unlock()
		return TextResult{}, status_error.RuntimeUnavailable
	}
	h.active.Add(1)
	h.activeCount++
	h.mutex.Unlock()
	defer func() { h.mutex.Lock(); h.activeCount--; h.mutex.Unlock(); h.active.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(h.root, cancel)
	defer func() { stop(); cancel() }()
	if resources != nil {
		stopRevocation := context.AfterFunc(ctx, resources.Revoke)
		defer func() { stopRevocation(); resources.Revoke() }()
	}
	environment, err := h.config.Resolve(ctx, input)
	if err != nil {
		return TextResult{}, err
	}
	_, exists := native.Lookup(environment.BaseRuntimeID)
	if !exists {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	workspace, err := os.MkdirTemp("", "neux-runtime-model-")
	if err != nil {
		logx.Errorf("create model workspace failed: error=%v", err)
		return TextResult{}, status_error.RuntimeUnavailable
	}
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		_ = os.RemoveAll(workspace)
		logx.Errorf("resolve model workspace failed: error=%v", err)
		return TextResult{}, status_error.RuntimeUnavailable
	}
	return h.execute(ctx, input, environment, resolved, contributions, resources)
}

func (h *Host) execute(ctx context.Context, input TextRequest, env Environment, workspace string, contributions c.Contributions, resources TaskResources) (TextResult, error) {
	messages := make([]native.Message, 0, len(input.Messages))
	for _, message := range input.Messages {
		messages = append(messages, native.Message{Role: message.Role, Content: message.Content})
	}
	compiled, err := native.CompileTaskMessages(env.BaseRuntimeID, messages)
	prompt, instructions, limitations := compiled.Text, compiled.Instructions, compiled.Limitations
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	plan := runtimecontract.SessionPlan{Target: runtimecontract.DeclareTarget(env.Target), Detection: env.Detection, Workspace: runtimecontract.Workspace{Path: workspace}, Policy: runtimecontract.RunPolicy{Model: input.Model, Reasoning: input.Reasoning, Thinking: &input.Thinking, Execution: runtimecontract.ExecutionPolicy{PermissionLevel: runtimeconfig.PermissionLevelFullAccess}}}
	if plan.Target.RuntimeID == "" {
		plan.Target.RuntimeID = input.RuntimeID
	}
	plan.Target.AccountID = input.AccountID
	if plan.Detection.Runtime.BaseRuntimeID == "" {
		plan.Detection.Runtime.BaseRuntimeID = env.BaseRuntimeID
	}
	for index, instruction := range instructions {
		plan.Instructions = append(plan.Instructions, runtimecontract.Instruction{Name: fmt.Sprintf("system-%d", index+1), Content: instruction})
	}
	technical, err := technicalSessionPlan(plan)
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, err
	}
	technical.Contributions.Instructions = append(technical.Contributions.Instructions, contributions.Instructions...)
	technical.Contributions.Skills = append(technical.Contributions.Skills, contributions.Skills...)
	technical.Contributions.MCP = append(technical.Contributions.MCP, contributions.MCP...)
	engine, err := agentruntime.New(agentruntime.Config{Logger: catalog.AdapterLogger(ctx).With("workspace", workspace)})
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	if err = engine.Start(ctx); err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	defer func() {
		// 在调用退出后交给清理重试，活动调用继续由宿主根 Context 负责取消。
		h.mutex.Lock()
		h.owners[engine] = struct{}{}
		h.mutex.Unlock()
		h.closeOwner(engine)
	}()
	prepared, err := engine.Prepare(ctx, c.PrepareRequest{Plan: technical, Session: c.SessionDirective{Mode: c.SessionNew}, Lifetime: c.SessionTemporary})
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	defer func() {
		if err := prepared.Close(context.WithoutCancel(ctx)); err != nil {
			logx.Errorf("close unused task preparation failed: error=%v", err)
		}
	}()
	target, release, acquireErr := h.config.Prepare(prepared.Context(), input, env, workspace)
	if target.RuntimeID == "" {
		target.RuntimeID = input.RuntimeID
	}
	if target.AccountID == "" {
		target.AccountID = input.AccountID
	}
	snapshot := runtimecontract.SessionResources{Target: target, Close: release}
	var taskLease c.ResourceLease
	if acquireErr == nil && resources != nil {
		taskLease, acquireErr = resources.Acquire(prepared.Context(), c.ResourceRequest{Invocation: prepared.Invocation(), Workspace: technical.Workspace, Contributions: technical.Contributions})
	}
	cleanup := func(context.Context) error {
		if err := os.RemoveAll(workspace); err != nil {
			logx.Errorf("remove model workspace failed: error=%v", err)
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}
	actual, prepareErr := prepareNativeResources(prepared.Context(), plan, snapshot, nativePreparationConfig{launch: h.config.Launch, driver: h.config.Driver, taskResources: resources, taskLease: taskLease, releaseWorkspace: cleanup, contributions: contributions, unattended: true, acquisitionError: acquireErr, nativeInstructions: instructions})
	bindErr := prepared.Bind(prepared.Context(), actual)
	if acquireErr != nil {
		return TextResult{}, mapFault(acquireErr)
	}
	if prepareErr != nil {
		return TextResult{}, mapFault(prepareErr)
	}
	if bindErr != nil {
		return TextResult{}, mapFault(bindErr)
	}
	result := prepared.Run(ctx, c.TurnInput{Input: c.Input{Text: prompt}, Interaction: c.InteractionUnattended, Output: c.OutputStrict})
	if result.CleanupFault != nil {
		logx.Errorf("model cleanup pending: invocation_id=%s session_id=%s error=%v", result.Invocation.ID, result.Invocation.SessionID, result.CleanupFault)
	}
	output := TextResult{StopReason: string(result.StopReason), ActualReasoning: result.Configuration.ActualReasoning, ThinkingDisabled: result.Configuration.ThinkingDisabled, ReasoningFallback: result.Configuration.ReasoningFallback, Limitations: limitations}
	if env.BaseRuntimeID != runtimeconfig.RuntimeIDCodex && !target.Isolated && resources == nil {
		output.Limitations = append(output.Limitations, "system_account_native_config_retained: global instructions and configured MCP may load; host does not mount external resources")
	}
	if env.BaseRuntimeID != runtimeconfig.RuntimeIDClaude && env.BaseRuntimeID != runtimeconfig.RuntimeIDWorkBuddy {
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
	if result.Status == c.StatusCompleted && (result.Output.Status == c.OutputAvailable || result.Output.Status == c.OutputTruncated || result.Output.Status == c.OutputEmpty || result.Output.Status == c.OutputRefused) {
		output.Text = result.Output.Text
		return output, nil
	}
	err = mapFault(result.Fault)
	if result.Fault == nil {
		err = status_error.RuntimeProtocolIncompatible
	}
	if result.Effects.PromptAccepted || result.Effects.ToolActivity || result.Effects.OutcomeUnknown || result.Status == c.StatusCompleted {
		err = status_error.WithoutRetry(err)
	}
	return output, err
}

func (h *Host) closeOwner(owner *agentruntime.Runtime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := owner.Close(ctx); err != nil {
		logx.Errorf("close model runtime pending: error=%v", err)
		return
	}
	h.mutex.Lock()
	delete(h.owners, owner)
	h.mutex.Unlock()
}

// Close 停止新调用并只重试资源关闭；可信完成结果不会再次提交。
func (h *Host) Close(ctx context.Context) error {
	h.mutex.Lock()
	h.closing = true
	if h.cancel != nil {
		h.cancel()
	}
	h.mutex.Unlock()
	done := make(chan struct{})
	go func() { h.active.Wait(); h.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return status_error.RuntimeShutdownFailed
	}
	h.mutex.Lock()
	owners := make([]*agentruntime.Runtime, 0, len(h.owners))
	for owner := range h.owners {
		owners = append(owners, owner)
	}
	h.mutex.Unlock()
	for _, owner := range owners {
		if err := owner.Close(ctx); err != nil {
			logx.Errorf("close model runtime failed: error=%v", err)
			return status_error.RuntimeShutdownFailed
		}
		h.mutex.Lock()
		delete(h.owners, owner)
		h.mutex.Unlock()
	}
	return nil
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
	var fault *c.Fault
	if errors.As(err, &fault) {
		logx.Errorf("model runtime failure: code=%s stage=%s error=%v", fault.Code, fault.Stage, fault)
		switch fault.Code {
		case c.FaultInvalidSpec:
			return status_error.BadRequest
		case c.FaultCancelled:
			return status_error.OperationCanceled
		case c.FaultInactivityTimeout:
			return status_error.RuntimeInactivityTimeout
		case c.FaultUnsupported, c.FaultInteractionRequired:
			return status_error.RuntimePolicyUnsupported
		case c.FaultProtocol:
			return status_error.RuntimeProtocolIncompatible
		case c.FaultClosed, c.FaultCapacity, c.FaultNotStarted, c.FaultPreparation:
			return status_error.RuntimeUnavailable
		}
	}
	return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": err.Error()})
}

// TaskResources 将会话资源租约与调用期授权明确分开；所有方法须支持并发撤销。
type TaskResources interface {
	c.ResourceProvider
	// LaunchEnvironment 只在原生启动前合并本次任务环境；base 是独立副本。
	LaunchEnvironment(base map[string]string) map[string]string
	// Revoke 立即撤销调用授权且可重复执行，不释放仍被原生进程使用的文件。
	Revoke()
}

// TaskRequest 为独立工具任务提供冻结输入与显式资源，不接受聊天或恢复身份。
type TaskRequest struct {
	// TextRequest 保存模型、账号和本次完整消息。
	TextRequest
	// Contributions 保存必须交付给驱动的指令和技能描述。
	Contributions c.Contributions
	// Resources 负责物化任务文件，租约由引擎持有至原生退出。
	Resources TaskResources
}

// ExecuteTask 复用独立调用内核，以全工具授权、全新会话和严格最终回答策略执行。
func (h *Host) ExecuteTask(ctx context.Context, input TaskRequest) (TextResult, error) {
	return h.invoke(ctx, input.TextRequest, input.Contributions, input.Resources)
}

// taskDriver 仅适配任务资源的启动环境和授权终点，不执行或重试任务。
type taskDriver struct {
	// Driver 是共享原生协议驱动。
	c.Driver
	// resources 保留本次任务的独立授权撤销端口。
	resources TaskResources
}

func (d *taskDriver) Open(ctx context.Context, request c.OpenRequest) (c.DriverSession, error) {
	// 任务连接在资源物化后才存在，最终环境经原生工厂交给统一启动器冻结。
	values := make(map[string]string, len(request.Credential.Environment))
	for key, value := range request.Credential.Environment {
		values[key] = value
	}
	request.Credential.Environment = d.resources.LaunchEnvironment(values)
	session, err := d.Driver.Open(ctx, request)
	if err != nil {
		d.resources.Revoke()
	}
	if session != nil {
		return &taskSession{DriverSession: session, resources: d.resources}, err
	}
	return nil, err
}

// taskSession 在 Prompt 返回时先撤销领域授权，再由引擎关闭原生会话与租约。
type taskSession struct {
	// DriverSession 是唯一原生会话所有者。
	c.DriverSession
	// resources 只撤销调用授权，不提前释放会话资源。
	resources TaskResources
}

func (s *taskSession) Prompt(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
	defer s.resources.Revoke()
	return s.DriverSession.Prompt(ctx, request, sink)
}

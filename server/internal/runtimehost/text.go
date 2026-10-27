// Package runtimehost 将产品选择、账号租约与稳定错误接入独立执行内核。
package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimeauth"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	acpcodex "github.com/JieWaZi/acp-go/pkg/codex"
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

// Environment 是接纳前读取的启动事实；BaseRuntimeID 决定唯一迁移路由。
type Environment struct {
	// BaseRuntimeID 是经过注册验证的内置智能体身份。
	BaseRuntimeID string
	// Target 保留实际二进制、参数和配置目录。
	Target runtimes.RuntimeTarget
	// Detection 是当前可运行探测快照。
	Detection runtimes.DetectionResult
}

// Config 注入产品权威端口，构造时不启动进程。
type Config struct {
	// Resolve 在执行前解析明确的环境路由。
	Resolve func(context.Context, TextRequest) (Environment, error)
	// Prepare 取得共享账号租约；关闭失败必须允许再次调用。
	Prepare func(context.Context, TextRequest, Environment, string) (runtimes.RuntimeTarget, func(context.Context) error, error)
	// Launch 复用 Runtime Registry、Profile、账号隔离与工具路径规则生成启动事实。
	Launch func(runtimes.RuntimeTarget, runtimes.DetectionResult) (runtimes.LaunchFacts, error)
	// Driver 允许行为测试替换原生协议边界。
	Driver func(Environment, acpdriver.Factory) (c.Driver, error)
}

// Host 保留清理失败的执行内核，直到同一所有者成功释放资源。
type Host struct {
	// config 是冻结的宿主端口。
	config Config
	// mutex 保护关闭接纳和待清理所有者。
	mutex sync.Mutex
	// owners 保存仍需 Close 的内核，禁止重放 Invoke。
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
	environment, err := h.config.Resolve(ctx, input)
	if err != nil {
		return TextResult{}, err
	}
	descriptor, exists := runtimeDescriptors[environment.BaseRuntimeID]
	if !exists {
		return TextResult{}, status_error.RuntimeUnavailable
	}
	workspace, err := os.MkdirTemp("", "neux-runtime-model-")
	if err != nil {
		logx.Errorf("create model workspace failed: error=%v", err)
		return TextResult{}, status_error.RuntimeUnavailable
	}
	return h.execute(ctx, input, environment, workspace, descriptor)
}

func (h *Host) execute(ctx context.Context, input TextRequest, env Environment, workspace string, descriptor runtimeDescriptor) (TextResult, error) {
	prompt, instructions, limitations, err := compileMessages(input.Messages)
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, err
	}
	if env.BaseRuntimeID != runtimes.RuntimeIDCodex && len(instructions) > 0 {
		var serialized strings.Builder
		for _, message := range input.Messages {
			fmt.Fprintf(&serialized, "<message role=%q>\n%s\n</message>\n", message.Role, message.Content)
		}
		prompt, instructions = serialized.String(), nil
		limitations = append(limitations, "native_system_role_unavailable: task messages serialized into one user text input")
	}
	credentials := &credentialProvider{host: h, input: input, environment: env, workspace: workspace, instructions: instructions}
	factory := func(factoryContext context.Context, request c.OpenRequest) (acp.Agent, error) {
		return credentials.agent(factoryContext, request)
	}
	var driver c.Driver
	if h.config.Driver != nil {
		driver, err = h.config.Driver(env, factory)
	} else {
		driver, err = acpdriver.New(acpdriver.Config{ID: input.RuntimeID, Factory: factory, CompletionPolicy: descriptor.completion, ModeCandidates: descriptor.modes, SingleModeFallback: descriptor.singleModeFallback})
	}
	if err != nil {
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	engine, err := agentruntime.New(agentruntime.Config{Drivers: []c.Driver{driver}, Credentials: credentials, Grants: c.Grants{Directories: []string{workspace}, AllowAllTools: true}})
	if err != nil {
		_ = driver.Close(ctx)
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	if err = engine.Start(ctx); err != nil {
		h.closeOwner(engine)
		_ = os.RemoveAll(workspace)
		return TextResult{}, mapFault(err)
	}
	thinking := input.Thinking
	model := nativeModelID(env.BaseRuntimeID, input.AccountID, input.Model)
	result := engine.Invoke(ctx, c.ExecutionSpec{Profile: c.Profile{DriverID: input.RuntimeID, Model: model, Reasoning: input.Reasoning, Thinking: &thinking, CredentialRef: input.AccountID + ":" + input.RuntimeID}, Input: c.Input{Text: prompt}, Session: c.SessionDirective{Mode: c.SessionNew}, Workspace: c.WorkspaceScope{Root: workspace}, Policies: c.Policies{Tools: c.ToolPolicy{Mode: c.ToolsAllowAll}, NativeConfig: c.NativeConfigInherit, Interaction: c.InteractionUnattended, Output: c.OutputStrict}})
	h.mutex.Lock()
	h.owners[engine] = struct{}{}
	h.mutex.Unlock()
	if !credentials.acquired {
		_ = os.RemoveAll(workspace)
	}
	if result.CleanupFault != nil {
		logx.Errorf("model cleanup pending: invocation_id=%s session_id=%s error=%v", result.Invocation.ID, result.Invocation.SessionID, result.CleanupFault)
	}
	h.closeOwner(engine)
	output := TextResult{StopReason: string(result.StopReason), ActualReasoning: result.Configuration.ActualReasoning, ThinkingDisabled: result.Configuration.ThinkingDisabled, ReasoningFallback: result.Configuration.ReasoningFallback, Limitations: limitations}
	if env.BaseRuntimeID != runtimes.RuntimeIDCodex && !credentials.target.Isolated {
		output.Limitations = append(output.Limitations, "system_account_native_config_retained: global instructions and configured MCP may load; host does not mount external resources")
	}
	if env.BaseRuntimeID != runtimes.RuntimeIDClaude && env.BaseRuntimeID != runtimes.RuntimeIDWorkBuddy {
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

func compileMessages(messages []Message) (string, []string, []string, error) {
	if len(messages) == 0 {
		return "", nil, nil, status_error.BadRequest
	}
	remaining := make([]Message, 0, len(messages))
	instructions := []string{}
	limitations := []string{}
	for _, message := range messages {
		switch message.Role {
		case "system":
			instructions = append(instructions, message.Content)
		case "user", "assistant":
			remaining = append(remaining, message)
			if message.Role == "assistant" {
				limitations = append(limitations, "assistant_history_serialized_as_text: ACP cannot install native assistant history")
			}
		default:
			return "", nil, nil, status_error.BadRequest
		}
	}
	if len(remaining) == 1 && remaining[0].Role == "user" {
		return remaining[0].Content, instructions, limitations, nil
	}
	var prompt strings.Builder
	for _, message := range remaining {
		fmt.Fprintf(&prompt, "<message role=%q>\n%s\n</message>\n", message.Role, message.Content)
	}
	return prompt.String(), instructions, append(limitations, "native_message_roles_unavailable: ACP serializes user/assistant roles into one user text input"), nil
}

// credentialProvider 在引擎接管后准备账号，并保护原生退出、认证回收和临时目录的释放顺序。
type credentialProvider struct {
	// host 提供现有账号准备权威。
	host *Host
	// input 是已冻结的选择。
	input TextRequest
	// environment 是已冻结的原生启动事实。
	environment Environment
	// workspace 随认证租约在原生进程退出与回收成功后删除。
	workspace string
	// target 在取得账号租约后冻结。
	target runtimes.RuntimeTarget
	// launch 是 SDK 解析完成后不可变的二进制、前缀和环境事实。
	launch runtimes.LaunchFacts
	// closeLease 只在原生进程成功关闭后回收认证投射。
	closeLease func(context.Context) error
	// broker 是本次唯一 OAuth 刷新入口。
	broker *runtimeauth.CodexRefreshBroker
	// sourceAuth 保存系统账号认证权威路径。
	sourceAuth string
	// acquired 表示引擎已经接管目录与认证所有权。
	acquired bool
	// values 是本次实际环境，不进入结果或日志。
	values map[string]string
	// instructions 是本次任务的系统输入，不安装 AGENTS、技能或连接器。
	instructions []string
}

// Acquire 取得既有账号租约并冻结本次启动环境。
func (p *credentialProvider) Acquire(ctx context.Context, _ c.CredentialRequest) (c.CredentialLease, error) {
	p.acquired = true
	target, release, err := p.host.config.Prepare(ctx, p.input, p.environment, p.workspace)
	if err != nil {
		return p, err
	}
	p.target, p.closeLease = target, release
	p.launch, err = p.host.config.Launch(target, p.environment.Detection)
	if err != nil {
		return p, err
	}
	if p.launch.BaseRuntimeID != p.environment.BaseRuntimeID {
		return p, status_error.RuntimeConfigurationInvalid
	}
	p.values = make(map[string]string, len(p.launch.Environment))
	for _, pair := range p.launch.Environment {
		if key, value, ok := strings.Cut(pair, "="); ok {
			p.values[key] = value
		}
	}
	return p, nil
}

// Credential 仅在原生启动边界交付环境。
func (p *credentialProvider) Credential() c.Credential { return c.Credential{Environment: p.values} }

// Close 只在原生所有者退出后依次释放刷新、认证和临时目录。
func (p *credentialProvider) Close(ctx context.Context) error {
	if p.broker != nil {
		if err := p.broker.Close(ctx); err != nil {
			return err
		}
	}
	if p.sourceAuth != "" {
		unlock, err := runtimeauth.CodexFileCredentialGate(p.sourceAuth).Lock(ctx)
		if err != nil {
			return err
		}
		_, err = runtimeauth.ReconcileCodexFileAuth(p.sourceAuth)
		unlock()
		if err != nil {
			return err
		}
	}
	if p.closeLease != nil {
		if err := p.closeLease(ctx); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(p.workspace); err != nil {
		logx.Errorf("remove model workspace failed: error=%v", err)
		return status_error.RuntimeShutdownFailed
	}
	return nil
}

func (p *credentialProvider) agent(ctx context.Context, request c.OpenRequest) (acp.Agent, error) {
	values := request.Credential.Environment
	if p.environment.BaseRuntimeID != runtimes.RuntimeIDCodex {
		return p.nativeAgent(ctx, request, values)
	}
	refresh := p.target.RefreshCredentials
	if refresh == nil {
		var err error
		p.sourceAuth, err = runtimeauth.CodexAuthPathForEnvironment(environmentList(values))
		if err != nil {
			return nil, err
		}
		refresh, err = runtimeauth.CodexFileRefresh(p.sourceAuth, nil)
		if err != nil {
			return nil, err
		}
	}
	if refresh != nil {
		var err error
		p.broker, err = runtimeauth.NewCodexRefreshBroker(refresh)
		if err != nil {
			return nil, err
		}
		values[runtimeauth.CodexRefreshURLVariable] = p.broker.URL()
	}
	args := append([]string(nil), p.launch.PrefixArgs...)
	if len(p.instructions) > 0 {
		path := filepath.Join(p.workspace, "model-instructions.md")
		if err := os.WriteFile(path, []byte(strings.Join(p.instructions, "\n\n")), 0600); err != nil {
			logx.Errorf("write model instructions failed: error=%v", err)
			return nil, status_error.RuntimeConfigurationInvalid
		}
		args = append(args, "-c", "model_instructions_file="+strconv.Quote(path))
	}
	return acpcodex.NewAgent(ctx, acpcodex.Config{Logger: slog.Default(), CodexPath: p.launch.Executable, PrefixArgs: args, Environment: environmentList(values)})
}

func environmentList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
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
	var business *statuserror.StatusErr
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

// Package native 隔离原生 CLI 工厂、认证租约及一次 ACP 会话连接。
package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/JieWaZi/acp-go/pkg/acpserver"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultCleanupTimeout = 5 * time.Second

// Factory 仅在通过策略校验后创建公开 ACP Agent；凭据由该启动边界消费。
type Factory func(context.Context, OpenRequest) (sdk.Agent, error)

// ConnectionConfig 冻结一次 ACP 连接的创建、配置与输入规则。
type ConnectionConfig struct {
	NativeID                 string                            // NativeID 指向唯一原生技术声明，用户 Profile 身份与其分别保存。
	MaterializedInstructions []string                          // MaterializedInstructions 是已经安装到原生配置、不能重复注入 Prompt 的贡献名称。
	ID                       string                            // ID 是宿主注册的稳定驱动身份。
	Factory                  Factory                           // Factory 创建独占原生 Agent。
	CleanupTimeout           time.Duration                     // CleanupTimeout 是关闭连接与 Adapter 的独立期限。
	CompletionPolicy         CompletionPolicy                  // CompletionPolicy 决定是否允许 ACP 回合结束证明。
	ModeCandidates           []string                          // ModeCandidates 按优先级选择原生实际声明的权限模式；为空则只使用 Profile.NativeMode。
	SingleModeFallback       string                            // SingleModeFallback 只允许旧 CLI 的唯一模式，不能覆盖多模式目录。
	PromptContext            func(OpenRequest) (string, error) // PromptContext 物化只在首次 Prompt 注入的上下文；nil 使用标准指令与技能前缀。
}

// normalizeConnectionConfig 冻结连接规则，在取得任何原生资源前校验配置。
func normalizeConnectionConfig(config ConnectionConfig) (ConnectionConfig, error) {
	if strings.TrimSpace(config.ID) == "" || config.Factory == nil || config.CleanupTimeout < 0 {
		return config, fault(FaultInvalidSpec, "configure", "ACP connection requires an identity, factory and nonnegative cleanup timeout", nil)
	}
	if config.CleanupTimeout == 0 {
		config.CleanupTimeout = defaultCleanupTimeout
	}
	if config.CompletionPolicy == "" {
		config.CompletionPolicy = CompletionNativePhase
	}
	if config.CompletionPolicy != CompletionNativePhase && config.CompletionPolicy != CompletionLastAssistant {
		return config, fault(FaultInvalidSpec, "configure", "ACP completion policy is unsupported", nil)
	}
	config.MaterializedInstructions = slices.Clone(config.MaterializedInstructions)
	if config.PromptContext == nil {
		config.PromptContext = func(request OpenRequest) (string, error) {
			contributions := request.Contributions
			contributions.Instructions = slices.DeleteFunc(slices.Clone(contributions.Instructions), func(instruction Instruction) bool {
				return slices.Contains(config.MaterializedInstructions, instruction.Name)
			})
			return contributionPrefix(contributions, request.Resources)
		}
	}
	config.ModeCandidates = slices.Clone(config.ModeCandidates)
	return config, nil
}

// Open 先拒绝未支持的策略和贡献，再创建并初始化独占 ACP 连接。
// Workspace.Root 仅作为原生 cwd；inherit_native 不证明目录沙箱隔离。
// 一旦创建原生 Agent，准备失败也返回非 nil Connection；调用者必须接管其 Close 后再释放依赖。
func Open(ctx context.Context, config ConnectionConfig, r OpenRequest) (*Connection, error) {
	if ctx == nil {
		return nil, fault(FaultInvalidSpec, "open", "ACP open requires a context", nil)
	}
	var err error
	config, err = normalizeConnectionConfig(config)
	if err != nil {
		return nil, err
	}
	if err := validateOpen(r); err != nil {
		return nil, err
	}
	prefix := ""
	if r.Session.Mode == SessionNew {
		prefix, err = config.PromptContext(r)
		if err != nil {
			return nil, err
		}
		prefix = historyPrefix(prefix, r.Seed)
	}
	openCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := openCtx.Err(); err != nil {
		return nil, fault(FaultCancelled, "open", "ACP preparation cancelled", err)
	}
	agent, err := config.Factory(openCtx, r)
	if agent == nil {
		return nil, fault(FaultPreparation, "open", "ACP agent creation failed", err)
	}
	// 从取得原生使用者起保留可关闭对象，即使工厂同时返回准备错误也不丢弃依赖所有权。
	nativeID := config.NativeID
	if nativeID == "" {
		nativeID = config.ID
	}
	descriptor, _ := Lookup(nativeID)
	s := &Connection{config: config, skillPrefix: descriptor.SkillPrefix, native: agent, initialPrefix: prefix, done: make(chan struct{}), closeGate: make(chan struct{}, 1)}
	partial := func(cause error) (*Connection, error) {
		s.closed = true
		close(s.done)
		return s, cause
	}
	if err != nil {
		return partial(fault(FaultPreparation, "open", "ACP agent creation failed", err))
	}
	if err = openCtx.Err(); err != nil {
		return partial(fault(FaultCancelled, "open", "ACP preparation cancelled", err))
	}
	if err = s.connect(); err != nil {
		s.closed = true
		return s, err
	}
	abort := context.AfterFunc(openCtx, s.stop)
	defer abort()
	fail := func(cause error) (*Connection, error) { s.stop(); return s, cause }
	initialized, err := s.initialize(openCtx)
	if err != nil {
		return fail(err)
	}
	s.promptCapabilities = initialized.AgentCapabilities.PromptCapabilities
	s.nativeResumable = initialized.AgentCapabilities.SessionCapabilities.Resume != nil || initialized.AgentCapabilities.LoadSession
	s.forkMode = acpmeta.ForkMode(initialized.Meta)
	directories := nativeDirectories(r.Workspace, initialized.AgentCapabilities.SessionCapabilities)
	servers, err := capabilityServers(r.Contributions, r.Resources, initialized.AgentCapabilities.McpCapabilities)
	if err != nil {
		return fail(err)
	}
	var options []sdk.SessionConfigOption
	var modes *sdk.SessionModeState
	if r.Session.Mode == SessionResume {
		if !s.nativeResumable {
			return fail(fault(FaultUnsupported, "resume_session", "ACP native restore is not advertised", nil))
		}
		s.nativeID = sdk.SessionId(r.Session.NativeHandle)
		if initialized.AgentCapabilities.SessionCapabilities.Resume != nil {
			response, restoreErr := s.client.ResumeSession(openCtx, sdk.ResumeSessionRequest{SessionId: s.nativeID, Cwd: r.Workspace.Root, AdditionalDirectories: directories, McpServers: servers})
			options, modes, err = response.ConfigOptions, response.Modes, restoreErr
		} else {
			response, restoreErr := s.client.LoadSession(openCtx, sdk.LoadSessionRequest{SessionId: s.nativeID, Cwd: r.Workspace.Root, AdditionalDirectories: directories, McpServers: servers})
			options, modes, err = response.ConfigOptions, response.Modes, restoreErr
		}
		if err != nil {
			return fail(fault(FaultPreparation, "resume_session", "ACP native session restore failed", err))
		}
	} else {
		response, createErr := s.client.NewSession(openCtx, sdk.NewSessionRequest{Cwd: r.Workspace.Root, AdditionalDirectories: directories, McpServers: servers})
		if createErr != nil {
			return fail(fault(FaultPreparation, "new_session", "ACP session creation failed", createErr))
		}
		if response.SessionId == "" {
			return fail(fault(FaultProtocol, "new_session", "ACP session identity is missing", nil))
		}
		s.nativeID, options, modes = response.SessionId, response.ConfigOptions, response.Modes
	}
	if err = s.configure(openCtx, options, modes, r.Profile); err != nil {
		return fail(err)
	}
	if err = openCtx.Err(); err != nil {
		return fail(fault(FaultCancelled, "open", "ACP preparation cancelled", err))
	}
	return s, nil
}

// nativeDirectories 仅向声明能力的原生会话传扩目录参数；宿主授权范围及项目入口由原工作区保留。
func nativeDirectories(workspace WorkspaceScope, capabilities sdk.SessionCapabilities) []string {
	if capabilities.AdditionalDirectories == nil {
		return nil
	}
	return workspace.Directories
}

// connect 为执行与管理复用同一条公开 ACP 管道及边界所有权。
func (s *Connection) connect() error {
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	s.closeTransport = func() error {
		_ = clientOut.Close()
		_ = clientIn.Close()
		_ = serverIn.Close()
		return serverOut.Close()
	}
	serveCtx, serveCancel := context.WithCancel(context.Background())
	s.serveCancel = serveCancel
	proxy := &deliveryAgent{Agent: s.native, connection: s}
	server, err := acpserver.New(proxy, serverIn, serverOut)
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		close(s.done)
		return fault(FaultPreparation, "connect", "ACP server creation failed", err)
	}
	s.client, err = sdk.NewClientSideConnectionWithOptions(s, clientOut, clientIn, sdk.ConnectionOptions{Context: serveCtx, NotificationQueueCapacity: 256, NotificationQueueBytes: 16 << 20, CloseTransport: s.closeTransport})
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		close(s.done)
		return fault(FaultPreparation, "connect", "ACP connection creation failed", err)
	}
	go func() {
		// Serve的唯一返回错误来自deliveryAgent.Close；该边界只取消请求，原生Close结果由session单独报告。
		_ = server.Serve(serveCtx)
		_ = s.closeTransport()
		close(s.done)
	}()
	return nil
}

// initialize 为执行与管理使用相同能力声明和协议版本校验。
func (s *Connection) initialize(ctx context.Context) (sdk.InitializeResponse, error) {
	response, err := s.client.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber, ClientCapabilities: sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{Form: &sdk.ElicitationFormCapabilities{}, Url: &sdk.ElicitationUrlCapabilities{}}}})
	if err != nil {
		return response, fault(FaultPreparation, "initialize", "ACP initialization failed", err)
	}
	if response.ProtocolVersion != sdk.ProtocolVersionNumber {
		return response, fault(FaultProtocol, "initialize", "ACP protocol version is unsupported", nil)
	}
	return response, nil
}

func validateOpen(r OpenRequest) error {
	if r.Policies.NativeConfig != NativeConfigInherit || r.Policies.Tools.Mode != ToolsAllowAll || len(r.Policies.Tools.Allow) > 0 {
		return fault(FaultUnsupported, "policy", "ACP configuration isolation and tool restrictions are not verified", nil)
	}
	if r.Session.Mode != SessionNew && (r.Session.Mode != SessionResume || r.Session.NativeHandle == "" || len(r.Seed) > 0) {
		return fault(FaultUnsupported, "session", "ACP open requires a new session or native resume identity", nil)
	}
	if r.Session.Mode == SessionNew && r.Session.NativeHandle != "" {
		return fault(FaultInvalidSpec, "session", "ACP new session cannot include a native resume identity", nil)
	}
	if !filepath.IsAbs(r.Workspace.Root) {
		return fault(FaultInvalidSpec, "workspace", "ACP working directory must be absolute", nil)
	}
	return nil
}

func fault(code FaultCode, stage, message string, cause error) *Fault {
	return &Fault{Code: code, Stage: stage, Message: message, Cause: cause}
}

// deliveryAgent 只在公开 Agent Prompt 入口确认交付，不解析 JSON-RPC 或私有协议。
type deliveryAgent struct {
	sdk.Agent              // Agent 复用公开 Agent 的全部原生方法。
	connection *Connection // Connection 关联当前唯一调用与清理所有者。
}

// SetAgentConnection 原样转交公开连接注入，不新增原生协议包装。
func (a *deliveryAgent) SetAgentConnection(c *sdk.AgentSideConnection) {
	if binder, ok := a.Agent.(interface {
		SetAgentConnection(*sdk.AgentSideConnection)
	}); ok {
		binder.SetAgentConnection(c)
	}
}

// LoadSession 保留公开 AgentLoader 能力，使仅支持 load 的原生适配器可在绑定本轮前恢复。
func (a *deliveryAgent) LoadSession(ctx context.Context, request sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
	loader, ok := a.Agent.(sdk.AgentLoader)
	if !ok {
		return sdk.LoadSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionLoad)
	}
	return loader.LoadSession(ctx, request)
}

// Prompt 在公开Agent方法收到请求后交付开始事实。
func (a *deliveryAgent) Prompt(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
	a.connection.mu.Lock()
	turn := a.connection.turn
	a.connection.mu.Unlock()
	if turn == nil {
		return sdk.PromptResponse{}, sdk.NewInvalidParams(nil)
	}
	nativeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(turn.ctx, cancel)
	defer stop()
	if err := a.connection.deliver(Event{Kind: EventPromptAccepted}); err != nil {
		return sdk.PromptResponse{}, err
	}
	return a.Agent.Prompt(nativeCtx, p)
}

// Close 仅收敛 acpserver 的请求边界；原生资源始终由 Connection.Close 独占关闭。
func (a *deliveryAgent) Close(ctx context.Context) error {
	a.connection.mu.Lock()
	if a.connection.turn != nil {
		a.connection.turn.cancel()
	}
	a.connection.mu.Unlock()
	return nil
}

// UnstableForkSession 保留可选原生分叉接口，执行与管理共用公开协议转发。
func (a *deliveryAgent) UnstableForkSession(ctx context.Context, request sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) {
	forker, ok := a.Agent.(interface {
		UnstableForkSession(context.Context, sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error)
	})
	if !ok {
		return sdk.UnstableForkSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionFork)
	}
	return forker.UnstableForkSession(ctx, request)
}

// Connection 拥有一个可顺序续聊的原生连接，Prompt 返回前通过 SDK 水位和交付屏障排空本轮。
type Connection struct {
	config             ConnectionConfig          // config 是本连接冻结的原生协议规则。
	skillPrefix        string                    // skillPrefix 是本轮明确技能的原生语法。
	native             sdk.Agent                 // native 是唯一由本会话负责关闭的公开Agent。
	closeGate          chan struct{}             // closeGate 串行化可取消的原生Close尝试。
	nativeClosed       bool                      // nativeClosed 只在公开Close成功后置位，受closeGate保护。
	client             *sdk.ClientSideConnection // client 是唯一客户端协议连接。
	configuration      Configuration             // configuration 保存准备时确认的模型、权限和推理配置。
	initialPrefix      string                    // initialPrefix 是创建原生使用者前冻结的首次普通文本贡献。
	promptCapabilities sdk.PromptCapabilities    // promptCapabilities 是原生握手确认的输入内容能力。
	nativeResumable    bool                      // nativeResumable 表示握手确认可以从原生历史恢复。
	forkMode           string                    // forkMode 是原生公开握手确认的分叉精度。
	nativeID           sdk.SessionId             // nativeID 是原生会话返回的真实身份。
	serveCancel        context.CancelFunc        // serveCancel 通知 acpserver 结束并释放 Agent。
	closeTransport     func() error              // closeTransport 中断本连接拥有的全部管道。
	stopOnce           sync.Once                 // stopOnce 保证清理启动一次。
	done               chan struct{}             // done 在ACP服务边界停止后关闭，不代表原生资源已释放。
	mu                 sync.Mutex                // mu 保护唯一调用槽和连接关闭状态。
	turn               *turnState                // turn 是当前唯一事件与交互接收者。
	closed             bool                      // closed 阻止连接关闭后再次发送 Prompt。
	deliveryMu         sync.Mutex                // deliveryMu 串行交付并与本轮结束建立屏障。
}

// turnState 保存本轮交付、取消和故障，不保存累计正文。
type turnState struct {
	request            TurnRequest        // request 提供有效调用与修订身份。
	sink               EventSink          // sink 是本轮同步有界交付端口。
	ctx                context.Context    // ctx 控制交付及原生 Prompt。
	cancel             context.CancelFunc // cancel 在交付失败时停止本轮。
	err                error              // err 保存首个交付或交互错误。
	interactionSeq     uint64             // interactionSeq 为同一工具的多次审批分配独立请求身份。
	activity           chan struct{}      // activity 合并原生更新与交互状态变化，供无活动监视器重置。
	pendingInput       int                // pendingInput 统计等待中的交互，不将人工等待误判为原生静默。
	inputDone          chan struct{}      // inputDone 在当前已接纳交互全部退出时关闭，结束回合先等待该边界。
	usage              Usage              // usage 分别保存本轮最后累计消费与最后真实上下文快照。
	planStarted        bool               // planStarted 表示本轮标准计划已经投射过首次事实。
	planCompleted      bool               // planCompleted 表示本轮标准计划已投射完成或移除。
	lastMessageID      string             // lastMessageID 是排空通知后最后出现的助手身份。
	lastPhase          MessagePhase       // lastPhase 保留该消息已知阶段，避免空边界抹掉过程分类。
	completionEligible bool               // completionEligible 只在最后正文之后没有工具、思考或过程消息时成立。
}

var _ sdk.Client = (*Connection)(nil)

// Prompt 发送冻结的首次贡献及本轮文本，响应返回前依赖 SDK 水位排空通知；最终资源关闭由宿主调用 Close。
func (s *Connection) Prompt(ctx context.Context, r TurnRequest, sink EventSink) TurnResult {
	var observedUsage Usage
	failed := func(f *Fault) TurnResult {
		return TurnResult{Status: StatusFailed, StopReason: StopFailed, Fault: f, Usage: observedUsage, Configuration: s.configuration, NativeSession: NativeSessionInfo{Handle: string(s.nativeID), Resumable: s.nativeResumable, ForkMode: s.forkMode}}
	}
	if sink == nil || strings.TrimSpace(r.Input.Text) == "" && len(r.Input.Attachments) == 0 && len(r.Input.CodeReferences) == 0 {
		return failed(fault(FaultInvalidSpec, "prompt", "ACP prompt requires input and a sink", nil))
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return failed(fault(FaultClosed, "prompt", "ACP session is closed", nil))
	}
	if s.turn != nil {
		s.mu.Unlock()
		return failed(fault(FaultSessionBusy, "prompt", "ACP session already has an active turn", nil))
	}
	prefix := s.initialPrefix
	s.mu.Unlock()
	blocks, err := promptBlocks(nativeSkillInput(r.Input, s.skillPrefix), prefix, s.promptCapabilities)
	if err != nil {
		var inputFault *Fault
		if !errors.As(err, &inputFault) {
			inputFault = fault(FaultPreparation, "input", "ACP input preparation failed", err)
		}
		return failed(inputFault)
	}
	s.mu.Lock()
	if s.closed || s.turn != nil {
		s.mu.Unlock()
		return failed(fault(FaultSessionBusy, "prompt", "ACP session is closed or already active", nil))
	}
	s.initialPrefix = ""
	turnCtx, cancel := context.WithCancel(ctx)
	turn := &turnState{request: r, sink: sink, ctx: turnCtx, cancel: cancel, activity: make(chan struct{}, 1)}
	s.turn = turn
	s.mu.Unlock()
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); s.watchInactivity(turn, r.Limits.InactivityTimeout) }()
	defer func() { cancel(); <-watchDone }()
	abort := context.AfterFunc(turnCtx, s.stop)
	defer abort()
	response, err := s.client.Prompt(turnCtx, sdk.PromptRequest{SessionId: s.nativeID, Prompt: blocks})
	// 在驱动自己的收尾取消前固定已有根因，避免把普通原生错误改成 cancelled。
	s.mu.Lock()
	cancellationCause := context.Cause(turnCtx)
	firstFault := turn.err
	s.mu.Unlock()
	// 成功响应已由 SDK 通知水位确认；错误返回仍需中断连接并解除活动交付。
	if err != nil {
		cancel()
		s.stop()
	}
	if err == nil && (response.StopReason == sdk.StopReasonEndTurn || response.StopReason == sdk.StopReasonMaxTokens || response.StopReason == sdk.StopReasonMaxTurnRequests) && s.config.CompletionPolicy == CompletionLastAssistant {
		s.mu.Lock()
		id, eligible := turn.lastMessageID, turn.completionEligible && turn.lastPhase == PhaseUnknown && turn.err == nil
		s.mu.Unlock()
		if eligible {
			_ = s.deliver(Event{Kind: EventAssistantMessage, Message: &AssistantMessage{MessageID: id, Phase: PhaseUnknown, Complete: true, CompletionVerified: true}})
		}
	}
	var deliveryErr error
	for {
		s.deliveryMu.Lock()
		s.mu.Lock()
		if turn.pendingInput == 0 {
			deliveryErr, observedUsage = turn.err, turn.usage
			s.turn = nil
			s.mu.Unlock()
			s.deliveryMu.Unlock()
			break
		}
		// 人工等待不占用交付锁；本轮接收者只在交互全部退出后解除。
		inputDone := turn.inputDone
		s.mu.Unlock()
		s.deliveryMu.Unlock()
		<-inputDone
	}
	if err != nil {
		// 错误返回之后发生的交付取消属于收尾，不覆盖已经固定的原始失败。
		deliveryErr = firstFault
	} else {
		// 原生响应后仍可能等待人工端口退出，该阶段的真实取消也属于本轮。
		cancellationCause = context.Cause(turnCtx)
	}
	var responseUsageFault *Fault
	if err == nil {
		if usage := response.Usage; usage != nil {
			if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.ThoughtTokens != nil && *usage.ThoughtTokens < 0 || usage.CachedReadTokens != nil && *usage.CachedReadTokens < 0 || usage.CachedWriteTokens != nil && *usage.CachedWriteTokens < 0 {
				responseUsageFault = fault(FaultProtocol, "usage", "ACP prompt usage contains negative tokens", nil)
			}
		}
		if reported, ok := acpmeta.ReadTurnUsage(response.Meta); ok && responseUsageFault == nil {
			observedUsage = mergeUsage(observedUsage, usageFromReported(reported))
		} else if usage := response.Usage; usage != nil && responseUsageFault == nil {
			final := Usage{Known: true, InputTokens: int64(usage.InputTokens), OutputTokens: int64(usage.OutputTokens)}
			if usage.ThoughtTokens != nil {
				final.ThoughtTokens = int64(*usage.ThoughtTokens)
			}
			if usage.CachedReadTokens != nil {
				final.CachedReadTokens = int64(*usage.CachedReadTokens)
			}
			if usage.CachedWriteTokens != nil {
				final.CachedWriteTokens = int64(*usage.CachedWriteTokens)
			}
			observedUsage = mergeUsage(observedUsage, final)
		}
	}

	if deliveryErr != nil {
		var f *Fault
		if !errors.As(deliveryErr, &f) {
			f = fault(FaultDelivery, "delivery", "ACP event delivery failed", deliveryErr)
		}
		return failed(f)
	}
	if cancellationCause != nil {
		return TurnResult{Status: StatusCancelled, StopReason: StopCancelled, Usage: observedUsage, Fault: fault(FaultCancelled, "prompt", "ACP prompt cancelled", cancellationCause), Configuration: s.configuration, NativeSession: NativeSessionInfo{Handle: string(s.nativeID), Resumable: s.nativeResumable, ForkMode: s.forkMode}}
	}
	if err != nil {
		return failed(fault(FaultDriver, "prompt", "ACP prompt failed", err))
	}
	result := TurnResult{Status: StatusCompleted, NativeSession: NativeSessionInfo{Handle: string(s.nativeID), Resumable: s.nativeResumable, ForkMode: s.forkMode, ForkPosition: acpmeta.ForkPosition(response.Meta)}, Usage: observedUsage, Configuration: s.configuration}
	switch response.StopReason {
	case sdk.StopReasonEndTurn:
		result.StopReason = StopEndTurn
	case sdk.StopReasonCancelled:
		result.Status = StatusCancelled
		result.StopReason = StopCancelled
	case sdk.StopReasonRefusal:
		result.StopReason = StopRefused
	case sdk.StopReasonMaxTokens, sdk.StopReasonMaxTurnRequests:
		result.StopReason = StopMaxTokens
	default:
		return failed(fault(FaultProtocol, "prompt", "ACP stop reason is unsupported", nil))
	}

	if responseUsageFault != nil {
		return failed(responseUsageFault)
	}
	return result
}

// usageFromReported 将同次规范快照投影到兼容数字，未知分量仍由 Reported 中的 nil 表达。
func usageFromReported(reported acpmeta.TurnUsage) Usage {
	usage := Usage{Known: true, Reported: &reported}
	if reported.InputTokens != nil {
		usage.InputTokens = *reported.InputTokens
	}
	if reported.OutputTokens != nil {
		usage.OutputTokens = *reported.OutputTokens
	}
	if reported.ReasoningTokens != nil {
		usage.ThoughtTokens = *reported.ReasoningTokens
	}
	if reported.CacheReadTokens != nil {
		usage.CachedReadTokens = *reported.CacheReadTokens
	}
	if reported.CacheWriteTokens != nil {
		usage.CachedWriteTokens = *reported.CacheWriteTokens
	}
	return usage
}

// mergeUsage 分别替换消费与上下文，不把累计快照相加，也不让单分量更新清除另一分量。
func mergeUsage(previous, current Usage) Usage {
	if current.Known {
		contextKnown, contextTokens, contextWindow := previous.ContextKnown, previous.ContextTokens, previous.ContextWindowTokens
		previous = current
		previous.ContextKnown, previous.ContextTokens, previous.ContextWindowTokens = contextKnown, contextTokens, contextWindow
	}
	if current.ContextKnown {
		previous.ContextKnown, previous.ContextTokens, previous.ContextWindowTokens = true, current.ContextTokens, current.ContextWindowTokens
	}
	return previous
}

func (s *Connection) stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.turn != nil {
			s.turn.cancel()
		}
		s.mu.Unlock()
		if s.client != nil {
			s.client.Close()
		}
		if s.serveCancel != nil {
			s.serveCancel()
		}
	})
}

// Close 先结束ACP边界，再串行调用公开原生Close；失败保留所有者，成功不重复释放。
func (s *Connection) Close(ctx context.Context) error {
	timeout := s.config.CleanupTimeout
	if timeout == 0 {
		timeout = defaultCleanupTimeout
	}
	closeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = closeCtx
	s.stop()
	select {
	case <-s.done:
	case <-ctx.Done():
		return fault(FaultCleanup, "close", "ACP boundary cleanup deadline exceeded", ctx.Err())
	}
	select {
	case s.closeGate <- struct{}{}:
	case <-ctx.Done():
		return fault(FaultCleanup, "close", "ACP native cleanup wait cancelled", ctx.Err())
	}
	defer func() { <-s.closeGate }()
	if s.nativeClosed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fault(FaultCleanup, "close", "ACP native cleanup cancelled", err)
	}
	if closer, ok := s.native.(interface{ Close(context.Context) error }); ok {
		if err := closer.Close(ctx); err != nil {
			return fault(FaultCleanup, "close", "ACP native cleanup failed", err)
		}
	}
	s.nativeClosed = true
	return nil
}

func (s *Connection) deliver(event Event) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if turn == nil {
		return nil
	}
	if err := turn.ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if message := event.Message; message != nil {
		if message.MessageID != turn.lastMessageID {
			turn.lastMessageID, turn.lastPhase, turn.completionEligible = message.MessageID, PhaseUnknown, false
		}
		if message.Phase != "" && message.Phase != PhaseUnknown {
			turn.lastPhase = message.Phase
		}
		if turn.lastPhase != PhaseUnknown || message.MessageID == "" {
			turn.completionEligible = false
		} else if message.Delta != "" {
			turn.completionEligible = true
		}
	} else if event.Kind == EventTool || event.Kind == EventThinking {
		turn.completionEligible = false
	}
	s.mu.Unlock()
	if event.Usage != nil {
		usage := event.Usage.Clone()
		event.Usage = &usage
	}
	err := turn.sink.Accept(turn.ctx, event)
	if err != nil {
		err = fault(FaultDelivery, "delivery", "ACP event delivery failed", err)
		s.failTurn(turn, err)
	}
	return err
}
func (s *Connection) failTurn(turn *turnState, err error) {
	s.mu.Lock()
	if s.turn == turn && turn.err == nil {
		turn.err = err
	}
	s.mu.Unlock()
	turn.cancel()
}

// configure 只把原生目录和每次写入回执中的当前值当作实际配置。
func (s *Connection) configure(ctx context.Context, options []sdk.SessionConfigOption, modes *sdk.SessionModeState, profile Profile) error {
	selectValue := func(category sdk.SessionConfigOptionCategory, value string) error {
		if value == "" {
			return nil
		}
		selected := categoryOption(options, category)
		if selected == nil || category != sdk.SessionConfigOptionCategoryModel && !containsValue(selected.Options, value) {
			return fault(FaultUnsupported, "configure", "ACP requested configuration is not advertised", nil)
		}
		response, err := s.client.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: s.nativeID, ConfigId: selected.Id, Value: sdk.SessionConfigValueId(value)}})
		if err != nil {
			return fault(FaultPreparation, "configure", "ACP configuration selection failed", err)
		}
		confirmed := categoryOption(response.ConfigOptions, category)
		if confirmed == nil || confirmed.Id != selected.Id || string(confirmed.CurrentValue) != value {
			return fault(FaultProtocol, "configure", "ACP configuration selection was not confirmed", nil)
		}
		options = response.ConfigOptions
		return nil
	}
	mode := profile.NativeMode
	if mode == "" && len(s.config.ModeCandidates) > 0 {
		for _, candidate := range s.config.ModeCandidates {
			if option := categoryOption(options, sdk.SessionConfigOptionCategoryMode); option != nil && containsValue(option.Options, candidate) {
				mode = candidate
				break
			}
			if modes != nil {
				for _, advertised := range modes.AvailableModes {
					if string(advertised.Id) == candidate {
						mode = candidate
						break
					}
				}
			}
			if mode != "" {
				break
			}
		}
		if fallback := s.config.SingleModeFallback; mode == "" && fallback != "" && modes != nil && len(modes.AvailableModes) == 1 && string(modes.AvailableModes[0].Id) == fallback {
			mode = fallback
		}
		if mode == "" {
			return fault(FaultUnsupported, "configure", "ACP permission mode is not advertised", nil)
		}
	}
	modeViaConfig := false
	if mode != "" {
		if categoryOption(options, sdk.SessionConfigOptionCategoryMode) != nil {
			if err := selectValue(sdk.SessionConfigOptionCategoryMode, mode); err != nil {
				return err
			}
			modeViaConfig = true
		} else {
			advertised := false
			if modes != nil {
				for _, available := range modes.AvailableModes {
					advertised = advertised || string(available.Id) == mode
				}
			}
			if !advertised {
				return fault(FaultUnsupported, "configure", "ACP permission mode is not advertised", nil)
			}
			if _, err := s.client.SetSessionMode(ctx, sdk.SetSessionModeRequest{SessionId: s.nativeID, ModeId: sdk.SessionModeId(mode)}); err != nil {
				return fault(FaultPreparation, "configure", "ACP permission mode selection failed", err)
			}
		}
	}
	if err := selectValue(sdk.SessionConfigOptionCategoryModel, profile.Model); err != nil {
		return err
	}
	reasoning := profile.Reasoning
	fallback := ""
	if reasoning == "" && profile.Thinking != nil && !*profile.Thinking {
		reasoning, fallback = minimumReasoning(categoryOption(options, sdk.SessionConfigOptionCategoryThoughtLevel))
	}
	if err := selectValue(sdk.SessionConfigOptionCategoryThoughtLevel, reasoning); err != nil {
		return err
	}
	actual := func(category sdk.SessionConfigOptionCategory) string {
		if option := categoryOption(options, category); option != nil {
			return string(option.CurrentValue)
		}
		return ""
	}
	if profile.Model != "" && actual(sdk.SessionConfigOptionCategoryModel) != profile.Model {
		return fault(FaultProtocol, "configure", "ACP later configuration reverted the requested model", nil)
	}
	if modeViaConfig && actual(sdk.SessionConfigOptionCategoryMode) != mode {
		return fault(FaultProtocol, "configure", "ACP later configuration reverted the requested permission mode", nil)
	}
	if mode == "" {
		mode = actual(sdk.SessionConfigOptionCategoryMode)
	}
	s.configuration = Configuration{
		ActualMode:        mode,
		ActualModel:       actual(sdk.SessionConfigOptionCategoryModel),
		ActualReasoning:   actual(sdk.SessionConfigOptionCategoryThoughtLevel),
		ReasoningFallback: fallback,
	}
	s.configuration.ThinkingDisabled = reasoningDisabled(s.configuration.ActualReasoning)
	return nil
}

func categoryOption(options []sdk.SessionConfigOption, category sdk.SessionConfigOptionCategory) *sdk.SessionConfigOptionSelect {
	for _, option := range options {
		if c := option.Select; c != nil && c.Category != nil && *c.Category == category {
			return c
		}
	}
	return nil
}

func reasoningDisabled(value string) bool {
	return strings.EqualFold(value, "none") || strings.EqualFold(value, "off") || strings.EqualFold(value, "disabled")
}

// minimumReasoning 使用当前模型公布的真实取值，不把低强度推理宣称为关闭。
func minimumReasoning(option *sdk.SessionConfigOptionSelect) (string, string) {
	if option == nil {
		return "", "reasoning_not_configurable"
	}
	var values []string
	if option.Options.Ungrouped != nil {
		for _, value := range *option.Options.Ungrouped {
			values = append(values, string(value.Value))
		}
	}
	if option.Options.Grouped != nil {
		for _, group := range *option.Options.Grouped {
			for _, value := range group.Options {
				values = append(values, string(value.Value))
			}
		}
	}
	for _, wanted := range []string{"none", "off", "disabled", "minimal", "min", "low", "medium", "high", "max", "xhigh"} {
		for _, value := range values {
			if strings.EqualFold(value, wanted) {
				if reasoningDisabled(value) {
					return value, ""
				}
				return value, "thinking_off_unsupported: selected " + value
			}
		}
	}
	if len(values) > 0 {
		return values[0], "thinking_off_unsupported: selected " + values[0]
	}
	return "", "thinking_off_unsupported: retained native default " + string(option.CurrentValue)
}

// markActivity 不持有交付锁，原生过程更新即使不对外投射也刷新无活动计时器。
func (s *Connection) markActivity(turn *turnState) {
	select {
	case turn.activity <- struct{}{}:
	default:
	}
}

func (s *Connection) setPendingInput(turn *turnState, delta int) {
	s.mu.Lock()
	turn.pendingInput += delta
	if turn.pendingInput == 0 {
		close(turn.inputDone)
	}
	s.mu.Unlock()
	s.markActivity(turn)
}

// watchInactivity 将原生静默、调用取消和人工交互等待分别处理。
func (s *Connection) watchInactivity(turn *turnState, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	var changes <-chan struct{}
	if turn.request.UserInputState != nil {
		changes = turn.request.UserInputState.Changed()
	}
	pending := func() bool {
		s.mu.Lock()
		nativeInput := turn.pendingInput > 0
		s.mu.Unlock()
		return nativeInput || turn.request.UserInputState != nil && turn.request.UserInputState.Pending()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	reset := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if !pending() {
			timer.Reset(timeout)
		}
	}
	reset()
	for {
		select {
		case <-turn.ctx.Done():
			return
		case <-turn.activity:
			reset()
		case _, open := <-changes:
			if !open {
				changes = nil
			}
			reset()
		case <-timer.C:
			if pending() {
				continue
			}
			s.failTurn(turn, fault(FaultInactivityTimeout, "inactivity_watchdog", "ACP prompt exceeded inactivity timeout", nil))
			return
		}
	}
}

func containsValue(options sdk.SessionConfigSelectOptions, value string) bool {
	if options.Ungrouped != nil {
		for _, option := range *options.Ungrouped {
			if string(option.Value) == value {
				return true
			}
		}
	}
	if options.Grouped != nil {
		for _, group := range *options.Grouped {
			for _, option := range group.Options {
				if string(option.Value) == value {
					return true
				}
			}
		}
	}
	return false
}

// ReadTextFile 拒绝未协商的客户端文件能力。
func (s *Connection) ReadTextFile(context.Context, sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	return sdk.ReadTextFileResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodFsReadTextFile)
}

// WriteTextFile 拒绝未协商的客户端文件能力。
func (s *Connection) WriteTextFile(context.Context, sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	return sdk.WriteTextFileResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodFsWriteTextFile)
}

// CreateTerminal 拒绝未协商的客户端终端能力。
func (s *Connection) CreateTerminal(context.Context, sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalCreate)
}

// KillTerminal 拒绝未协商的客户端终端能力。
func (s *Connection) KillTerminal(context.Context, sdk.KillTerminalRequest) (sdk.KillTerminalResponse, error) {
	return sdk.KillTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalKill)
}

// TerminalOutput 拒绝未协商的客户端终端能力。
func (s *Connection) TerminalOutput(context.Context, sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalOutput)
}

// ReleaseTerminal 拒绝未协商的客户端终端能力。
func (s *Connection) ReleaseTerminal(context.Context, sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalRelease)
}

// WaitForTerminalExit 拒绝未协商的客户端终端能力。
func (s *Connection) WaitForTerminalExit(context.Context, sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalWaitForExit)
}

const maxAttachmentBytes = 20 << 20

// promptBlocks 保留固定代码快照和文件引用，只在握手明确支持时附加图片内容。
func promptBlocks(input Input, prefix string, capabilities sdk.PromptCapabilities) ([]sdk.ContentBlock, error) {
	var text strings.Builder
	text.WriteString(prefix)
	text.WriteString(input.Text)
	if len(input.Attachments) > 0 {
		text.WriteString("\n\nAttachments:\n")
		for _, attachment := range input.Attachments {
			path := attachment.DisplayPath
			if path == "" {
				path = attachment.Path
			}
			fmt.Fprintf(&text, "- %s (%s)\n", path, attachment.Name)
		}
	}
	appendCodeReferences(&text, input.CodeReferences)
	if len(input.SelectedSkills) > 0 {
		fmt.Fprintf(&text, "\n\nUse the selected skills: %s.\n", strings.Join(input.SelectedSkills, ", "))
	}
	if len(input.SelectedMCP) > 0 {
		fmt.Fprintf(&text, "\n\nUse the selected MCP connections: %s.\n", strings.Join(input.SelectedMCP, ", "))
	}
	blocks := []sdk.ContentBlock{sdk.TextBlock(text.String())}
	if !capabilities.Image {
		return blocks, nil
	}
	for _, attachment := range input.Attachments {
		if !strings.HasPrefix(attachment.MIMEType, "image/") {
			continue
		}
		file, err := os.Open(attachment.Path)
		if err != nil {
			return nil, fault(FaultPreparation, "input", "ACP image attachment cannot be opened", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fault(FaultPreparation, "input", "ACP image attachment cannot be read", readErr)
		}
		if closeErr != nil {
			return nil, fault(FaultPreparation, "input", "ACP image attachment cannot be closed", closeErr)
		}
		if len(data) > maxAttachmentBytes {
			return nil, fault(FaultLimit, "input", "ACP image attachment exceeds byte budget", nil)
		}
		blocks = append(blocks, sdk.ImageBlock(base64.StdEncoding.EncodeToString(data), attachment.MIMEType))
	}
	return blocks, nil
}

// appendCodeReferences 保留片段发送时的文本，整文件只提交原始引用路径。
func appendCodeReferences(text *strings.Builder, references []CodeReference) {
	for _, reference := range references {
		if reference.StartLine == 0 && reference.EndLine == 0 && reference.Content == "" {
			fmt.Fprintf(text, "\n\nReferenced file: %s\n", reference.Path)
			continue
		}
		fmt.Fprintf(text, "\n\nCode reference: %s:%d-%d\n<code-reference>\n%s\n</code-reference>\n", reference.Path, reference.StartLine, reference.EndLine, reference.Content)
	}
}

// nativeSkillInput 只转换本轮技能调用语法，不改写稳定能力贡献。
func nativeSkillInput(input Input, prefix string) Input {
	if prefix == "" || len(input.SelectedSkills) == 0 {
		return input
	}
	selected := make([]string, len(input.SelectedSkills))
	for index, name := range input.SelectedSkills {
		selected[index] = prefix + name
	}
	input.SelectedSkills = selected
	return input
}

// maxContributionBytes 只限制新增静态前缀的构建，避免宿主贡献造成无界分配；不改变本轮输入或事件预算。
const maxContributionBytes = 1 << 20

// contributionPrefix 将冻结的描述及宿主租约目录转换为首次 Prompt 的普通文本。
// ACP 没有可移植的系统角色插入；技能正文与文件生命周期仍由宿主资源租约负责。
func contributionPrefix(c Contributions, resources []ResourceBinding) (string, error) {
	invalid := func() *Fault {
		return fault(FaultInvalidSpec, "contributions", "ACP static contribution prefix exceeds its byte budget", nil)
	}
	var prefix strings.Builder
	write := func(parts ...string) error {
		for _, part := range parts {
			if len(part) > maxContributionBytes-prefix.Len() {
				return invalid()
			}
			prefix.WriteString(part)
		}
		return nil
	}
	// 先检查原始字段，避免转义超大元数据时先进行无界复制。
	for _, instruction := range c.Instructions {
		if len(instruction.Name) > maxContributionBytes || len(instruction.Version) > maxContributionBytes || len(instruction.Text) > maxContributionBytes {
			return "", invalid()
		}
		if strings.TrimSpace(instruction.Name) == "" {
			return "", fault(FaultInvalidSpec, "contributions", "ACP instruction identity is missing", nil)
		}
	}
	skills := make(map[string]Skill)
	requested := make(map[string]bool)
	for _, skill := range c.Skills {
		if len(skill.Name) > maxContributionBytes || len(skill.Version) > maxContributionBytes || len(skill.ResourceID) > maxContributionBytes {
			return "", invalid()
		}
		if strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.ResourceID) == "" {
			return "", fault(FaultInvalidSpec, "contributions", "ACP skill identity is missing", nil)
		}
		if old, ok := skills[skill.Name]; ok && old != skill {
			return "", fault(FaultInvalidSpec, "contributions", "ACP skill contributions conflict", nil)
		}
		skills[skill.Name] = skill
		requested[skill.ResourceID] = true
	}
	bindings := make(map[string]string)
	for _, binding := range resources {
		if binding.MCP != nil {
			continue
		}
		if len(binding.ID) > maxContributionBytes || len(binding.Path) > maxContributionBytes {
			return "", invalid()
		}
		if binding.ID == "" || !filepath.IsAbs(binding.Path) {
			return "", fault(FaultInvalidSpec, "resources", "ACP skill binding requires an identity and an absolute directory path", nil)
		}
		if !requested[binding.ID] {
			return "", fault(FaultUnsupported, "resources", "ACP resource binding does not belong to a requested skill", nil)
		}
		if old, ok := bindings[binding.ID]; ok && old != binding.Path {
			return "", fault(FaultPreparation, "resources", "ACP skill resource bindings conflict", nil)
		}
		bindings[binding.ID] = binding.Path
	}
	instructions := make(map[string]Instruction)
	for _, instruction := range c.Instructions {
		if old, ok := instructions[instruction.Name]; ok {
			if old != instruction {
				return "", fault(FaultInvalidSpec, "contributions", "ACP instruction contributions conflict", nil)
			}
			continue
		}
		if len(instructions) == 0 {
			if err := write("Runtime instructions:\n\n"); err != nil {
				return "", err
			}
		}
		instructions[instruction.Name] = instruction
		if err := write("### ", strconv.Quote(instruction.Name), " (version ", strconv.Quote(instruction.Version), ")\n", instruction.Text, "\n\n"); err != nil {
			return "", err
		}
	}
	included := make(map[string]bool)
	for _, skill := range c.Skills {
		path := bindings[skill.ResourceID]
		if path == "" {
			if skill.Optional {
				continue
			}
			return "", fault(FaultPreparation, "resources", "ACP required skill resource is missing", nil)
		}
		if included[skill.Name] {
			continue
		}
		if len(included) == 0 {
			if err := write("Runtime skills:\nRead each skill's SKILL.md before using it. These paths refer to host-provided skill directories.\n\n"); err != nil {
				return "", err
			}
		}
		included[skill.Name] = true
		if err := write("- ", strconv.Quote(skill.Name), " (version ", strconv.Quote(skill.Version), "): ", strconv.Quote(filepath.Join(path, "SKILL.md")), "\n"); err != nil {
			return "", err
		}
	}
	if prefix.Len() == 0 {
		return "", nil
	}
	if err := write("\nTask:\n\n"); err != nil {
		return "", err
	}
	return prefix.String(), nil
}

// historyPrefix 只为新会话构造一次有界完整历史；恢复时原生历史拥有上下文。
func historyPrefix(prefix string, messages []SeedMessage) string {
	if len(messages) == 0 {
		return prefix
	}
	var text strings.Builder
	text.WriteString(prefix)
	text.WriteString("<conversation-history>\n")
	for _, message := range messages {
		text.WriteString(message.Role)
		text.WriteString(":\n")
		text.WriteString(message.Text)
		appendCodeReferences(&text, message.CodeReferences)
		text.WriteString("\n\n")
	}
	text.WriteString("</conversation-history>\n\n")
	return text.String()
}

// capabilityServers 只转换受宿主租约保护的公开 MCP 配置，远程传输必须由握手声明。
func capabilityServers(contributions Contributions, resources []ResourceBinding, capabilities sdk.McpCapabilities) ([]sdk.McpServer, error) {
	bindings := make(map[string]*MCPConnection)
	for _, binding := range resources {
		if binding.MCP != nil {
			bindings[binding.ID] = binding.MCP
		}
	}
	servers := make([]sdk.McpServer, 0, len(contributions.MCP))
	for _, resource := range contributions.MCP {
		connection := bindings[resource.ResourceID]
		if connection == nil {
			if resource.Optional {
				continue
			}
			return nil, fault(FaultPreparation, "mcp", "ACP required MCP resource is missing", nil)
		}
		if strings.TrimSpace(resource.Name) == "" {
			return nil, fault(FaultInvalidSpec, "mcp", "ACP MCP service name is missing", nil)
		}
		switch connection.Transport {
		case MCPStdio:
			if strings.TrimSpace(connection.Command) == "" {
				return nil, fault(FaultInvalidSpec, "mcp", "ACP MCP command is missing", nil)
			}
			environment := make([]sdk.EnvVariable, 0, len(connection.Environment))
			for _, name := range sortedKeys(connection.Environment) {
				environment = append(environment, sdk.EnvVariable{Name: name, Value: connection.Environment[name]})
			}
			servers = append(servers, sdk.McpServer{Stdio: &sdk.McpServerStdio{Name: resource.Name, Command: connection.Command, Args: append([]string{}, connection.Arguments...), Env: environment}})
		case MCPHTTP, MCPSSE:
			if connection.Transport == MCPHTTP && !capabilities.Http || connection.Transport == MCPSSE && !capabilities.Sse {
				return nil, fault(FaultUnsupported, "mcp", "ACP requested MCP transport is not advertised", nil)
			}
			if strings.TrimSpace(connection.URL) == "" {
				return nil, fault(FaultInvalidSpec, "mcp", "ACP MCP URL is missing", nil)
			}
			headers := make([]sdk.HttpHeader, 0, len(connection.Headers))
			for _, name := range sortedKeys(connection.Headers) {
				headers = append(headers, sdk.HttpHeader{Name: name, Value: connection.Headers[name]})
			}
			if connection.Transport == MCPHTTP {
				servers = append(servers, sdk.McpServer{Http: &sdk.McpServerHttpInline{Name: resource.Name, Url: connection.URL, Headers: headers}})
			} else {
				servers = append(servers, sdk.McpServer{Sse: &sdk.McpServerSseInline{Name: resource.Name, Url: connection.URL, Headers: headers}})
			}
		default:
			return nil, fault(FaultUnsupported, "mcp", "ACP MCP transport is unsupported", nil)
		}
	}
	return servers, nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SessionUpdate 将公开 ACP 内容转换为规范事实，消息身份与最终阶段只采纳明确证据。
func (s *Connection) SessionUpdate(ctx context.Context, n sdk.SessionNotification) error {
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if turn == nil || n.SessionId != s.nativeID {
		return nil
	}
	s.markActivity(turn)
	if chunk := n.Update.AgentMessageChunk; chunk != nil {
		message := &AssistantMessage{Phase: PhaseUnknown}
		if info, ok := acpmeta.ReadMessageInfo(chunk.Meta); ok {
			message.Complete = info.Complete
			switch info.Phase {
			case acpmeta.MessagePhaseCommentary:
				message.Phase = PhaseCommentary
			case acpmeta.MessagePhaseFinal:
				message.Phase = PhaseFinal
			}
		}
		if chunk.MessageId != nil {
			message.MessageID = *chunk.MessageId
		}
		if chunk.Content.Text != nil {
			message.Delta = chunk.Content.Text.Text
		} else if block, ok := canonicalContent(chunk.Content); ok {
			message.Content = []ContentBlock{block}
		}
		return s.deliver(Event{Kind: EventAssistantMessage, Message: message})
	}
	if usage := n.Update.UsageUpdate; usage != nil {
		current := Usage{}
		reported, hasReported := acpmeta.ReadTurnUsage(usage.Meta)
		if hasReported {
			current = usageFromReported(reported)
		}
		// 新合同以 ContextKnown 区分必填数字占位；旧上下文通知继续按标准字段解释。
		if !hasReported || reported.ContextKnown {
			if usage.Used < 0 || usage.Size < 0 {
				err := fault(FaultProtocol, "usage", "ACP context usage or window is negative", nil)
				s.failTurn(turn, err)
				return err
			}
			current.ContextKnown, current.ContextTokens, current.ContextWindowTokens = true, int64(usage.Used), int64(usage.Size)
		}
		s.mu.Lock()
		if s.turn == turn {
			turn.usage = mergeUsage(turn.usage, current)
		}
		s.mu.Unlock()
		return s.deliver(Event{Kind: EventUsage, Usage: &current})
	}

	if chunk := n.Update.AgentThoughtChunk; chunk != nil {
		if chunk.Content.Text != nil {
			return s.deliver(Event{Kind: EventThinking, Text: chunk.Content.Text.Text})
		}
		return nil
	}
	if plan := n.Update.Plan; plan != nil {
		return s.deliverPlan(turn, plan)
	}
	if n.Update.PlanRemoved != nil {
		s.mu.Lock()
		active := turn.planStarted && !turn.planCompleted
		if active {
			turn.planCompleted = true
		}
		s.mu.Unlock()
		if active {
			return s.deliver(Event{Kind: EventTool, Tool: &ToolEvent{ID: "acp-plan", Name: "update_plan", Title: "Update plan", Kind: string(sdk.ToolKindThink), Status: string(sdk.ToolCallStatusCompleted), Update: true}})
		}
		return nil
	}
	var tool *ToolEvent
	var rawInput, rawOutput any
	if call := n.Update.ToolCall; call != nil {
		tool = &ToolEvent{ID: string(call.ToolCallId), Title: call.Title, Kind: string(call.Kind), Status: string(call.Status), Content: canonicalToolContent(call.Content), Locations: canonicalLocations(call.Locations)}
		rawInput, rawOutput = call.RawInput, call.RawOutput
	} else if update := n.Update.ToolCallUpdate; update != nil {
		tool = &ToolEvent{ID: string(update.ToolCallId), Update: true, Content: canonicalToolContent(update.Content), Locations: canonicalLocations(update.Locations)}
		if update.Title != nil {
			tool.Title = *update.Title
		}
		if update.Kind != nil {
			tool.Kind = string(*update.Kind)
		}
		if update.Status != nil {
			tool.Status = string(*update.Status)
		}
		rawInput, rawOutput = update.RawInput, update.RawOutput
		// Codex 的 terminal_output_delta 扩展把原始追加字节放在指定终端身份下。
		// 仅补齐缺少标准正文的流式通知；标准 Content、完整结果及终态不再重放为增量。
		terminal := tool.Status == "completed" || tool.Status == "failed" || tool.Status == "cancelled"
		if len(tool.Content) == 0 && rawOutput == nil && !terminal {
			if delta, ok := update.Meta["terminal_output_delta"].(map[string]any); ok {
				terminalID, _ := delta["terminal_id"].(string)
				data, _ := delta["data"].(string)
				if terminalID != "" && terminalID == tool.ID && data != "" {
					tool.Content = []ContentBlock{{Type: "text", Text: data}}
				}
			}
		}
	}
	if tool == nil {
		return nil
	}
	var err error
	if rawInput != nil {
		tool.Input, err = json.Marshal(rawInput)
		if err != nil {
			err = fault(FaultProtocol, "tool", "ACP tool input is not JSON", err)
			s.failTurn(turn, err)
			return err
		}
	}
	if rawOutput != nil {
		tool.Output, err = json.Marshal(rawOutput)
		if err != nil {
			err = fault(FaultProtocol, "tool", "ACP tool output is not JSON", err)
			s.failTurn(turn, err)
			return err
		}
	}
	var text []string
	for _, block := range tool.Content {
		if block.Text != "" {
			text = append(text, block.Text)
		}
	}
	tool.Text = strings.Join(text, "\n")
	return s.deliver(Event{Kind: EventTool, Tool: tool})
}

func canonicalLocations(locations []sdk.ToolCallLocation) []Location {
	if locations == nil {
		return nil
	}
	result := make([]Location, 0, len(locations))
	for _, location := range locations {
		item := Location{Path: location.Path}
		if location.Line != nil {
			line := *location.Line
			item.Line = &line
		}
		result = append(result, item)
	}
	return result
}

func canonicalToolContent(content []sdk.ToolCallContent) []ContentBlock {
	if content == nil {
		return nil
	}
	result := make([]ContentBlock, 0, len(content))
	for _, item := range content {
		switch {
		case item.Content != nil:
			if block, ok := canonicalContent(item.Content.Content); ok {
				result = append(result, block)
			}
		case item.Diff != nil:
			diff := &Diff{Path: item.Diff.Path, NewText: item.Diff.NewText}
			if item.Diff.OldText != nil {
				diff.OldText = *item.Diff.OldText
			}
			result = append(result, ContentBlock{Type: "diff", Diff: diff})
		case item.Terminal != nil:
			result = append(result, ContentBlock{Type: "terminal", TerminalID: item.Terminal.TerminalId})
		}
	}
	return result
}

func canonicalContent(content sdk.ContentBlock) (ContentBlock, bool) {
	switch {
	case content.Text != nil:
		return ContentBlock{Type: "text", Text: content.Text.Text}, true
	case content.Image != nil:
		result := ContentBlock{Type: "image", Data: content.Image.Data, MIMEType: content.Image.MimeType}
		if content.Image.Uri != nil {
			result.URI = *content.Image.Uri
		}
		return result, true
	case content.Audio != nil:
		return ContentBlock{Type: "audio", Data: content.Audio.Data, MIMEType: content.Audio.MimeType}, true
	case content.ResourceLink != nil:
		resource := content.ResourceLink
		result := ContentBlock{Type: "resource_link", URI: resource.Uri, Name: resource.Name}
		if resource.MimeType != nil {
			result.MIMEType = *resource.MimeType
		}
		return result, true
	case content.Resource != nil:
		resource := content.Resource.Resource
		result := ContentBlock{Type: "resource"}
		if text := resource.TextResourceContents; text != nil {
			result.Text = text.Text
			result.URI = text.Uri
			if text.MimeType != nil {
				result.MIMEType = *text.MimeType
			}
		} else if blob := resource.BlobResourceContents; blob != nil {
			result.Data = blob.Blob
			result.URI = blob.Uri
			if blob.MimeType != nil {
				result.MIMEType = *blob.MimeType
			}
		} else {
			return ContentBlock{}, false
		}
		return result, true
	}
	return ContentBlock{}, false
}

// deliverPlan 只提取标准计划条目，独立保留进度与终态，不把协议元数据带到宿主。
func (s *Connection) deliverPlan(turn *turnState, plan *sdk.SessionUpdatePlan) error {
	entries := make([]map[string]string, 0, len(plan.Entries))
	completed := len(plan.Entries) > 0
	for _, entry := range plan.Entries {
		entries = append(entries, map[string]string{"content": entry.Content, "priority": string(entry.Priority), "status": string(entry.Status)})
		completed = completed && entry.Status == sdk.PlanEntryStatusCompleted
	}
	input, err := json.Marshal(map[string]any{"plan": entries})
	if err != nil {
		return err
	}
	s.mu.Lock()
	update := turn.planStarted
	turn.planStarted, turn.planCompleted = true, completed
	s.mu.Unlock()
	status := sdk.ToolCallStatusInProgress
	if completed {
		status = sdk.ToolCallStatusCompleted
	}
	return s.deliver(Event{Kind: EventTool, Tool: &ToolEvent{ID: "acp-plan", Name: "update_plan", Title: "Update plan", Kind: string(sdk.ToolKindThink), Status: string(status), Update: update, Input: input}})
}

// interact 将当前原生请求的撤销与整轮撤销分别交给同一个人工端口。
func (s *Connection) interact(ctx context.Context, kind InteractionKind, prompt string, approval *ApprovalRequest, input *InputRequest) (InteractionResponse, error) {
	s.mu.Lock()
	turn := s.turn
	if turn == nil || ctx.Err() != nil || turn.ctx.Err() != nil {
		s.mu.Unlock()
		return InteractionResponse{}, context.Canceled
	}
	turn.interactionSeq++
	sequence := turn.interactionSeq
	if turn.pendingInput == 0 {
		turn.inputDone = make(chan struct{})
	}
	turn.pendingInput++
	s.mu.Unlock()
	s.markActivity(turn)
	defer s.setPendingInput(turn, -1)
	interactionCtx, cancel := context.WithCancel(turn.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	request := InteractionRequest{Invocation: turn.request.Invocation, ID: string(kind) + "-" + strconv.FormatUint(sequence, 10), Kind: kind, Prompt: prompt, Approval: approval, Input: input}
	response, err := turn.sink.Interact(interactionCtx, request)
	if ctx.Err() != nil {
		return InteractionResponse{}, ctx.Err()
	}
	if turn.ctx.Err() != nil {
		return InteractionResponse{}, turn.ctx.Err()
	}
	if err != nil {
		s.failTurn(turn, err)
		return InteractionResponse{}, err
	}
	if response.Invocation != request.Invocation || response.ID != request.ID {
		err = fault(FaultProtocol, "interaction", "ACP interaction response identity mismatch", nil)
		s.failTurn(turn, err)
		return InteractionResponse{}, err
	}
	return response, nil
}

// RequestPermission 保留原生选项身份与未知类别，只回传本次请求允许的明确选项。
func (s *Connection) RequestPermission(ctx context.Context, request sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	if request.SessionId != s.nativeID {
		return permissionCancelled(), nil
	}
	approval := &ApprovalRequest{ToolCallID: string(request.ToolCall.ToolCallId), Locations: canonicalLocations(request.ToolCall.Locations)}
	if request.ToolCall.Title != nil {
		approval.Title = *request.ToolCall.Title
	}
	if request.ToolCall.Kind != nil {
		approval.Kind = string(*request.ToolCall.Kind)
	}
	for _, block := range canonicalToolContent(request.ToolCall.Content) {
		if block.Text != "" {
			if approval.Summary != "" {
				approval.Summary += "\n"
			}
			approval.Summary += block.Text
		}
	}
	if args, ok := request.ToolCall.RawInput.(map[string]any); ok {
		if command, ok := args["command"].(string); ok {
			approval.Command = command
		}
	}
	if approval.Summary == "" {
		approval.Summary = approval.Title
	}
	for _, option := range request.Options {
		if option.OptionId != "" {
			approval.Options = append(approval.Options, ApprovalOption{ID: string(option.OptionId), Name: option.Name, Kind: string(option.Kind)})
		}
	}
	if len(approval.Options) == 0 {
		return permissionCancelled(), nil
	}
	response, err := s.interact(ctx, InteractionApproval, approval.Title, approval, nil)
	if err != nil || response.Action == InteractionCancel {
		return permissionCancelled(), nil
	}
	for _, option := range request.Options {
		if string(option.OptionId) == response.OptionID && response.Action == InteractionAccept {
			return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Selected: &sdk.RequestPermissionOutcomeSelected{OptionId: option.OptionId}}}, nil
		}
	}
	return permissionCancelled(), nil
}

func permissionCancelled() sdk.RequestPermissionResponse {
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Cancelled: &sdk.RequestPermissionOutcomeCancelled{}}}
}

// UnstableCreateElicitation 将公开表单或 URL 输入交给宿主，不解析 CLI 私有问题协议。
func (s *Connection) UnstableCreateElicitation(ctx context.Context, request sdk.UnstableCreateElicitationRequest) (sdk.UnstableCreateElicitationResponse, error) {
	var prompt string
	input := &InputRequest{}
	if form := request.Form; form != nil {
		prompt = form.Message
		input.Mode = "form"
		data, err := json.Marshal(form.RequestedSchema)
		if err != nil || json.Unmarshal(data, &input.RequestedSchema) != nil {
			return sdk.NewUnstableCreateElicitationResponseCancel(), nil
		}
	} else if url := request.Url; url != nil {
		prompt = url.Message
		input.Mode = "url"
		input.URL = url.Url
	} else {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	if strings.TrimSpace(prompt) == "" || input.Mode == "url" && strings.TrimSpace(input.URL) == "" {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	response, err := s.interact(ctx, InteractionInput, prompt, nil, input)
	if err != nil {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	switch response.Action {
	case InteractionAccept:
		result := sdk.NewUnstableCreateElicitationResponseAccept()
		result.Accept.Content = response.Content
		return result, nil
	case InteractionDecline:
		return sdk.NewUnstableCreateElicitationResponseDecline(), nil
	default:
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
}

// UnstableCompleteElicitation 接收公开 URL 完成通知；持久化与交互展示由宿主拥有。
func (*Connection) UnstableCompleteElicitation(context.Context, sdk.UnstableCompleteElicitationNotification) error {
	return nil
}

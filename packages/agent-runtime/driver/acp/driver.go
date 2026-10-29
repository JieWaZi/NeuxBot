// Package acp 通过公开 SDK 与 acpserver 连接原生 Agent，不实现厂商协议或进程管理。
package acp

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/JieWaZi/acp-go/pkg/acpserver"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

const defaultCleanupTimeout = 5 * time.Second

// Factory 仅在通过策略校验后创建公开 ACP Agent；凭据由该启动边界消费。
type Factory func(context.Context, contract.OpenRequest) (sdk.Agent, error)

// CompletionPolicy 声明宿主允许采用的最终回答证明来源。
type CompletionPolicy string

const (
	// CompletionNativePhase 仅接收公开适配器验证的原生最终阶段。
	CompletionNativePhase CompletionPolicy = "native_phase"
	// CompletionLastAssistant 在成功 Prompt 排空通知后验证最后一个有效助手片段；长度终态仍输出截断状态。
	CompletionLastAssistant CompletionPolicy = "last_assistant"
)

// Config 显式配置一个 ACP Driver，不在构造时启动原生进程。
type Config struct {
	ID                 string                                     // ID 是宿主注册的稳定驱动身份。
	Factory            Factory                                    // Factory 创建独占原生 Agent。
	CleanupTimeout     time.Duration                              // CleanupTimeout 是关闭连接与 Adapter 的独立期限。
	CompletionPolicy   CompletionPolicy                           // CompletionPolicy 决定是否允许 ACP 回合结束证明。
	ModeCandidates     []string                                   // ModeCandidates 按优先级选择原生实际声明的权限模式；为空则只使用 Profile.NativeMode。
	SingleModeFallback string                                     // SingleModeFallback 只允许旧 CLI 的唯一模式，不能覆盖多模式目录。
	PromptContext      func(contract.OpenRequest) (string, error) // PromptContext 物化只在首次 Prompt 注入的上下文；nil 使用标准指令与技能前缀。
}

// Driver 拥有已打开连接和准备过程；首批不声明未经验证的配置或工具隔离。
type Driver struct {
	id                 string                                     // id 是固定驱动身份。
	factory            Factory                                    // factory 是唯一原生创建端口。
	cleanupTimeout     time.Duration                              // cleanupTimeout 是释放资源期限。
	ctx                context.Context                            // ctx 取消尚未完成的原生准备。
	cancel             context.CancelFunc                         // cancel 在关闭时停止接纳后的准备工作。
	mu                 sync.Mutex                                 // mu 保护关闭、准备计数及连接集合。
	closed             bool                                       // closed 阻止关闭后的新建连接。
	pending            int                                        // pending 统计尚未返回的 Open。
	sessions           map[*session]struct{}                      // sessions 保存仍由 Driver 拥有的连接。
	changed            chan struct{}                              // changed 在准备计数或原生所有者集合变化时唤醒关闭等待。
	completionPolicy   CompletionPolicy                           // completionPolicy 是冻结的结果证明策略。
	modeCandidates     []string                                   // modeCandidates 是冻结的原生模式候选。
	singleModeFallback string                                     // singleModeFallback 保留旧 CLI 唯一模式的明确兼容值。
	promptContext      func(contract.OpenRequest) (string, error) // promptContext 是宿主明确选择的首次上下文装配端口。
}

var _ contract.Driver = (*Driver)(nil)

// New 校验依赖并创建尚未启动任何 goroutine 的 Driver。
func New(config Config) (*Driver, error) {
	if strings.TrimSpace(config.ID) == "" || config.Factory == nil || config.CleanupTimeout < 0 {
		return nil, fault(contract.FaultInvalidSpec, "configure", "ACP driver requires an identity, factory and nonnegative cleanup timeout", nil)
	}
	if config.CleanupTimeout == 0 {
		config.CleanupTimeout = defaultCleanupTimeout
	}
	if config.CompletionPolicy == "" {
		config.CompletionPolicy = CompletionNativePhase
	}
	if config.CompletionPolicy != CompletionNativePhase && config.CompletionPolicy != CompletionLastAssistant {
		return nil, fault(contract.FaultInvalidSpec, "configure", "ACP completion policy is unsupported", nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if config.PromptContext == nil {
		config.PromptContext = func(request contract.OpenRequest) (string, error) {
			return contributionPrefix(request.Contributions, request.Resources)
		}
	}
	return &Driver{id: config.ID, factory: config.Factory, cleanupTimeout: config.CleanupTimeout, completionPolicy: config.CompletionPolicy, modeCandidates: append([]string(nil), config.ModeCandidates...), singleModeFallback: config.SingleModeFallback, promptContext: config.PromptContext, ctx: ctx, cancel: cancel, sessions: make(map[*session]struct{}), changed: make(chan struct{})}, nil
}

// ID 返回宿主配置的驱动身份。
func (d *Driver) ID() string { return d.id }

// Capabilities 公布驱动实现的会话操作；各原生 CLI 的冷恢复仍按真实握手确认。
func (d *Driver) Capabilities() contract.DriverCapabilities {
	return contract.DriverCapabilities{Version: "acp-message-v1", HotResume: true, ColdResume: true}
}

// Open 先拒绝未支持的策略和贡献，再创建并初始化独占 ACP 连接。
// Workspace.Root 仅作为原生 cwd；inherit_native 不证明目录沙箱隔离。
// 一旦创建原生 Agent，准备失败也返回非 nil Session；调用者必须接管其 Close 后再释放依赖。
func (d *Driver) Open(ctx context.Context, r contract.OpenRequest) (contract.DriverSession, error) {
	if err := validateOpen(r); err != nil {
		return nil, err
	}
	prefix := ""
	var err error
	if r.Session.Mode == contract.SessionNew {
		prefix, err = d.promptContext(r)
		if err != nil {
			return nil, err
		}
		prefix = historyPrefix(prefix, r.Session.Seed)
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, fault(contract.FaultClosed, "open", "ACP driver is closed", nil)
	}
	d.pending++
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.pending--; d.signalLocked(); d.mu.Unlock() }()
	openCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(d.ctx, cancel)
	defer stop()
	if err := openCtx.Err(); err != nil {
		return nil, fault(contract.FaultCancelled, "open", "ACP preparation cancelled", err)
	}
	agent, err := d.factory(openCtx, r)
	if agent == nil {
		return nil, fault(contract.FaultPreparation, "open", "ACP agent creation failed", err)
	}
	// 从取得原生使用者起保留可关闭对象，即使工厂同时返回准备错误也不丢弃依赖所有权。
	s := &session{owner: d, native: agent, initialPrefix: prefix, done: make(chan struct{}), closeGate: make(chan struct{}, 1)}
	partial := func(cause error) (contract.DriverSession, error) {
		s.closed = true
		close(s.done)
		d.retain(s)
		return s, cause
	}
	if err != nil {
		return partial(fault(contract.FaultPreparation, "open", "ACP agent creation failed", err))
	}
	if err = openCtx.Err(); err != nil {
		return partial(fault(contract.FaultCancelled, "open", "ACP preparation cancelled", err))
	}
	if err = s.connect(); err != nil {
		s.closed = true
		d.retain(s)
		return s, err
	}
	d.retain(s)
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		s.stop()
		return s, fault(contract.FaultClosed, "open", "ACP driver closed during preparation", nil)
	}
	abort := context.AfterFunc(openCtx, s.stop)
	defer abort()
	fail := func(cause error) (contract.DriverSession, error) { s.stop(); return s, cause }
	initialized, err := s.initialize(openCtx)
	if err != nil {
		return fail(err)
	}
	s.promptCapabilities = initialized.AgentCapabilities.PromptCapabilities
	s.nativeResumable = initialized.AgentCapabilities.SessionCapabilities.Resume != nil || initialized.AgentCapabilities.LoadSession
	s.forkMode = acpmeta.ForkMode(initialized.Meta)
	if len(r.Workspace.Directories) > 0 && initialized.AgentCapabilities.SessionCapabilities.AdditionalDirectories == nil {
		return fail(fault(contract.FaultUnsupported, "workspace", "ACP additional workspace roots are not advertised", nil))
	}
	servers, err := capabilityServers(r.Contributions, r.Resources, initialized.AgentCapabilities.McpCapabilities)
	if err != nil {
		return fail(err)
	}
	var options []sdk.SessionConfigOption
	var modes *sdk.SessionModeState
	if r.Session.Mode == contract.SessionResume {
		if !s.nativeResumable {
			return fail(fault(contract.FaultUnsupported, "resume_session", "ACP native restore is not advertised", nil))
		}
		s.nativeID = sdk.SessionId(r.Session.Checkpoint.NativeHandle)
		if initialized.AgentCapabilities.SessionCapabilities.Resume != nil {
			response, restoreErr := s.client.ResumeSession(openCtx, sdk.ResumeSessionRequest{SessionId: s.nativeID, Cwd: r.Workspace.Root, AdditionalDirectories: r.Workspace.Directories, McpServers: servers})
			options, modes, err = response.ConfigOptions, response.Modes, restoreErr
		} else {
			response, restoreErr := s.client.LoadSession(openCtx, sdk.LoadSessionRequest{SessionId: s.nativeID, Cwd: r.Workspace.Root, AdditionalDirectories: r.Workspace.Directories, McpServers: servers})
			options, modes, err = response.ConfigOptions, response.Modes, restoreErr
		}
		if err != nil {
			return fail(fault(contract.FaultPreparation, "resume_session", "ACP native session restore failed", err))
		}
	} else {
		response, createErr := s.client.NewSession(openCtx, sdk.NewSessionRequest{Cwd: r.Workspace.Root, AdditionalDirectories: r.Workspace.Directories, McpServers: servers})
		if createErr != nil {
			return fail(fault(contract.FaultPreparation, "new_session", "ACP session creation failed", createErr))
		}
		if response.SessionId == "" {
			return fail(fault(contract.FaultProtocol, "new_session", "ACP session identity is missing", nil))
		}
		s.nativeID, options, modes = response.SessionId, response.ConfigOptions, response.Modes
	}
	if err = s.configure(openCtx, options, modes, r.Profile); err != nil {
		return fail(err)
	}
	if err = openCtx.Err(); err != nil {
		return fail(fault(contract.FaultCancelled, "open", "ACP preparation cancelled", err))
	}
	return s, nil
}

// connect 为执行与管理复用同一条公开 ACP 管道及边界所有权。
func (s *session) connect() error {
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
	proxy := &deliveryAgent{Agent: s.native, session: s}
	server, err := acpserver.New(proxy, serverIn, serverOut)
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		close(s.done)
		return fault(contract.FaultPreparation, "connect", "ACP server creation failed", err)
	}
	s.client, err = sdk.NewClientSideConnectionWithOptions(s, clientOut, clientIn, sdk.ConnectionOptions{Context: serveCtx, NotificationQueueCapacity: 256, NotificationQueueBytes: 16 << 20, CloseTransport: s.closeTransport})
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		close(s.done)
		return fault(contract.FaultPreparation, "connect", "ACP connection creation failed", err)
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
func (s *session) initialize(ctx context.Context) (sdk.InitializeResponse, error) {
	response, err := s.client.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber, ClientCapabilities: sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{Form: &sdk.ElicitationFormCapabilities{}, Url: &sdk.ElicitationUrlCapabilities{}}}})
	if err != nil {
		return response, fault(contract.FaultPreparation, "initialize", "ACP initialization failed", err)
	}
	if response.ProtocolVersion != sdk.ProtocolVersionNumber {
		return response, fault(contract.FaultProtocol, "initialize", "ACP protocol version is unsupported", nil)
	}
	return response, nil
}

// Close 停止接纳、取消准备并尝试关闭当前原生所有者；失败对象保留供后续重试。
func (d *Driver) Close(ctx context.Context) error {
	closeCtx, cancel := context.WithTimeout(ctx, d.cleanupTimeout)
	defer cancel()
	d.mu.Lock()
	d.closed = true
	d.cancel()
	d.signalLocked()
	d.mu.Unlock()
	for {
		d.mu.Lock()
		sessions := make([]*session, 0, len(d.sessions))
		for s := range d.sessions {
			sessions = append(sessions, s)
		}
		d.mu.Unlock()
		for _, s := range sessions {
			s.stop()
		}
		for _, s := range sessions {
			if err := s.Close(closeCtx); err != nil {
				return err
			}
		}
		d.mu.Lock()
		if len(d.sessions) > 0 {
			d.mu.Unlock()
			continue
		}
		if d.pending == 0 {
			d.mu.Unlock()
			return nil
		}
		changed := d.changed
		d.mu.Unlock()
		select {
		case <-changed:
		case <-closeCtx.Done():
			return fault(contract.FaultCleanup, "close", "ACP driver cleanup deadline exceeded", closeCtx.Err())
		}
	}
}

func (d *Driver) retain(s *session) {
	d.mu.Lock()
	d.sessions[s] = struct{}{}
	d.signalLocked()
	d.mu.Unlock()
}
func (d *Driver) release(s *session) {
	d.mu.Lock()
	delete(d.sessions, s)
	d.signalLocked()
	d.mu.Unlock()
}
func (d *Driver) signalLocked() { close(d.changed); d.changed = make(chan struct{}) }

func validateOpen(r contract.OpenRequest) error {
	if r.Policies.NativeConfig != contract.NativeConfigInherit || r.Policies.Tools.Mode != contract.ToolsAllowAll || len(r.Policies.Tools.Allow) > 0 {
		return fault(contract.FaultUnsupported, "policy", "ACP configuration isolation and tool restrictions are not verified", nil)
	}
	if r.Session.Mode != contract.SessionNew && (r.Session.Mode != contract.SessionResume || r.Session.Checkpoint == nil || r.Session.Checkpoint.NativeHandle == "" || r.Session.Ref != "" || len(r.Session.Seed) > 0) {
		return fault(contract.FaultUnsupported, "session", "ACP open requires a new session or native checkpoint", nil)
	}
	if r.Session.Mode == contract.SessionNew && r.Session.Checkpoint != nil {
		return fault(contract.FaultInvalidSpec, "session", "ACP new session cannot include a native checkpoint", nil)
	}
	if !filepath.IsAbs(r.Workspace.Root) {
		return fault(contract.FaultInvalidSpec, "workspace", "ACP working directory must be absolute", nil)
	}
	return nil
}

func fault(code contract.FaultCode, stage, message string, cause error) *contract.Fault {
	return &contract.Fault{Code: code, Stage: stage, Message: message, Cause: cause}
}

// deliveryAgent 只在公开 Agent Prompt 入口确认交付，不解析 JSON-RPC 或私有协议。
type deliveryAgent struct {
	sdk.Agent          // Agent 复用公开 Agent 的全部原生方法。
	session   *session // session 关联当前唯一调用与清理所有者。
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
	a.session.mu.Lock()
	turn := a.session.turn
	a.session.mu.Unlock()
	if turn == nil {
		return sdk.PromptResponse{}, sdk.NewInvalidParams(nil)
	}
	nativeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(turn.ctx, cancel)
	defer stop()
	if err := a.session.deliver(contract.DriverEvent{Kind: contract.EventPromptAccepted}); err != nil {
		return sdk.PromptResponse{}, err
	}
	return a.Agent.Prompt(nativeCtx, p)
}

// Close 仅收敛 acpserver 的请求边界；原生资源始终由 session.Close 独占关闭。
func (a *deliveryAgent) Close(ctx context.Context) error {
	a.session.mu.Lock()
	if a.session.turn != nil {
		a.session.turn.cancel()
	}
	a.session.mu.Unlock()
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

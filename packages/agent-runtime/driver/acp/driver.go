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
	"github.com/JieWaZi/acp-go/pkg/acpserver"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

const defaultCleanupTimeout = 5 * time.Second

// Factory 仅在通过策略校验后创建公开 ACP Agent；凭据由该启动边界消费。
type Factory func(context.Context, contract.OpenRequest) (sdk.Agent, error)

// Config 显式配置一个 ACP Driver，不在构造时启动原生进程。
type Config struct {
	ID             string        // ID 是宿主注册的稳定驱动身份。
	Factory        Factory       // Factory 创建独占原生 Agent。
	CleanupTimeout time.Duration // CleanupTimeout 是关闭连接与 Adapter 的独立期限。
}

// Driver 拥有已打开连接和准备过程；首批不声明未经验证的配置或工具隔离。
type Driver struct {
	id             string                // id 是固定驱动身份。
	factory        Factory               // factory 是唯一原生创建端口。
	cleanupTimeout time.Duration         // cleanupTimeout 是释放资源期限。
	ctx            context.Context       // ctx 取消尚未完成的原生准备。
	cancel         context.CancelFunc    // cancel 在关闭时停止接纳后的准备工作。
	mu             sync.Mutex            // mu 保护关闭、准备计数及连接集合。
	closed         bool                  // closed 阻止关闭后的新建连接。
	pending        int                   // pending 统计尚未返回的 Open。
	sessions       map[*session]struct{} // sessions 保存仍由 Driver 拥有的连接。
	changed        chan struct{}         // changed 在准备计数或原生所有者集合变化时唤醒关闭等待。
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
	ctx, cancel := context.WithCancel(context.Background())
	return &Driver{id: config.ID, factory: config.Factory, cleanupTimeout: config.CleanupTimeout, ctx: ctx, cancel: cancel, sessions: make(map[*session]struct{}), changed: make(chan struct{})}, nil
}

// ID 返回宿主配置的驱动身份。
func (d *Driver) ID() string { return d.id }

// Capabilities 只公布首切片已实现的公共传输能力；单次连接不支持热恢复。
func (d *Driver) Capabilities() contract.DriverCapabilities {
	return contract.DriverCapabilities{Version: "acp-go/2f0ad2051741+sdk/d283676c207f"}
}

// Open 先拒绝未支持的策略和贡献，再创建并初始化独占 ACP 连接。
// Workspace.Root 仅作为原生 cwd；inherit_native 不证明目录沙箱隔离。
// 一旦创建原生 Agent，准备失败也返回非 nil Session；调用者必须接管其 Close 后再释放依赖。
func (d *Driver) Open(ctx context.Context, r contract.OpenRequest) (contract.DriverSession, error) {
	if err := validateOpen(r); err != nil {
		return nil, err
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
	s := &session{owner: d, native: agent, done: make(chan struct{}), closeGate: make(chan struct{}, 1)}
	partial := func(cause error) (contract.DriverSession, error) {
		s.spent = true
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
	proxy := &deliveryAgent{Agent: agent, session: s}
	server, err := acpserver.New(proxy, serverIn, serverOut)
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		return partial(fault(contract.FaultPreparation, "connect", "ACP server creation failed", err))
	}
	s.client, err = sdk.NewClientSideConnectionWithOptions(s, clientOut, clientIn, sdk.ConnectionOptions{Context: serveCtx, NotificationQueueCapacity: 256, NotificationQueueBytes: 16 << 20, CloseTransport: s.closeTransport})
	if err != nil {
		serveCancel()
		_ = s.closeTransport()
		return partial(fault(contract.FaultPreparation, "connect", "ACP connection creation failed", err))
	}
	d.retain(s)
	go func() {
		// Serve的唯一返回错误来自deliveryAgent.Close；该边界只取消请求，原生Close结果由session单独报告。
		_ = server.Serve(serveCtx)
		_ = s.closeTransport()
		close(s.done)
	}()
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
	initialized, err := s.client.Initialize(openCtx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
	if err != nil {
		return fail(fault(contract.FaultPreparation, "initialize", "ACP initialization failed", err))
	}
	if initialized.ProtocolVersion != sdk.ProtocolVersionNumber {
		return fail(fault(contract.FaultProtocol, "initialize", "ACP protocol version is unsupported", nil))
	}
	response, err := s.client.NewSession(openCtx, sdk.NewSessionRequest{Cwd: r.Workspace.Root, McpServers: []sdk.McpServer{}})
	if err != nil {
		return fail(fault(contract.FaultPreparation, "new_session", "ACP session creation failed", err))
	}
	if response.SessionId == "" {
		return fail(fault(contract.FaultProtocol, "new_session", "ACP session identity is missing", nil))
	}
	s.nativeID = response.SessionId
	if err = s.configure(openCtx, response.ConfigOptions, r.Profile); err != nil {
		return fail(err)
	}
	if err = openCtx.Err(); err != nil {
		return fail(fault(contract.FaultCancelled, "open", "ACP preparation cancelled", err))
	}
	return s, nil
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
	if r.Session.Mode != contract.SessionNew || len(r.Session.Seed) > 0 || r.Session.Checkpoint != nil {
		return fault(contract.FaultUnsupported, "session", "ACP resume and history seeding are not supported", nil)
	}
	if len(r.Contributions.Instructions) > 0 || len(r.Contributions.Skills) > 0 || len(r.Contributions.MCP) > 0 || len(r.Resources) > 0 || len(r.Workspace.Directories) > 0 {
		return fault(contract.FaultUnsupported, "resources", "ACP static contributions and additional workspace roots are not supported", nil)
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

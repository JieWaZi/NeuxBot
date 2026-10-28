package runtimes

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpserver"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/google/uuid"
)

const (
	acpManagementCloseTimeout = 5 * time.Second
	acpUpdateQueueCapacity    = 256
	acpUpdateQueueBytes       = 16 * 1024 * 1024
)

// acpManager 只提供不提交模型输入的 ACP 管理能力。
type acpManager interface {
	DetectOptions(context.Context, acpOptionsRequest) (acpOptionCatalog, error)
	ForkSession(context.Context, preparedRuntime, ForkSessionRequest) (ForkSessionResult, error)
	Close(context.Context) error
}

// acpOptionsRequest 保存一次 ACP 配置探测已经准备完成的启动事实。
type acpOptionsRequest struct {
	// RefreshCredentials 与执行共用账号刷新权威，探测不私自刷新副本。
	RefreshCredentials CredentialRefresh
	// Model 是隔离探测会话需要实际选择的原生模型。
	Model string
	// RuntimeID 是内置 Runtime 或本地 Profile 的稳定标识。
	RuntimeID string
	// Prepared 保存探测确认的 Runtime 与环境事实。
	Prepared preparedRuntime
	// WorkingDirectory 是临时 ACP Session 使用的工作目录。
	WorkingDirectory string
}

// acpOptionCatalog 保存 ACP 返回的模型目录和当前默认模型推理强度。
type acpOptionCatalog struct {
	// AgentConnected 表示目标 ACP Agent 已完成标准 initialize 握手。
	AgentConnected bool
	// SessionReady 表示探测用 ACP Session 已成功创建。
	SessionReady bool
	// Version 是 ACP Agent 通过 Implementation 声明的实现版本。
	Version string
	// Capabilities 是 ACP initialize 与薄 Binding 共同确认的产品能力。
	Capabilities CapabilitySet
	// Models 是 ACP model 配置声明的模型及各自推理强度。
	Models []ModelOption
	// ReasoningOptions 是 ACP 新建 Session 当前模型声明的推理强度。
	ReasoningOptions []ReasoningOption
	// DefaultModel 是 ACP 新建 Session 当前生效的模型标识。
	DefaultModel string
	// DefaultReasoning 是 ACP 新建 Session 当前生效的推理强度标识。
	DefaultReasoning string
	// PermissionLevels 是 ACP 新建 Session 当前模型声明的产品权限等级。
	PermissionLevels []PermissionLevel
	// Modes 是 ACP 新建 Session 声明的标准权限模式。
	Modes []acp.SessionMode
}

// acpBackend 为探测和分叉选择固定适配器，不缓存执行会话或进程。
type acpBackend struct {
	// mutex 保护 bindings 与关闭门禁。
	mutex sync.Mutex
	// bindings 保存基础智能体的原生工厂及目录解释规则。
	bindings map[string]acpRuntimeBinding
	// closing 阻止关闭后接纳新的管理连接。
	closing bool
}

// embeddedACPAgent 拥有一次探测或分叉所需的独占 ACP 连接。
type embeddedACPAgent struct {
	// diagnosticContext 关联当前管理操作的诊断身份。
	diagnosticContext context.Context
	// executable 是日志展示使用的真实程序路径。
	executable string
	// server 是此次独占的进程内 ACP 服务。
	server *acpserver.Server
	// serveContext 控制服务生命周期。
	serveContext context.Context
	// connection 用于初始化、目录读取或原生分叉。
	connection *acp.ClientSideConnection
	// initializeResponse 保存适配器声明的能力。
	initializeResponse acp.InitializeResponse
	// binding 保存工厂及目录差异。
	binding acpRuntimeBinding
	// cancelServe 结束服务生命周期。
	cancelServe context.CancelFunc
	// closers 关闭全部内存传输端点。
	closers []io.Closer
	// serveDone 保存服务退出结果。
	serveDone chan error
	// closeOnce 防止重复回收。
	closeOnce sync.Once
	// closeErr 保存唯一关闭结果。
	closeErr error
}

// newACPBackend 创建固定管理适配，不启动 goroutine 或进程。
func newACPBackend() *acpBackend {
	acpAdapterLogger(context.Background())
	return &acpBackend{bindings: defaultACPRuntimeBindings()}
}

// binding 在关闭门禁内读取工厂与目录规则。
func (backend *acpBackend) binding(baseRuntimeID string) (acpRuntimeBinding, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	binding, exists := backend.bindings[baseRuntimeID]
	if backend.closing || !exists || binding.baseRuntimeID != baseRuntimeID || binding.newAgent == nil {
		return acpRuntimeBinding{}, status_error.RuntimeUnavailable
	}
	return binding, nil
}

// Close 停止接纳新管理操作；已接纳操作在自身 Context 结束时关闭独占连接。
func (backend *acpBackend) Close(context.Context) error {
	backend.mutex.Lock()
	backend.closing = true
	backend.mutex.Unlock()
	return nil
}

// DetectOptions 使用独立 Agent 读取最新 CLI 版本和 ACP 配置，不影响正在执行的会话。
func (backend *acpBackend) DetectOptions(
	ctx context.Context,
	request acpOptionsRequest,
) (catalog acpOptionCatalog, detectionErr error) {
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return acpOptionCatalog{}, err
	}
	config := acpAdapterConfigForManagement(request.Prepared, RunRequest{
		Target:    RuntimeTarget{RuntimeID: request.RuntimeID, RefreshCredentials: request.RefreshCredentials},
		Workspace: Workspace{Path: request.WorkingDirectory},
	}, binding)
	config.DetectionID = uuid.NewString()
	ctx = acpAgentLogContext(ctx, config, binding)
	defer func() {
		if detectionErr != nil && !errors.Is(ctx.Err(), context.Canceled) {
			acpAdapterLogger(ctx).Warn("ACP option detection failed", "error", detectionErr)
		}
	}()
	agent, err := newEmbeddedACPAgent(ctx, config, binding)
	if err != nil {
		return acpOptionCatalog{}, err
	}
	if err := agent.Start(ctx); err != nil {
		return acpOptionCatalog{}, err
	}
	defer func() {
		closeContext, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
		if closeErr := agent.Close(closeContext); closeErr != nil {
			acpAdapterLogger(agent.diagnosticContext).Error("close ACP detection agent failed", "error", closeErr)
		}
		cancelClose()
	}()
	return agent.detectOptions(ctx, request.WorkingDirectory, request.Model)
}

// newEmbeddedACPAgent 通过两条内存 Pipe 创建尚未启动的标准 ACP 双向连接。
func newEmbeddedACPAgent(
	ctx context.Context,
	config acpAdapterConfig,
	binding acpRuntimeBinding,
) (*embeddedACPAgent, error) {
	if binding.newAgent == nil {
		return nil, status_error.RuntimeUnavailable
	}
	ctx = acpAgentLogContext(ctx, config, binding)
	agent, err := binding.newAgent(ctx, config)
	if err != nil {
		flushACPStderr(ctx)
		return nil, err
	}

	clientToAgentReader, clientToAgentWriter := io.Pipe()
	agentToClientReader, agentToClientWriter := io.Pipe()
	server, err := acpserver.New(
		agent,
		clientToAgentReader,
		agentToClientWriter,
	)
	if err != nil {
		_ = clientToAgentReader.Close()
		_ = clientToAgentWriter.Close()
		_ = agentToClientReader.Close()
		_ = agentToClientWriter.Close()
		return nil, err
	}
	serveContext, cancelServe := context.WithCancel(context.WithoutCancel(ctx))
	client := &acpManagementClient{}
	closers := []io.Closer{clientToAgentReader, clientToAgentWriter, agentToClientReader, agentToClientWriter}
	connection, err := acp.NewClientSideConnectionWithOptions(
		client,
		clientToAgentWriter,
		agentToClientReader,
		acp.ConnectionOptions{
			Context:                   serveContext,
			NotificationQueueCapacity: acpUpdateQueueCapacity,
			NotificationQueueBytes:    acpUpdateQueueBytes,
			CloseTransport: func() error {
				var closeErr error
				for _, closer := range closers {
					closeErr = errors.Join(closeErr, closer.Close())
				}
				return closeErr
			},
		},
	)
	if err != nil {
		cancelServe()
		for _, closer := range closers {
			_ = closer.Close()
		}
		return nil, err
	}
	return &embeddedACPAgent{
		diagnosticContext: ctx,
		executable:        config.Executable,
		server:            server,
		serveContext:      serveContext,
		connection:        connection,
		binding:           binding,
		cancelServe:       cancelServe,
		closers:           closers,
		serveDone:         make(chan error, 1),
	}, nil
}

// Start 启动 ACP Server 并完成客户端 initialize 握手。
func (agent *embeddedACPAgent) Start(ctx context.Context) error {
	go func() {
		agent.serveDone <- agent.server.Serve(agent.serveContext)
		close(agent.serveDone)
	}()

	initializeResponse, err := agent.connection.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{},
	})
	if err != nil {
		closeContext, cancelClose := context.WithTimeout(
			context.WithoutCancel(ctx),
			acpManagementCloseTimeout,
		)
		_ = agent.Close(closeContext)
		cancelClose()
		return err
	}
	agent.initializeResponse = initializeResponse
	acpAdapterLogger(agent.diagnosticContext).Info("ACP agent initialized", "agent_name", agent.binding.displayName, agent.binding.executableLogKey, agent.executable)
	return nil
}

// Close 结束内存连接，并等待 acpserver 释放目标 Runtime Adapter。
func (agent *embeddedACPAgent) Close(ctx context.Context) error {
	agent.closeOnce.Do(func() {
		defer flushACPStderr(agent.diagnosticContext)
		failed := false
		// 连接所有者先解除读写背压，Adapter.Close 负责回收仍存活的原生 Session。
		// 此时不能再发送依赖同一拥塞连接的 closeSession 请求。
		agent.cancelServe()
		agent.connection.Close()
		for _, closer := range agent.closers {
			if err := closer.Close(); err != nil {
				acpAdapterLogger(agent.diagnosticContext).Error("close ACP in-memory transport failed", "error", err)
				failed = true
			}
		}
		select {
		case err := <-agent.serveDone:
			if err != nil {
				acpAdapterLogger(agent.diagnosticContext).Error("stop ACP server failed", "error", err)
				failed = true
			}
		case <-ctx.Done():
			agent.closeErr = runtimeContextStatus(ctx.Err())
		}
		if agent.closeErr == nil && failed {
			agent.closeErr = status_error.RuntimeShutdownFailed
		}
	})
	return agent.closeErr
}

var _ acpManager = (*acpBackend)(nil)

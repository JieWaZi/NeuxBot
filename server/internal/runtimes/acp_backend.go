package runtimes

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpserver"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/google/uuid"
)

// acpRunner 是 Runtime SDK 消费的通用 ACP Client Runtime Module seam。
type acpRunner interface {
	// DetectOptions 通过 ACP Session 配置读取模型及其推理强度。
	DetectOptions(
		ctx context.Context,
		request acpOptionsRequest,
	) (acpOptionCatalog, error)
	// Run 执行一次 ACP Prompt，并返回 Runtime 统一终态。
	Run(ctx context.Context, request acpRunRequest) Result
	// Close 释放全部内嵌 ACP Agent 和 Runtime 进程。
	Close(ctx context.Context) error
}

// acpOptionsRequest 保存一次 ACP 配置探测已经准备完成的启动事实。
type acpOptionsRequest struct {
	// RefreshCredentials 与执行共用账号刷新权威，探测不私自刷新副本。
	RefreshCredentials CredentialRefresh
	// Model 是隔离探测会话需要实际选择的原生模型。
	Model string
	// RuntimeID 是内置 Runtime 或本地 Profile 的稳定标识。
	RuntimeID string
	// Prepared 保存探测确认的 Runtime、环境和默认限制。
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

// acpRunRequest 保存一次 ACP 调用已经准备完成的全部输入。
type acpRunRequest struct {
	// InvocationID 是交付给 Chat 的本次调用标识。
	InvocationID string
	// Prepared 保存探测确认的 Runtime 与环境事实。
	Prepared preparedRuntime
	// Request 是调用方提交的结构化 Runtime 输入。
	Request RunRequest
	// Prompt 是新建或恢复语义对应的最终文本输入。
	Prompt string
	// RuntimeFiles 保存本次物化的 Skill 目录和显式选择。
	RuntimeFiles runtimeFiles
	// Sink 接收跨 Runtime 统一事件。
	Sink EventSink
	// EphemeralSession 表示本次隔离调用完成后必须立即关闭原生 Session。
	EphemeralSession bool
	// Text 仅在无状态模型生成时启用独占进程、配置确认和工具拒绝。
	Text *textInvocation
}

// acpBackend 按 Runtime 启动指纹复用内嵌 ACP Agent。
type acpBackend struct {
	// mutex 保护 entries、closing 和 bindings。
	mutex sync.Mutex
	// entries 保存正在创建或已经可用的 Agent。
	entries map[string]*acpEntry
	// bindings 保存各基础 Runtime 的固定 ACP 薄适配，测试可在首次调用前替换工厂。
	bindings map[string]acpRuntimeBinding
	// closing 阻止关闭开始后创建新 Agent。
	closing bool
}

// acpEntry 合并同一启动指纹的并发创建请求。
type acpEntry struct {
	// owner 是可保留独占进程所属的 Runtime 与原生会话标识。
	owner string
	// ready 在 Agent 创建成功或失败后关闭。
	ready chan struct{}
	// agent 是创建成功的内嵌 ACP 连接。
	agent *embeddedACPAgent
	// err 是创建阶段的稳定技术错误。
	err error
}

// embeddedACPAgent 保存一条进程内 ACP Client/Agent 连接及其资源所有权。
type embeddedACPAgent struct {
	// diagnosticContext 关联当前连接的智能体、连接和探测日志身份。
	diagnosticContext context.Context
	// executable 是日志展示使用的已解析 Runtime 可执行文件路径。
	executable string
	// server 是尚未启动或正在服务的进程内 ACP Server。
	server *acpserver.Server
	// serveContext 控制 ACP Server 的完整服务生命周期。
	serveContext context.Context
	// connection 向当前 Runtime Agent 发送标准 ACP 请求。
	connection *acp.ClientSideConnection
	// initializeResponse 保存 ACP Agent 完成握手后返回的标准能力快照。
	initializeResponse acp.InitializeResponse
	// binding 保存当前连接对应的 Runtime 私有配置差异。
	binding acpRuntimeBinding
	// client 把 Agent 回调路由到当前 Session Invocation。
	client *acpClient
	// promptWriter 观察 Prompt 请求成功写入的协议屏障。
	promptWriter *acpPromptWriter
	// sessionMutex 保护当前连接仍保持打开的 ACP Session。
	sessionMutex sync.Mutex
	// sessions 保存原生 Session 与创建它的工作区及 MCP 范围指纹。
	sessions map[acp.SessionId]string
	// sessionModes 保存会话实际声明的原生模式，受 sessionMutex 保护。
	sessionModes map[acp.SessionId]*acp.SessionModeState
	// sessionDefaultModels 保存会话首次打开时的模型，供用户切回默认选择。
	sessionDefaultModels map[acp.SessionId]acp.SessionConfigValueId
	// sessionDefaultReasoning 按会话与模型保存官方默认思考值，供用户撤销显式强度选择。
	sessionDefaultReasoning map[acp.SessionId]map[acp.SessionConfigValueId]acp.SessionConfigValueId
	// sessionModels 保存适配器已确认的当前模型，避免重复触发上游初始化。
	sessionModels map[acp.SessionId]acp.SessionConfigValueId
	// cancelServe 结束进程内 ACP Server 生命周期。
	cancelServe context.CancelFunc
	// closers 关闭两条 Pipe 的读写端，解除 SDK reader。
	closers []io.Closer
	// serveDone 返回 ACP Server 的唯一退出结果。
	serveDone chan error
	// closeOnce 保证连接和 Agent 只释放一次。
	closeOnce sync.Once
	// closeErr 保存首次关闭结果。
	closeErr error
}

// newACPBackend 创建不启动 goroutine 或外部进程的共享 ACP Backend。
func newACPBackend() *acpBackend {
	// SDK 内部连接没有可在构造时注入的 Logger，统一接回日志隔离边界。
	acpAdapterLogger(context.Background())
	return &acpBackend{
		entries:  make(map[string]*acpEntry),
		bindings: defaultACPRuntimeBindings(),
	}
}

// binding 返回基础 Runtime 对应的薄适配快照。
func (backend *acpBackend) binding(baseRuntimeID string) (acpRuntimeBinding, error) {
	backend.mutex.Lock()
	binding, exists := backend.bindings[baseRuntimeID]
	backend.mutex.Unlock()
	if !exists || binding.baseRuntimeID != baseRuntimeID || binding.newAgent == nil {
		return acpRuntimeBinding{}, status_error.RuntimeUnavailable
	}
	return binding, nil
}

// DetectOptions 使用独立 Agent 读取最新 CLI 版本和 ACP 配置，不影响正在执行的会话。
func (backend *acpBackend) DetectOptions(
	ctx context.Context,
	request acpOptionsRequest,
) (catalog acpOptionCatalog, detectionErr error) {
	runRequest := acpRunRequest{
		Prepared: request.Prepared,
		Request: RunRequest{Target: RuntimeTarget{
			RuntimeID:          request.RuntimeID,
			RefreshCredentials: request.RefreshCredentials,
		}},
	}
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return acpOptionCatalog{}, err
	}
	if binding.nativeProcess {
		runRequest.Request.Workspace.Path = request.WorkingDirectory
	}
	backend.mutex.Lock()
	closing := backend.closing
	backend.mutex.Unlock()
	if closing {
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}
	config := acpAdapterConfigForRequest(runRequest, binding)
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
		closeContext, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), acpDeliveryDrainTimeout)
		if closeErr := agent.Close(closeContext); closeErr != nil {
			acpAdapterLogger(agent.diagnosticContext).Error("close ACP detection agent failed", "error", closeErr)
		}
		cancelClose()
	}()
	return agent.detectOptions(ctx, request.WorkingDirectory, request.Model)
}

// Run 获取长期 Agent，并把一次 Prompt 映射为 Runtime Result。
func (backend *acpBackend) Run(
	ctx context.Context,
	request acpRunRequest,
) Result {
	binding, bindingErr := backend.binding(request.Prepared.definition.baseRuntimeID)
	if bindingErr != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageConnect, bindingErr)
	}
	if binding.retainedProcess {
		backend.retireRetainedScope(ctx, request, binding)
	}
	agent, err := backend.agent(ctx, request)
	if err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageConnect, err)
	}
	if request.Request.Target.CloseAfterRun || (agent.binding.nativeProcess && !agent.binding.retainedProcess) {
		defer backend.invalidate(context.WithoutCancel(ctx), acpAgentKeyForBinding(request, agent.binding), agent)
	}
	result := agent.run(ctx, request)
	if agent.binding.retainedProcess && !request.Request.Target.CloseAfterRun {
		agent.sessionMutex.Lock()
		_, retained := agent.sessions[acp.SessionId(result.Session.OutputID)]
		agent.sessionMutex.Unlock()
		if request.EphemeralSession || !retained {
			backend.invalidate(ctx, acpAgentKeyForBinding(request, agent.binding), agent)
		} else {
			backend.retainSessionProcess(request, result.Session.OutputID, agent)
		}
	}
	if result.Failure != nil &&
		(result.Failure.Code == FailureCodeRuntimeUnavailable ||
			result.Failure.Code == FailureCodeProtocolIncompatible) {
		backend.invalidate(ctx, acpAgentKeyForBinding(request, agent.binding), agent)
	}
	return result
}

// retireRetainedScope 在权限、挂载或启动配置变化时释放该聊天之前持有的进程。
func (backend *acpBackend) retireRetainedScope(
	ctx context.Context,
	request acpRunRequest,
	binding acpRuntimeBinding,
) {
	if request.Request.Session.Mode != SessionModeResume {
		return
	}
	owner := request.Request.Target.RuntimeID + "\x00" + request.Request.Session.ID
	key := acpAgentKeyForBinding(request, binding)
	backend.mutex.Lock()
	stale := make(map[string]*embeddedACPAgent)
	for previousKey, entry := range backend.entries {
		if entry.owner == owner && previousKey != key && entry.agent != nil {
			stale[previousKey] = entry.agent
		}
	}
	backend.mutex.Unlock()
	for previousKey, agent := range stale {
		backend.invalidate(ctx, previousKey, agent)
	}
}

// retainSessionProcess 将首次调用的临时键替换为原生会话键，后续轮次沿用同一连接。
func (backend *acpBackend) retainSessionProcess(request acpRunRequest, sessionID string, agent *embeddedACPAgent) {
	key := acpAgentKeyForBinding(request, agent.binding)
	request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: sessionID}
	nextKey := acpAgentKeyForBinding(request, agent.binding)
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	entry := backend.entries[key]
	if backend.closing || entry == nil || entry.agent != agent {
		return
	}
	delete(backend.entries, key)
	entry.owner = request.Request.Target.RuntimeID + "\x00" + sessionID
	backend.entries[nextKey] = entry
}

// invalidate 只移除仍绑定目标 Agent 的失效条目，并在锁外释放资源。
func (backend *acpBackend) invalidate(
	ctx context.Context,
	key string,
	agent *embeddedACPAgent,
) {
	backend.mutex.Lock()
	entry := backend.entries[key]
	if entry == nil || entry.agent != agent {
		backend.mutex.Unlock()
		return
	}
	delete(backend.entries, key)
	backend.mutex.Unlock()
	closeContext, cancelClose := context.WithTimeout(
		context.WithoutCancel(ctx),
		acpDeliveryDrainTimeout,
	)
	_ = agent.Close(closeContext)
	cancelClose()
}

// agent 返回同一 Runtime 启动指纹当前可用的 Agent，并合并并发创建。
func (backend *acpBackend) agent(
	ctx context.Context,
	request acpRunRequest,
) (*embeddedACPAgent, error) {
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return nil, err
	}
	key := acpAgentKeyForBinding(request, binding)
	for {
		backend.mutex.Lock()
		if backend.closing {
			backend.mutex.Unlock()
			return nil, status_error.RuntimeUnavailable
		}
		entry := backend.entries[key]
		if entry == nil {
			entry = &acpEntry{ready: make(chan struct{})}
			backend.entries[key] = entry
			backend.mutex.Unlock()
			backend.createAgent(ctx, key, entry, request, binding)
		} else {
			backend.mutex.Unlock()
		}

		select {
		case <-entry.ready:
		case <-ctx.Done():
			return nil, runtimeContextStatus(ctx.Err())
		}
		if entry.err != nil {
			return nil, entry.err
		}
		if entry.agent != nil && entry.agent.alive() {
			return entry.agent, nil
		}

		backend.mutex.Lock()
		if backend.entries[key] == entry {
			delete(backend.entries, key)
		}
		backend.mutex.Unlock()
	}
}

// createAgent 在锁外启动目标 Runtime Agent，再原子发布创建结果。
func (backend *acpBackend) createAgent(
	ctx context.Context,
	key string,
	entry *acpEntry,
	request acpRunRequest,
	binding acpRuntimeBinding,
) {
	agent, err := newEmbeddedACPAgent(
		ctx,
		acpAdapterConfigForRequest(request, binding),
		binding,
	)
	if err == nil {
		err = agent.Start(ctx)
	}
	if err != nil {
		agent = nil
	}
	backend.mutex.Lock()
	entry.agent = agent
	entry.err = err
	if err != nil && backend.entries[key] == entry {
		delete(backend.entries, key)
	}
	close(entry.ready)
	closing := backend.closing
	backend.mutex.Unlock()
	if closing && agent != nil {
		closeContext, cancelClose := context.WithTimeout(
			context.WithoutCancel(ctx),
			acpDeliveryDrainTimeout,
		)
		_ = agent.Close(closeContext)
		cancelClose()
	}
}

// Close 阻止新建 Agent，等待正在创建的条目并逐个释放资源。
func (backend *acpBackend) Close(ctx context.Context) error {
	backend.mutex.Lock()
	if backend.closing {
		entries := acpEntries(backend.entries)
		backend.mutex.Unlock()
		return closeACPEntries(ctx, entries)
	}
	backend.closing = true
	entries := acpEntries(backend.entries)
	backend.mutex.Unlock()
	return closeACPEntries(ctx, entries)
}

// acpEntries 返回不共享 map 的条目快照。
func acpEntries(entries map[string]*acpEntry) []*acpEntry {
	result := make([]*acpEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result
}

// closeACPEntries 等待创建屏障，并把任一 Agent 关闭失败收敛为稳定状态错误。
func closeACPEntries(
	ctx context.Context,
	entries []*acpEntry,
) error {
	failed := false
	for _, entry := range entries {
		select {
		case <-entry.ready:
		case <-ctx.Done():
			return runtimeContextStatus(ctx.Err())
		}
		if entry.agent != nil {
			if err := entry.agent.Close(ctx); err != nil {
				logx.Errorf("close ACP agent entry failed: error=%v", err)
				failed = true
			}
		}
	}
	if failed {
		return status_error.RuntimeShutdownFailed
	}
	return nil
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
	newServer := acpserver.New
	if config.UserInput {
		newServer = acpserver.NewWithUserInput
	}
	server, err := newServer(
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
	client := newACPClient(binding)
	promptWriter := newACPPromptWriter(clientToAgentWriter)
	closers := []io.Closer{clientToAgentReader, clientToAgentWriter, agentToClientReader, agentToClientWriter}
	connection, err := acp.NewClientSideConnectionWithOptions(
		client,
		promptWriter,
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
		diagnosticContext:    ctx,
		executable:           config.Executable,
		server:               server,
		serveContext:         serveContext,
		connection:           connection,
		binding:              binding,
		client:               client,
		promptWriter:         promptWriter,
		sessions:             make(map[acp.SessionId]string),
		sessionModes:         make(map[acp.SessionId]*acp.SessionModeState),
		sessionDefaultModels: make(map[acp.SessionId]acp.SessionConfigValueId),
		sessionModels:        make(map[acp.SessionId]acp.SessionConfigValueId),
		cancelServe:          cancelServe,
		closers:              closers,
		serveDone:            make(chan error, 1),
	}, nil
}

// Start 启动 ACP Server 并完成客户端 initialize 握手。
func (agent *embeddedACPAgent) Start(ctx context.Context) error {
	go func() {
		agent.serveDone <- agent.server.Serve(agent.serveContext)
		close(agent.serveDone)
	}()

	initializeResponse, err := agent.connection.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Meta: map[string]any{constants.RuntimeACPMetaKeyTerminalOutput: true},
			Elicitation: &acp.ElicitationCapabilities{
				Form: &acp.ElicitationFormCapabilities{},
				Url:  &acp.ElicitationUrlCapabilities{},
			},
		},
	})
	if err != nil {
		closeContext, cancelClose := context.WithTimeout(
			context.WithoutCancel(ctx),
			acpDeliveryDrainTimeout,
		)
		_ = agent.Close(closeContext)
		cancelClose()
		return err
	}
	agent.initializeResponse = initializeResponse
	acpAdapterLogger(agent.diagnosticContext).Info("ACP agent initialized", "agent_name", agent.binding.displayName, agent.binding.executableLogKey, agent.executable)
	return nil
}

// alive 返回 ACP Server 是否仍在运行。
func (agent *embeddedACPAgent) alive() bool {
	select {
	case <-agent.serveDone:
		return false
	default:
		return true
	}
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

// acpFailedResult 把连接和调用错误转换为 Runtime 稳定终态。
func acpFailedResult(
	request acpRunRequest,
	stage string,
	err error,
) Result {
	code := acpFailureCode(err)
	// 创建或握手尚未完成时，未知错误属于执行环境不可用，而非已开始的回复失败。
	if code == constants.RuntimeFailureCodeExecutionFailed && stage == constants.RuntimeACPFailureStageConnect {
		code = FailureCodeRuntimeUnavailable
	}
	logx.Errorf(
		"ACP invocation failed before delivery: runtime_id=%s invocation_id=%s stage=%s model=%s failure_code=%s error=%v",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		stage,
		request.Request.Policy.Model,
		code,
		err,
	)
	session := SessionResult{
		Mode:    request.Request.Session.Mode,
		InputID: request.Request.Session.ID,
	}
	session.State = finalSessionState(session, &Failure{Code: code})
	failure := newFailureFromError(code, stage, err)
	failure.SessionState = session.State
	return Result{
		TerminationReason: constants.RuntimeTerminationReasonUnknown,
		ExitCode:          -1,
		Session:           session,
		Facts:             acpInvocationFacts(request),
		Diagnostics:       append([]Diagnostic(nil), request.Prepared.detection.Diagnostics...),
		Failure:           failure,
	}
}

// acpFailureCode 提升 ACP 标准错误，其余错误按 Context 或 Runtime 分类。
func acpFailureCode(err error) FailureCode {
	if code, ok := ErrorCode(err); ok {
		return code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return constants.RuntimeFailureCodeRequestTimeout
	}
	if errors.Is(err, context.Canceled) {
		return FailureCodeContextCanceled
	}
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		if code := acpProviderFailureCode(requestError.Data); code != "" {
			return code
		}
		if code := classifyRuntimeErrorText(err.Error()); code != "" {
			return code
		}
		if code := classifyRuntimeErrorText(acpRequestErrorMessage(requestError.Data)); code != "" {
			return code
		}
		switch requestError.Code {
		case -32800:
			return FailureCodeContextCanceled
		case -32002:
			return FailureCodeSessionNotFound
		case -32000:
			return FailureCodeAuthRequired
		case -32602:
			return FailureCodeInvalidRequest
		case -32601:
			return FailureCodeProtocolIncompatible
		}
	}
	if code := classifyRuntimeErrorText(err.Error()); code != "" {
		return code
	}
	return constants.RuntimeFailureCodeExecutionFailed
}

// acpRequestErrorMessage 提取 ACP Invalid params 等错误中的可读说明。
func acpRequestErrorMessage(data any) string {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	message, _ := metadata["message"].(string)
	return message
}

// acpProviderFailureCode 读取 Runtime Adapter 依据 upstream 发布的开放错误类别。
func acpProviderFailureCode(data any) FailureCode {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	errorKind, _ := metadata[constants.RuntimeACPMetaKeyErrorKind].(string)
	switch errorKind {
	case constants.RuntimeACPProviderErrorKindNetworkError:
		return constants.RuntimeFailureCodeNetworkError
	case constants.RuntimeACPProviderErrorKindTimeout:
		return constants.RuntimeFailureCodeRequestTimeout
	case constants.RuntimeACPProviderErrorKindContextTooLarge:
		return constants.RuntimeFailureCodeContextTooLarge
	case constants.RuntimeACPProviderErrorKindAuthenticationFailed,
		constants.RuntimeACPProviderErrorKindOAuthOrgNotAllowed:
		return FailureCodeAuthRequired
	case constants.RuntimeACPProviderErrorKindPlanUpgradeRequired:
		return FailureCodePlanUpgradeRequired
	case constants.RuntimeACPProviderErrorKindQuotaExhausted:
		return FailureCodeQuotaExhausted
	case constants.RuntimeACPProviderErrorKindBillingError:
		return FailureCodeQuotaExhausted
	case constants.RuntimeACPProviderErrorKindRateLimit:
		return FailureCodeRateLimited
	case constants.RuntimeACPProviderErrorKindOverloaded,
		constants.RuntimeACPProviderErrorKindServerError:
		return FailureCodeUpstreamUnavailable
	case constants.RuntimeACPProviderErrorKindModelNotFound:
		return FailureCodeModelUnavailable
	case constants.RuntimeACPProviderErrorKindInvalidRequest:
		return FailureCodeInvalidRequest
	default:
		return ""
	}
}

// acpInvocationFacts 构造与 legacy Runtime 相同的可持久化调用事实。
func acpInvocationFacts(request acpRunRequest) InvocationFacts {
	model := request.Request.Policy.Model
	if model == "" {
		model = request.Prepared.definition.defaultModel
	}
	return InvocationFacts{
		PrefixArgs:             append([]string(nil), request.Request.Target.PrefixArgs...),
		ConfigHome:             request.Request.Target.ConfigHome,
		InvocationID:           request.InvocationID,
		RuntimeID:              request.Prepared.definition.id,
		BaseRuntimeID:          request.Prepared.definition.baseRuntimeID,
		Binary:                 request.Prepared.detection.ResolvedBinary,
		Version:                request.Prepared.detection.Version,
		Model:                  model,
		Reasoning:              request.Request.Policy.Reasoning,
		Policy:                 request.Request.Policy.Execution,
		EnvironmentFingerprint: request.Prepared.environment.fingerprint,
		Capabilities: cloneCapabilitySet(
			request.Prepared.detection.Capabilities,
		),
	}
}

var _ acpRunner = (*acpBackend)(nil)

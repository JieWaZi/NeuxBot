package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	acpDeliveryDrainTimeout = 5 * time.Second
)

// acpPromptResult 保存阻塞 Prompt 的唯一终态。
type acpPromptResult struct {
	// response 是 Agent 返回的 Prompt stop reason 和 Turn Usage。
	response acp.PromptResponse
	// err 是协议、Session 或 Context 错误。
	err error
}

// acpSessionOpenResult 描述本次调用取得原生 Session 的方式。
type acpSessionOpenResult struct {
	// sessionID 是后续配置和 Prompt 使用的 ACP Session 标识。
	sessionID acp.SessionId
	// action 是 created、resumed、reused 或 reopened 之一。
	action string
	// scopeFingerprint 是工作目录、额外目录和 MCP 挂载范围的不可逆摘要。
	scopeFingerprint string
	// configOptions 是本次新建会话实际返回的模型与推理配置。
	configOptions []acp.SessionConfigOption
}

// run 执行 Session 打开、配置、Prompt 和事件收尾。
func (agent *embeddedACPAgent) run(
	ctx context.Context,
	request acpRunRequest,
) Result {
	invocationStartedAt := time.Now()
	mcp, err := prepareACPMCPConnectors(
		agent.initializeResponse.AgentCapabilities.McpCapabilities,
		request.Request.Connectors,
	)
	if err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageMCP, err)
	}
	mcpServers, err := acpMCPServers(agent.binding, mcp.compatible)
	if err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageMCP, err)
	}
	prompt := agent.binding.selectedSkillPrompt(
		applySelectedConnectorHint(request.Prompt, mcp.selected),
		request.RuntimeFiles.selectedSkills,
	)
	blocks, err := acpPromptBlocks(
		prompt,
		request.Request.Message.Attachments,
		agent.initializeResponse.AgentCapabilities.PromptCapabilities.Image,
	)
	if err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStagePromptContent, err)
	}

	sessionOpenStartedAt := time.Now()
	openedSession, err := agent.openSession(ctx, request, mcpServers)
	if err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageSessionOpen, err)
	}
	sessionID := openedSession.sessionID
	logx.Infof(
		"ACP invocation session ready: runtime_id=%s invocation_id=%s session_id=%s "+
			"session_action=%s scope_fingerprint=%s working_directory=%s additional_directory_count=%d "+
			"mcp_server_count=%d stage_duration_ms=%d total_duration_ms=%d",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		sessionID,
		openedSession.action,
		openedSession.scopeFingerprint,
		request.Request.Workspace.Path,
		len(request.Request.Workspace.AdditionalWritableDirectories),
		len(mcpServers),
		time.Since(sessionOpenStartedAt).Milliseconds(),
		time.Since(invocationStartedAt).Milliseconds(),
	)
	releaseSession := true
	defer func() {
		if !releaseSession {
			return
		}
		closeContext, cancelClose := context.WithTimeout(
			context.WithoutCancel(ctx),
			acpDeliveryDrainTimeout,
		)
		_ = agent.closeSession(closeContext, sessionID)
		cancelClose()
	}()
	sessionConfigStartedAt := time.Now()
	var configErr error
	if request.Text != nil {
		configErr = agent.configureTextSession(ctx, sessionID, &request, openedSession.configOptions)
	} else {
		configErr = agent.configureSession(ctx, sessionID, request.Request)
	}
	if configErr != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageSessionConfig, configErr)
	}
	logx.Infof(
		"ACP invocation session configured: runtime_id=%s invocation_id=%s session_id=%s stage_duration_ms=%d total_duration_ms=%d",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		sessionID,
		time.Since(sessionConfigStartedAt).Milliseconds(),
		time.Since(invocationStartedAt).Milliseconds(),
	)

	promptContext, cancelPrompt := context.WithCancel(ctx)
	defer cancelPrompt()
	bridge := newACPBridge(
		sessionID,
		request.InvocationID,
		request.Sink,
		request.Request.Workspace,
		request.Request.Policy.Execution,
		agent.binding,
		request.Prepared.defaults.EventDelivery,
	)
	bridge.runtimeID = request.Request.Target.RuntimeID
	bridge.cancelPrompt = cancelPrompt
	bridge.promptDone = promptContext.Done()
	bridge.timingStartedAt = invocationStartedAt
	bridge.approvalHandler = request.Request.ApprovalHandler
	bridge.elicitationHandler = request.Request.ElicitationHandler
	bridge.userInputState = request.Request.UserInputState
	if request.Text != nil {
		bridge.rejectTextInteraction = request.Text.reject
		bridge.textCollector = request.Text.collector
	}
	if err := agent.client.register(bridge); err != nil {
		return acpFailedResult(request, constants.RuntimeACPFailureStageSessionActive, err)
	}
	for _, event := range mcp.events {
		if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
			agent.client.unregister(bridge)
			return acpFailedResult(request, constants.RuntimeACPFailureStageEventQueue, err)
		}
	}

	promptWritten, cleanupPromptWriter := agent.promptWriter.observe(string(sessionID))
	defer cleanupPromptWriter()
	watchContext, cancelWatch := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWatch()
	go bridge.watchInactivity(
		watchContext,
		request.Prepared.defaults.Inactivity,
		cancelPrompt,
	)
	promptStartedAt := time.Now()
	promptResult := make(chan acpPromptResult, 1)
	go func() {
		response, promptErr := agent.connection.Prompt(
			promptContext,
			acp.PromptRequest{
				SessionId: sessionID,
				Prompt:    blocks,
			},
		)
		promptResult <- acpPromptResult{
			response: response,
			err:      promptErr,
		}
	}()

	var result acpPromptResult
	started := false
	select {
	case writeErr := <-promptWritten:
		if writeErr != nil {
			cancelPrompt()
			result = <-promptResult
			agent.client.unregister(bridge)
			if result.err != nil {
				logACPPromptFailure(
					"ACP prompt failed after prompt write failure",
					request,
					sessionID,
					result.err,
				)
			}
			return acpFailedBridgeResult(
				request, bridge,
				constants.RuntimeACPFailureStagePromptWrite,
				writeErr,
			)
		}
		logACPPromptWritten(request, sessionID, promptStartedAt, invocationStartedAt)
		if err := startACPBridge(ctx, request, bridge); err != nil {
			cancelPrompt()
			result = <-promptResult
			agent.client.unregister(bridge)
			if result.err != nil {
				logACPPromptFailure(
					"ACP prompt failed after initial delivery failure",
					request,
					sessionID,
					result.err,
				)
			}
			return acpFailedBridgeResult(
				request, bridge,
				constants.RuntimeACPFailureStageInitialDelivery,
				err,
			)
		}
		started = true
		result = <-promptResult
	case result = <-promptResult:
		if result.err == nil {
			if err := startACPBridge(ctx, request, bridge); err != nil {
				agent.client.unregister(bridge)
				return acpFailedBridgeResult(request, bridge, constants.RuntimeACPFailureStageInitialDelivery, err)
			}
			started = true
			break
		}
		agent.client.unregister(bridge)
		return acpFailedBridgeResult(request, bridge, constants.RuntimeACPFailureStagePromptWrite, result.err)
	case <-ctx.Done():
		cancelPrompt()
		result = <-promptResult
		select {
		case writeErr := <-promptWritten:
			if writeErr == nil {
				logACPPromptWritten(request, sessionID, promptStartedAt, invocationStartedAt)
				startContext, cancelStart := context.WithTimeout(
					context.WithoutCancel(ctx),
					request.Prepared.defaults.EventDelivery,
				)
				startErr := startACPBridge(startContext, request, bridge)
				cancelStart()
				if startErr == nil {
					started = true
					break
				}
			}
		default:
		}
		if !started {
			agent.client.unregister(bridge)
			if result.err != nil {
				logACPPromptFailure(
					"ACP prompt failed after invocation context ended",
					request,
					sessionID,
					result.err,
				)
			}
			return acpFailedBridgeResult(
				request, bridge,
				constants.RuntimeACPFailureStagePrompt,
				runtimeContextStatus(ctx.Err()),
			)
		}
	}
	if ctx.Err() != nil && result.err != nil {
		logACPPromptFailure(
			"ACP prompt returned an error after invocation context ended",
			request,
			sessionID,
			result.err,
		)
		result.err = runtimeContextStatus(ctx.Err())
	}
	logx.Infof(
		"ACP invocation prompt completed: runtime_id=%s invocation_id=%s session_id=%s stage_duration_ms=%d total_duration_ms=%d failed=%t",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		sessionID,
		time.Since(promptStartedAt).Milliseconds(),
		time.Since(invocationStartedAt).Milliseconds(),
		result.err != nil,
	)
	if errors.Is(agent.connection.Err(), acp.ErrNotificationQueueLimit) {
		bridge.setFailure(status_error.RuntimeEventQueueOverflow)
	}
	// Prompt 终止已知后先冻结原因；队列仍可有界排空，后续交付超时只能作为次要诊断。
	if ctx.Err() != nil {
		bridge.recordPrimaryCause(runtimeContextStatus(ctx.Err()), constants.RuntimeACPFailureStagePrompt)
	} else if result.err != nil {
		bridge.recordPrimaryCause(result.err, constants.RuntimeACPFailureStagePrompt)
	} else if result.response.StopReason == acp.StopReasonCancelled {
		bridge.recordPrimaryCause(status_error.OperationCanceled, constants.RuntimeACPFailureStagePrompt)
	}

	if result.response.Usage != nil && bridge.textCollector != nil {
		bridge.textCollector.collectUsage(*result.response.Usage)
	} else if result.response.Usage != nil {
		usageEvent, usageErr := acpPromptUsageEvent(*result.response.Usage)
		if usageErr != nil {
			bridge.setFailure(usageErr)
		} else if enqueueErr := bridge.enqueue(acpDelivery{
			events: []Event{usageEvent},
		}); enqueueErr != nil {
			bridge.setFailure(enqueueErr)
		}
	}
	agent.client.unregister(bridge)
	closeContext, cancelClose := context.WithTimeout(
		context.WithoutCancel(ctx),
		acpDeliveryDrainTimeout,
	)
	deliveryErr := bridge.closeDelivery(closeContext)
	cancelClose()
	retainSession := agent.supportsPersistentSessions() &&
		!request.EphemeralSession && deliveryErr == nil &&
		(result.err == nil || errors.Is(result.err, context.Canceled) ||
			errors.Is(result.err, context.DeadlineExceeded) ||
			agent.binding.retainPromptFailure(agent.binding.failureCode(result.err)))
	var sessionCloseErr error
	if retainSession {
		releaseSession = false
	} else {
		sessionCloseContext, cancelSessionClose := context.WithTimeout(
			context.WithoutCancel(ctx),
			acpDeliveryDrainTimeout,
		)
		sessionCloseErr = agent.closeSession(sessionCloseContext, sessionID)
		cancelSessionClose()
		releaseSession = false
	}
	final := acpResult(
		request,
		sessionID,
		result,
		bridge,
		deliveryErr,
		sessionCloseErr,
	)
	if result.err == nil {
		final.Session.ForkMode = acpmeta.ForkMode(agent.initializeResponse.Meta)
		final.Session.ForkPosition = acpmeta.ForkPosition(result.response.Meta)
	}
	return final
}

// acpFailedBridgeResult 在启动屏障前也保留触发 Prompt 取消的首个交付失败。
func acpFailedBridgeResult(request acpRunRequest, bridge *acpBridge, stage string, err error) Result {
	if cause := bridge.currentFailure(); cause != nil {
		code, known := ErrorCode(cause)
		if !known {
			code = FailureCodeEventDelivery
		}
		return acpFailedResult(request, failureStageForCode(code), cause)
	}
	return acpFailedResult(request, stage, err)
}

// logACPPromptFailure 统一记录提示阶段的 Runtime 与会话上下文。
func logACPPromptFailure(message string, request acpRunRequest, sessionID acp.SessionId, err error) {
	logx.Errorf(
		message+": runtime_id=%s invocation_id=%s session_id=%s error=%v",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		sessionID,
		err,
	)
}

// supportsPersistentSessions 按当前 Runtime 自己的恢复语义判断是否保留热会话。
func (agent *embeddedACPAgent) supportsPersistentSessions() bool {
	return agent.binding.supportsPersistent(agent.initializeResponse.AgentCapabilities)
}

// startACPBridge 为普通聊天先交付首事件，再启动消费者；纯文本只复用消费者收尾。
func startACPBridge(
	ctx context.Context,
	request acpRunRequest,
	bridge *acpBridge,
) error {
	if request.Text == nil {
		if err := deliverACPInvocationStarted(ctx, request); err != nil {
			return err
		}
	}
	go bridge.run(context.WithoutCancel(ctx))
	bridge.activateDelivery()
	return nil
}

// openSession 创建或恢复 Runtime Session，并返回可靠 ACP Session ID。
func (agent *embeddedACPAgent) openSession(
	ctx context.Context,
	request acpRunRequest,
	mcpServers []acp.McpServer,
) (acpSessionOpenResult, error) {
	additional := append(
		[]string{},
		request.Request.Workspace.AdditionalWritableDirectories...,
	)
	scopeFingerprint, err := acpSessionScopeFingerprint(
		request.Request.Workspace.Path,
		additional,
		mcpServers,
	)
	if err != nil {
		return acpSessionOpenResult{}, status_error.RuntimeProtocolIncompatible
	}
	if request.Request.Session.Mode == SessionModeResume {
		if request.Request.Session.ID == "" {
			return acpSessionOpenResult{}, status_error.BadRequest
		}
		sessionID := acp.SessionId(request.Request.Session.ID)
		agent.sessionMutex.Lock()
		defer agent.sessionMutex.Unlock()
		if existingScope, exists := agent.sessions[sessionID]; exists {
			if existingScope == scopeFingerprint {
				return acpSessionOpenResult{
					sessionID:        sessionID,
					action:           "reused",
					scopeFingerprint: scopeFingerprint,
				}, nil
			}
			if _, closeErr := agent.connection.CloseSession(
				ctx,
				acp.CloseSessionRequest{SessionId: sessionID},
			); closeErr != nil {
				return acpSessionOpenResult{}, closeErr
			}
			delete(agent.sessions, sessionID)
			delete(agent.sessionModes, sessionID)
			response, err := agent.resumeSession(ctx, acp.ResumeSessionRequest{
				SessionId:             sessionID,
				Cwd:                   request.Request.Workspace.Path,
				AdditionalDirectories: additional,
				McpServers:            mcpServers,
			})
			if err != nil {
				return acpSessionOpenResult{}, err
			}
			agent.sessions[sessionID] = scopeFingerprint
			agent.rememberSessionModes(sessionID, response.Modes)
			agent.rememberDefaultModel(sessionID, response.ConfigOptions)
			return acpSessionOpenResult{
				sessionID:        sessionID,
				action:           "reopened",
				scopeFingerprint: scopeFingerprint,
			}, nil
		}
		response, err := agent.resumeSession(ctx, acp.ResumeSessionRequest{
			SessionId:             sessionID,
			Cwd:                   request.Request.Workspace.Path,
			AdditionalDirectories: additional,
			McpServers:            mcpServers,
		})
		if err != nil {
			return acpSessionOpenResult{}, err
		}
		agent.sessions[sessionID] = scopeFingerprint
		agent.rememberSessionModes(sessionID, response.Modes)
		agent.rememberDefaultModel(sessionID, response.ConfigOptions)
		return acpSessionOpenResult{
			sessionID:        sessionID,
			action:           "resumed",
			scopeFingerprint: scopeFingerprint,
		}, nil
	}
	response, err := agent.connection.NewSession(ctx, acp.NewSessionRequest{
		Cwd:                   request.Request.Workspace.Path,
		AdditionalDirectories: additional,
		McpServers:            mcpServers,
	})
	if err != nil {
		return acpSessionOpenResult{}, err
	}
	if response.SessionId == "" {
		return acpSessionOpenResult{}, status_error.RuntimeProtocolIncompatible
	}
	agent.sessionMutex.Lock()
	agent.sessions[response.SessionId] = scopeFingerprint
	agent.rememberSessionModes(response.SessionId, response.Modes)
	agent.rememberDefaultModel(response.SessionId, response.ConfigOptions)
	agent.sessionMutex.Unlock()
	return acpSessionOpenResult{
		sessionID:        response.SessionId,
		action:           "created",
		scopeFingerprint: scopeFingerprint,
		configOptions:    response.ConfigOptions,
	}, nil
}

// logACPPromptWritten 记录用户输入已经越过 ACP Client 到 Agent 的协议写入边界。
func logACPPromptWritten(
	request acpRunRequest,
	sessionID acp.SessionId,
	promptStartedAt time.Time,
	invocationStartedAt time.Time,
) {
	logx.Infof(
		"ACP invocation prompt written: runtime_id=%s invocation_id=%s session_id=%s stage_duration_ms=%d total_duration_ms=%d",
		request.Request.Target.RuntimeID,
		request.InvocationID,
		sessionID,
		time.Since(promptStartedAt).Milliseconds(),
		time.Since(invocationStartedAt).Milliseconds(),
	)
}

// acpSessionScopeFingerprint 对影响原生 Session 挂载范围的输入生成不可逆摘要。
func acpSessionScopeFingerprint(
	workingDirectory string,
	additionalDirectories []string,
	mcpServers []acp.McpServer,
) (string, error) {
	directories := append([]string(nil), additionalDirectories...)
	sort.Strings(directories)
	servers := make([]string, 0, len(mcpServers))
	for _, server := range mcpServers {
		encodedServer, err := json.Marshal(server)
		if err != nil {
			return "", err
		}
		servers = append(servers, string(encodedServer))
	}
	sort.Strings(servers)
	payload := struct {
		// WorkingDirectory 是 ACP Session 的当前工作目录。
		WorkingDirectory string `json:"workingDirectory"`
		// AdditionalDirectories 是 ACP Session 的额外授权目录。
		AdditionalDirectories []string `json:"additionalDirectories"`
		// MCPServers 是 ACP Session 当前挂载 MCP 服务的规范 JSON。
		MCPServers []string `json:"mcpServers"`
	}{
		WorkingDirectory:      workingDirectory,
		AdditionalDirectories: directories,
		MCPServers:            servers,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// closeSession 关闭仍由当前 Agent 持有的原生 Session，并移除复用记录。
func (agent *embeddedACPAgent) closeSession(
	ctx context.Context,
	sessionID acp.SessionId,
) error {
	agent.sessionMutex.Lock()
	if _, exists := agent.sessions[sessionID]; !exists {
		agent.sessionMutex.Unlock()
		return nil
	}
	delete(agent.sessions, sessionID)
	delete(agent.sessionModes, sessionID)
	delete(agent.sessionDefaultModels, sessionID)
	delete(agent.sessionDefaultReasoning, sessionID)
	delete(agent.sessionModels, sessionID)
	agent.sessionMutex.Unlock()
	_, err := agent.connection.CloseSession(ctx, acp.CloseSessionRequest{SessionId: sessionID})
	return err
}

// configureSession 先应用模型和推理强度，最后应用用户冻结的权限选择。
func (agent *embeddedACPAgent) configureSession(
	ctx context.Context,
	sessionID acp.SessionId,
	request RunRequest,
) error {
	request = agent.binding.resolveSelections(agent, sessionID, request)
	selections := []struct {
		// id 是当前 acp-go Adapter 暴露的配置标识。
		id acp.SessionConfigId
		// value 是本次调用选择的配置值。
		value string
	}{
		{id: agent.binding.modelConfigID, value: request.Policy.Model},
		{id: agent.binding.reasoningConfigID, value: request.Policy.Reasoning},
	}
	for _, selection := range selections {
		if selection.value == "" {
			continue
		}
		if agent.binding.skipSelection(agent, sessionID, selection.id, selection.value) {
			continue
		}
		response, err := agent.connection.SetSessionConfigOption(
			ctx,
			acp.SetSessionConfigOptionRequest{
				ValueId: &acp.SetSessionConfigOptionValueId{
					SessionId: sessionID,
					ConfigId:  selection.id,
					Value:     acp.SessionConfigValueId(selection.value),
				},
			},
		)
		if err != nil {
			return err
		}
		if err := agent.binding.validateSelection(agent, sessionID, request, selection.id, selection.value, response.ConfigOptions); err != nil {
			return err
		}
	}
	agent.sessionMutex.Lock()
	modes := agent.sessionModes[sessionID]
	agent.sessionMutex.Unlock()
	mode, err := agent.binding.modeID(request.Policy.Execution, modes)
	if err != nil {
		return err
	}
	if mode == "" {
		return nil
	}
	_, err = agent.connection.SetSessionMode(ctx, acp.SetSessionModeRequest{
		SessionId: sessionID,
		ModeId:    mode,
	})
	return err
}

// rememberDefaultModel 在 sessionMutex 保护下保存会话默认模型及各模型的官方默认思考值。
func (agent *embeddedACPAgent) rememberDefaultModel(sessionID acp.SessionId, options []acp.SessionConfigOption) {
	if agent.sessionDefaultModels == nil {
		agent.sessionDefaultModels = make(map[acp.SessionId]acp.SessionConfigValueId)
	}
	if agent.sessionModels == nil {
		agent.sessionModels = make(map[acp.SessionId]acp.SessionConfigValueId)
	}
	agent.sessionModels[sessionID] = acpConfigCurrentValue(options, agent.binding.modelConfigID)
	if _, exists := agent.sessionDefaultModels[sessionID]; !exists {
		agent.sessionDefaultModels[sessionID] = acpConfigCurrentValue(options, agent.binding.modelConfigID)
	}
	if agent.sessionDefaultReasoning == nil {
		agent.sessionDefaultReasoning = make(map[acp.SessionId]map[acp.SessionConfigValueId]acp.SessionConfigValueId)
	}
	if _, exists := agent.sessionDefaultReasoning[sessionID]; !exists {
		defaults := make(map[acp.SessionConfigValueId]acp.SessionConfigValueId)
		if model, ok := acpSelectConfig(options, agent.binding.modelConfigID); ok {
			for _, option := range acpSelectValues(model.Options) {
				if configs, known := acpmeta.ModelConfigOptions(option.Meta); known {
					defaults[option.Value] = acpConfigCurrentValue(configs, agent.binding.reasoningConfigID)
				}
			}
		}
		agent.sessionDefaultReasoning[sessionID] = defaults
	}
}

// acpConfigCurrentValue 读取 Agent 确认的配置值，避免将本地选择误当作生效结果。
func acpConfigCurrentValue(options []acp.SessionConfigOption, id acp.SessionConfigId) acp.SessionConfigValueId {
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id {
			return option.Select.CurrentValue
		}
	}
	return ""
}

// deliverACPInvocationStarted 保持现有权威提交前的首事件不变量。
func deliverACPInvocationStarted(
	ctx context.Context,
	request acpRunRequest,
) error {
	event, err := newEvent(EventTypeInvocationStarted, InvocationStartedData{
		InvocationID: request.InvocationID,
	})
	if err != nil {
		return err
	}
	timeout := request.Prepared.defaults.EventDelivery
	deliveryContext, cancelDelivery := context.WithTimeout(ctx, timeout)
	defer cancelDelivery()
	return request.Sink.Accept(deliveryContext, event)
}

// acpResult 合并 Prompt、事件交付、Session 和副作用终态。
func acpResult(
	request acpRunRequest,
	sessionID acp.SessionId,
	prompt acpPromptResult,
	bridge *acpBridge,
	deliveryErr error,
	sessionCloseErr error,
) Result {
	terminationReason := constants.RuntimeTerminationReasonUnknown
	switch prompt.response.StopReason {
	case acp.StopReasonEndTurn:
		terminationReason = constants.RuntimeTerminationReasonNormal
	case acp.StopReasonMaxTokens:
		terminationReason = constants.RuntimeTerminationReasonTruncated
	case acp.StopReasonMaxTurnRequests:
		terminationReason = constants.RuntimeTerminationReasonTurnLimit
	case acp.StopReasonRefusal:
		terminationReason = constants.RuntimeTerminationReasonRefusal
	case acp.StopReasonCancelled:
		terminationReason = constants.RuntimeTerminationReasonCancelled
	}
	result := Result{
		StopReason:        string(prompt.response.StopReason),
		TerminationReason: terminationReason,
		ExitCode:          0,
		Session: SessionResult{
			Mode:     request.Request.Session.Mode,
			InputID:  request.Request.Session.ID,
			OutputID: string(sessionID),
		},
		Facts:           acpInvocationFacts(request),
		ProtocolHealthy: prompt.err == nil,
		Diagnostics: append(
			[]Diagnostic(nil),
			request.Prepared.detection.Diagnostics...,
		),
		SideEffects: bridge.sideEffectSnapshot(),
	}
	primaryCause, primaryStage := bridge.primaryCauseSnapshot()
	if primaryCause == nil {
		switch {
		case deliveryErr != nil:
			primaryCause = deliveryErr
		case prompt.err != nil:
			primaryCause, primaryStage = prompt.err, constants.RuntimeACPFailureStagePrompt
		case prompt.response.StopReason == acp.StopReasonCancelled:
			primaryCause, primaryStage = status_error.OperationCanceled, constants.RuntimeACPFailureStagePrompt
		}
	}
	if primaryCause != nil {
		code := bridge.binding.failureCode(primaryCause)
		if primaryStage == "" {
			code = FailureCodeEventDelivery
			if classified, ok := ErrorCode(primaryCause); ok {
				code = classified
			} else if errors.Is(primaryCause, context.DeadlineExceeded) {
				code = constants.RuntimeFailureCodeRequestTimeout
			} else if errors.Is(primaryCause, context.Canceled) {
				code = FailureCodeContextCanceled
			}
			primaryStage = failureStageForCode(code)
		}
		result.Failure = newFailureFromError(code, primaryStage, primaryCause)
	}
	if deliveryErr != nil && !errors.Is(deliveryErr, primaryCause) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "event_delivery_after_termination", Level: DiagnosticLevelWarning, Message: FailureDisplayMessage(deliveryErr)})
	}
	if sessionCloseErr != nil {
		if result.Failure == nil {
			result.Failure = newFailureFromError(FailureCodeRuntimeUnavailable, constants.RuntimeACPFailureStageSessionClose, sessionCloseErr)
		} else {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Code: "session_close_failed", Level: DiagnosticLevelWarning, Message: FailureDisplayMessage(sessionCloseErr),
			})
		}
	}
	result.Session.State = finalSessionState(result.Session, result.Failure)
	result.SideEffects = bridge.sideEffectSnapshot()
	if result.Failure != nil {
		result.ExitCode = -1
		result.Failure.SessionState = result.Session.State
		result.Failure.SideEffects = result.SideEffects
	}
	return result
}

// acpPromptUsageEvent 为普通聊天生成 PromptResponse 的完整 Turn Usage 事件。
func acpPromptUsageEvent(usage acp.Usage) (Event, error) {
	return newEvent(EventTypeUsage, UsageData{Turn: acpPromptTurnUsage(usage)})
}

// acpPromptTurnUsage 为聊天与最终文本结果统一转换 PromptResponse 的完整累计用量。
func acpPromptTurnUsage(usage acp.Usage) *TurnUsageData {
	turn := &TurnUsageData{
		InputTokens:  int64(usage.InputTokens),
		OutputTokens: int64(usage.OutputTokens),
	}
	if usage.CachedReadTokens != nil {
		turn.CacheReadTokens = int64(*usage.CachedReadTokens)
	}
	if usage.CachedWriteTokens != nil {
		turn.CacheCreationTokens = int64(*usage.CachedWriteTokens)
	}
	if usage.ThoughtTokens != nil {
		turn.ReasoningOutputTokens = int64(*usage.ThoughtTokens)
	}
	return turn
}

// acpPromptBlocks 构造文本和可用本地图片内容。
func acpPromptBlocks(
	prompt string,
	attachments []Attachment,
	supportsImages bool,
) ([]acp.ContentBlock, error) {
	blocks := []acp.ContentBlock{acp.TextBlock(prompt)}
	if !supportsImages {
		return blocks, nil
	}
	for _, attachment := range attachments {
		if !strings.HasPrefix(attachment.MediaType, "image/") ||
			attachment.RuntimePath == "" {
			continue
		}
		data, err := os.ReadFile(attachment.RuntimePath)
		if err != nil {
			return nil, status_error.RuntimeUnavailable
		}
		blocks = append(blocks, acp.ImageBlock(
			base64.StdEncoding.EncodeToString(data),
			attachment.MediaType,
		))
	}
	return blocks, nil
}

// acpMCPServers 转换经过兼容性校验的 Connector。
func acpMCPServers(
	binding acpRuntimeBinding,
	connectors []Connector,
) ([]acp.McpServer, error) {
	servers := make([]acp.McpServer, 0, len(connectors))
	for _, connector := range connectors {
		switch connector.Transport {
		case ConnectorTransportStdio:
			environmentKeys := make([]string, 0, len(connector.Environment))
			for key := range connector.Environment {
				environmentKeys = append(environmentKeys, key)
			}
			sort.Strings(environmentKeys)
			environment := make([]acp.EnvVariable, 0, len(environmentKeys))
			for _, key := range environmentKeys {
				environment = append(environment, acp.EnvVariable{
					Name:  key,
					Value: connector.Environment[key],
				})
			}
			servers = append(servers, acp.McpServer{
				Stdio: &acp.McpServerStdio{
					Name:    connector.RuntimeKey,
					Command: connector.Command,
					Args:    append([]string{}, connector.Arguments...),
					Env:     environment,
				},
			})
		case ConnectorTransportStreamableHTTP:
			servers = append(servers, acp.McpServer{
				Http: &acp.McpServerHttpInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		case ConnectorTransportSSE:
			servers = append(servers, acp.McpServer{
				Sse: &acp.McpServerSseInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		default:
			return nil, status_error.SkillUnavailable
		}
	}
	return servers, nil
}

// acpHTTPHeaders 把 Connector header 转为顺序稳定的 ACP 列表。
func acpHTTPHeaders(values map[string]string) []acp.HttpHeader {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	headers := make([]acp.HttpHeader, 0, len(keys))
	for _, key := range keys {
		headers = append(headers, acp.HttpHeader{Name: key, Value: values[key]})
	}
	return headers
}

// resumeSession 根据上游能力选择恢复方法；加载历史发生在本次 Invocation 绑定前。
func (agent *embeddedACPAgent) resumeSession(ctx context.Context, request acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	if agent.initializeResponse.AgentCapabilities.SessionCapabilities.Resume != nil {
		return agent.connection.ResumeSession(ctx, request)
	}
	if !agent.initializeResponse.AgentCapabilities.LoadSession {
		return acp.ResumeSessionResponse{}, status_error.RuntimeProtocolIncompatible
	}
	loadRequest := acp.LoadSessionRequest{
		SessionId:             request.SessionId,
		Cwd:                   request.Cwd,
		AdditionalDirectories: request.AdditionalDirectories,
		McpServers:            request.McpServers,
	}
	response, err := agent.connection.LoadSession(ctx, loadRequest)
	return acp.ResumeSessionResponse{Meta: response.Meta, ConfigOptions: response.ConfigOptions, Modes: response.Modes}, err
}

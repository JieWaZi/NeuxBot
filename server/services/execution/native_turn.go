package execution

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/shrewx/ginx/pkg/statuserror"
	"go.opentelemetry.io/otel/trace"
)

// nativeResourceProjection 记录现有连接实际持有的交付资源。
type nativeResourceProjection struct {
	// plugins 记录实际交付的插件版本。
	plugins []plugins.Snapshot
	// connectors 记录实际物化的连接器集合。
	connectors []agent.Connector
	// updateUsage 设置或清除共享插件租约当前接纳的执行用途。
	updateUsage func(...authorization.Usage)
}

func nativeProvider(environment models.RuntimeEnvironment) string {
	if environment.BaseRuntimeID != "" {
		return environment.BaseRuntimeID
	}
	if environment.Detection.Runtime.BaseRuntimeID != "" {
		return environment.Detection.Runtime.BaseRuntimeID
	}
	return environment.RuntimeID
}

func nativeThreadRequest(chatID string, workspace agent.Workspace, instructions []agent.Instruction, request pendingMessage, environment models.RuntimeEnvironment, resources agent.ResourceSelection, key string) agent.ThreadRequest {
	accountID := request.runtime.AccountID
	if accountID == "" {
		accountID = agent.SystemAccountID
	}
	executable := environment.Detection.ResolvedBinary
	if executable == "" {
		executable = environment.Binary
	}
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			env[name] = value
		}
	}
	mode := "agent"
	if request.runtime.WorkspaceReadOnly || request.runtime.PermissionLevel == constants.PermissionLevelDefault {
		mode = "read-only"
	} else if request.runtime.PermissionLevel == constants.PermissionLevelFullAccess {
		mode = "agent-full-access"
	}
	digest := sha256.Sum256([]byte(key))
	// A workspace already supplied as cwd is not an additional native capability.
	additional := make([]string, 0, len(workspace.AdditionalWritableDirectories))
	for _, directory := range workspace.AdditionalWritableDirectories {
		if filepath.Clean(directory) != filepath.Clean(workspace.Path) {
			additional = append(additional, directory)
		}
	}
	if supported, known := agent.AdditionalDirectorySupport(environment.Detection); known && !supported {
		additional = nil
	}
	thread := agent.ThreadRequest{
		ChatID: chatID,
		Connection: agent.ProviderConnectionRequest{
			Account: agent.AccountRequest{Executable: executable, PrefixArgs: environment.PrefixArgs, Provider: nativeProvider(environment), AccountID: accountID, CWD: workspace.Path, Model: request.runtime.Model},
			Launch: agent.ProviderLaunch{
				Executable: executable, PrefixArgs: environment.PrefixArgs, ConfigHome: environment.ConfigHome, BaseEnvironment: env,
				CodexConfig: map[string]json.RawMessage{
					"features.goals": json.RawMessage("false"),
				},
			},
			Resources: resources,
		},
		Session: agent.AgentSessionOptions{AdditionalDirectories: additional},
		Mode:    mode, ResourceGeneration: binary.BigEndian.Uint64(digest[:8]),
		ForceNew:     request.runtime.SessionPolicy == constants.RuntimeSessionPolicyNew,
		Summarize:    request.runtime.HistoryMode == constants.HistoryModeSummary,
		HistoryTurns: request.runtime.HistoryTurns,
	}
	thread.Selection = &agent.TurnSelection{Reasoning: request.runtime.Reasoning, Fast: request.runtime.Fast, ContextWindow: request.runtime.ContextWindow}
	defaultAccountID := environment.DefaultAccountID
	if defaultAccountID == "" {
		defaultAccountID = agent.SystemAccountID
	}
	if !request.runtime.ConfigurationResolved && accountID == defaultAccountID {
		thread.Selection.DefaultFast = environment.DefaultFast
	}
	switch thread.Connection.Account.Provider {
	case constants.RuntimeIDClaude, constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder:
		thread.Mode = "default"
		if request.runtime.PermissionLevel == constants.PermissionLevelAuto {
			thread.Mode = "auto"
		}
		if request.runtime.PermissionLevel == constants.PermissionLevelFullAccess {
			thread.Mode = "bypassPermissions"
			if thread.Connection.Account.Provider == constants.RuntimeIDQoder {
				thread.Mode = "yolo"
			}
		}
	case constants.RuntimeIDCursor:
		// Cursor's ACP policy is process-scoped; it is not a session task mode.
		thread.Mode = ""
		thread.Connection.Launch.CursorPermissionMode = "default"
		if request.runtime.PermissionLevel == constants.PermissionLevelAuto {
			thread.Connection.Launch.CursorPermissionMode = "auto"
		}
		if request.runtime.PermissionLevel == constants.PermissionLevelFullAccess {
			thread.Connection.Launch.CursorPermissionMode = "full-access"
		}
	case constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
		thread.Mode = ""
		thread.Connection.Launch.ProcessPermissionMode = string(request.runtime.PermissionLevel)
	}
	provider := thread.Connection.Account.Provider
	if accountID != agent.SystemAccountID && (provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi) {
		// The native provider namespace is transport detail; account validation and
		// public choices continue to use the user's configured model IDs.
		thread.Connection.Account.Model = accountconfig.NativeModelID(provider, request.runtime.Model)
	}

	if instructions := agent.DeveloperInstructions(instructions); instructions != "" {
		thread.Session.DeveloperInstructions = &instructions
	}
	return thread
}

// trustedWorkbenchEntryPoint 核对内部入口和冻结的对话归属，排除同为 user 的其他来源。
func trustedWorkbenchEntryPoint(input pendingMessage, state messageChat) string {
	chat := state.chat
	if input.entryPoint != WorkbenchEntryPoint || input.message.Actor != "user" || input.message.Workflow != nil ||
		input.imReplyLinkID != "" || input.automaticRunID != "" || input.workflowCreationSessionID != "" ||
		input.parentExecutionID != "" || state.threadWorkspaceID != "" || chat.HiddenFromWakerHistory || state.profileOptimization ||
		chat.WakerID == "" || chat.ID == requestidempotency.WakerOnboardingChatID(chat.WakerID) {
		return ""
	}
	return WorkbenchEntryPoint
}

// ConversationRuleTeamEntryPoint 标识用户直接提交给团队负责人的回合。
const ConversationRuleTeamEntryPoint = "team-rule-management"

func allowsTeamRuleManagement(input pendingMessage, state messageChat) bool {
	return state.threadWorkspaceID != "" && input.message.Actor == string(constants.ChannelMessageActorUser) &&
		input.imReplyLinkID == "" && input.automaticRunID == "" && input.parentExecutionID == "" &&
		input.message.Workflow == nil && input.entryPoint == ""
}

// nativeUsageSource classifies the host entry point from admission evidence.
func nativeUsageSource(input pendingMessage, state messageChat) string {
	switch input.message.Actor {
	case "workflow":
		return "workflow"
	case constants.AutomaticTaskActor:
		return "automatic"
	case string(constants.ChannelMessageActorLead), string(constants.ChannelMessageActorMemberReport), string(constants.ChannelMessageActorSystem):
		return "team"
	}
	if input.automaticRunID != "" {
		return "automatic"
	}
	if state.threadWorkspaceID != "" {
		return "team"
	}
	if !state.chat.HiddenFromWakerHistory {
		return "chat"
	}
	return ""
}

// nativeTurnFacts projects references as data owned by the current input.
func nativeTurnFacts(message agent.Message, contextInstructions []agent.Instruction, selectedSkills []string) []agent.TurnFact {
	var facts []agent.TurnFact
	var attachments []map[string]string
	for _, attachment := range message.Attachments {
		attachments = append(attachments, map[string]string{"name": attachment.Name, "path": attachment.RuntimePath})
	}
	if len(attachments) != 0 {
		facts = append(facts, agent.TurnFact{Name: "Attachments", Value: attachments})
	}
	if len(message.CodeReferences) != 0 {
		facts = append(facts, agent.TurnFact{Name: "Code references", Value: message.CodeReferences})
	}
	if len(selectedSkills) > 0 {
		facts = append(facts, agent.TurnFact{Name: "Selected skills", Value: selectedSkills})
	}
	for _, instruction := range contextInstructions {
		var value any = instruction.Content
		if encoded := json.RawMessage(instruction.Content); json.Valid(encoded) {
			value = encoded
		}
		facts = append(facts, agent.TurnFact{Name: instruction.Name, Value: value})
	}
	return facts
}

// nativeTurnInput includes bounded native image data when the detected runtime advertises support.
func nativeTurnInput(id string, message agent.Message, contextInstructions []agent.Instruction, selectedSkills []string, detection agent.DetectionResult) (agent.TurnRequest, error) {
	request := agent.TurnRequest{HostInvocationID: id, ExecutionID: id, AssistantMessageID: id, Text: message.Text,
		Context: nativeTurnFacts(message, contextInstructions, selectedSkills)}
	if !detection.Capabilities[agent.CapabilityImagePath] {
		return request, nil
	}
	const attachmentLimit = 20 << 20
	for _, attachment := range message.Attachments {
		if !strings.HasPrefix(attachment.MediaType, "image/") {
			continue
		}
		file, err := os.Open(attachment.RuntimePath)
		if err != nil {
			return agent.TurnRequest{}, status_error.AgentInputInvalid
		}
		data, readErr := io.ReadAll(io.LimitReader(file, attachmentLimit+1))
		if err := errors.Join(readErr, file.Close()); err != nil || len(data) > attachmentLimit {
			return agent.TurnRequest{}, status_error.AgentInputInvalid
		}
		if request.Text != "" {
			request.Content = []agent.AgentContent{{Kind: "text", Text: request.Text}}
			request.Text = ""
		}
		request.Content = append(request.Content, agent.AgentContent{Kind: "image", MIMEType: attachment.MediaType,
			Data: base64.StdEncoding.EncodeToString(data)})
	}
	return request, nil
}

func (m *service) startNativeExecution(
	ctx, executionContext context.Context,
	state messageChat, request pendingMessage, executionID, userMessageID string,
	files *preparedMessageFiles, enabledSkills []models.SkillWithState, skillCatalogUnavailable bool, enabledConnectors []models.MCPConnector,
	environment models.RuntimeEnvironment, frozen frozenPluginResources, releaseReservation *bool,
) (AcceptedExecution, error) {
	journal, ok := m.journal.(chatservice.NativeJournal)
	if !ok || m.threads == nil {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}

	workspace := nativeWorkspace(state, request, files)
	skills := m.nativeSkills(enabledSkills)
	configuredConnectors := runtimeConnectorDeclarations(enabledConnectors)
	connectors := configuredConnectors
	instructions, contextInstructions := nativeInstructionInputs(request, files)
	message := nativeMessage(request, files)
	agentRequest := request
	agentRequest.entryPoint = trustedWorkbenchEntryPoint(request, state)
	if allowsTeamRuleManagement(request, state) {
		agentRequest.entryPoint = ConversationRuleTeamEntryPoint
	}
	if (request.entryPoint == MainConversationEntryPoint || request.entryPoint == MainConversationSourceEntryPoint) &&
		request.message.Actor == string(constants.MessageRoleUser) && request.automaticRunID == "" &&
		request.imReplyLinkID == "" && request.parentExecutionID == "" && request.message.Workflow == nil {
		// 能力绑定仍需验证这是主体的固定普通工作 Chat。
		agentRequest.entryPoint = request.entryPoint
	}
	// 接纳前没有执行日志，通过请求追踪和稳定错误码定位失败边界。
	logPreparationFailure := func(stage string, err error) {
		logx.Warnf("native execution preparation failed: stage=%s trace_id=%s chat_id=%s execution_id=%s runtime_id=%s error_code=%s",
			stage, trace.SpanContextFromContext(ctx).TraceID(), state.chat.ID, executionID, environment.RuntimeID, util.ErrorCode(err, "unknown"))
	}
	grant, err := m.prepareAgent(ctx, executionContext, environment, workspace, &skills, &instructions, &connectors, &contextInstructions, state.chat, executionID, agentRequest, files.referencedFiles, files.attachments.RuntimePaths)
	if err != nil {
		logPreparationFailure("prepare_agent_connection", err)
		return AcceptedExecution{}, err
	}
	if grant.finish != nil {
		defer func() {
			if *releaseReservation {
				grant.finish()
			}
		}()
	}

	selection := agent.ResourceSelection{Plugins: make(map[string]string, len(frozen.declarations))}
	for _, item := range frozen.declarations {
		selection.Plugins[item.ID] = item.VersionID
	}
	for _, connector := range connectors {
		selection.ConnectorIDs = append(selection.ConnectorIDs, connector.ID)
	}
	slices.Sort(selection.ConnectorIDs)
	pluginSkills, err := m.selectNativePluginSkills(ctx, frozen, skills...)
	if err != nil {
		return AcceptedExecution{}, err
	}
	selectedSkills := mergePluginSkills(skills, pluginSkills)
	selection.Skills, err = agent.FreezeOptionalSkills(environment.Detection, workspace, selectedSkills, selection.Plugins, state.chat.WakerID, skillCatalogUnavailable)
	if err != nil {
		return AcceptedExecution{}, err
	}

	// 内部 MCP 的启动参数由宿主冷连接交付，只有公开工具集合参与历史兼容。
	resourceFacts := []any{instructions, configuredConnectors, workspace, frozen.native}
	if len(grant.mcpOperations) > 0 {
		resourceFacts = append(resourceFacts, grant.mcpOperations)
	}
	metadata, err := json.Marshal(resourceFacts)
	if err != nil {
		return AcceptedExecution{}, status_error.RuntimeConfigurationInvalid
	}
	key := sessionResourceKey(frozen.key, string(metadata), enabledConnectors)
	thread := nativeThreadRequest(state.chat.ID, workspace, instructions, request, environment, selection, key)
	thread.ReplaceRejectedSession = (request.admission == constants.ExecutionAdmissionCommitBeforeDispatch ||
		request.admission == constants.ExecutionAdmissionCommitAtQueuedNativeGate) &&
		state.chat.HiddenFromWakerHistory && request.message.Actor == "user" &&
		(request.entryPoint == MainConversationEntryPoint || request.entryPoint == MainConversationSourceEntryPoint)
	if agentRequest.entryPoint == WorkbenchEntryPoint {
		thread.CoreMemoryOwner = state.chat.WakerID
	}
	thread.Connection.Launch.BotCLIContextFile = grant.cliContextFile
	thread.Connection.Launch.BotCLIExecutable = grant.cliExecutable
	thread.Connection.Account, err = m.resolveAccount(ctx, thread.Connection.Account, thread.Connection.Launch.ConfigHome)
	if err != nil {
		logPreparationFailure("resolve_account", err)
		return AcceptedExecution{}, err
	}

	if thread.Connection.Account.ConfigHome != "" {
		thread.Connection.Launch.ConfigHome = thread.Connection.Account.ConfigHome
	}

	work := nativeExecutionWork{
		chat: state.chat, state: state, files: files, journal: journal,
		input: request, execution: newExecutionIndex(state, request, executionID, userMessageID),
		cliContextFile: grant.cliContextFile, activate: grant.activate, finishToolScope: grant.finish, started: make(chan executionStartResult, 1),
	}
	if grant.singleExecution {
		work.cliContextFile = ""
	}
	ports := work.ports(m)
	ports.AcquireResources = func(ctx context.Context, selection agent.ResourceSelection) (agent.ResourceLease, error) {
		return m.acquireNativeResources(ctx, state.chat.ID, state.chat.WakerID, workspace, environment.Detection, skills, skillCatalogUnavailable, connectors, enabledConnectors, frozen, selection)
	}
	nativeInput, err := nativeTurnInput(executionID, message, contextInstructions, selectedSkillNames(request, enabledSkills), environment.Detection)
	if err != nil {
		return AcceptedExecution{}, err
	}
	if reply := request.replyTarget; reply != nil {
		nativeInput.Context = append(nativeInput.Context, agent.TurnFact{Name: constants.ChatReplyTargetContextName, Value: struct {
			// Role 是所引用消息作者的公开角色。
			Role constants.MessageRole `json:"role"`
			// SourceWakerID 指向团队内报告或答复的实际伙伴。
			SourceWakerID string `json:"sourceWakerId,omitempty"`
			// Text 是本轮可参考的公开正文摘录。
			Text string `json:"text"`
			// Truncated 表示摘录省略了原正文的一部分。
			Truncated bool `json:"truncated,omitempty"`
		}{Role: reply.Reference.Role, SourceWakerID: reply.Reference.SourceWakerID, Text: reply.Text, Truncated: reply.Truncated}})
	}
	nativeInput.UsageWakerID = state.chat.WakerID
	nativeInput.UsageSource = nativeUsageSource(request, state)
	if thread.CoreMemoryOwner != "" && m.coreMemory != nil {
		memoryCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		snapshot, err := m.coreMemory(memoryCtx, thread.CoreMemoryOwner)
		cancel()
		if err == nil && snapshot.Revision != 0 {
			nativeInput.CoreMemory = &agent.CoreMemoryContext{Revision: snapshot.Revision, JSON: snapshot.JSON}
		} else if err != nil && ctx.Err() == nil {
			logx.Warnf("read core memory snapshot: chat_id=%s error=%v", state.chat.ID, err)
		}
	}
	turn, err := m.threads.Prepare(ctx, thread, nativeInput, ports)
	if err != nil {
		logPreparationFailure("prepare_thread", err)
		return AcceptedExecution{}, err
	}
	defer func() {
		if *releaseReservation {
			if err := turn.Close(context.WithoutCancel(ctx)); err != nil {
				logx.Errorf("close unstarted native turn: execution_id=%s error=%v", executionID, err)
			}
		}
	}()
	if thread.Selection != nil {
		request.runtime.Fast = turn.RequestedFast()
		work.input.runtime.Fast = request.runtime.Fast
		work.execution.Fast = request.runtime.Fast
	}
	rawResources, exists := m.nativeResources.Load(state.chat.ID)
	if !exists {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	work.resources = rawResources.(*nativeResourceProjection)
	selectedConnectors, ok := selectedConnectorNames(request.message.ConnectorIDs, work.resources.connectors)
	if !ok {
		return AcceptedExecution{}, status_error.BadRequest
	}
	if len(selectedConnectors) != 0 {
		if err := turn.AddContext(agent.TurnFact{Name: "Selected connectors", Value: selectedConnectors}); err != nil {
			return AcceptedExecution{}, err
		}
	}
	for _, id := range request.message.PluginIDs {
		index := slices.IndexFunc(work.resources.plugins, func(snapshot plugins.Snapshot) bool { return snapshot.ID == id })
		if index < 0 {
			return AcceptedExecution{}, status_error.PluginNotReady
		}
		work.selectedPlugins = append(work.selectedPlugins, work.resources.plugins[index])
	}
	if len(work.selectedPlugins) != 0 {
		if err := turn.AddContext(agent.TurnFact{Name: "Selected plugins", Value: pluginSelectionFacts(work.selectedPlugins, work.resources.connectors)}); err != nil {
			return AcceptedExecution{}, err
		}
	}
	work.execution.PluginResources = slices.Clone(work.resources.plugins)
	work.skills = selectedSkillReferences(request.message.SkillIDs, enabledSkills)
	work.connectors = selectedConnectorReferences(request.message.ConnectorIDs, work.resources.connectors)

	// A reliable entrypoint commits the pending job before dispatch. The native admission
	// batch then adds execution.started and the actual input identity together.
	// 只传递链路标识；后台执行的取消和超时仍归执行注册表管理。
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		executionContext = trace.ContextWithSpanContext(executionContext, span)
	}
	if request.admission == constants.ExecutionAdmissionCommitBeforeDispatch {
		var accepted chatservice.AcceptedTurn
		err := request.admissionBoundary.Commit(ctx, func(ctx context.Context) error {
			var err error
			accepted, err = journal.CommitAcceptedTurn(ctx, work.admission(""))
			return err
		})
		if err != nil && accepted.Execution.ID == "" {
			return AcceptedExecution{}, err
		}
		files.commit()
		work.chat, work.execution, work.precommitted = accepted.Chat, accepted.Execution, true
		if err != nil {
			// 输入已进入权威日志。保留附件，将未派发的执行写成失败终态，原键重试只恢复回执。
			m.finishExecution(work.chat, work.execution, work.input.toolCapabilities, agent.TurnResult{}, eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusFailed, ErrorCode: util.ErrorCode(err, status_error.RuntimeExecutionFailed.Key()),
				FailureCode: constants.RuntimeFailureCodeEventDelivery, ErrorMessageSource: constants.ErrorMessageSourceLocalized,
			})
			return AcceptedExecution{}, err
		}
		*releaseReservation = false
		m.executions.launch(state.chat.ID, executionID, executionContext, func(ctx context.Context) { m.runNativeExecution(ctx, &work, turn) })
		return AcceptedExecution{Chat: accepted.Chat, Execution: accepted.Execution}, nil
	}
	*releaseReservation = false
	m.executions.launch(state.chat.ID, executionID, executionContext, func(ctx context.Context) { m.runNativeExecution(ctx, &work, turn) })
	select {
	case result := <-work.started:
		return AcceptedExecution{Chat: result.chat, Execution: result.execution}, result.err
	case <-ctx.Done():
		m.executions.cancelNativeExecution(state.chat.ID, executionID)
		result := <-work.started
		return AcceptedExecution{Chat: result.chat, Execution: result.execution}, result.err
	}
}

func (m *service) acquireNativeResources(
	ctx context.Context, chatID, wakerID string, workspace agent.Workspace, detection agent.DetectionResult, skills []agent.Skill, skillCatalogUnavailable bool, connectors []agent.ConnectorDeclaration,
	baseConnectors []models.MCPConnector, frozen frozenPluginResources, selection agent.ResourceSelection,
) (agent.ResourceLease, error) {
	materialized, err := m.acquirePluginResources(ctx, frozen)
	projection := &nativeResourceProjection{}
	var closeViews func(context.Context) error
	lease := agent.ResourceLease{Close: func(context.Context) error {
		m.nativeResources.CompareAndDelete(chatID, projection)
		if materialized.release != nil {
			materialized.release()
		}
		return nil
	}, CloseFiles: func(ctx context.Context) error {
		if closeViews != nil {
			if err := closeViews(ctx); err != nil {
				return err
			}
		}
		if materialized.releaseTargets != nil {
			materialized.releaseTargets()
		}
		return nil
	}}
	if err != nil {
		return lease, err
	}
	lease.NativePlugins, closeViews, err = agent.AcquirePluginViews(ctx, workspace, materialized.targets)
	if err != nil {
		return lease, err
	}
	projection.plugins = materialized.snapshots
	projection.updateUsage = materialized.updateUsage
	skills = mergePluginSkills(skills, materialized.skills)

	// 实际插件租约取得后再次确认精确内容；ProviderRuntime 持有目标预约并负责发布。
	if selection.Skills.Retained() {
		lease.Skills = selection.Skills
	} else {
		lease.Skills, err = agent.FreezeOptionalSkills(detection, workspace, skills, selection.Plugins, wakerID, skillCatalogUnavailable)
		if err != nil {
			return lease, err
		}
	}

	if m.connectorController != nil && len(baseConnectors) > 0 {
		baseConnectors, err = m.connectorController.AcquireEnabled(ctx, wakerID, baseConnectors)
		if err != nil {
			return lease, err
		}
	}
	projection.connectors = runtimeConnectorResources(append(slices.Clone(baseConnectors), materialized.connectors...))
	for _, declaration := range connectors {
		if declaration.ID == "neuxbot-internal-capabilities" {
			projection.connectors = append(projection.connectors, agent.Connector{ID: declaration.ID, Name: declaration.Name,
				RuntimeKey: declaration.RuntimeKey, Transport: declaration.Transport, Command: declaration.Command, Arguments: declaration.Arguments})
		}
	}
	for _, connector := range projection.connectors {
		name := connector.RuntimeKey
		if name == "" {
			name = connector.Name
		}
		transport := string(connector.Transport)
		if connector.Transport == constants.RuntimeConnectorTransportStreamableHTTP {
			transport = "http"
		}
		server := agent.AgentMCPServer{Name: name, Transport: transport,
			Command: connector.Command, Arguments: connector.Arguments, Environment: connector.Environment, URL: connector.URL, Headers: connector.Headers}
		if route, ok := materialized.hostedRoutes[connector.ID]; ok {
			server.URL = route.URL
			server.Headers = map[string]string{"Authorization": "Bearer " + route.Bearer}
		}
		lease.MCPServers = append(lease.MCPServers, server)
	}
	for index := range projection.connectors {
		projection.connectors[index].Environment = nil
		projection.connectors[index].Headers = nil
	}

	lease.Environment = materialized.environment
	m.nativeResources.Store(chatID, projection)
	return lease, nil
}

// nativeExecutionWork contains the business state carried across the native admission boundary.
type nativeExecutionWork struct {
	// journal owns all durable Chat business facts and public projections.
	journal chatservice.NativeJournal
	// state freezes the Chat before its current admission.
	state messageChat
	// chat is the current committed business projection after acceptance.
	chat models.Chat
	// execution holds the pending or admitted execution projection.
	execution models.Execution
	// input is the frozen business input, without native prompt additions.
	input pendingMessage
	// files remain reversible until their first business fact commits.
	files *preparedMessageFiles
	// skills are the current message's selected skills.
	skills []eventlog.MessageSkillReference
	// connectors are the current message's selected connectors.
	connectors []eventlog.MessageConnectorReference
	// selectedPlugins are the current message's selected plugin versions.
	selectedPlugins []plugins.Snapshot
	// resources is the native connection's acquired resource projection.
	resources *nativeResourceProjection
	// cliContextFile 是当前回合的常驻 CLI 连接文件，内部 MCP 分支不设置此路径。
	cliContextFile string
	// activate enables a grant only after durable admission and projection.
	activate func()
	// finishToolScope 结束本轮工具范围。
	finishToolScope func()
	// started reports the first durable admission or pre-admission failure to Start.
	started chan executionStartResult
	// precommitted records a reliable entrypoint's durable pending execution.
	precommitted bool
	// projected ensures body observers never wake ahead of business admission.
	projected atomic.Bool
	// admissionReceipt records the first durable admission callback for canceled authorization.
	admissionReceipt atomic.Pointer[agent.CommitReceipt]
	// admissionFacts are prepared by Admit before any durable observation is called.
	admissionFacts chatservice.StartedTurn
	// recording captures workspace changes around the admitted turn.
	recording *chatservice.FileChangesRecording
}

func (work *nativeExecutionWork) admission(invocationID string) chatservice.StartedTurn {
	admission := chatservice.StartedTurn{NewChat: work.state.isNew && !work.precommitted, Chat: work.chat, Execution: work.execution}
	if !work.precommitted {
		admission.Facts = startedMessageEvents(work.state, work.input, work.execution, invocationID,
			work.files.attachmentReferences, work.files.codeReferences, work.skills, work.connectors,
			slices.Concat(work.selectedPlugins, work.input.messagePlugins))
	}
	if invocationID != "" {
		admission.Facts = append(admission.Facts, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted,
			ExecutionID: work.execution.ID, InvocationID: invocationID})
	}
	return admission
}

func (work *nativeExecutionWork) ports(m *service) agent.TurnPorts {
	chatID, executionID := work.chat.ID, work.execution.ID
	var live *chatservice.NativeLiveMessage
	ports := agent.TurnPorts{
		Admit: func(ctx context.Context, admitted agent.TurnAdmission) (agent.ConversationBatch, error) {
			work.admissionFacts = work.admission(admitted.Input.HostInvocationID)
			return work.journal.PrepareTurnAdmission(ctx, work.admissionFacts, "turn:"+admitted.Input.HostInvocationID)
		},
		Authorize: func(ctx context.Context, admitted agent.TurnAdmission, receipt agent.CommitReceipt) (func(), error) {
			if err := work.projectAdmission(context.WithoutCancel(ctx), receipt, m.events); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if session := admitted.Binding.Session; session.Provider == constants.RuntimeIDCodex && work.cliContextFile != "" {
				if err := m.agent.BindCodexShellConnection(work.cliContextFile, executionID, session.SessionID); err != nil {
					if !errors.Is(err, status_error.AgentContextStorageError) {
						return nil, err
					}
					logx.Warnf("prepare Codex shell capability lookup: execution_id=%s error=%v", executionID, err)
				}
			}
			// 接纳批次的最后一项业务事实是 execution.started。
			startedRevision := receipt.First + int64(len(work.admissionFacts.Facts)) - 1
			live = work.journal.BeginNativeLive(chatID, executionID, startedRevision)
			if work.activate != nil {
				work.activate()
			}
			if work.resources.updateUsage != nil {
				work.resources.updateUsage(authorization.Usage{ChatID: chatID, ExecutionID: executionID,
					WakerID: work.chat.WakerID, Title: work.chat.Title})
			}
			return work.finishToolScope, nil
		},
		Committed: func(receipt agent.CommitReceipt) {
			if work.projected.Load() {
				work.journal.ObserveCommitted(chatID, receipt)
			} else {
				work.admissionReceipt.CompareAndSwap(nil, &receipt)
			}
		},
		AppendLive: func(change agent.MessageChange) {
			if live != nil {
				live.Append(change)
			}
		},
		CheckpointLive: func(ctx context.Context, receipt agent.CommitReceipt, change agent.MessageChange) {
			if live == nil {
				work.journal.ObserveCommitted(chatID, receipt)
				return
			}
			live.Checkpoint(ctx, receipt, change)
		},
		FinishLive: func() {
			if live != nil {
				live.Close()
			}
		},
		Input: func(ctx context.Context, _ agent.AgentScope, input agent.AgentInputRequest) (agent.AgentInputResponse, error) {
			if work.input.message.Actor == constants.WakerOnboardingActor && m.onboardingCommands != nil {
				err := m.checkWakerOnboardingProgress(ctx, work.chat, false)
				if errors.Is(err, status_error.WakerOnboardingInterviewRequired) {
					return agent.AgentInputResponse{Action: "cancel"}, nil
				}
				if err != nil {
					return agent.AgentInputResponse{Action: "cancel"}, err
				}
			}
			return nativeInput(ctx, m.elicitations.handler(chatID, executionID), executionID, input)
		},
	}
	if work.input.entryPoint == MainConversationEntryPoint || work.input.entryPoint == MainConversationSourceEntryPoint || work.input.watchdogTicket != nil {
		authorize := ports.Authorize
		ports.Authorize = func(ctx context.Context, admitted agent.TurnAdmission, receipt agent.CommitReceipt) (func(), error) {
			revoke, err := authorize(ctx, admitted, receipt)
			if err == nil {
				var guard func(context.Context, func() error) error
				if ticket := work.input.watchdogTicket; ticket != nil {
					guard = func(ctx context.Context, stop func() error) error {
						return ticket.tickets.WhileExecutionCurrent(ctx, ticket.binding, stop)
					}
				}
				m.executions.startWatchdog(chatID, executionID, guard)
			}
			return revoke, err
		}
		ports.Observe = func(_ context.Context, _ agent.AgentScope, event agent.AgentEvent) error {
			m.executions.observeWatchdog(chatID, executionID, event)
			return nil
		}
		ports.PromptReturned = func() { m.executions.finishWatchdog(chatID, executionID) }
	}
	if work.input.admission == constants.ExecutionAdmissionCommitAtQueuedNativeGate {
		ports.CommitAdmission = func(ctx context.Context, commit func(context.Context) error) error {
			return work.input.admissionBoundary.Commit(ctx, func(ctx context.Context) error {
				if err := work.input.beforeQueuedNativeCommit(ctx); err != nil {
					return err
				}
				return commit(ctx)
			})
		}
	}
	if !work.input.runtime.WorkspaceReadOnly {
		ports.Approve = func(ctx context.Context, _ agent.AgentScope, request agent.AgentApproval) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if work.input.runtime.PermissionLevel == constants.PermissionLevelFullAccess {
				// Prefer a single-use native approval so this turn's full access
				// does not persist a broader permission into subsequent turns.
				for _, kind := range []string{"allow_once", "allow_always"} {
					for _, option := range request.Options {
						if option.Kind == kind && option.ID != "" {
							return option.ID, nil
						}
					}
				}
				return "", nil
			}
			resume, allowed := m.executions.pauseWatchdog(chatID, executionID)
			if !allowed {
				return "", status_error.OperationCanceled
			}
			defer resume()
			return nativeApproval(ctx, m.approvals.handler(chatID, executionID), executionID, request)
		}
	}
	return ports
}

func (work *nativeExecutionWork) projectAdmission(ctx context.Context, receipt agent.CommitReceipt, hub *events.Hub) error {
	// The original admission is authoritative even if cancellation interrupted projection.
	if !work.precommitted {
		work.files.commit()
	}
	accepted, err := work.journal.ProjectTurnAdmission(ctx, work.admissionFacts, receipt)
	if err != nil {
		return err
	}
	work.chat, work.execution = accepted.Chat, accepted.Execution
	work.projected.Store(true)
	if ticket := work.input.ticket; hub != nil && work.precommitted && work.chat.HiddenFromWakerHistory &&
		work.input.entryPoint == MainConversationEntryPoint && work.execution.Status == constants.ExecutionStatusRunning &&
		ticket.Address.Kind == constants.WorkTicketOriginMain && ticket.PublicMessageID == work.execution.ID &&
		(ticket.OwnerKind == "waker" || ticket.OwnerKind == "team") && ticket.OwnerID != "" {
		// 接纳已设置 active_execution_id，开始事实需要单独通知原主会话复读。
		resource := events.Resource{Kind: ticket.OwnerKind, ID: ticket.OwnerID}
		_ = events.Emit(hub, constants.MainConversationEventTopic, constants.MainConversationEventTopic+".changed", resource, resource)
	}
	if work.recording != nil {
		if err := work.recording.PublishPending(ctx); err != nil {
			logx.Warnf("publish pending file result failed: chat_id=%s execution_id=%s error=%v", work.chat.ID, work.execution.ID, err)
		}
	}
	if !work.precommitted {
		work.started <- executionStartResult{chat: work.chat, execution: work.execution}
	}
	return nil
}

func nativeApproval(ctx context.Context, handler agent.ApprovalHandler, executionID string, request agent.AgentApproval) (string, error) {
	input := agent.ApprovalRequest{InvocationID: executionID, ToolCallID: request.Tool.ID}
	if request.Tool.Title != nil {
		input.Title = *request.Tool.Title
	}
	if request.Tool.Kind != nil {
		input.Kind = *request.Tool.Kind
	}
	for _, location := range request.Tool.Locations {
		input.Locations = append(input.Locations, agent.ApprovalLocation{Path: location.Path, Line: location.Line})
	}
	for _, option := range request.Options {
		input.Options = append(input.Options, agent.ApprovalOption{ID: option.ID, Name: option.Name, Kind: option.Kind})
	}
	decision, err := handler.Request(ctx, input)
	return decision.OptionID, err
}

func nativeInput(ctx context.Context, handler agent.ElicitationHandler, executionID string, input agent.AgentInputRequest) (agent.AgentInputResponse, error) {
	request := agent.ElicitationRequest{InvocationID: executionID, Mode: "form", Message: input.Message, URL: input.URL}
	if input.URL != "" {
		request.Mode = "url"
	} else if err := json.Unmarshal(input.Schema, &request.RequestedSchema); err != nil {
		return agent.AgentInputResponse{Action: "cancel"}, status_error.AgentInputInvalid
	}
	decision, err := handler.Request(ctx, request)
	response := agent.AgentInputResponse{Action: "cancel"}
	if decision.Accepted {
		response.Action, response.Content = "accept", decision.Content
	} else if decision.Declined {
		response.Action = "decline"
	}
	return response, err
}

func (m *service) runNativeExecution(ctx context.Context, work *nativeExecutionWork, turn *agent.Turn) {
	chatID, executionID := work.chat.ID, work.execution.ID
	finalize := context.WithoutCancel(ctx)
	var admitted bool
	defer func() {
		recording := work.recording
		if recording == nil {
			m.executions.freezeFileSnapshots(chatID, executionID)
			return
		}
		if !admitted && !work.precommitted {
			defer m.executions.freezeFileSnapshots(chatID, executionID)
			if err := recording.Discard(finalize, work.state.isNew); err != nil {
				logx.Warnf("discard unaccepted file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
			}
			return
		}
		if err := recording.Freeze(finalize); err != nil {
			_ = recording.Close()
			m.executions.freezeFileSnapshots(chatID, executionID)
			logx.Warnf("freeze native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
			return
		}
		m.executions.freezeFileSnapshots(chatID, executionID)
		if err := recording.Finish(finalize); err != nil {
			logx.Warnf("finish native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
		}
	}()
	defer func() {
		if err := turn.Close(finalize); err != nil {
			logx.Warnf("close native turn: execution_id=%s error=%v", executionID, err)
		}
	}()
	if err := m.executions.waitForFileSnapshots(ctx, ctx, chatID, executionID); err != nil {
		work.settle(ctx, m, agent.TurnResult{OperationError: err})
		return
	}
	if recorder, ok := m.journal.(chatservice.FileChangesRecorder); ok {
		var err error
		work.recording, err = recorder.BeginFileChanges(ctx, work.chat, executionID, work.state.additionalDirectories, work.files.referencedFiles)
		if err != nil {
			logx.Warnf("prepare native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
		}
	}
	result := turn.Run(ctx)
	admitted = result.BusinessAdmitted
	work.settle(ctx, m, result)
}

func (work *nativeExecutionWork) settle(ctx context.Context, m *service, result agent.TurnResult) {
	stop := m.executions.finishWatchdog(work.chat.ID, work.execution.ID)
	if ticket := work.input.watchdogTicket; ticket != nil && ticket.event != nil && result.StorageError == nil && result.Outcome != nil &&
		slices.Contains(result.Outcome.Turn.ConsumedInputIDs, result.Input.InputID) && result.Input.InputID != "" {
		if err := m.workTickets.MarkEventInputRead(context.WithoutCancel(ctx), work.execution.ID); err != nil {
			logx.Warnf("confirm event input read: execution_id=%s error=%v", work.execution.ID, err)
		}
	}
	if work.finishToolScope != nil {
		work.finishToolScope()
	}
	if work.resources.updateUsage != nil {
		work.resources.updateUsage()
	}
	m.elicitations.finishQuestions(work.chat.ID, work.execution.ID)
	if result.BusinessAdmitted && !work.projected.Load() {
		receipt := work.admissionReceipt.Load()
		if receipt == nil {
			result.StorageError = errors.Join(result.StorageError, status_error.ChatLogError)
		} else if err := work.projectAdmission(context.Background(), *receipt, m.events); err != nil {
			result.StorageError = errors.Join(result.StorageError, err)
		}
	}
	outcome := watchdogExecutionOutcome(nativeExecutionOutcome(result), result, stop)
	if outcome.status == constants.ExecutionStatusCompleted && work.input.message.Actor == constants.WakerOnboardingActor &&
		m.agent != nil && m.onboardingCommands != nil && m.onboardingTransactions != nil {
		if err := m.checkWakerOnboardingProgress(context.Background(), work.chat, true); err != nil {
			result.OperationError = err
			outcome = nativeExecutionOutcome(result)
		}
	}
	if outcome.status == constants.ExecutionStatusFailed {
		var rpcCode, nativeKind string
		if result.Outcome != nil {
			if result.Outcome.NativeErrorCode != 0 {
				rpcCode = strconv.Itoa(result.Outcome.NativeErrorCode)
			}
			nativeKind = result.Outcome.NativeErrorKind
		}
		stageErrorCode := func(err error) string {
			if err == nil {
				return ""
			}
			return util.ErrorCode(err, "unclassified")
		}
		logx.Errorf("native execution settlement failed: trace_id=%s chat_id=%s execution_id=%s runtime_id=%s error_code=%s failure_code=%s native_rpc_code=%s native_kind=%s storage_error_code=%s delivery_error_code=%s cleanup_error_code=%s operation_error_code=%s",
			trace.SpanContextFromContext(ctx).TraceID(), work.chat.ID, work.execution.ID, work.execution.RuntimeID, outcome.code, outcome.failureCode, rpcCode, nativeKind,
			stageErrorCode(result.StorageError), stageErrorCode(result.DeliveryError), stageErrorCode(result.CleanupError), stageErrorCode(result.OperationError))
	}
	if !work.precommitted && !work.projected.Load() {
		// Reconcile the file journal before the caller is allowed to roll back a new Chat.
		if work.recording != nil && !result.BusinessAdmitted {
			_ = work.recording.Discard(context.Background(), work.state.isNew)
			work.recording = nil
		}
		work.started <- executionStartResult{err: outcome.err}
		if !result.BusinessAdmitted {
			return
		}
	}
	if work.execution.Status == constants.ExecutionStatusPending && result.BusinessAdmitted {
		work.execution.Status = constants.ExecutionStatusRunning
	}
	m.finishExecution(work.chat, work.execution, work.input.toolCapabilities, result, eventlog.ExecutionFinishedData{Status: outcome.status, ErrorCode: outcome.code, ErrorMessage: outcome.message, ErrorParams: outcome.params, FailureCode: outcome.failureCode, ErrorMessageSource: outcome.messageSource})
}

// nativeExecutionStatus maps native settlement to the existing business status.
type nativeExecutionStatus struct {
	// status 是业务执行最终状态。
	status constants.ExecutionStatus
	// code 是用于本地化的稳定应用错误身份。
	code string
	// message 仅保留显式公开的原生可读原因。
	message string
	// messageSource 明确标记正文是否来自公开原生原因。
	messageSource constants.ErrorMessageSource
	// failureCode 保留与应用错误身份分开的原生技术分类。
	failureCode constants.RuntimeFailureCode
	// params 是应用错误模板允许使用的安全参数。
	params map[string]interface{}
	// err 是拒绝尚未接纳请求时返回的同一应用错误。
	err error
}

func nativeExecutionOutcome(result agent.TurnResult) nativeExecutionStatus {
	out := nativeExecutionStatus{status: constants.ExecutionStatusFailed, err: status_error.RuntimeExecutionFailed}
	failure := errors.Join(result.StorageError, result.DeliveryError, result.CleanupError)
	if failure == nil && result.Outcome != nil {
		failure = result.Outcome.NativeFailure
		switch result.Outcome.Turn.Status {
		case acpmeta.NativeTurnCompleted:
			if failure == nil && result.OperationError == nil {
				return nativeExecutionStatus{status: constants.ExecutionStatusCompleted}
			}
		case acpmeta.NativeTurnCancelled:
			return nativeExecutionStatus{status: constants.ExecutionStatusCanceled, code: status_error.OperationCanceled.Key(), failureCode: constants.RuntimeFailureCodeContextCanceled,
				messageSource: constants.ErrorMessageSourceLocalized, err: status_error.OperationCanceled}
		}
	}
	failure = errors.Join(failure, result.OperationError)
	if result.StorageError == nil && result.DeliveryError == nil && result.CleanupError == nil && errors.Is(failure, context.Canceled) {
		return nativeExecutionStatus{status: constants.ExecutionStatusCanceled, code: status_error.OperationCanceled.Key(), failureCode: constants.RuntimeFailureCodeContextCanceled,
			messageSource: constants.ErrorMessageSourceLocalized, err: status_error.OperationCanceled}
	}
	var safe statuserror.CommonError
	if public := status_error.PublicError(failure); public != nil {
		out.err = public
		_ = errors.As(public, &safe)
	}
	out.code, out.message = string(constants.RuntimeFailureCodeExecutionFailed), status_error.PublicMessage(out.err)
	isStatus := func(want status_error.StatusError) bool {
		return safe != nil && safe.Code() == want.Code()
	}
	switch {
	case result.StorageError != nil || result.DeliveryError != nil:
		out.code = string(constants.RuntimeFailureCodeEventDelivery)
	case isStatus(status_error.RuntimeAuthRequired):
		out.code = string(constants.RuntimeFailureCodeAuthRequired)
	case isStatus(status_error.RuntimeQuotaExhausted):
		out.code = string(constants.RuntimeFailureCodeQuotaExhausted)
	case isStatus(status_error.RuntimeRateLimited):
		out.code = string(constants.RuntimeFailureCodeRateLimited)
	case isStatus(status_error.RuntimePolicyUnsupported):
		out.code = string(constants.RuntimeFailureCodePolicyUnsupported)
	case isStatus(status_error.RuntimeProtocolIncompatible):
		out.code = string(constants.RuntimeFailureCodeProtocolIncompatible)
	case isStatus(status_error.RuntimeUnavailable):
		out.code = string(constants.RuntimeFailureCodeRuntimeUnavailable)
	case errors.Is(failure, context.DeadlineExceeded):
		out.code = string(constants.RuntimeFailureCodeRequestTimeout)
	}
	if result.Outcome != nil && result.Outcome.NativeErrorKind == "rate_limit" &&
		result.StorageError == nil && result.DeliveryError == nil && result.CleanupError == nil {
		out.code = string(constants.RuntimeFailureCodeRateLimited)
	}
	if result.Outcome != nil && result.Outcome.NativeErrorKind == "quota_exhausted" &&
		result.StorageError == nil && result.DeliveryError == nil && result.CleanupError == nil {
		out.code = string(constants.RuntimeFailureCodeQuotaExhausted)
	}
	out.messageSource = constants.ErrorMessageSourceLocalized
	if out.message != "" {
		out.messageSource = constants.ErrorMessageSourcePublic
	}
	out.failureCode = constants.RuntimeFailureCode(out.code)
	if out.err == status_error.RuntimeExecutionFailed {
		switch out.failureCode {
		case constants.RuntimeFailureCodeQuotaExhausted:
			out.err = status_error.RuntimeQuotaExhausted
		case constants.RuntimeFailureCodeRateLimited:
			out.err = status_error.RuntimeRateLimited
		}
	}
	var public statuserror.CommonError
	if errors.As(out.err, &public) {
		out.code = public.Key()
		out.params = status_error.PublicParams(out.err)
	}
	return out
}

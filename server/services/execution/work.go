package execution

import (
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"os"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// userMessage 保存需要进入权威日志的一条用户消息。
type userMessage struct {
	// SteerExecutionID 是本次追加输入所接续的执行，不改变各轮真实生命周期。
	SteerExecutionID string `json:",omitempty"`

	// Actor 是产生输入的入口无关语义参与者。
	Actor string
	// Text 是消息正文。
	Text string
	// Attachments 是按用户选择顺序提交的本地文件路径。
	Attachments []AttachmentInput
	// SkillIDs 是用户在本轮消息中明确选择调用的技能标识。
	SkillIDs []string
	// PluginIDs 是本轮明确引用的稳定插件身份。
	PluginIDs []string
	// ConnectorIDs 是用户在本轮消息中明确选择使用的 MCP 连接器标识。
	ConnectorIDs []string
	// CodeReferences 是用户从工作范围内选择的有序整文件或代码片段引用。
	CodeReferences []CodeReferenceInput
}

// CodeReferenceInput 是跨入口共享的待校验内容引用。
type CodeReferenceInput = workinput.CodeReference

// AttachmentInput 是跨入口共享的待校验附件。
type AttachmentInput = workinput.Attachment

// runtimeSelection 保存一次 Execution 冻结的 Runtime 参数。
type runtimeSelection struct {
	// ConfigurationResolved 区分已冻结的合法空选择与尚待解析的用户默认选择。
	ConfigurationResolved bool
	// AccountID 是本次冻结的账号标识；空值使用智能体默认配置。
	AccountID string
	// RuntimeID 是本次执行选择的开放 Runtime 标识。
	RuntimeID string
	// Model 是本次执行选择的模型标识。
	Model string
	// Reasoning 是本次执行选择的推理强度。
	Reasoning string
	// Fast 是本轮可冻结的 Fast 选择。
	Fast *bool `json:"Fast,omitempty"`
	// ContextWindow 是本轮冻结的原生窗口档位。
	ContextWindow string `json:"ContextWindow,omitempty"`
	// PermissionLevel 是本次执行冻结的跨 Runtime 产品权限等级。
	PermissionLevel agent.PermissionLevel
	// WorkspaceReadOnly 表示 Runtime 只能读取工作区，不能请求或执行文件修改。
	WorkspaceReadOnly bool
	// SessionPolicy 决定本次是续接还是强制新建 Runtime Session。
	SessionPolicy constants.RuntimeSessionPolicy
	// HistoryTurns 是新原生会话携带的最近完整对话轮数。
	HistoryTurns int
	// HistoryMode 决定新会话使用近期对话还是隐藏交接内容。
	HistoryMode constants.HistoryMode
	// SummaryModel 是旧会话生成交接内容时使用的模型。
	SummaryModel string
	// SummaryReasoning 是旧会话生成交接内容时使用的推理强度。
	SummaryReasoning string
}

// pendingChat 保存新 Chat 尚未提交的身份和工作范围。
type pendingChat struct {
	// hiddenFromWakerHistory 表示该 Chat 的历史由上层产品入口负责展示。
	hiddenFromWakerHistory bool
	// wakerID 是新 Chat 永久绑定的伙伴标识。
	wakerID string
	// projectID 是选择的项目标识，临时文件夹 Chat 为空。
	projectID string
	// workingDirectory 是选择的临时文件夹，项目 Chat 为空。
	workingDirectory string
	// executionWorkspaceDirectory 是宿主领域拥有的隔离执行目录，普通聊天为空。
	executionWorkspaceDirectory string
	// additionalWorkingDirectories 是宿主入口冻结的附加受管目录，不接受智能体参数扩展。
	additionalWorkingDirectories []string
}

// pendingMessage 保存尚未被 Runtime 接受、尚未进入权威日志的输入。
type pendingMessage struct {
	// requestedAt 是服务端收到本轮输入的时间，计入资源准备和原生会话初始化耗时。
	requestedAt int64
	// automaticRunID 固定服务端编排来源，恢复和重试保持原调用链。
	automaticRunID string
	// submissionID 是耐久入口可选的续发请求身份。
	submissionID string
	// submissionHash 是接纳前冻结的规范化请求摘要。
	submissionHash string
	// executionID 是持久编排预留的可选执行标识；普通入口为空。
	executionID string
	// clientRequestID 是新建 Chat 首次发送时由客户端稳定复用的幂等标识。
	clientRequestID string
	// clientRequestHash 是规范化首发业务内容的摘要。
	clientRequestHash string
	// createChatID 是由首次发送标识和伙伴范围稳定派生的普通 Chat 标识。
	createChatID string
	// chatID 是续接已有对话时的标识，创建时为空。
	chatID string
	// create 是创建新对话时需要锁定的身份和工作范围。
	create *pendingChat
	// message 是本轮用户输入。
	message userMessage
	// runtime 是本轮 Runtime 选择。
	runtime runtimeSelection
	// parentExecutionID 是编排产生的可选父执行标识。
	parentExecutionID string
	// admission 决定首批事实在 Runtime 调度前还是接受后提交。
	admission constants.ExecutionAdmission
	// toolCapabilities 保存上层入口为本次执行签发的短期内部能力。
	toolCapabilities []ToolCapability
	// allowedAgentDomains 冻结默认软件能力的领域白名单。
	allowedAgentDomains *[]string
	// allowedConnectorIDs 是入口冻结的 MCP 白名单，nil 与空列表具有不同业务含义。
	allowedConnectorIDs *[]string
	// contextInstructions 是本轮动态业务上下文，不参与原生会话资源指纹。
	contextInstructions []agent.Instruction
	// supplementalInstructions 是稳定的业务角色与成员上下文。
	supplementalInstructions []agent.Instruction
	// historyMessageIDsToOmit 是已包含在本次输入中的权威消息标识。
	historyMessageIDsToOmit []string
}

// normalizePendingMessage 清理标识和选择字段，并复制调用方持有的可变切片。
func normalizePendingMessage(input pendingMessage) pendingMessage {
	input.executionID = strings.TrimSpace(input.executionID)
	input.clientRequestID = strings.TrimSpace(input.clientRequestID)
	input.chatID = strings.TrimSpace(input.chatID)
	input.parentExecutionID = strings.TrimSpace(input.parentExecutionID)
	input.message.Actor = strings.TrimSpace(input.message.Actor)
	input.message.Text = strings.TrimSpace(input.message.Text)
	input.message.Attachments = append([]AttachmentInput(nil), input.message.Attachments...)
	input.toolCapabilities = cloneToolCapabilities(input.toolCapabilities)
	input.allowedAgentDomains = cloneConnectorAllowlist(input.allowedAgentDomains)
	input.allowedConnectorIDs = cloneConnectorAllowlist(input.allowedConnectorIDs)
	input.historyMessageIDsToOmit = append([]string(nil), input.historyMessageIDsToOmit...)
	input.contextInstructions = append([]agent.Instruction(nil), input.contextInstructions...)
	input.supplementalInstructions = append(
		[]agent.Instruction(nil),
		input.supplementalInstructions...,
	)
	input.message.PluginIDs = append([]string(nil), input.message.PluginIDs...)
	for index := range input.message.PluginIDs {
		input.message.PluginIDs[index] = strings.TrimSpace(input.message.PluginIDs[index])
	}
	input.message.SkillIDs = append([]string(nil), input.message.SkillIDs...)
	input.message.ConnectorIDs = append([]string(nil), input.message.ConnectorIDs...)
	input.message.CodeReferences = append([]CodeReferenceInput(nil), input.message.CodeReferences...)
	for index := range input.message.CodeReferences {
		input.message.CodeReferences[index].Path = strings.TrimSpace(input.message.CodeReferences[index].Path)
	}
	for index := range input.message.SkillIDs {
		input.message.SkillIDs[index] = strings.TrimSpace(input.message.SkillIDs[index])
	}
	for index := range input.message.ConnectorIDs {
		input.message.ConnectorIDs[index] = strings.TrimSpace(
			input.message.ConnectorIDs[index],
		)
	}
	for index := range input.message.Attachments {
		input.message.Attachments[index].SourcePath = strings.TrimSpace(
			input.message.Attachments[index].SourcePath,
		)
	}
	for index := range input.toolCapabilities {
		input.toolCapabilities[index].ID = strings.TrimSpace(input.toolCapabilities[index].ID)
		input.toolCapabilities[index].Name = strings.TrimSpace(input.toolCapabilities[index].Name)
	}

	for index := range input.supplementalInstructions {
		input.supplementalInstructions[index].Name = strings.TrimSpace(
			input.supplementalInstructions[index].Name,
		)
		input.supplementalInstructions[index].Content = strings.TrimSpace(
			input.supplementalInstructions[index].Content,
		)
	}
	input.runtime.AccountID = strings.TrimSpace(input.runtime.AccountID)
	input.runtime.RuntimeID = strings.TrimSpace(input.runtime.RuntimeID)
	input.runtime.Model = strings.TrimSpace(input.runtime.Model)
	input.runtime.Reasoning = strings.TrimSpace(input.runtime.Reasoning)
	input.runtime.ContextWindow = strings.TrimSpace(input.runtime.ContextWindow)
	input.runtime.SummaryModel = strings.TrimSpace(input.runtime.SummaryModel)
	input.runtime.SummaryReasoning = strings.TrimSpace(input.runtime.SummaryReasoning)
	input.runtime.HistoryMode = constants.HistoryMode(strings.TrimSpace(string(input.runtime.HistoryMode)))
	input.runtime.SessionPolicy = constants.RuntimeSessionPolicy(
		strings.TrimSpace(string(input.runtime.SessionPolicy)),
	)
	if input.create == nil {
		return input
	}
	create := *input.create
	create.wakerID = strings.TrimSpace(create.wakerID)
	create.projectID = strings.TrimSpace(create.projectID)
	create.workingDirectory = strings.TrimSpace(create.workingDirectory)
	create.additionalWorkingDirectories = append([]string(nil), create.additionalWorkingDirectories...)
	for index := range create.additionalWorkingDirectories {
		create.additionalWorkingDirectories[index] = strings.TrimSpace(create.additionalWorkingDirectories[index])
	}
	input.create = &create
	return input
}

func validCodeReferences(references []CodeReferenceInput) bool {
	for _, reference := range references {
		if reference.Path == "" {
			return false
		}
		if reference.StartLine == 0 && reference.EndLine == 0 && reference.Content == "" {
			if !filepath.IsAbs(reference.Path) {
				return false
			}
			continue
		}
		cleanPath := filepath.Clean(reference.Path)
		escapesWorkingDirectory := !filepath.IsAbs(reference.Path) &&
			(cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)))
		invalidSelection := reference.StartLine < 1 ||
			reference.EndLine < reference.StartLine || reference.Content == ""
		if escapesWorkingDirectory || invalidSelection {
			return false
		}
	}
	return true
}

// startMessage 完成本次输入从未接受状态到权威消息的唯一转换。
func (m *service) startMessage(
	ctx context.Context,
	request pendingMessage,
	claimAttachments func([]AttachmentInput) (func(bool), error),
) (AcceptedExecution, error) {
	if request.requestedAt == 0 {
		request.requestedAt = nowMillis()
	}
	// 先完成纯输入规范化和契约校验，避免任何外部副作用发生后才拒绝请求。
	request = normalizePendingMessage(request)
	if !validCodeReferences(request.message.CodeReferences) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	isNew := request.chatID == ""
	if request.submissionID != "" {
		identity, err := requestidempotency.NewSubmission(request.submissionID, []any{request.chatID, request.message, request.runtime, request.parentExecutionID, request.admission, stableToolCapabilities(request.toolCapabilities), request.supplementalInstructions, request.contextInstructions, request.allowedConnectorIDs, request.allowedAgentDomains})
		if err != nil {
			return AcceptedExecution{}, err
		}
		request.submissionID, request.submissionHash = identity.ID, identity.Hash
		if !isNew {
			receipt, err := m.journal.ReadSubmission(ctx, request.chatID, identity.ID)
			if err != nil {
				return AcceptedExecution{}, err
			}
			if receipt != nil {
				if err := identity.ValidateReplay(receipt.SubmissionID, receipt.SubmissionHash, true); err != nil {
					return AcceptedExecution{}, err
				}
				chat, err := m.journal.LoadExecutionChat(ctx, request.chatID)
				if err != nil {
					return AcceptedExecution{}, err
				}
				return AcceptedExecution{Chat: chat, Execution: *receipt, Replayed: true}, nil
			}
		}
	}
	if isNew != (request.create != nil) ||
		isNew != (request.clientRequestID != "") ||
		(!isNew && claimAttachments != nil) ||
		request.message.Actor == "" ||
		validateMessage(request.message.Text) != nil ||
		request.runtime.RuntimeID == "" ||
		!constants.SelectablePermissionLevel(request.runtime.PermissionLevel) ||
		(request.runtime.SessionPolicy != constants.RuntimeSessionPolicyContinue &&
			request.runtime.SessionPolicy != constants.RuntimeSessionPolicyNew) ||
		request.runtime.HistoryTurns < 0 ||
		request.runtime.HistoryTurns > constants.RuntimeHistoryMaximumTurns ||
		(request.runtime.HistoryMode != "" && request.runtime.HistoryMode != constants.HistoryModeRecent && request.runtime.HistoryMode != constants.HistoryModeSummary) ||
		(request.runtime.HistoryMode == constants.HistoryModeSummary && request.runtime.SessionPolicy != constants.RuntimeSessionPolicyNew) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	for _, capability := range request.toolCapabilities {
		if capability.ID == "" || capability.Name == "" || len(capability.Bindings) == 0 {
			return AcceptedExecution{}, status_error.BadRequest
		}
	}

	var files *preparedMessageFiles
	var createRequestIdentity requestidempotency.Identity
	if isNew {
		var err error
		createRequestIdentity, err = requestidempotency.New(
			request.clientRequestID,
			createRequestContent(request),
		)
		if err != nil {
			return AcceptedExecution{}, err
		}
		request.clientRequestID = createRequestIdentity.ID
		request.clientRequestHash = createRequestIdentity.Hash
		request.createChatID = createRequestIdentity.ResourceID("chat", request.create.wakerID)
		if replayed, exists, err := m.replayCreateRequest(
			ctx,
			request.createChatID,
			createRequestIdentity,
		); err != nil || exists {
			return replayed, err
		}
		if m.createRequests == nil || !m.createRequests.reserve(request.createChatID) {
			return AcceptedExecution{}, status_error.CreateRequestInProgress
		}
		defer m.createRequests.release(request.createChatID)
		// 第一次检查与进程内占位之间可能已有请求提交，再检查一次权威事实。
		if replayed, exists, err := m.replayCreateRequest(
			ctx,
			request.createChatID,
			createRequestIdentity,
		); err != nil || exists {
			return replayed, err
		}
		if claimAttachments != nil {
			// 已提交的请求只需原引用即可对账，新请求仍须领取实际附件。
			releaseAttachments, err := claimAttachments(request.message.Attachments)
			if err != nil {
				return AcceptedExecution{}, err
			}
			defer func() { releaseAttachments(files != nil && files.committed) }()
		}
	}
	if m.runtimeEnvironments == nil {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	runtimeEnvironment, err := m.runtimeEnvironments.Get(
		ctx,
		request.runtime.RuntimeID,
	)
	if err != nil {
		return AcceptedExecution{}, err
	}
	if !runtimeEnvironment.Enabled {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	// Connector allowlists require native enforcement over inherited MCP settings.
	// Agent domains are enforced separately by the host's exact capability grants.
	if !agent.SupportsProvider(nativeProvider(runtimeEnvironment)) || request.allowedConnectorIDs != nil {
		return AcceptedExecution{}, status_error.RuntimePolicyUnsupported
	}
	requiredPermissionLevel := request.runtime.PermissionLevel
	if request.runtime.WorkspaceReadOnly {
		// Approval modes and workspace write confinement do not enforce read-only access.
		if nativeProvider(runtimeEnvironment) != constants.RuntimeIDCodex {
			return AcceptedExecution{}, status_error.RuntimePolicyUnsupported
		}
		requiredPermissionLevel = constants.PermissionLevelDefault
	}
	// 一次性生成本轮全部稳定标识，后续 JSONL、SQLite 查询索引和 Runtime 调用共用同一身份。
	executionID := request.executionID
	if executionID == "" {
		executionID = util.New()
	}
	userMessageID := util.New()
	// 现有 Chat 在读取准备状态前取得原操作占位，一次读取即为本次接纳的状态基线。
	reservationChatID := request.chatID
	if isNew {
		reservationChatID = request.createChatID
	}
	executionContext, err := m.executions.reserve(reservationChatID, executionID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	ctx, cancelPreparation := context.WithCancel(ctx)
	stopPreparation := context.AfterFunc(executionContext, cancelPreparation)
	defer func() {
		stopPreparation()
		cancelPreparation()
	}()
	releaseReservation := true
	defer func() {
		if releaseReservation {
			m.executions.release(reservationChatID, executionID)
		}
	}()
	if !isNew && request.submissionID != "" {
		receipt, err := m.journal.ReadSubmission(ctx, request.chatID, request.submissionID)
		if err != nil {
			return AcceptedExecution{}, err
		}
		if receipt != nil {
			identity := requestidempotency.Identity{ID: request.submissionID, Hash: request.submissionHash}
			if err := identity.ValidateReplay(receipt.SubmissionID, receipt.SubmissionHash, true); err != nil {
				return AcceptedExecution{}, err
			}
			chat, err := m.journal.LoadExecutionChat(ctx, request.chatID)
			if err != nil {
				return AcceptedExecution{}, err
			}
			return AcceptedExecution{Chat: chat, Execution: *receipt, Replayed: true}, nil
		}
	}
	// 前一轮已公开终态仍可能正在冻结结束侧；附件、工作区修复及原生资源准备
	// 都可能写入同一目录，必须在它们的首次副作用之前等待，而不只拦住 Run。
	if err := m.executions.waitForFileSnapshots(ctx, executionContext, reservationChatID, executionID); err != nil {
		return AcceptedExecution{}, status_error.OperationCanceled
	}
	state, err := m.resolveMessageChat(ctx, request, executionID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	if !isNew && request.message.Actor == constants.WakerOnboardingActor {
		if state.chat.HiddenFromWakerHistory || state.chat.ID != requestidempotency.WakerOnboardingChatID(state.chat.WakerID) {
			return AcceptedExecution{}, status_error.AgentContextUnauthorized
		}
		waker, err := m.wakerController.Get(ctx, state.chat.WakerID)
		if err != nil {
			return AcceptedExecution{}, err
		}
		if waker.OnboardingStatus != constants.WakerOnboardingStatusPending || waker.OnboardingChatID != state.chat.ID ||
			state.chat.ActiveExecutionID != "" || state.chat.LastEndedExecutionID == "" || state.chat.LastEndedExecutionID != request.parentExecutionID {
			return AcceptedExecution{}, status_error.WakerOnboardingStateChanged
		}
	}
	// 普通续聊只在同一 Runtime 中沿用最近有效选择；已接纳配置的空值不再继承默认。
	config := ExecutionConfiguration{Resolved: request.runtime.ConfigurationResolved, RuntimeID: request.runtime.RuntimeID, AccountID: request.runtime.AccountID, Model: request.runtime.Model, Reasoning: request.runtime.Reasoning, Fast: request.runtime.Fast, PermissionLevel: requiredPermissionLevel}
	if !config.Resolved && !state.isNew && state.chat.LastRuntimeID == config.RuntimeID && state.lastExecutionID != "" {
		if config.AccountID == "" {
			config.AccountID = state.chat.LastAccountID
			if config.AccountID == "" {
				config.AccountID = constants.AiAccountSystemID
			}
		}
		lastAccountID := state.chat.LastAccountID
		if lastAccountID == "" {
			lastAccountID = constants.AiAccountSystemID
		}
		if config.AccountID == lastAccountID {
			if config.Model == "" {
				config.Model = state.chat.LastModel
			}
			if config.Reasoning == "" && config.Model == state.chat.LastModel {
				config.Reasoning = state.chat.LastReasoning
			}
			if config.Fast == nil {
				config.Fast = state.chat.LastFast
			}
			if request.runtime.ContextWindow == "" {
				request.runtime.ContextWindow = state.chat.LastContextWindow
			}
			config.Resolved = true
		}
	}
	config, err = m.resolveConfiguration(ctx, config, runtimeEnvironment)
	if err != nil {
		return AcceptedExecution{}, err
	}
	request.runtime.AccountID, request.runtime.Model, request.runtime.Reasoning, request.runtime.Fast = config.AccountID, config.Model, config.Reasoning, config.Fast
	request.runtime.ConfigurationResolved = true
	if !constants.SelectablePermissionLevel(requiredPermissionLevel) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	// 已有对话的活跃事实来自权威 JSONL；SQLite 只保存可重建查询投影，不能反向阻断新执行。
	if !state.isNew && state.chat.ActiveExecutionID != "" {
		return AcceptedExecution{}, status_error.ChatHasActiveExecution
	}
	// 上下文和附件必须先完整准备，并在权威提交失败时统一回滚。
	files, err = m.prepareMessageFiles(
		ctx,
		state,
		userMessageID,
		request.message.Attachments,
		request.message.CodeReferences,
		request.runtime.RuntimeID,
		runtimeEnvironment.Detection,
	)
	if err != nil {
		return AcceptedExecution{}, err
	}
	defer files.rollback()

	if m.threads == nil {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	var enabledSkills []models.SkillWithState
	if m.skillController != nil {
		enabledSkills, err = m.skillController.ListEnabled(ctx, state.chat.WakerID)
		if err != nil {
			return AcceptedExecution{}, err
		}
	}
	if !selectedSkillsAreEnabled(request.message.SkillIDs, enabledSkills) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	enabledConnectors := []models.MCPConnector{}
	if m.connectorController != nil {
		enabledConnectors, err = m.connectorController.ExecutionDeclarations(
			ctx,
			state.chat.WakerID,
		)
		if err != nil {
			return AcceptedExecution{}, err
		}
	}
	enabledConnectors = slices.DeleteFunc(enabledConnectors, func(connector models.MCPConnector) bool {
		supported, _ := agent.ConnectorCompatibility(nativeProvider(runtimeEnvironment), agent.ConnectorTransport(connector.Transport))
		return !supported
	})
	if !selectedConnectorsAreEnabled(request.message.ConnectorIDs, enabledConnectors) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	frozenPlugins, err := m.freezePluginResources(ctx, state.chat.WakerID, request, runtimeEnvironment.Detection)
	if err != nil {
		return AcceptedExecution{}, err
	}
	return m.startNativeExecution(ctx, executionContext, state, request, executionID, userMessageID,
		files, enabledSkills, enabledConnectors, runtimeEnvironment, frozenPlugins, &releaseReservation)

}

// createRequestContent 返回普通 Chat 首发幂等摘要覆盖的全部规范化业务输入。
func createRequestContent(request pendingMessage) []interface{} {
	content := []interface{}{
		request.executionID,
		request.create.wakerID,
		request.create.projectID,
		request.create.workingDirectory,
		request.create.executionWorkspaceDirectory,
		request.create.hiddenFromWakerHistory,
		request.message.Actor,
		request.message.Text,
		request.message.Attachments,
		request.message.SkillIDs,
		request.message.ConnectorIDs,
		request.message.PluginIDs,
		request.message.CodeReferences,
		request.runtime.ConfigurationResolved,
		request.runtime.AccountID,
		request.runtime.RuntimeID,
		request.runtime.Model,
		request.runtime.Reasoning,
		request.runtime.PermissionLevel,
		request.runtime.WorkspaceReadOnly,
		request.runtime.SessionPolicy,
		request.runtime.HistoryTurns,
		request.runtime.HistoryMode,
		request.runtime.SummaryModel,
		request.runtime.SummaryReasoning,
		request.parentExecutionID,
		request.admission,
		stableToolCapabilities(request.toolCapabilities),
		request.supplementalInstructions,
		request.contextInstructions,
		request.allowedAgentDomains,
	}
	if request.runtime.Fast != nil {
		content = append(content, map[string]any{"fast": *request.runtime.Fast})
	}
	if request.runtime.ContextWindow != "" {
		content = append(content, map[string]any{"contextWindow": request.runtime.ContextWindow})
	}
	if request.automaticRunID != "" {
		content = append(content, map[string]any{"automaticRunId": request.automaticRunID})
	}
	if request.allowedConnectorIDs != nil {
		content = append(content, map[string]any{"allowedConnectorIds": *request.allowedConnectorIDs})
	}
	if len(request.create.additionalWorkingDirectories) > 0 {
		content = append(content, map[string]any{"additionalWorkingDirectories": request.create.additionalWorkingDirectories})
	}
	return content
}

func cloneConnectorAllowlist(input *[]string) *[]string {
	if input == nil {
		return nil
	}
	copy := append([]string{}, (*input)...)
	return &copy
}

// stableToolCapabilities 只把稳定能力身份与排序后的操作集合纳入接纳摘要。
func stableToolCapabilities(input []ToolCapability) []map[string]any {
	result := make([]map[string]any, 0, len(input))
	for _, capability := range input {
		operations := make([]string, 0, len(capability.Bindings))
		for _, binding := range capability.Bindings {
			operations = append(operations, binding.Operation)
		}
		slices.Sort(operations)
		result = append(result, map[string]any{"id": capability.ID, "name": capability.Name, "operations": operations})
	}
	return result
}

// replayCreateRequest 从确定的 Chat JSONL 返回同键同内容的首次执行结果。
func (m *service) replayCreateRequest(
	ctx context.Context,
	chatID string,
	identity requestidempotency.Identity,
) (AcceptedExecution, bool, error) {
	detail, err := m.journal.ReadInitialExecution(ctx, chatID)
	if err != nil {
		var commonErr statuserror.CommonError
		if errors.As(err, &commonErr) && commonErr.Code() == status_error.ChatNotFound.Code() {
			return AcceptedExecution{}, false, nil
		}
		return AcceptedExecution{}, false, err
	}
	ready := detail.InitialExecution != nil && detail.InitialExecution.ID != ""
	if err := identity.ValidateReplay(
		detail.CreateRequestID,
		detail.CreateRequestHash,
		ready,
	); err != nil {
		return AcceptedExecution{}, true, err
	}
	if detail.Chat.ID != chatID {
		return AcceptedExecution{}, true, status_error.ChatLogCorrupted
	}
	return AcceptedExecution{
		Chat:      detail.Chat,
		Execution: *detail.InitialExecution,
		Replayed:  true,
	}, true, nil
}

// messageChat 保存一次消息发送所需的对话权威状态。
type messageChat struct {
	// isNew 表示本次提交同时创建 Chat。
	isNew bool
	// chat 是新建或从权威日志恢复的对话状态。
	chat models.Chat
	// lastExecutionID 是最近助手执行或继承消息的原续接游标。
	lastExecutionID string
	// workspaceDirectory 是伙伴固定 cwd 或宿主领域提供的隔离成员 cwd。
	workspaceDirectory string
	// additionalDirectories 是本轮仍可用的授权目录子集，不改写 Chat 保存的授权上限。
	additionalDirectories []string
	// projectLinks 只保存真实项目来源及展示标签，不对应磁盘链接。
	projectLinks []workspace.ProjectLink
	// projectSources 是本轮有效项目来源及其已接纳的指令策略。
	projectSources []eventlog.ProjectSourceSnapshot
	// threadWorkspaceID 非空时表示 Chat 绑定 thread 级共享执行工作区。
	threadWorkspaceID string
}

// resolvedChatWorkspace 保存新 Chat 已解析并可冻结的完整工作范围。
type resolvedChatWorkspace struct {
	// projectID 是项目标识，临时文件夹工作范围为空。
	projectID string
	// projectName 是冻结的项目名称或临时文件夹名称。
	projectName string
	// additionalDirectories 是 Runtime 获得授权的真实目录，首项是用户终端起始目录。
	additionalDirectories []string
	// projectLinks 是真实项目来源与展示标签，不创建工作区入口。
	projectLinks []workspace.ProjectLink
	// projectSources 是包含目录级指令策略的权威冻结快照。
	projectSources []eventlog.ProjectSourceSnapshot
}

// preparedMessageFiles 保存发送前已经物化且可能需要回滚的文件副作用。
type preparedMessageFiles struct {
	// executionWorkspaceDirectory 是本轮冻结的真实 cwd，附件原件另由 Conversation 拥有。
	executionWorkspaceDirectory string
	// chatID 是执行工作区归属校验使用的对话标识。
	chatID string
	// newConversation 表示未提交的 Chat，可在回滚后移除空私有目录。
	newConversation bool
	// projectEntries 是本轮原生工具使用的 Chat 独有项目入口，真实来源保持独立。
	projectEntries []workspace.ProjectEntry
	// rollbackProjectEntries 仅撤销本次准备新建的 Chat 独有入口。
	rollbackProjectEntries func() error
	// storage 用于在新 Chat 提交失败后清理空聚合目录。
	storage *chatstorage.Layout
	// attachments 保存附件运行路径和可回滚文件操作。
	attachments workspace.PreparedAttachments
	// attachmentReferences 是可以写入权威消息事件的安全引用。
	attachmentReferences []eventlog.AttachmentReference
	// codeReferences 保存引用文件的真实路径及本轮选中的代码片段。
	codeReferences []eventlog.CodeReference
	// referencedFiles 是本轮引用校验后冻结的真实文件，仅用于精确采集与能力授权。
	referencedFiles []string
	// instructions 是本次执行实际读取并交给 Runtime 的当前指令。
	instructions []agent.Instruction
	// committed 表示权威消息和执行事实已经成功提交。
	committed bool
}

// runtimeWorkspaceFiles 保存 Chat 读取 Runtime 指令所需的文件名。
type runtimeWorkspaceFiles struct {
	// instructionFileName 是所选 Runtime 在工作区根目录读取的指令文件。
	instructionFileName string
}

func (m *service) resolveRuntimeWorkspaceFiles(
	runtimeID string,
) (runtimeWorkspaceFiles, error) {
	var files runtimeWorkspaceFiles
	for _, summary := range m.runtimeSummaries {
		if summary.ID != runtimeID {
			continue
		}
		files.instructionFileName = summary.InstructionFileName
	}
	if files.instructionFileName == "" {
		return runtimeWorkspaceFiles{}, status_error.RuntimeUnavailable
	}
	return files, nil
}

// commit 标记文件副作用已经被权威 JSONL 引用，不再允许回滚。
func (f *preparedMessageFiles) commit() {
	f.committed = true
}

// rollback 在权威提交前失败时撤销附件，并按需删除新 Chat 执行工作区。
func (f *preparedMessageFiles) rollback() {
	if f == nil || f.committed {
		return
	}
	if err := f.attachments.Rollback(); err != nil {
		logx.Errorf(
			"rollback chat attachments failed: chat_id=%s error=%v",
			f.chatID,
			err,
		)
	}
	if f.rollbackProjectEntries != nil {
		if err := f.rollbackProjectEntries(); err != nil {
			logx.Errorf("rollback chat project entries failed: chat_id=%s error=%v", f.chatID, err)
		}
	}
	if !f.newConversation {
		return
	}
	if f.storage != nil {
		if err := f.storage.RemoveEmptyChatAggregate(f.chatID); err != nil {
			logx.Errorf("rollback empty chat aggregate failed: chat_id=%s error=%v", f.chatID, err)
		}
	}
}

// resolveMessageChat 创建新对话状态，或从权威日志恢复已有对话状态。
func (m *service) resolveMessageChat(
	ctx context.Context,
	request pendingMessage,
	executionID string,
) (messageChat, error) {
	if request.chatID != "" {
		detail, err := m.journal.LoadExecutionContext(ctx, request.chatID)
		if err != nil {
			return messageChat{}, err
		}
		state := messageChat{
			chat:               detail.Chat,
			lastExecutionID:    detail.LastExecutionID,
			workspaceDirectory: detail.Chat.WorkspaceDirectory,
			projectSources:     append([]eventlog.ProjectSourceSnapshot(nil), detail.ProjectSources...),
			threadWorkspaceID:  strings.TrimSpace(detail.ThreadWorkspaceID),
		}
		if state.workspaceDirectory == "" {
			return messageChat{}, status_error.ChatDirectoryOwnershipInvalid
		}
		return availableMessageWorkspace(state, request.message.CodeReferences)
	}

	input := request.create
	if input.wakerID == "" || (input.projectID != "" && input.workingDirectory != "") {
		return messageChat{}, status_error.BadRequest
	}
	waker, err := m.wakerController.Get(ctx, input.wakerID)
	if err != nil {
		return messageChat{}, err
	}
	isOnboarding := request.message.Actor == constants.WakerOnboardingActor
	if isOnboarding != (waker.OnboardingChatID != "" && waker.OnboardingChatID == request.createChatID) {
		return messageChat{}, status_error.AgentContextUnauthorized
	}
	resolvedWorkspace, err := m.resolveNewChatWorkspace(
		ctx,
		*input,
		request.runtime.RuntimeID,
	)
	if err != nil {
		return messageChat{}, err
	}
	chatID := request.createChatID
	if chatID == "" {
		return messageChat{}, status_error.BadRequest
	}
	workspaceDirectory, err := m.storage.AgentWorkspace(input.wakerID)
	if err != nil {
		return messageChat{}, err
	}
	if input.executionWorkspaceDirectory != "" {
		workspaceDirectory = input.executionWorkspaceDirectory
	}
	if err := ensureInitialExecutionWorkspace(workspaceDirectory, chatID, input.wakerID, input.executionWorkspaceDirectory != ""); err != nil {
		return messageChat{}, status_error.ChatDirectoryOwnershipInvalid
	}
	if input.projectID == "" && input.workingDirectory == "" && len(resolvedWorkspace.additionalDirectories) == 0 {
		resolvedWorkspace.additionalDirectories = []string{workspaceDirectory}
	}

	now := nowMillis()
	title := defaultTitle(request.message.Text)
	if isOnboarding {
		title = waker.Name
	}
	chat := models.Chat{
		HiddenFromWakerHistory:       input.hiddenFromWakerHistory,
		ID:                           chatID,
		WakerID:                      input.wakerID,
		ProjectID:                    resolvedWorkspace.projectID,
		ProjectName:                  resolvedWorkspace.projectName,
		WorkspaceDirectory:           workspaceDirectory,
		AdditionalWorkingDirectories: resolvedWorkspace.additionalDirectories,
		Title:                        title,
		ActiveExecutionID:            executionID,
		LastAccountID:                request.runtime.AccountID,
		LastRuntimeID:                request.runtime.RuntimeID,
		LastModel:                    request.runtime.Model,
		LastFast:                     request.runtime.Fast,
		LastContextWindow:            request.runtime.ContextWindow,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	return messageChat{
		isNew:                 true,
		chat:                  chat,
		workspaceDirectory:    workspaceDirectory,
		additionalDirectories: resolvedWorkspace.additionalDirectories,
		projectLinks:          resolvedWorkspace.projectLinks,
		projectSources:        resolvedWorkspace.projectSources,
	}, nil
}

// resolveNewChatWorkspace 将用户的项目或临时文件夹选择解析为冻结工作范围。
func (m *service) resolveNewChatWorkspace(
	ctx context.Context,
	input pendingChat,
	runtimeID string,
) (resolvedChatWorkspace, error) {
	resolved, err := m.resolveChatWorkspaceScope(ctx, input, runtimeID)
	if err != nil {
		return resolvedChatWorkspace{}, err
	}
	usedNames := make(map[string]bool, len(resolved.projectLinks))
	for _, link := range resolved.projectLinks {
		usedNames[link.EntryName] = true
	}
	for _, directory := range input.additionalWorkingDirectories {
		source, err := resolveWorkingDirectory(directory)
		if err != nil {
			return resolvedChatWorkspace{}, status_error.ProjectSourceUnavailable
		}
		if slices.Contains(resolved.additionalDirectories, source.Path) {
			continue
		}
		entryName := filepath.Base(source.Path)
		for suffix, base := 2, entryName; usedNames[entryName]; suffix++ {
			entryName = fmt.Sprintf("%s-%d", base, suffix)
		}
		usedNames[entryName] = true
		resolved.additionalDirectories = append(resolved.additionalDirectories, source.Path)
		resolved.projectLinks = append(resolved.projectLinks, workspace.ProjectLink{EntryName: entryName, SourcePath: source.Path})
		resolved.projectSources = append(resolved.projectSources, eventlog.ProjectSourceSnapshot{EntryName: entryName, SourcePath: source.Path})
	}
	return resolved, nil
}

// resolveChatWorkspaceScope 保留用户项目或临时目录的默认目录、归属及指令策略。
func (m *service) resolveChatWorkspaceScope(ctx context.Context, input pendingChat, runtimeID string) (resolvedChatWorkspace, error) {
	if m.threads == nil {
		return resolvedChatWorkspace{}, status_error.RuntimeUnavailable
	}
	_, err := m.resolveRuntimeWorkspaceFiles(runtimeID)
	if err != nil {
		return resolvedChatWorkspace{}, status_error.RuntimeUnavailable
	}
	if input.projectID == "" {
		if input.workingDirectory == "" {
			return resolvedChatWorkspace{}, nil
		}
		source, err := resolveWorkingDirectory(input.workingDirectory)
		if err != nil {
			return resolvedChatWorkspace{}, status_error.ProjectSourceUnavailable
		}
		entryName := filepath.Base(source.Path)
		return resolvedChatWorkspace{
			projectName:           source.Name,
			additionalDirectories: []string{source.Path},
			projectLinks: []workspace.ProjectLink{{
				EntryName:  entryName,
				SourcePath: source.Path,
			}},
			projectSources: []eventlog.ProjectSourceSnapshot{{
				EntryName:        entryName,
				SourcePath:       source.Path,
				LoadInstructions: true,
			}},
		}, nil
	}
	if m.projectController == nil {
		return resolvedChatWorkspace{}, status_error.ProjectServiceUnavailable
	}
	project, err := m.projectController.Get(ctx, input.projectID)
	if err != nil {
		return resolvedChatWorkspace{}, err
	}
	if project.WakerID != input.wakerID {
		return resolvedChatWorkspace{}, status_error.ProjectNotFound.WithParams(
			map[string]interface{}{"ProjectID": input.projectID},
		)
	}
	directories, err := m.projectController.ListDirectories(ctx, input.projectID)
	if err != nil {
		return resolvedChatWorkspace{}, err
	}
	// 项目工作范围必须整体可用；不能静默忽略失效的任何目录。
	authorizedDirectories := make([]string, 0, len(directories))
	projectLinks := make([]workspace.ProjectLink, 0, len(directories))
	projectSources := make([]eventlog.ProjectSourceSnapshot, 0, len(directories))
	usedNames := make(map[string]struct{}, len(directories))
	defaultWorkingDirectory := ""
	for _, directory := range directories {
		source := directory.Source
		entryName := source.Name
		if entryName == "" {
			entryName = filepath.Base(source.Path)
		}
		baseName := entryName
		for suffix := 2; ; suffix++ {
			if _, exists := usedNames[entryName]; !exists {
				break
			}
			entryName = fmt.Sprintf("%s-%d", baseName, suffix)
		}
		usedNames[entryName] = struct{}{}
		projectLinks = append(projectLinks, workspace.ProjectLink{
			EntryName:  entryName,
			SourcePath: source.Path,
		})
		if !projectsource.Available(source) {
			return resolvedChatWorkspace{}, status_error.ProjectSourceUnavailable
		}
		authorizedDirectories = append(authorizedDirectories, source.Path)
		projectSources = append(projectSources, eventlog.ProjectSourceSnapshot{
			EntryName:        entryName,
			SourcePath:       source.Path,
			LoadInstructions: directory.LoadInstructions,
		})
		if directory.IsDefault {
			defaultWorkingDirectory = source.Path
		}
	}
	if len(directories) == 0 || defaultWorkingDirectory == "" {
		return resolvedChatWorkspace{}, status_error.ProjectSourceUnavailable
	}
	additionalDirectories := make([]string, 0, len(authorizedDirectories))
	additionalDirectories = append(additionalDirectories, defaultWorkingDirectory)
	for _, directory := range authorizedDirectories {
		if directory != defaultWorkingDirectory {
			additionalDirectories = append(additionalDirectories, directory)
		}
	}
	return resolvedChatWorkspace{
		projectID:             project.ID,
		projectName:           project.Name,
		additionalDirectories: additionalDirectories,
		projectLinks:          projectLinks,
		projectSources:        projectSources,
	}, nil
}

// availableMessageWorkspace 从原授权根收窄本轮有效目录，所有后续消费者共用该集合。
func availableMessageWorkspace(state messageChat, references []CodeReferenceInput) (messageChat, error) {
	cwd, err := resolveWorkingDirectory(state.workspaceDirectory)
	if err != nil || !sameFilesystemPath(cwd.Path, state.workspaceDirectory) {
		return messageChat{}, status_error.ChatDirectoryOwnershipInvalid
	}
	available := make(map[string]bool, len(state.chat.AdditionalWorkingDirectories))
	directories := make([]string, 0, len(state.chat.AdditionalWorkingDirectories))
	for _, directory := range state.chat.AdditionalWorkingDirectories {
		resolved, err := resolveWorkingDirectory(directory)
		valid := err == nil && sameFilesystemPath(resolved.Path, directory)
		available[filepath.Clean(directory)] = valid
		if valid {
			directories = append(directories, directory)
			continue
		}
		for _, reference := range references {
			if filepath.IsAbs(reference.Path) && pathWithinRoot(directory, reference.Path) {
				return messageChat{}, status_error.ProjectSourceUnavailable
			}
		}
	}
	state.additionalDirectories = directories
	sources := make([]eventlog.ProjectSourceSnapshot, 0, len(state.projectSources))
	for _, source := range state.projectSources {
		if available[filepath.Clean(source.SourcePath)] {
			sources = append(sources, source)
			continue
		}
		for _, reference := range references {
			if !filepath.IsAbs(reference.Path) && pathWithinRoot(source.EntryName, reference.Path) {
				return messageChat{}, status_error.ProjectSourceUnavailable
			}
		}
	}
	state.projectSources = sources
	links := make([]workspace.ProjectLink, 0, len(state.projectLinks))
	for _, link := range state.projectLinks {
		if available[filepath.Clean(link.SourcePath)] {
			links = append(links, link)
		}
	}
	state.projectLinks = links
	return state, nil
}

// pathWithinRoot 仅比较已记录授权的词法范围，不跟随可能被替换的符号链接。
func pathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func ensureMessageExecutionWorkspace(state messageChat) error {
	threadWorkspaceID := strings.TrimSpace(state.threadWorkspaceID)
	if threadWorkspaceID != "" {
		return workspace.ValidateThreadMemberExecutionWorkspace(
			state.workspaceDirectory,
			threadWorkspaceID,
			state.chat.ID,
			state.chat.WakerID,
		)
	}
	owner, err := workspace.ReadExecutionWorkspaceOwner(state.workspaceDirectory)
	if err != nil {
		return err
	}
	if owner.Kind == constants.IsolatedExecutionWorkspaceOwnerKind {
		return workspace.EnsureIsolatedExecutionWorkspace(state.workspaceDirectory, state.chat.ID, state.chat.WakerID)
	}
	return workspace.EnsureAgentWorkspace(state.workspaceDirectory, state.chat.WakerID)
}

// prepareMessageFiles 原子物化执行上下文并准备消息附件。
// prepareNativeInstructions reads current stable profile and project instructions for a native session.
func (m *service) prepareNativeInstructions(ctx context.Context, state messageChat, runtimeID string) ([]agent.Instruction, error) {
	runtimeFiles, err := m.resolveRuntimeWorkspaceFiles(runtimeID)
	if err != nil {
		return nil, status_error.RuntimeUnavailable
	}
	if err := ensureMessageExecutionWorkspace(state); err != nil {
		logx.Errorf(
			"prepare chat execution workspace failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	storedDocuments, err := m.profileController.ListWakerDocuments(ctx, state.chat.WakerID)
	if err != nil {
		return nil, err
	}
	profileDocument := builtinrolepreset.ResolveWakerAgents(state.chat.WakerID, storedDocuments, 0)
	instructions := []agent.Instruction{{Name: "Workspace directories", Content: "The working directory is persistent and may be shared by several chats of this partner. Organize your persistent files here as needed; no output or temporary subdirectory is required. Use the system temporary directory (TMPDIR or /tmp) for disposable intermediate files. User projects remain at their authorized absolute roots."}}
	if profileDocument.Content != "" {
		instructions = append(instructions, agent.Instruction{Name: profileDocument.FileName, Content: profileDocument.Content})
	}
	for _, source := range state.projectSources {
		if !source.LoadInstructions {
			continue
		}
		files, readErr := workspace.ReadProjectRootInstructions(
			source.SourcePath,
			[]string{runtimeFiles.instructionFileName},
		)
		if readErr != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		if len(files) == 0 {
			continue
		}
		if content, exists := files[runtimeFiles.instructionFileName]; exists {
			instructions = append(instructions, agent.Instruction{
				Name:    filepath.Join(source.SourcePath, runtimeFiles.instructionFileName),
				Content: content,
			})
		}
	}
	return instructions, nil
}

func (m *service) prepareMessageFiles(
	ctx context.Context,
	state messageChat,
	messageID string,
	attachments []AttachmentInput,
	codeReferences []CodeReferenceInput,
	runtimeID string,
	detection agent.DetectionResult,
) (*preparedMessageFiles, error) {
	if m.threads == nil {
		return nil, status_error.RuntimeUnavailable
	}
	workspaceDirectory := state.workspaceDirectory
	if workspaceDirectory == "" {
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	projectLinks := append([]workspace.ProjectLink(nil), state.projectLinks...)
	if len(projectLinks) == 0 {
		for _, source := range state.projectSources {
			projectLinks = append(projectLinks, workspace.ProjectLink{
				EntryName:  source.EntryName,
				SourcePath: source.SourcePath,
			})
		}
	}
	instructions, err := m.prepareNativeInstructions(ctx, state, runtimeID)
	if err != nil {
		return nil, err
	}
	conversationDirectory, err := m.storage.ChatRoot(state.chat.ID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(conversationDirectory, 0o700); err != nil {
		return nil, status_error.ManagedWorkspaceError
	}
	files := &preparedMessageFiles{
		executionWorkspaceDirectory: workspaceDirectory,
		chatID:                      state.chat.ID,
		newConversation:             state.isNew,
		storage:                     m.storage,
		attachments: workspace.PreparedAttachments{
			Rollback: func() error { return nil },
		},
		instructions: instructions,
	}
	if supported, known := agent.AdditionalDirectorySupport(detection); known && !supported {
		projectionRoots := append([]workspace.ProjectLink(nil), projectLinks...)
		usedNames := make(map[string]bool, len(projectionRoots))
		for _, source := range projectionRoots {
			usedNames[source.EntryName] = true
		}
		for _, directory := range state.additionalDirectories {
			if sameFilesystemPath(directory, workspaceDirectory) || slices.ContainsFunc(projectionRoots, func(source workspace.ProjectLink) bool { return sameFilesystemPath(source.SourcePath, directory) }) {
				continue
			}
			name := filepath.Base(directory)
			for suffix, base := 2, name; usedNames[name]; suffix++ {
				name = fmt.Sprintf("%s-%d", base, suffix)
			}
			usedNames[name] = true
			projectionRoots = append(projectionRoots, workspace.ProjectLink{EntryName: name, SourcePath: directory})
		}
		files.projectEntries, files.rollbackProjectEntries, err = workspace.PrepareProjectEntries(workspaceDirectory, state.chat.ID, projectionRoots)
		if err != nil {
			files.rollback()
			return nil, err
		}
	}
	referenceRoots := append([]workspace.ProjectLink(nil), projectLinks...)
	referenceRoots = append(referenceRoots, workspace.ProjectLink{EntryName: filepath.Base(workspaceDirectory), SourcePath: workspaceDirectory})
	files.codeReferences, err = prepareCodeReferences(referenceRoots, codeReferences)
	if err != nil {
		logx.Errorf(
			"prepare chat code references failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
		files.rollback()
		return nil, status_error.BadRequest
	}
	for _, reference := range files.codeReferences {
		if !slices.Contains(files.referencedFiles, reference.Path) {
			files.referencedFiles = append(files.referencedFiles, reference.Path)
		}
	}
	// 附件在执行工作区物化成功后准备；任一附件失败会撤销此前产生的全部可回滚文件。
	attachmentInputs := make([]workspace.AttachmentInput, 0, len(attachments))
	for _, attachment := range attachments {
		attachmentInputs = append(attachmentInputs, workspace.AttachmentInput{
			SourcePath: attachment.SourcePath,
		})
	}
	files.attachments, err = workspace.PrepareChatAttachments(
		conversationDirectory,
		state.chat.ID,
		messageID,
		attachmentInputs,
	)
	if err != nil {
		logx.Errorf(
			"prepare chat attachments failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
		files.rollback()
		return nil, status_error.BadRequest
	}
	files.attachmentReferences = make(
		[]eventlog.AttachmentReference,
		0,
		len(files.attachments.References),
	)
	for _, attachment := range files.attachments.References {
		files.attachmentReferences = append(
			files.attachmentReferences,
			eventlog.AttachmentReference{
				Name:      attachment.Name,
				Path:      attachment.Path,
				MediaType: attachment.MediaType,
				Managed:   attachment.Managed,
			},
		)
	}
	return files, nil
}

func prepareCodeReferences(
	projectLinks []workspace.ProjectLink,
	inputs []CodeReferenceInput,
) ([]eventlog.CodeReference, error) {
	sourcePaths := make([]string, 0, len(inputs))
	for _, input := range inputs {
		sourcePaths = append(sourcePaths, input.Path)
	}
	projectFiles, err := workspace.ResolveProjectFiles(projectLinks, sourcePaths)
	if err != nil {
		return nil, err
	}
	references := make([]eventlog.CodeReference, 0, len(inputs))
	for index, input := range inputs {
		references = append(references, eventlog.CodeReference{
			Path:      projectFiles[index].Path,
			StartLine: input.StartLine,
			EndLine:   input.EndLine,
			Content:   input.Content,
		})
	}
	return references, nil
}

// newExecutionIndex 构造本轮权威事件和 SQLite 查询索引共用的执行身份。
func newExecutionIndex(
	state messageChat,
	request pendingMessage,
	executionID string,
	userMessageID string,
) models.Execution {
	now := nowMillis()
	submissionID, submissionHash := request.submissionID, request.submissionHash
	if state.isNew && submissionID == "" {
		submissionID, submissionHash = request.clientRequestID, request.clientRequestHash
	}
	return models.Execution{
		AutomaticRunID:    request.automaticRunID,
		ID:                executionID,
		ChatID:            state.chat.ID,
		UserMessageID:     userMessageID,
		SubmissionID:      submissionID,
		SubmissionHash:    submissionHash,
		ParentExecutionID: request.parentExecutionID,
		AccountID:         request.runtime.AccountID,
		RuntimeID:         request.runtime.RuntimeID,
		Model:             request.runtime.Model,
		Reasoning:         request.runtime.Reasoning,
		Fast:              request.runtime.Fast,
		ContextWindow:     request.runtime.ContextWindow,
		PermissionLevel:   request.runtime.PermissionLevel,
		WorkspaceReadOnly: request.runtime.WorkspaceReadOnly,
		Status:            constants.ExecutionStatusPending,
		Revision:          1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// startedMessageEvents assembles the frozen business facts for admission.
func startedMessageEvents(
	state messageChat,
	request pendingMessage,
	execution models.Execution,
	invocationID string,
	attachments []eventlog.AttachmentReference,
	codeReferences []eventlog.CodeReference,
	skills []eventlog.MessageSkillReference,
	connectors []eventlog.MessageConnectorReference,
	pluginReferences ...[]plugins.Snapshot,
) []eventlog.AppendInput {
	var selectedPlugins []plugins.Snapshot
	if len(pluginReferences) > 0 {
		selectedPlugins = pluginReferences[0]
	}
	inputs := make([]eventlog.AppendInput, 0, 3)
	if state.isNew {
		inputs = append(inputs, eventlog.AppendInput{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            state.chat.WakerID,
				ClientRequestID:    request.clientRequestID,
				ClientRequestHash:  request.clientRequestHash,
				ProjectID:          state.chat.ProjectID,
				ProjectName:        state.chat.ProjectName,
				WorkspaceDirectory: state.workspaceDirectory,
				AdditionalWorkingDirectories: append(
					[]string(nil),
					state.chat.AdditionalWorkingDirectories...,
				),
				Title:                  state.chat.Title,
				ProjectSources:         append([]eventlog.ProjectSourceSnapshot(nil), state.projectSources...),
				HiddenFromWakerHistory: state.chat.HiddenFromWakerHistory,
			}),
		})
	}
	return append(inputs,
		eventlog.AppendInput{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: execution.UserMessageID,
			Data: marshalData(eventlog.MessageCreatedData{
				SubmissionID:   execution.SubmissionID,
				Role:           constants.MessageRoleUser,
				Actor:          request.message.Actor,
				Content:        request.message.Text,
				Attachments:    attachments,
				Plugins:        selectedPlugins,
				Skills:         skills,
				Connectors:     connectors,
				CodeReferences: codeReferences,
			}),
		},
		eventlog.AppendInput{
			Type:         constants.ChatEventTypeExecutionCreated,
			ExecutionID:  execution.ID,
			InvocationID: invocationID,
			Data: marshalData(eventlog.ExecutionCreatedData{
				RequestedAt:       request.requestedAt,
				SteerExecutionID:  request.message.SteerExecutionID,
				AutomaticRunID:    execution.AutomaticRunID,
				PluginResources:   execution.PluginResources,
				UserMessageID:     execution.UserMessageID,
				SubmissionID:      execution.SubmissionID,
				SubmissionHash:    execution.SubmissionHash,
				ParentExecutionID: execution.ParentExecutionID,
				AccountID:         request.runtime.AccountID,
				RuntimeID:         request.runtime.RuntimeID,
				Model:             request.runtime.Model,
				Reasoning:         request.runtime.Reasoning,
				Fast:              request.runtime.Fast,
				ContextWindow:     request.runtime.ContextWindow,
				PermissionLevel:   request.runtime.PermissionLevel,
				WorkspaceReadOnly: request.runtime.WorkspaceReadOnly,
			}),
		},
	)
}

func selectedSkillReferences(selectedIDs []string, enabled []models.SkillWithState) []eventlog.MessageSkillReference {
	selected := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = struct{}{}
	}
	references := make([]eventlog.MessageSkillReference, 0, len(selected))
	for _, item := range enabled {
		if _, ok := selected[item.SkillPackage.ID]; !ok {
			continue
		}
		references = append(references, eventlog.MessageSkillReference{
			ID:      item.SkillPackage.ID,
			Name:    item.SkillPackage.Name,
			Version: item.CurrentVersion.Version,
		})
	}
	return references
}

func selectedConnectorReferences(
	selectedIDs []string,
	enabled []agent.Connector,
) []eventlog.MessageConnectorReference {
	selected := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = struct{}{}
	}
	references := make([]eventlog.MessageConnectorReference, 0, len(selected))
	for _, connector := range enabled {
		if _, ok := selected[connector.ID]; !ok {
			continue
		}
		references = append(references, eventlog.MessageConnectorReference{
			ID:   connector.ID,
			Name: connector.Name,
		})
	}
	return references
}

// nativeMessage 冻结本轮附件与文件引用，历史交接由 ThreadLifecycle 负责。
func nativeMessage(request pendingMessage, files *preparedMessageFiles) agent.Message {
	attachments := make(
		[]agent.Attachment,
		0,
		len(files.attachmentReferences),
	)
	for index, attachment := range files.attachmentReferences {
		runtimePath := ""
		if index < len(files.attachments.RuntimePaths) {
			runtimePath = files.attachments.RuntimePaths[index]
		}
		attachments = append(attachments, agent.Attachment{
			Name:        attachment.Name,
			Path:        attachment.Path,
			MediaType:   attachment.MediaType,
			RuntimePath: runtimePath,
		})
	}
	codeReferences := make([]agent.CodeReference, 0, len(files.codeReferences))
	for _, reference := range files.codeReferences {
		codeReferences = append(codeReferences, agent.CodeReference{
			Path: runtimeProjectEntryPath(reference.Path, files.projectEntries), StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content,
		})
	}
	return agent.Message{Text: request.message.Text, Attachments: attachments, CodeReferences: codeReferences}
}

// nativeInstructions 合并伙伴与入口已经读取的稳定指令。
func nativeInstructions(request pendingMessage, files *preparedMessageFiles) []agent.Instruction {
	instructions := append([]agent.Instruction(nil), files.instructions...)
	for _, instruction := range request.supplementalInstructions {
		if instruction.Name != "" && instruction.Content != "" {
			instructions = append(instructions, instruction)
		}
	}

	return instructions
}

// nativeSkills 直接读取启用技能的已发布版本位置。
func (m *service) nativeSkills(enabledSkills []models.SkillWithState) []agent.Skill {
	skills := make([]agent.Skill, 0, len(enabledSkills))
	for _, item := range enabledSkills {
		if item.CurrentVersion.ID == "" {
			continue
		}
		skills = append(skills, agent.Skill{
			Name:   item.CurrentVersion.Name,
			Path:   filepath.Join(m.skillDataDirectory, item.CurrentVersion.ContentPath),
			Digest: item.CurrentVersion.ContentDigest,
		})
	}
	return skills
}

// nativeWorkspace 将已接纳的目录授权交给现有原生资源边界。
func nativeWorkspace(state messageChat, request pendingMessage, files *preparedMessageFiles) agent.Workspace {
	directories := slices.Clone(state.additionalDirectories)
	if request.runtime.WorkspaceReadOnly {
		directories = nil
	}
	return agent.Workspace{Path: files.executionWorkspaceDirectory, Managed: true, AdditionalWritableDirectories: directories, ProjectDirectories: runtimeProjectDirectories(state.additionalDirectories, state.projectSources)}
}

// nativeContextInstructions 只携带本轮路径及业务引用。
func nativeContextInstructions(request pendingMessage, files *preparedMessageFiles) []agent.Instruction {
	taskContext := fmt.Sprintf("Your persistent space directory is %q. Organize new files there as needed, or use the user-authorized working directories when the task requires it. Keep modifications to existing files at their existing true paths. Use the system temporary directory (TMPDIR or /tmp) for disposable intermediate files. Attachment paths in this turn identify the exact input files; do not modify the originals or enumerate their parent directories.", files.executionWorkspaceDirectory)
	if request.message.Actor == "workflow" {
		taskContext = fmt.Sprintf("This workflow worker has an isolated execution directory at %q. The current workflow workspace context specifies the owning scripts, state, cache, temporary and run-result directories. Use those owning directories for workflow files. Attachment paths identify the exact input files; do not modify the originals or enumerate their parent directories.", files.executionWorkspaceDirectory)
	}
	contextInstructions := append([]agent.Instruction{{Name: "Current task directories", Content: taskContext}}, request.contextInstructions...)
	if len(files.projectEntries) > 0 {
		encoded, _ := json.Marshal(files.projectEntries)
		contextInstructions = append(contextInstructions, agent.Instruction{Name: "Current project entries", Content: "This native client uses the following cwd-relative project entries. Use each entry path for project tools; sourcePath remains the original authorized file location.\n" + string(encoded)})
	}
	return contextInstructions
}

func selectedSkillNames(request pendingMessage, enabledSkills []models.SkillWithState) []string {
	names := []string{}
	for _, item := range enabledSkills {
		if item.CurrentVersion.ID != "" && slices.Contains(request.message.SkillIDs, item.SkillPackage.ID) {
			names = append(names, item.CurrentVersion.Name)
		}
	}
	return names
}

func selectedRuntimeConnectorsAreEnabled(selected []string, connectors []agent.Connector) bool {
	for _, id := range selected {
		if !slices.ContainsFunc(connectors, func(connector agent.Connector) bool { return connector.ID == id }) {
			return false
		}
	}
	return true
}

// runtimeProjectDirectories 把冻结项目来源转换为 Runtime 可理解的逻辑目录和默认落点。
func runtimeProjectDirectories(
	authorized []string,
	sources []eventlog.ProjectSourceSnapshot,
) []agent.WorkspaceProjectDirectory {
	directories := make([]agent.WorkspaceProjectDirectory, 0, len(sources))
	defaultDirectory := ""
	if len(authorized) > 0 {
		defaultDirectory = authorized[0]
	}
	for _, source := range sources {
		directories = append(directories, agent.WorkspaceProjectDirectory{
			Path:    source.SourcePath,
			Default: sameFilesystemPath(source.SourcePath, defaultDirectory),
		})
	}
	return directories
}

// sameFilesystemPath 按当前平台规则比较两个已经解析的真实目录路径。
func sameFilesystemPath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func connectorSecretValues(
	definitions []models.MCPSecretDefinition,
) map[string]string {
	result := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		if definition.Configured {
			result[definition.Name] = definition.Value
		}
	}
	return result
}

func selectedSkillsAreEnabled(
	selectedSkillIDs []string,
	enabledSkills []models.SkillWithState,
) bool {
	if len(selectedSkillIDs) == 0 {
		return true
	}
	enabled := make(map[string]struct{}, len(enabledSkills))
	for _, item := range enabledSkills {
		enabled[item.SkillPackage.ID] = struct{}{}
	}
	for _, skillID := range selectedSkillIDs {
		if skillID == "" {
			return false
		}
		if _, exists := enabled[skillID]; !exists {
			return false
		}
	}
	return true
}

func selectedConnectorsAreEnabled(
	selectedConnectorIDs []string,
	enabledConnectors []models.MCPConnector,
) bool {
	if len(selectedConnectorIDs) == 0 {
		return true
	}
	enabled := make(map[string]struct{}, len(enabledConnectors))
	for _, connector := range enabledConnectors {
		enabled[connector.ID] = struct{}{}
	}
	for _, connectorID := range selectedConnectorIDs {
		if connectorID == "" {
			return false
		}
		if _, exists := enabled[connectorID]; !exists {
			return false
		}
	}
	return true
}

// ensureInitialExecutionWorkspace 保留普通伙伴目录与宿主领域隔离工作区的不同归属。
func ensureInitialExecutionWorkspace(root, chatID, wakerID string, isolated bool) error {
	if isolated {
		return workspace.EnsureIsolatedExecutionWorkspace(root, chatID, wakerID)
	}
	return workspace.EnsureAgentWorkspace(root, wakerID)
}

// runtimeProjectEntryPath 只映射发给原生工具的路径，不改写日志或成果的真实身份。
func runtimeProjectEntryPath(path string, entries []workspace.ProjectEntry) string {
	selected := ""
	matched := ""
	for _, entry := range entries {
		relative, err := filepath.Rel(entry.SourcePath, path)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if len(entry.SourcePath) > len(matched) {
			matched = entry.SourcePath
			selected = filepath.ToSlash(filepath.Join(entry.Path, relative))
		}
	}
	if selected != "" {
		return selected
	}
	return path
}

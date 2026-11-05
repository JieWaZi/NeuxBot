package execution

import (
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"

	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
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
	// additionalWorkingDirectories 是宿主入口冻结的附加受管目录，不接受智能体参数扩展。
	additionalWorkingDirectories []string
}

// pendingMessage 保存尚未被 Runtime 接受、尚未进入权威日志的输入。
type pendingMessage struct {
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
	// parentExecutionID 是编排或重试产生的可选父执行标识。
	parentExecutionID string
	// retry 表示用户要求重放固定父执行，取得聊天占位后必须重新核对最新资格。
	retry bool
	// pluginSnapshots 在失败重试时限制为原执行冻结的插件资源。
	pluginSnapshots []plugins.Snapshot
	// admission 决定首批事实在 Runtime 调度前还是接受后提交。
	admission constants.ExecutionAdmission
	// toolCapabilities 保存上层入口为本次执行签发的短期内部能力。
	toolCapabilities []ToolCapability
	// allowedAgentDomains 冻结默认软件能力的领域白名单。
	allowedAgentDomains *[]string
	// allowedConnectorIDs 是入口冻结的 MCP 白名单，nil 与空列表具有不同业务含义。
	allowedConnectorIDs *[]string
	// supplementalInstructions 是业务层追加的虚拟上下文文件。
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
) (AcceptedExecution, error) {
	// 先完成纯输入规范化和契约校验，避免任何外部副作用发生后才拒绝请求。
	request = normalizePendingMessage(request)
	if !validCodeReferences(request.message.CodeReferences) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	isNew := request.chatID == ""
	if request.submissionID != "" {
		identity, err := requestidempotency.NewSubmission(request.submissionID, []any{request.chatID, request.message, request.runtime, request.parentExecutionID, request.admission, stableToolCapabilities(request.toolCapabilities), request.supplementalInstructions, request.allowedConnectorIDs, request.allowedAgentDomains})
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
	// Scoped entries require native enforcement over inherited MCP and plugin settings.
	if !agent.SupportsProvider(nativeProvider(runtimeEnvironment)) || request.allowedConnectorIDs != nil || request.allowedAgentDomains != nil {
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
	if request.runtime.AccountID == "" && !request.runtime.ConfigurationResolved {
		if !state.isNew && state.lastExecutionID != "" {
			request.runtime.AccountID = state.chat.LastAccountID
		} else {
			request.runtime.AccountID = runtimeEnvironment.DefaultAccountID
		}
	}
	if request.runtime.AccountID == "system" {
		request.runtime.AccountID = ""
	}
	if request.runtime.AccountID != "" {
		if m.accounts == nil {
			return AcceptedExecution{}, status_error.AIAccountConfigurationInvalid
		}
		account, err := m.accounts.Get(ctx, request.runtime.AccountID)
		if err != nil {
			return AcceptedExecution{}, err
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey {
			if len(account.SelectedModels) == 0 {
				return AcceptedExecution{}, status_error.AIAccountConfigurationInvalid
			}
			if request.runtime.Model == "" && !request.runtime.ConfigurationResolved {
				request.runtime.Model = account.SelectedModels[0]
				if nativeProvider(runtimeEnvironment) == constants.RuntimeIDClaude {
					request.runtime.Model, _ = aiaccount.ManagedClaudeModelTarget(account, "")
				}
			}
			allowed := slices.Contains(account.SelectedModels, request.runtime.Model)
			if nativeProvider(runtimeEnvironment) == constants.RuntimeIDClaude {
				_, allowed = aiaccount.ManagedClaudeModelTarget(account, request.runtime.Model)
			}

			if !allowed {
				return AcceptedExecution{}, status_error.BadRequest
			}
		}
	}
	defaultAccountID := runtimeEnvironment.DefaultAccountID
	if defaultAccountID == constants.AiAccountSystemID {
		defaultAccountID = ""
	}
	if !request.runtime.ConfigurationResolved && request.runtime.AccountID == defaultAccountID {
		if request.runtime.Model == "" {
			request.runtime.Model = runtimeEnvironment.DefaultModel
		}
		if request.runtime.Reasoning == "" && request.runtime.Model == runtimeEnvironment.DefaultModel {
			request.runtime.Reasoning = runtimeEnvironment.DefaultReasoning
		}
	}
	if !constants.SelectablePermissionLevel(requiredPermissionLevel) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	// 已有对话的活跃事实来自权威 JSONL；SQLite 只保存可重建查询投影，不能反向阻断新执行。
	if !state.isNew && state.chat.ActiveExecutionID != "" {
		return AcceptedExecution{}, status_error.ChatHasActiveExecution
	}
	// 上下文和附件必须先完整准备，并在权威提交失败时统一回滚。
	files, err := m.prepareMessageFiles(
		ctx,
		state,
		userMessageID,
		request.message.Attachments,
		request.message.CodeReferences,
		request.runtime.RuntimeID,
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
		enabledConnectors, err = m.connectorController.ListEnabled(
			ctx,
			state.chat.WakerID,
		)
		if err != nil {
			return AcceptedExecution{}, err
		}
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
		request.allowedAgentDomains,
	}
	if request.runtime.Fast != nil {
		content = append(content, map[string]any{"fast": *request.runtime.Fast})
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
	// workspaceDirectory 是 Chat 私有的指令、技能与附件目录。
	workspaceDirectory string
	// projectLinks 是工作区 projects/ 下的来源软链接集合。
	projectLinks []workspace.ProjectLink
	// projectSources 是工作区 projects/ 下全部来源入口的冻结快照。
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
	// projectLinks 是对话执行工作区 projects/ 下的来源软链接。
	projectLinks []workspace.ProjectLink
	// projectSources 是包含目录级指令策略的权威冻结快照。
	projectSources []eventlog.ProjectSourceSnapshot
}

// preparedMessageFiles 保存发送前已经物化且可能需要回滚的文件副作用。
type preparedMessageFiles struct {
	// executionWorkspaceDirectory 是 Chat 私有的受管资源与附件目录。
	executionWorkspaceDirectory string
	// chatID 是执行工作区归属校验使用的对话标识。
	chatID string
	// removeExecutionWorkspaceOnRollback 表示提交失败时需要删除本次新建的执行工作区。
	removeExecutionWorkspaceOnRollback bool
	// storage 用于在新 Chat 提交失败后清理空聚合目录。
	storage *chatstorage.Layout
	// attachments 保存附件运行路径和可回滚文件操作。
	attachments workspace.PreparedAttachments
	// attachmentReferences 是可以写入权威消息事件的安全引用。
	attachmentReferences []eventlog.AttachmentReference
	// codeReferences 保存整文件的权威工作区路径及按输入保留路径的代码片段。
	codeReferences []eventlog.CodeReference
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
	if !f.removeExecutionWorkspaceOnRollback {
		return
	}
	if err := workspace.DeleteExecutionWorkspace(f.executionWorkspaceDirectory, f.chatID); err != nil {
		logx.Errorf(
			"rollback uncommitted chat execution workspace failed: chat_id=%s error=%v",
			f.chatID,
			err,
		)
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
		if err := validateFrozenWorkspace(state.chat); err != nil {
			return messageChat{}, err
		}

		return state, nil
	}

	input := request.create
	if input.wakerID == "" || (input.projectID != "" && input.workingDirectory != "") {
		return messageChat{}, status_error.BadRequest
	}
	if _, err := m.wakerController.Get(ctx, input.wakerID); err != nil {
		return messageChat{}, err
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
	workspaceDirectory, err := m.storage.ChatWorkspace(chatID)
	if err != nil {
		return messageChat{}, err
	}
	if err := workspace.EnsureExecutionWorkspace(
		workspaceDirectory,
		chatID,
		input.wakerID,
		resolvedWorkspace.projectLinks,
	); err != nil {
		return messageChat{}, status_error.ChatDirectoryOwnershipInvalid
	}
	if input.projectID == "" && input.workingDirectory == "" && len(resolvedWorkspace.additionalDirectories) == 0 {
		resolvedWorkspace.additionalDirectories = []string{workspaceDirectory}
	}

	now := nowMillis()
	chat := models.Chat{
		HiddenFromWakerHistory:       input.hiddenFromWakerHistory,
		ID:                           chatID,
		WakerID:                      input.wakerID,
		ProjectID:                    resolvedWorkspace.projectID,
		ProjectName:                  resolvedWorkspace.projectName,
		WorkspaceDirectory:           workspaceDirectory,
		AdditionalWorkingDirectories: resolvedWorkspace.additionalDirectories,
		Title:                        defaultTitle(request.message.Text),
		ActiveExecutionID:            executionID,
		LastAccountID:                request.runtime.AccountID,
		LastRuntimeID:                request.runtime.RuntimeID,
		LastModel:                    request.runtime.Model,
		LastFast:                     request.runtime.Fast,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	return messageChat{
		isNew:              true,
		chat:               chat,
		workspaceDirectory: workspaceDirectory,
		projectLinks:       resolvedWorkspace.projectLinks,
		projectSources:     resolvedWorkspace.projectSources,
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

// validateFrozenWorkspace 确认已有 Chat 冻结的全部目录仍然可用。
func validateFrozenWorkspace(chat models.Chat) error {
	if chat.WorkspaceDirectory == "" || len(chat.AdditionalWorkingDirectories) == 0 {
		return status_error.ProjectSourceUnavailable
	}
	for _, directory := range chat.AdditionalWorkingDirectories {
		if _, resolveErr := resolveWorkingDirectory(directory); resolveErr != nil {
			return status_error.ProjectSourceUnavailable
		}
	}
	return nil
}

func ensureMessageExecutionWorkspace(state messageChat, projectLinks []workspace.ProjectLink) error {
	threadWorkspaceID := strings.TrimSpace(state.threadWorkspaceID)
	if threadWorkspaceID != "" {
		return workspace.EnsureExistingThreadMemberExecutionWorkspace(
			state.workspaceDirectory,
			threadWorkspaceID,
			state.chat.ID,
			state.chat.WakerID,
		)
	}
	return workspace.EnsureExecutionWorkspace(
		state.workspaceDirectory,
		state.chat.ID,
		state.chat.WakerID,
		projectLinks,
	)
}

// prepareMessageFiles 原子物化执行上下文并准备消息附件。
func (m *service) prepareMessageFiles(
	ctx context.Context,
	state messageChat,
	messageID string,
	attachments []AttachmentInput,
	codeReferences []CodeReferenceInput,
	runtimeID string,
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
	if len(projectLinks) == 0 {
		managedDirectory := workspaceDirectory
		if state.threadWorkspaceID != "" {
			var err error
			managedDirectory, err = m.storage.TeamWorkspace(state.threadWorkspaceID)
			if err != nil {
				return nil, err
			}
			owner, err := workspace.ReadExecutionWorkspaceOwner(managedDirectory)
			if err != nil || owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != state.threadWorkspaceID {
				return nil, status_error.ChatDirectoryOwnershipInvalid
			}
			managedDirectory, err = m.storage.TeamProjects(state.threadWorkspaceID)
			if err != nil {
				return nil, err
			}
		}
		validDirectories := len(state.chat.AdditionalWorkingDirectories) == 1 && filepath.Clean(state.chat.AdditionalWorkingDirectories[0]) == filepath.Clean(managedDirectory)

		if state.chat.ProjectID != "" || !validDirectories {
			return nil, status_error.ChatDirectoryOwnershipInvalid
		}
	}
	runtimeFiles, err := m.resolveRuntimeWorkspaceFiles(runtimeID)
	if err != nil {
		return nil, status_error.RuntimeUnavailable
	}
	if err := ensureMessageExecutionWorkspace(state, projectLinks); err != nil {
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
	profileContents, err := builtinrolepreset.ResolveWakerContents(
		state.chat.WakerID,
		storedDocuments,
	)
	if err != nil {
		return nil, err
	}
	instructions := make([]agent.Instruction, 0, len(profileContents))
	// 档案按固定业务顺序进入静态配置，避免相同内容因 map 遍历顺序变化重建原生会话。
	for _, kind := range profile.EditableKinds() {
		content, exists := profileContents[kind]
		if !exists {
			continue
		}
		fileName, ok := constants.ProfileDocumentFileName(kind)
		if !ok {
			fileName = constants.ProfileDocumentDefaultFileName
		}
		instructions = append(instructions, agent.Instruction{
			Name:    fileName,
			Content: content,
		})
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
				Name: filepath.Join(
					constants.ExecutionWorkspaceProjectsDirectory,
					source.EntryName,
					runtimeFiles.instructionFileName,
				),
				Content: content,
			})
		}
	}
	files := &preparedMessageFiles{
		executionWorkspaceDirectory:        workspaceDirectory,
		chatID:                             state.chat.ID,
		removeExecutionWorkspaceOnRollback: state.isNew,
		storage:                            m.storage,
		attachments: workspace.PreparedAttachments{
			Rollback: func() error { return nil },
		},
		instructions: instructions,
	}
	files.codeReferences, err = prepareCodeReferences(projectLinks, codeReferences)
	if err != nil {
		logx.Errorf(
			"prepare chat code references failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
		files.rollback()
		return nil, status_error.BadRequest
	}
	// 附件在执行工作区物化成功后准备；任一附件失败会撤销此前产生的全部可回滚文件。
	attachmentInputs := make([]workspace.AttachmentInput, 0, len(attachments))
	for _, attachment := range attachments {
		attachmentInputs = append(attachmentInputs, workspace.AttachmentInput{
			SourcePath: attachment.SourcePath,
		})
	}
	files.attachments, err = workspace.PrepareChatAttachments(
		workspaceDirectory,
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
		if input.StartLine == 0 && input.EndLine == 0 && input.Content == "" {
			sourcePaths = append(sourcePaths, input.Path)
		}
	}
	projectFiles, err := workspace.ResolveProjectFiles(projectLinks, sourcePaths)
	if err != nil {
		return nil, err
	}
	projectFileIndex := 0
	references := make([]eventlog.CodeReference, 0, len(inputs))
	for _, input := range inputs {
		if input.StartLine == 0 && input.EndLine == 0 && input.Content == "" {
			if projectFileIndex >= len(projectFiles) {
				return nil, status_error.ProjectSourceUnavailable
			}
			references = append(references, eventlog.CodeReference{
				Path: projectFiles[projectFileIndex].Path,
			})
			projectFileIndex++
			continue
		}
		references = append(references, eventlog.CodeReference{
			Path:      filepath.ToSlash(filepath.Clean(input.Path)),
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

// buildSessionPlan 分别声明稳定会话、本轮输入和续接指令。
func (m *service) buildSessionPlan(
	state messageChat,
	request pendingMessage,
	files *preparedMessageFiles,
	enabledSkills []models.SkillWithState,
	enabledConnectors []models.MCPConnector,
	runtimeEnvironment models.RuntimeEnvironment,
) (agent.SessionPlan, agent.TurnInput) {
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
			Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content,
		})
	}
	skills := make([]agent.Skill, 0, len(enabledSkills))
	selectedSkillIDs := make(map[string]struct{}, len(request.message.SkillIDs))
	for _, skillID := range request.message.SkillIDs {
		selectedSkillIDs[skillID] = struct{}{}
	}
	for _, item := range enabledSkills {
		if item.CurrentVersion.ID == "" {
			continue
		}
		skills = append(skills, agent.Skill{
			Name: item.CurrentVersion.Name,
			Path: filepath.Join(m.skillDataDirectory, item.CurrentVersion.ContentPath),
		})
	}
	connectors := runtimeConnectorDeclarations(enabledConnectors)
	instructions := append([]agent.Instruction(nil), files.instructions...)
	for _, instruction := range request.supplementalInstructions {
		if instruction.Name == "" || instruction.Content == "" {
			continue
		}
		instructions = append(instructions, instruction)
	}

	additionalWritableDirectories := append([]string(nil), state.chat.AdditionalWorkingDirectories...)

	if request.runtime.WorkspaceReadOnly {
		additionalWritableDirectories = nil
	}
	sessionPlan := agent.SessionPlan{
		Target: agent.SessionTarget{
			RuntimeID:  request.runtime.RuntimeID,
			Binary:     runtimeEnvironment.Binary,
			PrefixArgs: append([]string(nil), runtimeEnvironment.PrefixArgs...),
			ConfigHome: runtimeEnvironment.ConfigHome,
		},
		Detection: runtimeEnvironment.Detection,
		Workspace: agent.Workspace{
			Path:                          files.executionWorkspaceDirectory,
			Managed:                       true,
			AdditionalWritableDirectories: additionalWritableDirectories,
			ProjectDirectories:            runtimeProjectDirectories(state.chat, state.projectSources),
		},
		Instructions: instructions,
		Skills:       skills,
		Connectors:   connectors,
		Policy: agent.RunPolicy{
			Model:     request.runtime.Model,
			Reasoning: request.runtime.Reasoning,
			Execution: executionPolicy(
				request.runtime.PermissionLevel,
				request.runtime.WorkspaceReadOnly,
			),
		},
	}
	turn := agent.TurnInput{
		Message: agent.Message{Text: request.message.Text, Attachments: attachments, CodeReferences: codeReferences},
	}
	for _, item := range enabledSkills {
		if item.CurrentVersion.ID != "" && hasSkillID(selectedSkillIDs, item.SkillPackage.ID) {
			turn.SelectedSkills = append(turn.SelectedSkills, item.CurrentVersion.Name)
		}
	}
	turn.SelectedConnectors = slices.Clone(request.message.ConnectorIDs)
	return sessionPlan, turn
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
	chat models.Chat,
	sources []eventlog.ProjectSourceSnapshot,
) []agent.WorkspaceProjectDirectory {
	directories := make([]agent.WorkspaceProjectDirectory, 0, len(sources))
	defaultDirectory := ""
	if len(chat.AdditionalWorkingDirectories) > 0 {
		defaultDirectory = chat.AdditionalWorkingDirectories[0]
	}
	for _, source := range sources {
		directories = append(directories, agent.WorkspaceProjectDirectory{
			Path: filepath.ToSlash(filepath.Join(
				constants.ExecutionWorkspaceProjectsDirectory,
				source.EntryName,
			)),
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

func hasSkillID(skillIDs map[string]struct{}, skillID string) bool {
	_, exists := skillIDs[skillID]
	return exists
}

func executionPolicy(level agent.PermissionLevel, workspaceReadOnly bool) agent.ExecutionPolicy {
	return agent.ExecutionPolicy{PermissionLevel: level, WorkspaceReadOnly: workspaceReadOnly}
}

func runtimePermissionSupported(
	environment models.RuntimeEnvironment,
	modelID string,
	level agent.PermissionLevel,
) bool {
	selectedID := strings.TrimSpace(modelID)
	if selectedID == "" {
		selectedID = environment.DefaultModel
	}
	levels := agent.ModelPermissionLevels(
		environment.Detection,
		environment.CustomModels,
		selectedID,
	)
	// 未登录时可能无法取得模型权限目录，留待原生 Runtime 返回鉴权错误。
	if len(levels) == 0 && environment.Detection.Auth == agent.AuthStatusMissing {
		return constants.SelectablePermissionLevel(level)
	}
	return agent.PermissionLevelSupported(levels, level)
}

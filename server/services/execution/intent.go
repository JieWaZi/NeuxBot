package execution

import (
	"context"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agentcap "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ToolCapability 是上层业务为单次执行签发的精确软件操作。
type ToolCapability struct {
	// ID 是业务能力集合的稳定内部身份。
	ID string
	// Name 是能力集合的业务名称。
	Name string
	// Bindings 是服务端冻结的领域操作；函数和凭据不参与序列化。
	Bindings []agentcap.Binding `json:"-"`
	// validateWork 在准备连接时核对这组能力是否仍属于实际运行的 Chat 和执行。
	validateWork func(models.Chat, string) error
	// publishFinal 在执行终态提交前交付本次已保存、可辨认的用户答复。
	publishFinal func(context.Context, func(context.Context) (string, error)) error
	// finishTurn 让工单记录已持久化的本轮终态，生命周期仍由执行宿主管理。
	finishTurn func(context.Context, constants.ExecutionStatus) error
}

// WorkflowTurnBinding 保存为当前接纳回合准备的工作流选择与受限能力。
type WorkflowTurnBinding struct {
	// Reference 是验证并固定后的用户选择，普通历史询问为空。
	Reference *workinput.WorkflowReference
	// Capability 是可用于本轮的工作流命令，声明为空时不注入工具。
	Capability *ToolCapability
	// AuthoringDefinitionID 是该创作会话拥有的固定定义，普通聊天留空。
	AuthoringDefinitionID string
	// DeveloperInstruction 说明本地或消费工具集的稳定用法。
	DeveloperInstruction string
}

// AdmissionBoundary 让宿主在耐久接纳时校验仍然有效的提交来源。
type AdmissionBoundary func(context.Context, func(context.Context) error) error

// Commit 在可选来源授权边界内提交接纳事实。
func (boundary AdmissionBoundary) Commit(ctx context.Context, commit func(context.Context) error) error {
	if boundary != nil {
		return boundary(ctx, commit)
	}
	return commit(ctx)
}

// WorkIntent 是所有产品入口提交给 Execution Core 的中立工作契约。
type WorkIntent struct {
	// AutomaticRunID 是服务端编排传入的原自动运行，外部正文不能绑定。
	AutomaticRunID string `json:"-"`
	// ExecutionID 是上层持久编排预先分配的可选执行标识；普通入口留空并由 Execution Core 生成。
	ExecutionID string
	// UserMessageID 是宿主为需要引用耐久用户输入的入口预留的消息标识。
	UserMessageID string `json:"-"`
	// ClientRequestID 是新建 Chat 首次发送时由客户端稳定复用的幂等标识；已有 Chat 续发必须为空。
	ClientRequestID string
	// Chat 描述目标已有 Chat 或新建 Chat 规格。
	Chat ChatTarget
	// Input 描述本次用户或系统输入及文件引用。
	Input WorkInput
	// Ticket 描述显式开单或已经核对过的公开引用。
	Ticket WorkTicketInput `json:"-"`
	// TeamReplyThreadID 是团队域为主会话回复冻结的归属，普通续发为空。
	TeamReplyThreadID string `json:"-"`
	// MessagePlugins 是上层为公开消息冻结的其他成员插件引用，不授予当前执行插件能力。
	MessagePlugins []plugins.Snapshot `json:"-"`
	// Options 描述本次 Runtime、模型和 Session 选择。
	Options WorkOptions
	// Admission 描述本次工作何时进入权威事实源。
	Admission constants.ExecutionAdmission
	// AdmissionBoundary 可选地保护派发前的耐久接纳，不随原生执行传递。
	AdmissionBoundary AdmissionBoundary `json:"-"`
	// BeforeQueuedNativeCommit 在队列输入完成授权后、原生输入批次落盘前登记交接边界。
	BeforeQueuedNativeCommit func(context.Context) error `json:"-"`
	// ClaimAttachments 在幂等检查后将原附件引用转换为本地路径，回调由宿主提供。
	// 返回的释放函数以权威接纳结果结算附件租约。
	ClaimAttachments func([]AttachmentInput) (func(bool), error) `json:"-"`
}

// ChatTarget 描述一个已有 Chat 或待创建 Chat，两者必须且只能提供一个。
type ChatTarget struct {
	// ChatID 是已有 Chat 的稳定标识。
	ChatID string
	// Create 是新建 Chat 时冻结伙伴和工作范围的规格。
	Create *CreateChatSpec
	// MainConversationSource 允许主会话入口在已预留的 ChatID 下首次建立隐藏工作记录。
	MainConversationSource *CreateChatSpec `json:"-"`
}

// CreateChatSpec 保存新 Chat 需要永久冻结的伙伴和工作范围。
type CreateChatSpec struct {
	// ProfileOptimization 表示 Chat 用于持续修订伙伴档案。
	ProfileOptimization bool
	// HiddenFromWakerHistory 表示 Chat 由上层工作入口拥有，仅从该入口展示历史。
	HiddenFromWakerHistory bool
	// WakerID 是新 Chat 永久绑定的伙伴标识。
	WakerID string
	// ProjectID 是用户选择的伙伴项目标识，临时文件夹 Chat 为空。
	ProjectID string
	// ProjectScope 是主会话交办在选定工作 Chat 前冻结的项目范围。
	ProjectScope *MainProjectScope `json:"-"`
	// WorkingDirectory 是用户授权的临时目录，项目 Chat 为空。
	WorkingDirectory string
	// ExecutionWorkspaceDirectory 是宿主领域拥有的真实隔离 cwd；普通 Chat 留空。
	ExecutionWorkspaceDirectory string `json:"-"`
	// AdditionalWorkingDirectories 是宿主业务入口追加的受管目录，外部输入不能扩大工作范围。
	AdditionalWorkingDirectories []string `json:"-"`
}

// PrepareChatGroupIntent 描述团队首发前需要成组建立的一人一个成员 Chat。
type PrepareChatGroupIntent struct {
	// ThreadID 是即将创建的团队对话标识，也是共享执行工作区目录名。
	ThreadID string
	// ProvisioningID 是持久 Thread 锚点与临时工作区共同使用的唯一首发标识。
	ProvisioningID string
	// Scope 是由团队负责人身份解析项目或临时目录的工作范围。
	Scope CreateChatSpec
	// Title 是所有成员 Chat 首发时共享的本地标题。
	Title string
	// Members 是需要建立独立 Chat 的有序成员执行快照。
	Members []PrepareChatMember
}

// PrepareChatMember 描述一位团队成员的 Chat 身份和冻结执行默认值。
type PrepareChatMember struct {
	// ChatID 是 Thread 锚点在文件物化前预分配的成员 Chat 标识。
	ChatID string
	// WakerID 是成员 Chat 永久绑定的伙伴标识。
	WakerID string
	// RuntimeID 是团队对话为该成员冻结的 Runtime 标识。
	RuntimeID string
	// AccountID 是团队对话为该成员冻结的账号；system 表示原生登录。
	AccountID string
	// Model 是团队对话为该成员冻结的模型标识。
	Model string
	// Reasoning 是团队对话为该成员冻结的推理强度。
	Reasoning string
	// Fast 是团队对话为该成员冻结的加速选择；nil 保留原生默认。
	Fast *bool
	// PermissionLevel 是团队对话为该成员冻结的产品权限等级。
	PermissionLevel constants.PermissionLevel
}

// PreparedChatGroup 是团队首发前已经进入权威日志的有序成员 Chat。
type PreparedChatGroup struct {
	// Chats 是与输入成员顺序一致、只有 chat.created 事实的一人一个 Chat。
	Chats []models.Chat
}

// WorkInput 是跨执行与持久队列边界共享的中立工作输入。
type WorkInput = workinput.Input

// WorkbenchEntryPoint 标识由普通伙伴工作台入口接纳的用户输入。
const WorkbenchEntryPoint = "workbench"

// WorkOptions 保存一次执行的 Runtime 和 Session 选择。
type WorkOptions struct {
	// EntryPoint 是宿主认证的入口标签，不接受来自外部请求的覆盖。
	EntryPoint string `json:"-"`
	// ConfigurationResolved 表示编排入口已冻结配置，合法空值不得再次继承当前默认选择。
	ConfigurationResolved bool
	// AccountID 是本次冻结的账号标识；空值使用智能体默认配置。
	AccountID string
	// RuntimeID 是本次使用的开放 Runtime 标识。
	RuntimeID string
	// Model 是本次使用的模型标识。
	Model string
	// Reasoning 是本次使用的推理强度。
	Reasoning string
	// Fast 是本轮明确选择的 Fast 开关。
	Fast *bool
	// ContextWindow 是本轮选择的原生窗口档位。
	ContextWindow string
	// PermissionLevel 是本次执行冻结的跨 Runtime 产品权限等级。
	PermissionLevel constants.PermissionLevel
	// WorkspaceReadOnly 表示 Runtime 只能读取工作区，不能请求或执行文件修改。
	WorkspaceReadOnly bool
	// SessionPolicy 表示续接、强制新建或未来禁用 Session 的行为策略。
	SessionPolicy constants.RuntimeSessionPolicy
	// HistorySourceChatID 将主会话的显式接力选择限制在用户选定的来源。
	HistorySourceChatID string `json:",omitempty"`
	// HistoryTurns 是新建原生会话时携带的最近完整对话轮数，零值使用默认值。
	HistoryTurns int
	// HistoryMode 决定新会话使用近期对话还是旧会话生成的隐藏交接内容。
	HistoryMode constants.HistoryMode
	// SummaryModel 是旧会话生成交接内容时使用的模型。
	SummaryModel string
	// SummaryReasoning 是旧会话生成交接内容时使用的推理强度。
	SummaryReasoning string
	// ParentExecutionID 是重试或编排衍生工作的可选父执行标识。
	ParentExecutionID string
	// ToolCapabilities 是调用入口为本次工作签发的短期内部工具能力。
	ToolCapabilities []ToolCapability
	// WorkflowCreationSessionID 提供创作页当前工作流的会话及保存记录归属。
	WorkflowCreationSessionID string
	// AllowedAgentDomains 限制默认软件能力领域；nil 使用普通对话范围，空列表不签发默认能力。
	AllowedAgentDomains *[]string
	// AllowedConnectorIDs 是入口冻结的 MCP 白名单；nil 继承伙伴，空白名单排除全部伙伴 MCP。
	AllowedConnectorIDs *[]string
	// SupplementalInstructions 是业务层追加的虚拟上下文文件（如 TEAM.md）。
	SupplementalInstructions []agentapi.Instruction
	// HistoryMessageIDsToOmit 是本次输入已完整包含、无需再从权威历史重放的消息标识。
	HistoryMessageIDsToOmit []string
}

// AcceptedExecution 是首批事实 durable 后返回给入口的稳定结果。
type AcceptedExecution struct {
	// Chat 是已经进入权威事实源的 Chat 投影。
	Chat models.Chat `json:"chat"`
	// Execution 是已经持久、处于 pending 或 running 的执行投影。
	Execution models.Execution `json:"execution"`
	// Replayed 表示本次返回的是同一输入请求已经提交的确定结果。
	Replayed bool `json:"replayed,omitempty"`
	// Published 表示原聊天输入投递已接纳，实际执行位于其他工作来源。
	Published bool `json:"published,omitempty"`
}

// Start 校验并执行一个入口无关的工作意图。
func (m *service) Start(ctx context.Context, intent WorkIntent) (AcceptedExecution, error) {
	requestedAt := nowMillis()
	if intent.Chat.MainConversationSource == nil && m.validateIntent != nil {
		if err := m.validateIntent(ctx, intent); err != nil {
			return AcceptedExecution{}, err
		}
	}

	inputContext := ctx
	intent.Chat.ChatID = strings.TrimSpace(intent.Chat.ChatID)
	intent.Input.ReplyToMessageID = strings.TrimSpace(intent.Input.ReplyToMessageID)
	if intent.Input.AttachmentOnly && len(intent.Input.Attachments) == 0 {
		return AcceptedExecution{}, status_error.MessageAttachmentInvalid
	}
	teamReply := intent.TeamReplyThreadID != ""
	if intent.Input.ReplyToMessageID != "" &&
		(intent.Chat.ChatID == "" || intent.Chat.Create != nil || (!teamReply && intent.Options.EntryPoint != WorkbenchEntryPoint && intent.Options.EntryPoint != MainConversationSourceEntryPoint) ||
			(teamReply && intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch) ||
			strings.TrimSpace(intent.Input.Actor) != string(constants.ChannelMessageActorUser) ||
			(!teamReply && intent.AutomaticRunID != "") || intent.Input.IMReplyLinkID != "" || intent.Options.WorkflowCreationSessionID != "") {
		return AcceptedExecution{}, status_error.ChatReplyTargetUnavailable
	}
	releaseTeam := func() {}
	if m.acquireTeamChatInput != nil && intent.Chat.ChatID != "" {
		admissionContext := ctx
		if intent.Input.ReplyToMessageID != "" && !teamReply {
			admissionContext = teamwork.WithMemberReplyRoute(ctx)
		}
		teamUserInput := intent.Input.Actor == "" || intent.Input.Actor == string(constants.ChannelMessageActorUser)
		locked, release, err := m.acquireTeamChatInput(admissionContext, intent.Chat.ChatID, teamUserInput)
		if err != nil {
			return AcceptedExecution{}, err
		}
		ctx = locked
		releaseTeam = sync.OnceFunc(release)
	}
	defer releaseTeam()
	wakerID, err := m.admissionWaker(ctx, intent.Chat)
	if err != nil {
		return AcceptedExecution{}, err
	}
	releaseOperation, operationErr := chatstorage.AcquireAdmission(m.storage.Root(), wakerID, intent.Chat.ChatID)
	if operationErr != nil {
		return AcceptedExecution{}, operationErr
	}
	defer releaseOperation()
	// 预留来源没有 Chat 日志。取得同一个删除边界后再核对绑定，防止团队删除后重新物化。
	if intent.Chat.MainConversationSource != nil && m.validateIntent != nil {
		if err := m.validateIntent(ctx, intent); err != nil {
			return AcceptedExecution{}, err
		}
	}
	if err := m.ensureMainConversationSource(ctx, intent); err != nil {
		return AcceptedExecution{}, err
	}
	if intent.Admission == "" {
		intent.Admission = constants.ExecutionAdmissionCommitOnRuntimeAccept
	}
	if intent.Options.SessionPolicy == "" {
		intent.Options.SessionPolicy = constants.RuntimeSessionPolicyContinue
	}
	queuedGate := intent.Admission == constants.ExecutionAdmissionCommitAtQueuedNativeGate
	if intent.Admission != constants.ExecutionAdmissionCommitOnRuntimeAccept &&
		intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch && !queuedGate {
		return AcceptedExecution{}, status_error.BadRequest
	}
	if intent.AdmissionBoundary != nil && intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch && !queuedGate {
		return AcceptedExecution{}, status_error.BadRequest
	}
	if queuedGate != (intent.BeforeQueuedNativeCommit != nil) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	if queuedGate && (intent.AdmissionBoundary == nil || intent.Chat.ChatID == "" || intent.Chat.Create != nil ||
		intent.Input.SubmissionID == "" || intent.Input.SteerExecutionID != "" ||
		(intent.Options.EntryPoint != MainConversationEntryPoint && intent.Options.EntryPoint != MainConversationSourceEntryPoint)) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	request := pendingMessage{automaticRunID: intent.AutomaticRunID, imReplyLinkID: intent.Input.IMReplyLinkID,
		entryPoint:        intent.Options.EntryPoint,
		executionID:       intent.ExecutionID,
		userMessageID:     intent.UserMessageID,
		submissionID:      intent.Input.SubmissionID,
		clientRequestID:   intent.ClientRequestID,
		chatID:            intent.Chat.ChatID,
		replyToMessageID:  intent.Input.ReplyToMessageID,
		ticket:            intent.Ticket,
		teamReplyThreadID: intent.TeamReplyThreadID,
		message: userMessage{
			Actor:             intent.Input.Actor,
			SteerExecutionID:  intent.Input.SteerExecutionID,
			Text:              intent.Input.Content,
			AttachmentOnly:    intent.Input.AttachmentOnly,
			CodeReferenceOnly: intent.Input.CodeReferenceOnly,
			Attachments:       append([]AttachmentInput(nil), intent.Input.Attachments...),
			CodeReferences:    append([]CodeReferenceInput(nil), intent.Input.CodeReferences...),
			Workflow:          intent.Input.Workflow,
			SkillIDs:          append([]string(nil), intent.Input.SkillIDs...),
			ConnectorIDs:      append([]string(nil), intent.Input.ConnectorIDs...),
			PluginIDs:         append([]string(nil), intent.Input.PluginIDs...),
		},
		messagePlugins: intent.MessagePlugins,
		runtime: runtimeSelection{
			ConfigurationResolved: intent.Options.ConfigurationResolved,
			AccountID:             intent.Options.AccountID,
			RuntimeID:             intent.Options.RuntimeID,
			Model:                 intent.Options.Model,
			Reasoning:             intent.Options.Reasoning,
			Fast:                  intent.Options.Fast,
			ContextWindow:         intent.Options.ContextWindow,
			PermissionLevel:       intent.Options.PermissionLevel,
			WorkspaceReadOnly:     intent.Options.WorkspaceReadOnly,
			SessionPolicy:         intent.Options.SessionPolicy,
			HistorySourceChatID:   intent.Options.HistorySourceChatID,
			HistoryTurns:          intent.Options.HistoryTurns,
			HistoryMode:           intent.Options.HistoryMode,
			SummaryModel:          intent.Options.SummaryModel,
			SummaryReasoning:      intent.Options.SummaryReasoning,
		},
		parentExecutionID:         intent.Options.ParentExecutionID,
		admission:                 intent.Admission,
		admissionBoundary:         intent.AdmissionBoundary,
		beforeQueuedNativeCommit:  intent.BeforeQueuedNativeCommit,
		toolCapabilities:          cloneToolCapabilities(intent.Options.ToolCapabilities),
		workflowCreationSessionID: intent.Options.WorkflowCreationSessionID,
		allowedConnectorIDs:       cloneConnectorAllowlist(intent.Options.AllowedConnectorIDs),
		allowedAgentDomains:       cloneConnectorAllowlist(intent.Options.AllowedAgentDomains),
		historyMessageIDsToOmit:   append([]string(nil), intent.Options.HistoryMessageIDsToOmit...),
		supplementalInstructions: append(
			[]agentapi.Instruction(nil),
			intent.Options.SupplementalInstructions...,
		),
	}
	if intent.Chat.Create != nil {
		request.create = &pendingChat{
			profileOptimization:          intent.Chat.Create.ProfileOptimization,
			hiddenFromWakerHistory:       intent.Chat.Create.HiddenFromWakerHistory,
			executionWorkspaceDirectory:  intent.Chat.Create.ExecutionWorkspaceDirectory,
			wakerID:                      intent.Chat.Create.WakerID,
			projectID:                    intent.Chat.Create.ProjectID,
			workingDirectory:             intent.Chat.Create.WorkingDirectory,
			additionalWorkingDirectories: append([]string(nil), intent.Chat.Create.AdditionalWorkingDirectories...),
		}
	}
	for _, instruction := range intent.Input.ContextInstructions {
		request.contextInstructions = append(request.contextInstructions, agentapi.Instruction{Name: instruction.Name, Content: instruction.Content})
	}
	request.requestedAt = requestedAt
	// 只传入调用方的 Context，执行占位接管团队保护后不会继续携带本入口的重入标记。
	return m.startMessage(inputContext, request, intent.ClaimAttachments, releaseTeam)
}

// cloneToolCapabilities 深拷贝内部能力，避免调用方在执行期间改变审批白名单。
func cloneToolCapabilities(capabilities []ToolCapability) []ToolCapability {
	result := make([]ToolCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		clone := capability
		clone.Bindings = append([]agentcap.Binding(nil), capability.Bindings...)
		result = append(result, clone)
	}
	return result
}

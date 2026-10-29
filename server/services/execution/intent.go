package execution

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
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
	Bindings []agentcapability.Binding `json:"-"`
}

// WorkIntent 是所有产品入口提交给 Execution Core 的中立工作契约。
type WorkIntent struct {
	// AutomaticRunID 是服务端编排传入的原自动运行，外部正文不能绑定。
	AutomaticRunID string `json:"-"`
	// RetryExecutionID 表示由用户重试的已失败执行；输入与配置从权威记录恢复。
	RetryExecutionID string
	// ExecutionID 是上层持久编排预先分配的可选执行标识；普通入口留空并由 Execution Core 生成。
	ExecutionID string
	// ClientRequestID 是新建 Chat 首次发送时由客户端稳定复用的幂等标识；已有 Chat 续发必须为空。
	ClientRequestID string
	// Chat 描述目标已有 Chat 或新建 Chat 规格。
	Chat ChatTarget
	// Input 描述本次用户或系统输入及文件引用。
	Input WorkInput
	// Options 描述本次 Runtime、模型和 Session 选择。
	Options WorkOptions
	// Admission 描述本次工作何时进入权威事实源。
	Admission constants.ExecutionAdmission
}

// ChatTarget 描述一个已有 Chat 或待创建 Chat，两者必须且只能提供一个。
type ChatTarget struct {
	// ChatID 是已有 Chat 的稳定标识。
	ChatID string
	// Create 是新建 Chat 时冻结伙伴和工作范围的规格。
	Create *CreateChatSpec
}

// CreateChatSpec 保存新 Chat 需要永久冻结的伙伴和工作范围。
type CreateChatSpec struct {
	// HiddenFromWakerHistory 表示 Chat 由上层工作入口拥有，仅从该入口展示历史。
	HiddenFromWakerHistory bool
	// WakerID 是新 Chat 永久绑定的伙伴标识。
	WakerID string
	// ProjectID 是用户选择的伙伴项目标识，临时文件夹 Chat 为空。
	ProjectID string
	// WorkingDirectory 是用户授权的临时目录，项目 Chat 为空。
	WorkingDirectory string
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

// WorkOptions 保存一次执行的 Runtime 和 Session 选择。
type WorkOptions struct {
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
	// PermissionLevel 是本次执行冻结的跨 Runtime 产品权限等级。
	PermissionLevel constants.PermissionLevel
	// WorkspaceReadOnly 表示 Runtime 只能读取工作区，不能请求或执行文件修改。
	WorkspaceReadOnly bool
	// SessionPolicy 表示续接、强制新建或未来禁用 Session 的行为策略。
	SessionPolicy constants.RuntimeSessionPolicy
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
	// AllowedAgentDomains 限制默认软件能力领域；nil 使用普通对话范围，空列表不签发默认能力。
	AllowedAgentDomains *[]string
	// AllowedConnectorIDs 是入口冻结的 MCP 白名单；nil 继承伙伴，空白名单排除全部伙伴 MCP。
	AllowedConnectorIDs *[]string
	// SupplementalInstructions 是业务层追加的虚拟上下文文件（如 TEAM.md）。
	SupplementalInstructions []runtimecontract.Instruction
	// HistoryMessageIDsToOmit 是本次输入已完整包含、无需再从权威历史重放的消息标识。
	HistoryMessageIDsToOmit []string
}

// AcceptedExecution 是首批事实 durable 后返回给入口的稳定结果。
type AcceptedExecution struct {
	// Chat 是已经进入权威事实源的 Chat 投影。
	Chat models.Chat `json:"chat"`
	// Execution 是已经持久、处于 pending 或 running 的执行投影。
	Execution models.Execution `json:"execution"`
	// WorkspaceWarningCode 是 Runtime 不支持附加目录时的稳定提示码。
	WorkspaceWarningCode string `json:"workspaceWarningCode,omitempty"`
	// Replayed 表示本次返回的是相同首次创建请求已经提交的确定结果。
	Replayed bool `json:"replayed,omitempty"`
}

// Start 校验并执行一个入口无关的工作意图。
func (m *service) Start(ctx context.Context, intent WorkIntent) (AcceptedExecution, error) {
	if m.validateIntent != nil {
		if err := m.validateIntent(ctx, intent); err != nil {
			return AcceptedExecution{}, err
		}
	}
	if intent.RetryExecutionID != "" {
		return m.retryExecution(ctx, strings.TrimSpace(intent.Chat.ChatID), strings.TrimSpace(intent.RetryExecutionID))
	}

	inputContext := ctx
	releaseTeam := func() {}
	if m.acquireTeamChatInput != nil && strings.TrimSpace(intent.Chat.ChatID) != "" {
		locked, release, err := m.acquireTeamChatInput(ctx, strings.TrimSpace(intent.Chat.ChatID), intent.Input.Actor == "" || intent.Input.Actor == string(constants.ChannelMessageActorUser))
		if err != nil {
			return AcceptedExecution{}, err
		}
		ctx = locked
		releaseTeam = release
	}
	intent.Chat.ChatID = strings.TrimSpace(intent.Chat.ChatID)
	releaseOperation, operationErr := chatstorage.AcquireOperation(m.storage.Root(), intent.Chat.ChatID)
	// Chat 独占已接管续聊准备与删除互斥；本入口自己的团队锁不覆盖隐藏摘要和 Runtime 接纳等待。
	// 上层持有的团队保护仍由上层释放，恢复原 Context 防止已释放锁的重入标记继续传播。
	releaseTeam()
	ctx = inputContext
	if operationErr != nil {
		return AcceptedExecution{}, operationErr
	}
	defer releaseOperation()
	if intent.Admission == "" {
		intent.Admission = constants.ExecutionAdmissionCommitOnRuntimeAccept
	}
	if intent.Options.SessionPolicy == "" {
		intent.Options.SessionPolicy = constants.RuntimeSessionPolicyContinue
	}
	if intent.Admission != constants.ExecutionAdmissionCommitOnRuntimeAccept &&
		intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch {
		return AcceptedExecution{}, status_error.BadRequest
	}
	request := pendingMessage{automaticRunID: intent.AutomaticRunID,
		executionID:     intent.ExecutionID,
		submissionID:    intent.Input.SubmissionID,
		clientRequestID: intent.ClientRequestID,
		chatID:          intent.Chat.ChatID,
		message: userMessage{
			Actor:          intent.Input.Actor,
			Text:           intent.Input.Content,
			Attachments:    append([]AttachmentInput(nil), intent.Input.Attachments...),
			CodeReferences: append([]CodeReferenceInput(nil), intent.Input.CodeReferences...),
			SkillIDs:       append([]string(nil), intent.Input.SkillIDs...),
			ConnectorIDs:   append([]string(nil), intent.Input.ConnectorIDs...),
			PluginIDs:      append([]string(nil), intent.Input.PluginIDs...),
		},
		runtime: runtimeSelection{
			ConfigurationResolved: intent.Options.ConfigurationResolved,
			AccountID:             intent.Options.AccountID,
			RuntimeID:             intent.Options.RuntimeID,
			Model:                 intent.Options.Model,
			Reasoning:             intent.Options.Reasoning,
			PermissionLevel:       intent.Options.PermissionLevel,
			WorkspaceReadOnly:     intent.Options.WorkspaceReadOnly,
			SessionPolicy:         intent.Options.SessionPolicy,
			HistoryTurns:          intent.Options.HistoryTurns,
			HistoryMode:           intent.Options.HistoryMode,
			SummaryModel:          intent.Options.SummaryModel,
			SummaryReasoning:      intent.Options.SummaryReasoning,
		},
		parentExecutionID:       intent.Options.ParentExecutionID,
		admission:               intent.Admission,
		toolCapabilities:        cloneToolCapabilities(intent.Options.ToolCapabilities),
		allowedConnectorIDs:     cloneConnectorAllowlist(intent.Options.AllowedConnectorIDs),
		allowedAgentDomains:     cloneConnectorAllowlist(intent.Options.AllowedAgentDomains),
		historyMessageIDsToOmit: append([]string(nil), intent.Options.HistoryMessageIDsToOmit...),
		supplementalInstructions: append(
			[]runtimecontract.Instruction(nil),
			intent.Options.SupplementalInstructions...,
		),
	}
	if intent.Chat.Create != nil {
		request.create = &pendingChat{
			hiddenFromWakerHistory: intent.Chat.Create.HiddenFromWakerHistory,
			wakerID:                intent.Chat.Create.WakerID,
			projectID:              intent.Chat.Create.ProjectID,
			workingDirectory:       intent.Chat.Create.WorkingDirectory,
		}
	}
	for _, instruction := range intent.Input.ContextInstructions {
		request.supplementalInstructions = append(request.supplementalInstructions, runtimecontract.Instruction{Name: instruction.Name, Content: instruction.Content})
	}
	return m.startMessage(ctx, request)
}

// cloneToolCapabilities 深拷贝内部能力，避免调用方在执行期间改变审批白名单。
func cloneToolCapabilities(capabilities []ToolCapability) []ToolCapability {
	result := make([]ToolCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		clone := capability
		clone.Bindings = append([]agentcapability.Binding(nil), capability.Bindings...)
		result = append(result, clone)
	}
	return result
}

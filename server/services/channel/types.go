package channel

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// CreateThreadInput 是团队草稿首次发送时提交的完整输入。
type CreateThreadInput struct {
	// AutomaticRunID 是服务端传入的原自动运行，不能从用户或模型正文绑定。
	AutomaticRunID string `json:"-"`
	// ExecutionID 是持久编排预留的负责人首发执行标识；普通交互为空。
	ExecutionID string
	// ChannelID 是需要快照的团队标识。
	ChannelID string
	// ClientRequestID 是桌面为同一次首次发送稳定复用的幂等标识。
	ClientRequestID string
	// ProjectID 是用户选择的可选负责人项目标识。
	ProjectID string
	// WorkingDirectory 是用户选择的可选临时工作目录。
	WorkingDirectory string
	// MentionWakerID 是结构化显式提及的一位可选成员。
	MentionWakerID string
	// MemberConfigs 是本次新对话覆盖团队默认值的可选成员执行配置。
	MemberConfigs []MemberExecutionConfigInput
	// Input 是进入负责人 Chat 的首条用户输入。
	Input execution.WorkInput
}

// MemberExecutionConfigInput 是成员后续新执行使用的完整可变配置。
type MemberExecutionConfigInput struct {
	// WakerID 是需要调整配置的团队成员标识。
	WakerID string
	// RuntimeID 是后续新执行使用的 Runtime。
	RuntimeID string
	// AccountID 是后续新执行使用的账号；system 表示原生登录。
	AccountID string
	// Model 是后续新执行使用的模型。
	Model string
	// Reasoning 是后续新执行使用的推理强度。
	Reasoning string
	// PermissionLevel 是后续新执行使用的权限等级。
	PermissionLevel constants.PermissionLevel
}

// ThreadResult 是首发接纳后返回的团队对话快照和负责人执行。
type ThreadResult struct {
	// Thread 是已经就绪的团队对话。
	Thread models.ChannelThread `json:"thread"`
	// Members 是与团队快照顺序一致的成员 Chat 映射。
	Members []models.ChannelThreadMember `json:"members"`
	// LeadExecution 是本次新建时已经接纳的负责人执行；幂等重放时为空并由详情快照恢复。
	LeadExecution *models.Execution `json:"leadExecution,omitempty"`
	// Replayed 表示响应来自同键同内容的既有就绪团队对话。
	Replayed bool `json:"replayed,omitempty"`
	// Tasks 是首发显式提及非负责人成员时创建的正式任务；普通首发或提及负责人时为空。
	Tasks []models.ChannelTask `json:"tasks"`
}

// SendMessageInput 是既有团队对话中的一条用户输入。
type SendMessageInput struct {
	// ThreadID 是需要继续使用原成员 Chat 的团队对话标识。
	ThreadID string
	// MentionWakerID 是结构化显式提及的一位可选成员。
	MentionWakerID string
	// Input 是本轮用户输入和附件引用。
	Input execution.WorkInput
}

// SendMessageResult 是团队消息接纳后的负责人执行或正式任务。
type SendMessageResult struct {
	// Execution 是直接交给负责人的可选 Chat Execution。
	Execution *models.Execution `json:"execution,omitempty"`
	// Task 是显式提及非负责人成员时产生或补充的可选正式任务。
	Task *models.ChannelTask `json:"task,omitempty"`
	// QueuedInput 是直接给负责人且已进入可靠队列的用户输入。
	QueuedInput *QueuedLeadInput `json:"queuedInput,omitempty"`
}

// SubmitMessageInput 统一表示团队首次发送或既有对话续发。
type SubmitMessageInput struct {
	// Create 在首次发送时提供团队和工作区快照输入。
	Create *CreateThreadInput
	// Continue 在既有团队对话发送时提供路由输入。
	Continue *SendMessageInput
}

// SubmitMessageResult 返回首次创建或既有对话发送的互斥结果。
type SubmitMessageResult struct {
	// Thread 是首次发送成功后的可选团队对话结果。
	Thread *ThreadResult
	// Message 是既有对话发送成功后的可选接纳结果。
	Message *SendMessageResult
}

// QueuedLeadInput 是不暴露内部序列化载荷的负责人输入接纳结果。
type QueuedLeadInput struct {
	// ID 是可靠队列中的用户输入标识。
	ID string `json:"id"`
	// Status 是返回时的投递状态。
	Status constants.ChannelInputStatus `json:"status"`
	// CreatedAt 是输入进入可靠队列的 Unix 毫秒时间。
	CreatedAt int64 `json:"createdAt"`
}

// CreateTaskInput 是负责人或显式提及非负责人成员时创建正式任务的输入。
type CreateTaskInput struct {
	// ThreadID 是任务所属团队对话标识。
	ThreadID string
	// SenderWakerID 是发起任务的伙伴标识；空值表示用户直接发起。
	SenderWakerID string
	// AssigneeWakerID 是执行伙伴标识。
	AssigneeWakerID string
	// Subject 是任务板展示主题。
	Subject string
	// Description 是交给执行伙伴的完整说明。
	Description string
	// Priority 是数值越大越先派发的优先级。
	Priority int
	// DependencyTaskIDs 是必须先进入任意终态的上游任务标识。
	DependencyTaskIDs []string
	// RequiresHumanReview 表示成功结果是否必须由用户验收。
	RequiresHumanReview bool
	// DirectDelivery 表示单项成功报告可在成员确认完整后直接交付用户。
	DirectDelivery bool
	// DirectInput 是显式 @ 非负责人成员时必须原样持久化并交给成员 Chat 的用户输入。
	DirectInput *execution.WorkInput
	// DeferDispatch 表示由负责人当前规划回合结束后统一派发本次创建的任务。
	DeferDispatch bool
}

// TaskResult 返回创建后的任务及其可能已经接纳的执行。
type TaskResult struct {
	// Task 是已经进入持久任务板的任务。
	Task models.ChannelTask `json:"task"`
	// Execution 是满足调度条件时已经接纳的成员执行。
	Execution *models.Execution `json:"execution,omitempty"`
}

// UpdateTaskInput 是负责人修改尚未执行任务的输入。
type UpdateTaskInput struct {
	// ThreadID 是任务所属团队对话标识。
	ThreadID string
	// TaskID 是需要修改的任务标识。
	TaskID string
	// AssigneeWakerID 是可选的新执行伙伴标识。
	AssigneeWakerID string
	// Subject 是可选的新任务主题。
	Subject string
	// Description 是可选的新完整任务说明。
	Description string
	// Priority 是可选的新派发优先级。
	Priority *int
}

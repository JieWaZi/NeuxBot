package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkTicket{})
	dbhelper.RegisterTable(&WorkTicketAttempt{})
	dbhelper.RegisterTable(&WorkTicketInput{})
	dbhelper.RegisterTable(&WorkTicketReport{})
}

// WorkTicket 持有一次受权委托的原会话、冻结范围和当前结论。
type WorkTicket struct {
	// 工单标识，复用原交办的公开用户消息。
	ID string `gorm:"size:32;primaryKey;index:idx_work_ticket_awaiting_returns,where:awaiting_delegation_returns = 1;index:idx_work_ticket_pending_transfer,where:transferred = 1 AND status = 'active' AND current_execution_id = '' AND pending_execution_id <> ''" json:"id"`
	// 工作对话
	WorkChatID string `gorm:"size:32;not null;index" json:"workChatId"`
	// 工单状态
	Status constants.WorkTicketStatus `gorm:"size:16;not null" json:"status"`
	// 当前报告摘要
	Summary string `gorm:"type:text;not null" json:"summary,omitempty"`
	// 更新时间
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
	// OriginKind 区分普通和主会话地址。
	OriginKind constants.WorkTicketOrigin `gorm:"size:8;not null;index:idx_work_ticket_origin,priority:1" json:"-"`
	// OriginID 是唯一获准收件和补充输入的原会话。
	OriginID string `gorm:"size:32;not null;index:idx_work_ticket_origin,priority:2" json:"-"`
	// OwnerKind 是受托主体类型。
	OwnerKind string `gorm:"size:12;not null" json:"ownerKind"`
	// OwnerID 是承担本单责任的主体。
	OwnerID string `gorm:"size:32;not null" json:"ownerId"`
	// OwnerWakerID 是原授权允许实际执行的伙伴。
	OwnerWakerID string `gorm:"size:32;not null" json:"-"`
	// ProjectID 是本单冻结的原项目，临时或受管工作空间为空。
	ProjectID string `gorm:"size:32;not null" json:"-"`
	// ProjectScope 固定原项目目录与指令授权的版本。
	ProjectScope string `gorm:"type:text;not null" json:"-"`
	// WorkingDirectory 是本单冻结的临时目录。
	WorkingDirectory string `gorm:"type:text;not null" json:"-"`
	// AllowedDirectories 是接纳时实际允许的项目目录列表。
	AllowedDirectories []string `gorm:"type:text;serializer:json" json:"-"`
	// PermissionLevel 是接单时冻结的执行权限。
	PermissionLevel constants.PermissionLevel `gorm:"size:24;not null" json:"-"`
	// Generation 是最近接纳的尝试代次。
	Generation int64 `gorm:"not null" json:"generation"`
	// CurrentExecutionID 是最近接纳的执行，仅允许这轮推进工单。
	CurrentExecutionID string `gorm:"size:32;not null" json:"-"`
	// PendingExecutionID 串行预留本单下一轮尚未确认的输入。
	PendingExecutionID string `gorm:"size:32;not null" json:"-"`
	// CreatedAt 是首次接纳边界开始的时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"-"`
	// ParentTicketID 是子单回件唯一的直接请求单，根工单为空。
	ParentTicketID string `gorm:"size:32;not null;default:'';index:idx_work_ticket_direct_children,priority:1" json:"-"`
	// ParentGeneration 是当前有权接收该子单回件的直接父单代次。
	ParentGeneration int64 `gorm:"not null;default:0;index:idx_work_ticket_direct_children,priority:2" json:"-"`
	// DelegationContent 是交给目标的已接纳任务说明。
	DelegationContent string `gorm:"type:text;not null;default:''" json:"-"`
	// DelegationContext 保存为这次委派明确提供的文本背景。
	DelegationContext string `gorm:"type:text;not null;default:''" json:"-"`
	// QuestionOnly 允许父单继续消化已失败的问答子单，普通子单仍须成功。
	QuestionOnly bool `gorm:"not null;default:false" json:"-"`
	// DelegationFailed 指示这轮子单已有确定失败终态并已回传失败。
	DelegationFailed bool `gorm:"not null;default:false" json:"-"`
	// AwaitingDelegationReturns 表示父单仍需要接纳本批已经登记的直接子单结果。
	AwaitingDelegationReturns bool `gorm:"not null;default:false" json:"-"`
	// Transferred 表示原址与当前工作的来源已由用户明确分离。
	Transferred bool `gorm:"not null;default:false" json:"-"`
	// DeletedOrigin 保留在排他删除事务中核对的原普通会话回投信息。
	DeletedOrigin *WorkTicketDeletedOrigin `gorm:"type:text;serializer:json" json:"-"`
}

// WorkTicketDeletedOrigin 是原址确实删除后保留的原收件伙伴和展示标题。
type WorkTicketDeletedOrigin struct {
	// WakerID 是原普通会话的实际所有者。
	WakerID string `json:"wakerId"`
	// Title 是删除前最后保存的原会话标题。
	Title string `json:"title"`
}

// TableName 返回工单表名。
func (WorkTicket) TableName() string { return "work_tickets" }

// WorkTicketAttempt 持有一次可继续多轮的业务尝试。
type WorkTicketAttempt struct {
	// TicketID 与 Generation 共同标识一次工单尝试。
	TicketID string `gorm:"size:32;primaryKey;uniqueIndex:ux_work_ticket_transfer_request,priority:1,where:transfer_request_id <> ''"`
	// Generation 与工单当前代次核对后才能结算。
	Generation int64 `gorm:"primaryKey;autoIncrement:false"`
	// Status 表示尝试是否仍有效。
	Status constants.WorkTicketAttemptStatus `gorm:"size:16;not null"`
	// TransferRequestID 是用户确认本次移交的稳定请求键。
	TransferRequestID string `gorm:"size:64;not null;default:'';uniqueIndex:ux_work_ticket_transfer_request,priority:2"`
	// Transfer 保存本次移交的可重放回执、前任和显式交接内容。
	Transfer *WorkTicketTransfer `gorm:"type:text;serializer:json"`
}

// WorkTicketTransfer 是原工单一次责任变化的持久回执和派发快照。
type WorkTicketTransfer struct {
	// 工单标识
	TicketID string `json:"ticketId"`
	// 新代次
	Generation int64 `json:"generation"`
	// 新执行标识
	ExecutionID string `json:"executionId"`
	// PreviousExecutionID 是授权已封存的原执行。
	PreviousExecutionID string `json:"previousExecutionId"`
	// PreviousWorkChatID 定位原执行的实际记录。
	PreviousWorkChatID string `json:"previousWorkChatId"`
	// PreviousOwnerKind 记录移交前的责任主体类型。
	PreviousOwnerKind string `json:"previousOwnerKind"`
	// PreviousOwnerID 记录移交前的责任主体。
	PreviousOwnerID string `json:"previousOwnerId"`
	// PreviousOwnerWakerID 记录移交前的实际执行伙伴。
	PreviousOwnerWakerID string `json:"previousOwnerWakerId"`
	// RequestDigest 冻结移交动作的原始参数。
	RequestDigest string `json:"requestDigest"`
	// Content 是用户明确同意交给接手人的文本。
	Content string `json:"content"`
	// TargetName 是移交确认时的接手主体名称，用于原会话通知及重放。
	TargetName string `json:"targetName,omitempty"`
}

// TableName 返回工单尝试表名。
func (WorkTicketAttempt) TableName() string { return "work_ticket_attempts" }

// WorkTicketInput 是一次用户输入的接纳回执及该次执行与尝试的绑定。
type WorkTicketInput struct {
	// ExecutionID 是已经预留的执行，也是输入接纳主键。
	ExecutionID string `gorm:"size:32;primaryKey;index:idx_work_ticket_unfinished,where:confirmed_at IS NOT NULL AND finished_at IS NULL;index:idx_work_ticket_origin_replies,where:origin_reply IS NOT NULL"`
	// TicketID 是显式接收这条输入的唯一工单。
	TicketID string `gorm:"size:32;not null;index:idx_work_ticket_inputs,priority:1"`
	// Generation 是接纳输入时绑定的尝试代次。
	Generation int64 `gorm:"not null"`
	// OriginKind 与原会话构成提交键的命名空间。
	OriginKind constants.WorkTicketOrigin `gorm:"size:8;not null;uniqueIndex:ux_work_ticket_submission,priority:1;uniqueIndex:ux_work_ticket_public_input,priority:1"`
	// OriginID 确定能够重投此输入的原始会话。
	OriginID string `gorm:"size:32;not null;uniqueIndex:ux_work_ticket_submission,priority:2;uniqueIndex:ux_work_ticket_public_input,priority:2"`
	// SubmissionID 是用户提交时已有的稳定身份。
	SubmissionID string `gorm:"size:64;not null;uniqueIndex:ux_work_ticket_submission,priority:3"`
	// SubmissionHash 核对同一提交键的消息和选项未变。
	SubmissionHash string `gorm:"size:64;not null"`
	// MessageID 是原会话中该次输入的公开消息身份。
	MessageID string `gorm:"size:32;not null;uniqueIndex:ux_work_ticket_public_input,priority:3"`
	// WorkChatID 是保存实际提供给 Worker 的来源记录。
	WorkChatID string `gorm:"size:32;not null;index:idx_work_ticket_source_input,priority:1"`
	// SourceMessageID 是工作记录中该次输入的身份。
	SourceMessageID string `gorm:"size:32;not null;index:idx_work_ticket_source_input,priority:2"`
	// ConfirmedAt 表示宿主已确认权威聊天日志接纳。
	ConfirmedAt *int64
	// FinishedAt 表示这一轮的原生执行已结束。
	FinishedAt *int64
	// ConsumesDelegationReturns 只标记宿主以已核验回件启动的续办执行。
	ConsumesDelegationReturns bool `gorm:"not null;default:false"`
	// DelegationReplyID 标识本轮接纳的直接父单问题答复。
	DelegationReplyID string `gorm:"size:32;not null;default:''"`
	// DelegationQuestionID 标识唤醒本父单回答的直接子单等待报告。
	DelegationQuestionID string `gorm:"size:32;not null;default:''"`
	// OriginReplyDigest 核对普通原会话中这次转发输入的稳定请求参数。
	OriginReplyDigest string `gorm:"size:64;not null;default:''"`
	// OriginReply 保留待写入普通原会话的消息，确认写入后释放副本。
	OriginReply *eventlog.MessageCreatedData `gorm:"type:text;serializer:json"`
	// EventSubscription 是首次实际接纳工单时一起启动的讨论授权。
	EventSubscription *WorkTicketEventSubscription `gorm:"type:text;serializer:json"`
	// EventDeliveryID 指向本轮确切接纳的来源事件。
	EventDeliveryID string `gorm:"size:32;not null;default:''"`
}

// TableName 返回工单输入和执行绑定表名。
func (WorkTicketInput) TableName() string { return "work_ticket_inputs" }

// WorkTicketReport 固定 Worker 报告内容、原执行和原会话回执。
type WorkTicketReport struct {
	// ID 从原执行和请求身份派生，用于回件幂等。
	ID string `gorm:"size:32;primaryKey;index:idx_work_ticket_undelivered,where:message_id = '';index:idx_work_ticket_direct_receipts,priority:3,where:message_id = id AND reply_to_report_id = ''"`
	// TicketID 是报告要推进的原工单。
	TicketID string `gorm:"size:32;not null;index"`
	// ExecutionID 是 Worker 真正发起报告的执行。
	ExecutionID string `gorm:"size:32;not null;index:idx_work_ticket_pending_report,where:message_id = '';index:idx_work_ticket_direct_receipts,priority:1,where:message_id = id AND reply_to_report_id = ''"`
	// Status 是这份报告提出的工单状态。
	Status constants.WorkTicketStatus `gorm:"size:16;not null;index:idx_work_ticket_direct_receipts,priority:2,where:message_id = id AND reply_to_report_id = ''"`
	// Content 是报告首次接纳时保存的不可变内容。
	Content string `gorm:"type:text;not null"`
	// ReplyToReportID 仅用于从直接父单返回到子单的私有答复，引用原等待报告。
	ReplyToReportID string `gorm:"size:32;not null;default:'';uniqueIndex:ux_work_ticket_question_reply,where:reply_to_report_id <> ''"`
	// MessageID 保存实际收件会话的已提交消息标识，未交付时为空。
	MessageID string `gorm:"size:32;not null;index"`
	// DeliveryConversationID 在删除原会话后保留实际收件地址和结案凭据。
	DeliveryConversationID string `gorm:"size:32;not null;default:''"`
}

// TableName 返回工单报告和回执表名。
func (WorkTicketReport) TableName() string { return "work_ticket_reports" }

// WorkTicketMessageView 将一条原会话消息关联到工单当前视图。
type WorkTicketMessageView struct {
	// 消息标识
	MessageID string `json:"messageId"`
	// 关联工单
	Ticket WorkTicket `json:"ticket"`
}

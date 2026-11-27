package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&MainConversation{})
	dbhelper.RegisterTable(&MainConversationWork{})
	dbhelper.RegisterTable(&MainConversationMessage{})
	dbhelper.RegisterTable(&MainConversationReadCursor{})
}

// MainConversation 是主体在本机持久可寻址的公开主会话。
type MainConversation struct {
	// 会话标识
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// 主体类型
	SubjectType string `gorm:"size:12;not null;uniqueIndex:ux_main_subject,priority:1" json:"subjectType"`
	// 主体标识
	SubjectID string `gorm:"size:32;not null;uniqueIndex:ux_main_subject,priority:2" json:"subjectId"`
}

// TableName 返回公开主会话表名。
func (MainConversation) TableName() string { return "main_conversations" }

// MainConversationWork 将执行时取得的范围与工作 Chat 绑定，多个回合可使用同一来源。
type MainConversationWork struct {
	// WorkChatID 是服务端为这份授权分配的隐藏工作 Chat 身份。
	WorkChatID string `gorm:"size:32;primaryKey" json:"workChatId"`
	// ConversationID 是工作获准发布到的唯一公开主会话。
	ConversationID string `gorm:"size:32;not null;uniqueIndex:ux_main_work_scope,priority:1" json:"conversationId"`
	// WakerID 是在此范围内实际执行工作的伙伴。
	WakerID string `gorm:"size:32;not null;uniqueIndex:ux_main_work_scope,priority:2" json:"wakerId"`
	// ScopeKey 是宿主根据冻结的工作范围及执行配置确定的键。
	ScopeKey string `gorm:"not null;uniqueIndex:ux_main_work_scope,priority:3" json:"-"`
}

// TableName 返回工作来源绑定表名。
func (MainConversationWork) TableName() string { return "main_conversation_work" }

// MainConversationMessage 是用户输入或明确发往主会话的一条发言。
type MainConversationMessage struct {
	// 消息顺序
	Sequence int64 `gorm:"not null;uniqueIndex:ux_main_timeline,priority:2;index:idx_main_conversation_readable,priority:2,where:role = 'assistant'" json:"sequence"`
	// 消息标识
	ID string `gorm:"size:32;primaryKey;index:idx_main_pending_admissions,where:admission_pending = 1" json:"id"`
	// 会话标识
	ConversationID string `gorm:"size:32;not null;uniqueIndex:ux_main_timeline,priority:1;index:idx_main_conversation_readable,priority:1" json:"conversationId"`
	// 发送角色
	Role string `gorm:"size:12;not null" json:"role"`
	// 消息正文
	Content string `gorm:"type:text;not null" json:"content"`
	// 来源对话
	SourceChatID string `gorm:"size:32;not null" json:"sourceChatId"`
	// SourceExecutionID 指向接纳交办或发布答复的来源执行，历史交办沿用公开消息标识。
	SourceExecutionID string `gorm:"size:32;not null;default:'';index" json:"-"`
	// SourceMessageID 指向来源对话中冻结附件和代码引用的原输入，纯文本和旧记录留空。
	SourceMessageID string `gorm:"size:32;not null;default:''" json:"-"`
	// 发言伙伴
	SourceWakerID string `gorm:"size:32;not null" json:"sourceWakerId,omitempty"`
	// 引用消息
	ReplyToMessageID string `gorm:"size:32;not null;index" json:"replyToMessageId,omitempty"`
	// RequestedDirectory 保存用户提交时选择的临时路径，供重投核对原请求。
	RequestedDirectory string `gorm:"type:text;not null" json:"-"`
	// InputDigest 核对重投时的原始消息、附件引用与执行选项。
	InputDigest string `gorm:"size:64;not null;default:''" json:"-"`
	// AdmissionPending 表示已预留提交键，还未确认来源日志接纳。
	AdmissionPending bool `gorm:"not null;default:false" json:"-"`
	// AdmissionSubmissionID 让恢复只读取来源的接纳回执。
	AdmissionSubmissionID string `gorm:"size:64;not null;default:''" json:"-"`
	// 创建时间
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// RemovedAt 记录消息从主会话移除的时间，保留执行来源和发布收据。
	RemovedAt int64 `gorm:"not null;default:0" json:"-"`
}

// TableName 返回按稳定序号读取的公开消息表名。
func (MainConversationMessage) TableName() string { return "main_conversation_messages" }

// IsReadablePublication 判断伙伴发言是否仍可在主会话阅读。
func (m MainConversationMessage) IsReadablePublication() bool {
	return m.Role == string(constants.MessageRoleAssistant) && m.RemovedAt == 0
}

// 主会话阅读游标
type MainConversationReadCursor struct {
	// 阅读者
	ReaderID string `gorm:"size:32;primaryKey" json:"-"`
	// 主会话标识
	ConversationID string `gorm:"size:32;primaryKey;index" json:"conversationId"`
	// 已读序号
	ThroughSequence int64 `gorm:"not null;default:0" json:"throughSequence"`
	// 未读代次
	UnreadGeneration int64 `gorm:"not null;default:0" json:"unreadGeneration"`
	// 保持未读
	HoldUnread bool `gorm:"not null;default:false" json:"holdUnread"`
	// 更新时间
	UpdatedAt int64 `gorm:"not null;autoUpdateTime:false" json:"updatedAt"`
}

// TableName 返回主会话阅读位置表。
func (MainConversationReadCursor) TableName() string { return "main_conversation_read_cursors" }

// 主会话阅读状态
type MainConversationReadingState struct {
	// 阅读游标
	MainConversationReadCursor
	// 未读消息数
	UnreadMessageCount int64 `json:"unreadMessageCount"`
	// 首条未读消息
	FirstUnreadMessageID string `json:"firstUnreadMessageID,omitempty"`
	// 最后可读消息
	LatestReadableMessageID string `json:"latestReadableMessageID,omitempty"`
}

// 主会话活跃工作
type MainConversationActivity struct {
	// 工作对话
	WorkChatID string `json:"workChatId"`
	// 活跃执行
	ExecutionID string `json:"executionId"`
	// 公开交办消息
	InputMessageID string `json:"inputMessageId,omitempty"`
	// 已有公开答复
	HasPublicReply bool `json:"hasPublicReply"`
	// 执行伙伴
	SourceWakerID string `json:"sourceWakerId"`
	// 等待审批
	WaitingApproval bool `json:"waitingApproval,omitempty"`
	// 等待补充输入
	WaitingElicitation bool `json:"waitingElicitation,omitempty"`
	// RequestCount 是等待请求数量，仅用于执行主体活动摘要。
	RequestCount int64 `json:"-"`
}

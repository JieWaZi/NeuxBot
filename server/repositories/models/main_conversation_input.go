package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&MainConversationInput{})
}

// MainConversationTransferredReply 区分原址引用的续办与对接手来源的常规访问。
const MainConversationTransferredReply = "main-transferred-reply"

// MainConversationInput 是主会话已接纳、等待路由或交给来源 Chat 的完整输入。
type MainConversationInput struct {
	// Sequence 是数据库分配的接纳顺序，路由和来源各自按此顺序领取。
	Sequence int64 `gorm:"primaryKey;autoIncrement;index:idx_main_input_boot_claim,where:commit_started = 1 AND status != 'accepted' AND status != 'failed';index:idx_main_input_conversation_open,priority:2,where:status != 'accepted' AND status != 'failed';index:idx_main_input_conversation_failed,priority:2,where:status = 'failed';index:idx_main_input_work,priority:2;index:idx_main_input_open,priority:2,where:status != 'accepted' AND status != 'failed'"`
	// ConversationID 是公开主会话标识。
	ConversationID string `gorm:"size:32;not null;index;index:idx_main_input_conversation_open,priority:1,where:status != 'accepted' AND status != 'failed';index:idx_main_input_conversation_failed,priority:1,where:status = 'failed'"`
	// WorkChatID 是已经冻结工作范围的真实 Chat 标识，路由前为空。
	WorkChatID string `gorm:"size:32;not null;index:idx_main_input_work,priority:1;index:idx_main_input_open,priority:1,where:status != 'accepted' AND status != 'failed'"`
	// RouterWakerID 是接纳时负责判断新消息的伙伴，直接发送到来源时为空。
	RouterWakerID string `gorm:"size:32;not null;default:''"`
	// InputDigest 是待路由输入的原摘要，派工后保存已选接纳路径的摘要。
	InputDigest string `gorm:"size:64;not null;default:''"`
	// RoutedSource 是路由派工所选的范围，供首次物化已预留的工作 Chat。
	RoutedSource *MainConversationRoutedSource `gorm:"type:text;serializer:json"`
	// PublicMessageID 是原公开用户发言标识，也用于队列重投查重。
	PublicMessageID string `gorm:"size:32;not null;uniqueIndex"`
	// ExecutionID 是两种原发送入口各自预留的执行标识。
	ExecutionID string `gorm:"size:32;not null;index:idx_main_input_execution"`
	// SourceMessageID 是交办在来源 Chat 中预留的用户消息标识。
	SourceMessageID string `gorm:"size:32;not null"`
	// PublicReplyID 是公开用户发言所引用的原消息标识。
	PublicReplyID string `gorm:"size:32;not null"`
	// TicketReplyID 是执行层用于恢复工单关联的公开消息标识。
	TicketReplyID string `gorm:"size:32;not null"`
	// StartWork 表示入口已确定本轮需要创建工单。
	StartWork bool `gorm:"not null"`
	// PrivateSource limits this continuation and its pending text to the original Chat.
	PrivateSource bool `gorm:"not null;default:false"`
	// EntryPoint 是主会话交办或来源续聊的原入口标记。
	EntryPoint string `gorm:"size:32;not null"`
	// PublicContent 是已经校验的公开消息正文。
	PublicContent string `gorm:"type:text;not null"`
	// RequestedDirectory 保留公开接纳用于重投核对的本次目录参数。
	RequestedDirectory string `gorm:"type:text;not null"`
	// RequestHash 核对入口原始请求和附件引用。
	RequestHash string `gorm:"size:64;not null"`
	// Input 保存完整输入及固定附件、代码和能力引用。
	Input workinput.Input `gorm:"type:text;serializer:json;not null"`
	// Options 保存原入口已经冻结的 Runtime 选择。
	Options MainConversationInputOptions `gorm:"type:text;serializer:json;not null"`
	// Status 是持久队列的当前交接状态。
	Status constants.MainConversationInputStatus `gorm:"size:20;not null"`
	// CommitStarted 表示此领取已到达 Chat 权威日志提交边界。
	CommitStarted bool `gorm:"not null;default:false"`
	// CreatedAt 是 Core 持久接纳输入的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"not null;autoCreateTime:milli"`
	// ErrorCode 是可展示的失败或对账错误标识。
	ErrorCode string `gorm:"size:32;not null;default:''"`
}

// MainConversationRoutedSource 描述一次已核准派工的来源范围。
type MainConversationRoutedSource struct {
	// Acknowledgment 是接纳工作后公开的准备说明。
	Acknowledgment string `json:"acknowledgment,omitempty"`
	// Shared 表示使用持久伙伴 Chat，复核时沿用其独立于单条输入的范围标识。
	Shared bool `json:"shared,omitempty"`
	// ProjectID 是伙伴项目标识，临时目录和受管工作区为空。
	ProjectID string `json:"projectId,omitempty"`
	// WorkingDirectory 是本次交办授权的真实目录。
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// ScopeKey 是路由时授权范围的版本，用于物化前核对项目配置。
	ScopeKey string `json:"scopeKey"`
}

// TableName 返回主会话输入队列表名。
func (MainConversationInput) TableName() string { return "main_conversation_inputs" }

// MainConversationInputOptions 保存主会话两个输入入口可选择的非凭据配置。
type MainConversationInputOptions struct {
	// AccountID 是本机的账号配置标识。
	AccountID string `json:"accountId,omitempty"`
	// RuntimeID 是接纳时选中的智能体程序。
	RuntimeID string `json:"runtimeId"`
	// Model 是本轮明确选中的模型标识。
	Model string `json:"model,omitempty"`
	// DefaultSourceModel 是仅在路由回到常驻来源时使用的入队前选择。
	DefaultSourceModel string `json:"defaultSourceModel,omitempty"`
	// DefaultSourceReasoning 是常驻来源选择模型时输入明确指定的推理强度。
	DefaultSourceReasoning string `json:"defaultSourceReasoning,omitempty"`
	// Reasoning 是本轮选中的推理强度。
	Reasoning string `json:"reasoning,omitempty"`
	// Fast 是本轮选择的 Fast 开关。
	Fast *bool `json:"fast,omitempty"`
	// ContextWindow 是本轮选中的原生上下文窗口档位。
	ContextWindow string `json:"contextWindow,omitempty"`
	// PermissionLevel 是本轮确定的权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// ConfigurationResolved 保留合法的空选项，派发时沿用冻结配置。
	ConfigurationResolved bool `json:"configurationResolved"`
	// SessionPolicy 是显式选择的续接或新建策略。
	SessionPolicy constants.RuntimeSessionPolicy `json:"sessionPolicy,omitempty"`
	// HistorySourceChatID 将显式交接限制在路由实际选中的同一个来源。
	HistorySourceChatID string `json:"historySourceChatId,omitempty"`
	// HistoryTurns 是显式新建会话时要保留的历史回合数。
	HistoryTurns int `json:"historyTurns,omitempty"`
	// HistoryMode 是显式新建会话时的上下文交接方式。
	HistoryMode constants.HistoryMode `json:"historyMode,omitempty"`
	// SummaryModel 是显式摘要交接所选模型。
	SummaryModel string `json:"summaryModel,omitempty"`
	// SummaryReasoning 是显式摘要交接所选推理强度。
	SummaryReasoning string `json:"summaryReasoning,omitempty"`
}

// MainConversationInputReceipt 是公开队列回执，供提交、重投和恢复读取。
type MainConversationInputReceipt struct {
	// 接纳顺序
	Sequence int64 `json:"sequence"`
	// 主会话标识
	ConversationID string `json:"conversationId"`
	// 来源对话标识
	WorkChatID string `json:"workChatId"`
	// 原提交标识
	SubmissionID string `json:"submissionId"`
	// 原公开消息标识
	PublicMessageID string `json:"publicMessageId"`
	// 原执行标识
	ExecutionID string `json:"executionId"`
	// 接纳状态
	Status constants.MainConversationInputStatus `json:"status"`
	// 公开输入
	Content string `json:"content"`
	// 接纳时间
	CreatedAt int64 `json:"createdAt"`
	// 交接错误标识
	ErrorCode string `json:"errorCode,omitempty"`
	// RequestHash 用于比较同键重投的原始请求内容。
	RequestHash string `json:"-"`
	// SourceMessageID 是来源 Chat 中预留的原用户消息身份。
	SourceMessageID string `json:"-"`
	// PublicReplyID 是公开输入实际引用的原消息。
	PublicReplyID string `json:"-"`
	// RouterWakerID 在路由阶段标记尚无可公开的来源。
	RouterWakerID string `json:"-"`
	// TransferredReply 表示公开回执不包含已移交工单的接手来源标识。
	TransferredReply bool `json:"-"`
	// PrivateSource excludes a source-only continuation from public history.
	PrivateSource bool `json:"-"`
	// EntryPoint 是派工与来源直接续聊的可信接纳入口。
	EntryPoint string `json:"-"`
}

// Receipt 返回可公开的身份和当前交接结果。
func (q MainConversationInput) Receipt() MainConversationInputReceipt {
	status := q.Status
	if status == constants.MainConversationInputDispatching {
		status = constants.MainConversationInputQueued
	}
	return MainConversationInputReceipt{
		Sequence: q.Sequence, ConversationID: q.ConversationID, WorkChatID: q.WorkChatID,
		SubmissionID: q.Input.SubmissionID, PublicMessageID: q.PublicMessageID, ExecutionID: q.ExecutionID,
		Status: status, Content: q.PublicContent, ErrorCode: q.ErrorCode, CreatedAt: q.CreatedAt,
		RequestHash: q.RequestHash, SourceMessageID: q.SourceMessageID, PublicReplyID: q.PublicReplyID, RouterWakerID: q.RouterWakerID,
		TransferredReply: q.EntryPoint == MainConversationTransferredReply, PrivateSource: q.PrivateSource, EntryPoint: q.EntryPoint,
	}
}

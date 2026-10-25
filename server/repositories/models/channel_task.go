package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
)

// ChannelTask 是团队负责人动态维护并派发到成员 Chat 的任务板条目。
type ChannelTask struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// ThreadID 是任务所属团队对话标识。
	ThreadID string `gorm:"column:thread_id;type:varchar(32);not null;index" json:"threadId"`
	// SenderWakerID 是创建并派发任务的伙伴标识；空值表示由用户直接发起。
	SenderWakerID string `gorm:"column:sender_waker_id;type:varchar(32);not null;default:''" json:"senderWakerId,omitempty"`
	// AssigneeWakerID 是当前执行伙伴标识，重试前允许由负责人调整。
	AssigneeWakerID string `gorm:"column:assignee_waker_id;type:varchar(32);not null;index" json:"assigneeWakerId"`
	// Subject 是任务板展示的简短主题。
	Subject string `gorm:"column:subject;type:varchar(200);not null" json:"subject"`
	// Description 是交付给执行伙伴的完整任务说明。
	Description string `gorm:"column:description;type:text;not null" json:"description"`
	// DirectInput 是显式 @ 成员时需要原样交付的完整输入；负责人任务工具创建时为空。
	DirectInput *workinput.Input `gorm:"column:direct_input;type:text;serializer:json" json:"-"`
	// DeferUntilLeadIdle 表示负责人规划回合尚未结束，调度器不得提前启动该任务。
	DeferUntilLeadIdle bool `gorm:"column:defer_until_lead_idle;not null;default:false;index" json:"-"`
	// Status 是任务当前持久化生命周期状态。
	Status constants.ChannelTaskStatus `gorm:"column:status;type:varchar(24);not null;index" json:"status"`
	// Priority 是负责人设置的排序优先级，数值越大越先派发。
	Priority int `gorm:"column:priority;not null;default:0;index" json:"priority"`
	// RequiresHumanReview 表示执行成功后必须进入用户验收。
	RequiresHumanReview bool `gorm:"column:requires_human_review;not null;default:false" json:"requiresHumanReview"`
	// DirectDelivery 表示负责人允许单项完整成果直接进入团队主时间线，无需再次生成汇总。
	DirectDelivery bool `gorm:"column:direct_delivery;not null;default:false" json:"directDelivery"`
	// CancelRequestedAt 是 CLI 持久接纳停止当前尝试的时间。
	CancelRequestedAt *int64 `json:"cancelRequestedAt,omitempty"`
	// CancelRequestedFence 保证停止意图不能作用于后续尝试。
	CancelRequestedFence int `gorm:"not null;default:0" json:"-"`
	// CurrentFence 是当前有效尝试的单调递增提交围栏。
	CurrentFence int `gorm:"column:current_fence;not null;default:0" json:"currentFence"`
	// Result 是最近一次有效尝试提交给任务板的结果摘要。
	Result string `gorm:"column:result;type:text;not null;default:''" json:"result"`
	// ErrorMessage 是最近一次失败的安全展示摘要。
	ErrorMessage string `gorm:"column:error_message;type:text;not null;default:''" json:"errorMessage,omitempty"`
	// ChangedPaths 是执行伙伴报告或系统采集的改动路径。
	ChangedPaths []string `gorm:"column:changed_paths;type:text;not null;serializer:json" json:"changedPaths"`
	// CreatedAt 是任务创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli;index" json:"createdAt"`
	// UpdatedAt 是任务最近改变时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
	// StartedAt 是当前或最近一次尝试开始时的 Unix 毫秒时间。
	StartedAt *int64 `gorm:"column:started_at" json:"startedAt,omitempty"`
	// FinishedAt 是任务最近进入终态或等待验收时的 Unix 毫秒时间。
	FinishedAt *int64 `gorm:"column:finished_at" json:"finishedAt,omitempty"`
}

// TableName 返回团队任务使用的表名。
func (ChannelTask) TableName() string { return "channel_tasks" }

// ChannelTaskDependency 保存同一团队对话内任务之间的先后依赖。
type ChannelTaskDependency struct {
	// TaskID 是等待其他任务结束的下游任务标识。
	TaskID string `gorm:"column:task_id;type:varchar(32);primaryKey" json:"taskId"`
	// DependsOnTaskID 是必须先进入任意终态的上游任务标识。
	DependsOnTaskID string `gorm:"column:depends_on_task_id;type:varchar(32);primaryKey;index" json:"dependsOnTaskId"`
	// CreatedAt 是依赖关系建立时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回团队任务依赖使用的表名。
func (ChannelTaskDependency) TableName() string { return "channel_task_dependencies" }

// ChannelTaskAttempt 把一次任务尝试与成员 Chat Execution 及提交围栏关联起来。
type ChannelTaskAttempt struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// TaskID 是尝试所属团队任务标识，并与 Fence 唯一确定一次领取。
	TaskID string `gorm:"column:task_id;type:varchar(32);not null;index;uniqueIndex:idx_channel_task_attempt_fence,priority:1" json:"taskId"`
	// ChatID 是本次尝试复用的执行伙伴 Chat 标识。
	ChatID string `gorm:"column:chat_id;type:varchar(32);not null;index" json:"chatId"`
	// ExecutionID 是执行接纳后关联的 Chat Execution 标识。
	ExecutionID string `gorm:"column:execution_id;type:varchar(32);not null;default:'';uniqueIndex:idx_channel_task_attempt_execution,where:execution_id <> ''" json:"executionId,omitempty"`
	// AssigneeWakerID 是本次尝试实际使用的执行伙伴标识。
	AssigneeWakerID string `gorm:"column:assignee_waker_id;type:varchar(32);not null" json:"assigneeWakerId"`
	// Fence 是本次尝试提交最终结果必须匹配的围栏值。
	Fence int `gorm:"column:fence;not null;uniqueIndex:idx_channel_task_attempt_fence,priority:2" json:"fence"`
	// Reason 说明本次尝试由首次派发、用户补充还是负责人重试触发。
	Reason constants.ChannelAttemptReason `gorm:"column:reason;type:varchar(24);not null" json:"reason"`
	// Status 是 reservation、运行中或终态对应的 Execution 生命周期状态。
	Status constants.ExecutionStatus `gorm:"column:status;type:varchar(24);not null;default:'';index" json:"status,omitempty"`
	// Result 是该尝试执行成功时提交的阶段结果。
	Result string `gorm:"column:result;type:text;not null;default:''" json:"result"`
	// UserReady 表示成员已确认报告正文可直接作为完整答复交给用户。
	UserReady bool `gorm:"column:user_ready;not null;default:false" json:"-"`
	// ErrorMessage 是该尝试执行失败时的安全展示摘要。
	ErrorMessage string `gorm:"column:error_message;type:text;not null;default:''" json:"errorMessage,omitempty"`
	// StartedAt 是对应 Execution 被接纳时的 Unix 毫秒时间。
	StartedAt *int64 `gorm:"column:started_at" json:"startedAt,omitempty"`
	// FinishedAt 是对应 Execution 结束时的 Unix 毫秒时间。
	FinishedAt *int64 `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	// CreatedAt 是尝试记录进入持久化队列时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是尝试记录最近变化时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回团队任务尝试使用的表名。
func (ChannelTaskAttempt) TableName() string { return "channel_task_attempts" }

// ChannelTaskFollowUp 保存等待追加到原任务和原成员 Chat 的用户补充。
type ChannelTaskFollowUp struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// TaskID 是补充内容所属团队任务标识。
	TaskID string `gorm:"column:task_id;type:varchar(32);not null;index" json:"taskId"`
	// Text 是用户提供并将在当前尝试结束后交付的补充内容。
	Text string `gorm:"column:text;type:text;not null" json:"text"`
	// Actor 是补充消息的真实作者，区分用户显式 @ 与负责人内部协调。
	Actor constants.ChannelMessageActor `gorm:"column:actor;type:varchar(24);not null" json:"actor"`
	// Payload 是包含附件、代码引用、技能与连接器选择的完整输入。
	Payload workinput.Input `gorm:"column:payload;type:text;not null;serializer:json" json:"-"`
	// DeliveredAt 是补充进入新尝试的可选 Unix 毫秒时间；空表示仍在排队。
	DeliveredAt *int64 `gorm:"column:delivered_at;index" json:"deliveredAt,omitempty"`
	// DeliveredAttemptID 是领取该补充的尝试标识，用于启动失败或崩溃后的精确补偿。
	DeliveredAttemptID string `gorm:"column:delivered_attempt_id;type:varchar(32);not null;default:'';index" json:"-"`
	// CreatedAt 是补充内容持久化时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回团队任务补充队列使用的表名。
func (ChannelTaskFollowUp) TableName() string { return "channel_task_follow_ups" }

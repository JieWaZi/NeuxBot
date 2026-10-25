package models

import (
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
)

// ChannelLeadInput 保存等待进入原负责人 Chat 的协调或直接执行用户输入。
type ChannelLeadInput struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// ThreadID 是补充消息所属团队对话标识。
	ThreadID string `gorm:"column:thread_id;type:varchar(32);not null;index" json:"threadId"`
	// Payload 是完整用户输入，包含正文与有序能力引用。
	Payload workinput.Input `gorm:"column:payload;type:text;not null;serializer:json" json:"payload"`
	// DirectExecution 表示用户显式提及负责人，本条输入应在负责人 Chat 中获得可写执行能力而不创建任务。
	DirectExecution bool `gorm:"column:direct_execution;not null;default:false" json:"directExecution"`
	// Status 是补充消息从排队到进入负责人 Chat 的投递状态。
	Status constants.ChannelInputStatus `gorm:"column:status;type:varchar(24);not null;index" json:"status"`
	// LeadExecutionID 是接纳该消息的负责人 Chat Execution 标识。
	LeadExecutionID string `gorm:"column:lead_execution_id;type:varchar(32);not null;default:'';index" json:"leadExecutionId,omitempty"`
	// CreatedAt 是用户补充进入可靠队列时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli;index" json:"createdAt"`
	// UpdatedAt 是投递状态最近变化时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回负责人用户输入队列使用的表名。
func (ChannelLeadInput) TableName() string { return "channel_lead_inputs" }

// ChannelLeadNotification 保存等待批量交给团队负责人 Chat 的任务结果。
type ChannelLeadNotification struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// ThreadID 是通知所属团队对话标识。
	ThreadID string `gorm:"column:thread_id;type:varchar(32);not null;index" json:"threadId"`
	// TaskID 是产生结果的团队任务标识。
	TaskID string `gorm:"column:task_id;type:varchar(32);not null;index" json:"taskId"`
	// AttemptID 是产生结果的有效尝试标识；人工验收通知使用稳定合成标识占用同一去重键。
	AttemptID string `gorm:"column:attempt_id;type:varchar(32);not null;uniqueIndex" json:"attemptId"`
	// Status 是通知从待投递到已消费的持久化状态。
	Status constants.ChannelNotificationStatus `gorm:"column:status;type:varchar(24);not null;index" json:"status"`
	// DirectDelivered 表示该报告已经直接作为成员答复交付用户，无需负责人汇总。
	DirectDelivered bool `gorm:"column:direct_delivered;not null;default:false" json:"-"`
	// Payload 是交给团队负责人用于协调和汇总的结构化文本。
	Payload string `gorm:"column:payload;type:text;not null" json:"payload"`
	// LeadExecutionID 是批量接收该通知的负责人 Chat Execution 标识。
	LeadExecutionID string `gorm:"column:lead_execution_id;type:varchar(32);not null;default:'';index" json:"leadExecutionId,omitempty"`
	// CreatedAt 是任务结果和通知原子提交时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是通知投递状态最近变化时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回团队负责人结果通知使用的表名。
func (ChannelLeadNotification) TableName() string { return "channel_lead_notifications" }

// ChannelEventPayload 保存事件类型对应的原始 JSON 对象。
//
// openapi:type object
type ChannelEventPayload []byte

// MarshalJSON 将事件载荷作为原始 JSON 写入，而不是将底层字节编码为 Base64。
func (p ChannelEventPayload) MarshalJSON() ([]byte, error) {
	return json.RawMessage(p).MarshalJSON()
}

// UnmarshalJSON 保留事件载荷的原始 JSON 表示。
func (p *ChannelEventPayload) UnmarshalJSON(data []byte) error {
	return (*json.RawMessage)(p).UnmarshalJSON(data)
}

// ChannelEvent 是团队主时间线和进度面板共用的持久事件游标。
type ChannelEvent struct {
	// ID 是 32 位无连字符 UUID，也是事件流的稳定游标。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// ChannelID 是事件所属团队标识。
	ChannelID string `gorm:"column:channel_id;type:varchar(32);not null;index" json:"channelId"`
	// ThreadID 是事件所属团队对话标识。
	ThreadID string `gorm:"column:thread_id;type:varchar(32);not null;index" json:"threadId"`
	// Type 是前端增量解释事件时使用的稳定类型。
	Type constants.ChannelEventType `gorm:"column:type;type:varchar(80);not null;index" json:"type"`
	// Payload 是事件类型对应的结构化 JSON 数据。
	Payload ChannelEventPayload `gorm:"column:payload;type:text;not null;serializer:json" json:"payload"`
	// CreatedAt 是事件在业务事务中持久化的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli;index" json:"createdAt"`
}

// TableName 返回团队持久事件使用的表名。
func (ChannelEvent) TableName() string { return "channel_events" }

package models

import (
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkTicketEventSubscription{})
	dbhelper.RegisterTable(&WorkTicketEventDelivery{})
	dbhelper.RegisterTable(&WorkTicketEventWait{})
}

// WorkTicketEventSubscription 保存一个工单允许接收动态的来源讨论。
type WorkTicketEventSubscription struct {
	// TicketID 对应接收上下文和实际执行的工单。
	TicketID string `gorm:"size:32;primaryKey"`
	// RuleID 固定为这项订阅持续提供读取授权的规则。
	RuleID string `gorm:"size:32;not null;index:idx_work_ticket_event_topic,priority:1;uniqueIndex:ux_work_ticket_primary_event_topic,priority:1,where:is_primary = 1"`
	// Topic 是来源适配器保存的仓库与讨论对象身份。
	Topic string `gorm:"size:200;not null;index:idx_work_ticket_event_topic,priority:2;uniqueIndex:ux_work_ticket_primary_event_topic,priority:2"`
	// InitialEventID 记录创建该工单或订阅时已处理的首个事件。
	InitialEventID string `gorm:"size:256;not null"`
	// Primary 表示原规则的当前负责工单，其他上下文订阅仍可接收同一事件。
	Primary bool `gorm:"column:is_primary;not null"`
	// ChecksAllowed 保存登记时规则是否订阅了 CI 成功事件。
	ChecksAllowed bool `gorm:"not null;default:false"`
	// RequestID 保留通过工具创建该上下文订阅的原始请求。
	RequestID string `gorm:"size:64;not null;default:''"`
}

// TableName 返回工单讨论订阅表名。
func (WorkTicketEventSubscription) TableName() string { return "work_ticket_event_subscriptions" }

// WorkTicketEventDelivery 持有某张单接纳某条来源动态的稳定投递身份。
type WorkTicketEventDelivery struct {
	// ID 由原工单和来源事件派生。
	ID string `gorm:"size:32;primaryKey"`
	// TicketID 对应获得本次事件上下文的原工单。
	TicketID string `gorm:"size:32;not null;index"`
	// RuleID 保存登记时有效的规则归属。
	RuleID string `gorm:"size:32;not null"`
	// Event 是不可变的来源事件快照。
	Event eventsource.Event `gorm:"type:text;serializer:json;not null"`
	// WaitID 指向本次动态唤醒的原等待，普通上下文更新为空。
	WaitID string `gorm:"size:32;not null;default:''"`
	// ExecutionID 是原 Work Chat 已确认接纳的本次续办执行。
	ExecutionID string `gorm:"size:32;not null;default:'';index:idx_work_ticket_event_read,where:execution_id <> '' AND read_at IS NULL"`
	// Retry 仅在已证明前次原生输入未派发时重新预留执行。
	Retry int `gorm:"not null;default:0"`
	// RecoveryAfterExecutionID 约束补派只适用于原中断执行仍持有当前工单。
	RecoveryAfterExecutionID string `gorm:"size:32;not null;default:''"`
	// ReadAt 仅在原生执行确认消费该输入后设置。
	ReadAt *int64
	// ErrorCode 保留无法在原工作中继续的稳定原因。
	ErrorCode string `gorm:"size:120;not null;default:''"`
}

// TableName 返回工单事件输入表名。
func (WorkTicketEventDelivery) TableName() string { return "work_ticket_event_deliveries" }

// WorkTicketEventWait 关联原请求与唯一一次满足其条件的事件。
type WorkTicketEventWait struct {
	// ID 由原执行和创建等待的稳定工具请求身份派生。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// TicketID 对应持有这项原等待条件的工单。
	TicketID string `gorm:"size:32;not null;index:idx_work_ticket_event_wait,priority:1"`
	// ExecutionID 同时标识请求所属已确认的原输入。
	ExecutionID string `gorm:"size:32;not null"`
	// Generation 拒绝向已经移交或重开的原尝试续办。
	Generation int64 `gorm:"not null;index:idx_work_ticket_event_wait,priority:2"`
	// Kind 是从原来源支持的类型中选择的唤醒条件。
	Kind string `gorm:"size:24;not null" json:"kind"`
	// WakeDeliveryID 保存首次符合条件的事件投递身份。
	WakeDeliveryID string `gorm:"size:32;not null;default:''" json:"wakeDeliveryId,omitempty"`
}

// TableName 返回工单条件等待表名。
func (WorkTicketEventWait) TableName() string { return "work_ticket_event_waits" }

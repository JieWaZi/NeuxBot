package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() { dbhelper.RegisterTable(&AgentCommandReceipt{}) }

// AgentCommandReceipt 只保存主数据库领域命令的原子接纳事实。
type AgentCommandReceipt struct {
	// ID 绑定服务端作用域、精确操作和调用方请求身份。
	ID string `gorm:"type:varchar(64);primaryKey;index:idx_agent_command_pending,priority:2"`
	// Digest 是规范输入摘要，不保存输入原文或凭据。
	Digest string `gorm:"type:varchar(64);not null"`
	// Data 保存首次提交的最小业务回执。
	Data map[string]any `gorm:"serializer:json;type:text;not null"`
	// CreatedAt 是与领域变更一起提交的 Unix 毫秒。
	CreatedAt int64 `gorm:"autoCreateTime:milli"`
	// ManagedChatID 标识接收已提交资源变更的来源聊天。
	ManagedChatID string `gorm:"not null;default:'';index:idx_agent_command_pending,priority:1,where:managed_chat_id <> '' AND managed_projected = 0;index:idx_agent_command_chat_operation,priority:1,where:managed_chat_id <> ''"`
	// ManagedExecutionID 标识发起资源变更的原执行。
	ManagedExecutionID string `gorm:"not null;default:''"`
	// ManagedWakerID 标识发起资源变更的原伙伴。
	ManagedWakerID string `gorm:"not null;default:''"`
	// ManagedOperation 保存已授权的资源操作。
	ManagedOperation string `gorm:"not null;default:'';index:idx_agent_command_chat_operation,priority:2"`
	// ManagedProjected 表示资源变更已在 Chat 日志中结算。
	ManagedProjected bool `gorm:"not null;default:false"`
}

func (AgentCommandReceipt) TableName() string { return "agent_command_receipts" }

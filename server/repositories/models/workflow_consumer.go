package models

import (
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() { dbhelper.RegisterTable(&WorkflowConsumerLease{}) }

// WorkflowConsumerLease 记录用户为一个真实聊天回合选择的工作流能力。
type WorkflowConsumerLease struct {
	// ID 同时是唯一 Execution 身份和 scoped 能力身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// ChatID 是唯一获准使用本能力的权威 Chat。
	ChatID string `gorm:"type:varchar(32);not null;index;index:idx_workflow_consumer_thread_stop,priority:2,where:stopped_at IS NULL" json:"chatId"`
	// DefinitionIDs 仅保留原表的非空列约束；使用授权以 Selection 为准。
	DefinitionIDs []string `gorm:"serializer:json;type:text;not null" json:"-"`
	// Selection 固定用户明确选择的版本和名称，在模型调用时校验一致。
	Selection *workinput.WorkflowReference `gorm:"serializer:json;type:text" json:"-"`
	// ThreadID 是本回合所属的团队对话，普通伙伴对话为空。
	ThreadID string `gorm:"size:32;index:idx_workflow_consumer_thread_stop,priority:1,where:stopped_at IS NULL" json:"-"`
	// StoppedAt 记录明确停止，阻止与停止并发的迟到接纳。
	StoppedAt *int64 `json:"-"`
	// PayloadDigest 固定本回合输入、选择和能力配置，防止首次请求重放扩大范围。
	PayloadDigest string `json:"-"`
	// DeadlineAt 限定能力可以调用的最长时间，实际 Execution 终态会更早撤销。
	DeadlineAt int64 `gorm:"index:idx_workflow_consumer_thread_stop,priority:3,where:stopped_at IS NULL" json:"deadlineAt"`
	// CreatedAt 是签发能力的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回使用者能力持久表名。
func (WorkflowConsumerLease) TableName() string { return "workflow_consumer_leases" }

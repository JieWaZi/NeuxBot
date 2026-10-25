package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkResultReceipt{})
	dbhelper.RegisterTable(&WorkResultTracking{})
}

// WorkResultReference 引用原结果身份，不复制正文或执行状态。
type WorkResultReference struct {
	// ChatID 是结果所在的真实对话。
	ChatID string `json:"chatId,omitempty"`
	// ExecutionID 是产生结果的真实执行。
	ExecutionID string `json:"executionId,omitempty"`
	// MessageID 是持久交付给用户的精确消息。
	MessageID string `json:"messageId,omitempty"`
	// WorkflowRunID 是形成最终结果的工作流运行。
	WorkflowRunID string `json:"workflowRunId,omitempty"`
}

// WorkResultReceipt 保存每个既有容器的最新交付与用户查看或主动清空确认。
type WorkResultReceipt struct {
	// OwnerKind 与 OwnerID 共同确定原工作容器。
	OwnerKind constants.WorkResultOwnerKind `gorm:"size:24;primaryKey;index:idx_work_result_unread,priority:1,where:latest_version > viewed_version" json:"ownerKind"`
	// OwnerID 是原容器身份，不另建 WorkID。
	OwnerID string `gorm:"size:32;primaryKey;index:idx_work_result_unread,priority:3,sort:desc,where:latest_version > viewed_version" json:"ownerId"`
	// LatestVersion 是最新已交付可读结果的单调版本。
	LatestVersion int64 `gorm:"not null" json:"latestVersion"`
	// ViewedVersion 是用户实际查看或主动清空待查看标记的最大版本。
	ViewedVersion int64 `gorm:"not null;default:0" json:"viewedVersion"`
	// Reference 精确定位结果，不保存展示摘要。
	Reference WorkResultReference `gorm:"serializer:json;type:text;not null" json:"reference"`
	// AvailableAt 是该结果真实交付时间及未读排序键。
	AvailableAt int64 `gorm:"not null;index:idx_work_result_unread,priority:2,sort:desc,where:latest_version > viewed_version" json:"availableAt"`
	// ViewedAt 是最后一次有效查看或主动清空确认时间，不改变工作活动时间。
	ViewedAt *int64 `json:"viewedAt,omitempty"`
}

// TableName 返回结果确认事实表名。
func (WorkResultReceipt) TableName() string { return "work_result_receipts" }

// WorkResultTracking 保存唯一启用边界，恢复不把旧历史变成新成果。
type WorkResultTracking struct {
	// ID 是唯一启用记录的固定身份。
	ID string `gorm:"size:32;primaryKey" json:"-"`
	// StartedAt 是首次启用时间，不保存工作数据。
	StartedAt int64 `gorm:"not null" json:"-"`
}

// TableName 返回单行启用元数据表名。
func (WorkResultTracking) TableName() string { return "work_result_tracking" }

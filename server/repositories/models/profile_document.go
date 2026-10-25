// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&RolePresetDocument{})
	dbhelper.RegisterTable(&WakerDocument{})
}

// RolePresetDocument 保存角色预设的一份初始档案内容。
type RolePresetDocument struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// RolePresetID 是拥有该文档的角色预设标识。
	RolePresetID string `gorm:"column:role_preset_id;type:varchar(32);not null;index;uniqueIndex:idx_role_preset_document_owner_kind" json:"rolePresetId"`
	// Kind 是固定的角色档案文档种类。
	Kind constants.ProfileDocumentKind `gorm:"column:kind;type:varchar(24);not null;uniqueIndex:idx_role_preset_document_owner_kind" json:"kind"`
	// Content 是完整且未经执行的 Markdown 文本。
	Content string `gorm:"column:content;type:text;not null" json:"content"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是记录最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回角色预设文档模型使用的表名。
func (RolePresetDocument) TableName() string {
	return "role_preset_documents"
}

// WakerDocument 保存伙伴独立维护的一份档案或记忆内容。
type WakerDocument struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// WakerID 是拥有该文档的伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);not null;index;uniqueIndex:idx_waker_document_owner_kind" json:"wakerId"`
	// Kind 是固定的伙伴档案或记忆文档种类。
	Kind constants.ProfileDocumentKind `gorm:"column:kind;type:varchar(24);not null;uniqueIndex:idx_waker_document_owner_kind" json:"kind"`
	// Content 是完整且未经执行的 Markdown 文本。
	Content string `gorm:"column:content;type:text;not null" json:"content"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是记录最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回伙伴文档模型使用的表名。
func (WakerDocument) TableName() string {
	return "waker_documents"
}

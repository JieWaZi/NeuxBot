// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&RolePreset{})
}

// RolePreset 保存创建伙伴时可复用的一次性初始化内容。
type RolePreset struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// RoleName 是角色名称并同时作为预设展示名称。
	RoleName string `gorm:"column:role_name;type:varchar(80);not null;uniqueIndex" json:"roleName"`
	// DefaultAvatar 是创建伙伴时默认使用的头像。
	DefaultAvatar string `gorm:"column:default_avatar;type:text;not null" json:"defaultAvatar"`
	// Description 是用于预设展示和预填伙伴简介的角色描述。
	Description string `gorm:"column:description;type:text;not null" json:"description"`
	// Source 表示预设由系统还是用户提供。
	Source constants.RolePresetSource `gorm:"column:source;type:varchar(24);not null;index" json:"source"`
	// Expertise 保存从身份档案解析出的擅长领域展示投影。
	Expertise []profile.Expertise `gorm:"column:expertise;type:text;not null;serializer:json" json:"expertise"`
	// WorkStyle 保存从角色风格档案解析出的工作风格展示投影。
	WorkStyle []string `gorm:"column:work_style;type:text;not null;serializer:json" json:"workStyle"`
	// WorkMethods 保存从工作方式档案解析出的具名流程展示投影。
	WorkMethods []profile.WorkMethod `gorm:"column:work_methods;type:text;not null;serializer:json" json:"workMethods"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是记录最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回角色预设模型使用的表名。
func (RolePreset) TableName() string {
	return "role_presets"
}

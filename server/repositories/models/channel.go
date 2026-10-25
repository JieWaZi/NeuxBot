// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&Channel{})
	dbhelper.RegisterTable(&ChannelMember{})
	dbhelper.RegisterTable(&ChannelThread{})
	dbhelper.RegisterTable(&ChannelThreadMember{})
	dbhelper.RegisterTable(&ChannelTask{})
	dbhelper.RegisterTable(&ChannelTaskDependency{})
	dbhelper.RegisterTable(&ChannelTaskAttempt{})
	dbhelper.RegisterTable(&ChannelTaskFollowUp{})
	dbhelper.RegisterTable(&ChannelLeadInput{})
	dbhelper.RegisterTable(&ChannelLeadNotification{})
	dbhelper.RegisterTable(&ChannelEvent{})
}

// Channel 是用户配置并长期复用的团队定义。
type Channel struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是团队列表和对话入口使用的名称。
	Name string `gorm:"column:name;type:varchar(80);not null;uniqueIndex" json:"name"`
	// Description 是帮助用户识别团队定位的简短说明。
	Description string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	// LeadWakerID 是当前团队唯一负责人的伙伴标识。
	LeadWakerID string `gorm:"column:lead_waker_id;type:varchar(32);not null;index" json:"leadWakerId"`
	// CreatedAt 是团队建立时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是团队配置最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli;index" json:"updatedAt"`
}

// TableName 返回团队定义使用的表名。
func (Channel) TableName() string { return "channels" }

// ChannelMember 保存团队成员及其未来对话使用的默认执行配置。
type ChannelMember struct {
	// ChannelID 是成员所属团队标识。
	ChannelID string `gorm:"column:channel_id;type:varchar(32);primaryKey" json:"channelId"`
	// WakerID 是成员对应的伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);primaryKey;index" json:"wakerId"`
	// RuntimeID 是新团队对话为该成员选择的默认 Runtime。
	RuntimeID string `gorm:"column:runtime_id;type:varchar(120);not null" json:"runtimeId"`
	// AccountID 是新团队对话为该成员选择的账号；system 表示原生登录。
	AccountID string `gorm:"column:account_id;type:varchar(32);not null;default:''" json:"accountId"`
	// Model 是新团队对话为该成员选择的默认模型。
	Model string `gorm:"column:model;type:varchar(200);not null;default:''" json:"model"`
	// Reasoning 是新团队对话为该成员选择的默认推理强度。
	Reasoning string `gorm:"column:reasoning;type:varchar(80);not null;default:''" json:"reasoning"`
	// PermissionLevel 是新团队对话为该成员选择的默认权限等级。
	PermissionLevel constants.PermissionLevel `gorm:"column:permission_level;type:varchar(24);not null" json:"permissionLevel"`
	// Position 是成员在团队配置和头像列表中的稳定顺序。
	Position int `gorm:"column:position;not null;default:0" json:"position"`
	// CreatedAt 是成员加入团队配置时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是成员默认配置最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回团队成员配置使用的表名。
func (ChannelMember) TableName() string { return "channel_members" }

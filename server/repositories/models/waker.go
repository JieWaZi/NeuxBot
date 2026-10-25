// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&Waker{})
}

// Waker 保存伙伴当前资料。
type Waker struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是用户识别伙伴时看到的名称。
	Name string `gorm:"column:name;type:varchar(80);not null;uniqueIndex" json:"name"`
	// RoleName 是伙伴当前承担的角色名称。
	RoleName string `gorm:"column:role_name;type:varchar(80);not null;default:''" json:"roleName"`
	// Avatar 是头像 URL、Data URL 或内置头像标识。
	Avatar string `gorm:"column:avatar;type:text" json:"avatar"`
	// Bio 是面向用户介绍当前伙伴的简短文本。
	Bio string `gorm:"column:bio;type:text;not null;default:''" json:"bio"`
	// Expertise 保存从身份档案解析出的擅长领域展示投影。
	Expertise []profile.Expertise `gorm:"column:expertise;type:text;not null;serializer:json" json:"expertise"`
	// WorkStyle 保存从角色风格档案解析出的工作风格展示投影。
	WorkStyle []string `gorm:"column:work_style;type:text;not null;serializer:json" json:"workStyle"`
	// WorkMethods 保存从工作方式档案解析出的具名流程展示投影。
	WorkMethods []profile.WorkMethod `gorm:"column:work_methods;type:text;not null;serializer:json" json:"workMethods"`
	// ActivityState 是列表查询按伙伴普通 Chat 聚合出的只读活动状态。
	ActivityState constants.WakerActivityState `gorm:"-" json:"activityState,omitempty"`
	// RunningCount 是列表查询聚合出的运行中普通会话数量。
	RunningCount int `gorm:"-" json:"runningCount,omitempty"`
	// WaitingCount 是列表查询聚合出的待审批或待输入普通会话数量。
	WaitingCount int `gorm:"-" json:"waitingCount,omitempty"`
	// IsDefault 表示该伙伴由系统提供且不允许删除。
	IsDefault bool `gorm:"-" json:"isDefault,omitempty"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是记录最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回伙伴模型使用的表名。
func (Waker) TableName() string {
	return "wakers"
}

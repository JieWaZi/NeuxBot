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
	// DefaultModel 是此伙伴为新对话选定的模型；缺省跟随全局默认。
	DefaultModel *WakerDefaultModel `gorm:"column:default_model;type:text;serializer:json" json:"defaultModel,omitempty"`
	// OnboardingStatus 是伙伴建档状态；旧伙伴保持空值。
	OnboardingStatus constants.WakerOnboardingStatus `gorm:"column:onboarding_status;type:varchar(16);not null;default:''" json:"onboardingStatus,omitempty"`
	// OnboardingChatID 是唯一允许建档的普通 Chat 身份，先于接纳首轮保存。
	OnboardingChatID string `gorm:"column:onboarding_chat_id;type:varchar(32);not null;default:''" json:"onboardingChatId,omitempty"`
	// OnboardingInterview 定位建档问答在 Chat 日志中的原始请求。
	OnboardingInterview *WakerOnboardingInterview `gorm:"column:onboarding_interview;type:text;serializer:json" json:"-"`
	// CreateRequestHash 是创建伙伴时的规范化请求摘要，供重放比对。
	CreateRequestHash string `gorm:"column:create_request_hash;type:varchar(64);not null;default:''" json:"-"`
	// Expertise 保存从身份档案解析出的擅长领域展示投影。
	Expertise []profile.Expertise `gorm:"column:expertise;type:text;not null;serializer:json" json:"expertise"`
	// WorkStyle 保存从角色风格档案解析出的工作风格展示投影。
	WorkStyle []string `gorm:"column:work_style;type:text;not null;serializer:json" json:"workStyle"`
	// WorkMethods 保存从工作方式档案解析出的具名流程展示投影。
	WorkMethods []profile.WorkMethod `gorm:"column:work_methods;type:text;not null;serializer:json" json:"workMethods"`
	// OverviewRevision 是查询返回的概览编辑版本。
	OverviewRevision string `gorm:"-" json:"overviewRevision,omitempty"`
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

// ProfileOverview 返回当前的结构化档案概览。
func (w Waker) ProfileOverview() profile.Projection {
	return profile.Projection{Expertise: w.Expertise, WorkStyle: w.WorkStyle, WorkMethods: w.WorkMethods}
}

// WakerDefaultModel 保存伙伴选择的明确执行配置。
type WakerDefaultModel struct {
	// 智能体
	RuntimeID string `json:"runtimeId,omitempty" validate:"max=120"`
	// 账号
	AccountID string `json:"accountId,omitempty" validate:"max=32"`
	// 模型
	Model string `json:"model,omitempty" validate:"max=200"`
	// 推理强度
	Reasoning string `json:"reasoning,omitempty" validate:"max=80"`
}

// WakerOnboardingInterview 定位用户填写的结构化建档问答。
type WakerOnboardingInterview struct {
	// ID 是问答的稳定标识。
	ID string `json:"id"`
	// ExecutionID 是问答所属的原执行。
	ExecutionID string `json:"executionId"`
}

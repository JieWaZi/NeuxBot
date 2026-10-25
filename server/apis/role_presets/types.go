// Package role_presets 提供角色预设及初始档案管理接口。
package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
)

// 角色预设请求
type RolePresetWriteRequest struct {
	// 角色名称
	RoleName string `json:"roleName" validate:"required,max=80"`
	// 默认头像
	DefaultAvatar string `json:"defaultAvatar" validate:"omitempty,max=2800000"`
	// 描述
	Description string `json:"description" validate:"required,max=1000"`
	// 输入模式
	InputMode constants.RolePresetInputMode `json:"inputMode" validate:"required"`
	// 文档列表
	Documents profile.DocumentsInput `json:"documents"`
	// 关于我
	Manual profile.ManualInput `json:"manual"`
}

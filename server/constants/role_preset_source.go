// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum RolePresetSource

// RolePresetSource 表示角色预设由系统还是用户提供。
type RolePresetSource string

const (
	// RolePresetSourceUnspecified 表示数据库或框架产生的空来源。
	RolePresetSourceUnspecified RolePresetSource = ""
	// RolePresetSourceSystem 表示只读的系统角色预设。
	RolePresetSourceSystem RolePresetSource = "system"
	// RolePresetSourceUser 表示用户创建并可管理的角色预设。
	RolePresetSourceUser RolePresetSource = "user"
)

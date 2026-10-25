package constants

//go:generate toolx gen enum PermissionLevel

// PermissionLevel 是 NeuxBot 跨 Runtime 使用的三档产品权限。
type PermissionLevel string

const (
	// PermissionLevelUnspecified 表示尚未选择或未写入的空权限等级。
	PermissionLevelUnspecified PermissionLevel = ""
	// PermissionLevelDefault 表示敏感操作需要用户逐次确认。
	PermissionLevelDefault PermissionLevel = "default"
	// PermissionLevelAuto 表示 Runtime 可以自动执行风险可接受的操作。
	PermissionLevelAuto PermissionLevel = "auto"
	// PermissionLevelFullAccess 表示用户明确允许 Runtime 使用最大可用权限。
	PermissionLevelFullAccess PermissionLevel = "full-access"
)

// SelectablePermissionLevel 判断权限是否可用于一次用户配置。
func SelectablePermissionLevel(level PermissionLevel) bool {
	return level == PermissionLevelDefault ||
		level == PermissionLevelAuto ||
		level == PermissionLevelFullAccess
}

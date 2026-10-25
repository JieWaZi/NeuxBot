// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum MessageRole

// MessageRole 表示 Chat 中一条消息的业务发送方。
type MessageRole string

const (
	// MessageRoleUnspecified 表示框架或损坏数据产生的空消息角色。
	MessageRoleUnspecified MessageRole = ""
	// MessageRoleUser 表示用户提交的消息。
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant 表示伙伴回复消息。
	MessageRoleAssistant MessageRole = "assistant"
)

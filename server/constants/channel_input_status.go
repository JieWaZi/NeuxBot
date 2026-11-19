package constants

//go:generate toolx gen enum ChannelInputStatus

// ChannelInputStatus 是用户补充消息进入负责人 Chat 前的可靠投递状态。
type ChannelInputStatus string

const (
	// ChannelInputStatusPending 表示消息已经持久化但尚未进入负责人 Chat。
	ChannelInputStatusPending ChannelInputStatus = "pending"
	// ChannelInputStatusDelivering 表示消息已经关联一次负责人 Chat Execution。
	ChannelInputStatusDelivering ChannelInputStatus = "delivering"
	// ChannelInputStatusConsumed 表示消息已经作为负责人 Chat 的权威事实接纳。
	ChannelInputStatusConsumed ChannelInputStatus = "consumed"
	// ChannelInputStatusFailed 表示负责人输入在进入 Chat 之前被明确拒绝。
	ChannelInputStatusFailed ChannelInputStatus = "failed"
	// ChannelInputStatusCancelled 表示用户停止当前团队工作时取消了尚未执行的消息。
	ChannelInputStatusCancelled ChannelInputStatus = "cancelled"
)

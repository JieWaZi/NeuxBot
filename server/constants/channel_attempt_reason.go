package constants

//go:generate toolx gen enum ChannelAttemptReason

// ChannelAttemptReason 说明团队任务为何产生一次新的执行尝试。
type ChannelAttemptReason string

const (
	// ChannelAttemptReasonInitial 表示任务首次派发。
	ChannelAttemptReasonInitial ChannelAttemptReason = "initial"
	// ChannelAttemptReasonUserFollowUp 表示用户补充或驳回反馈触发续跑。
	ChannelAttemptReasonUserFollowUp ChannelAttemptReason = "user_follow_up"
	// ChannelAttemptReasonLeadFollowUp 表示团队负责人跨成员补充触发续跑。
	ChannelAttemptReasonLeadFollowUp ChannelAttemptReason = "lead_follow_up"
	// ChannelAttemptReasonRetry 表示团队负责人显式重试失败任务。
	ChannelAttemptReasonRetry ChannelAttemptReason = "retry"
	// ChannelAttemptReasonReassigned 表示排队中的补充任务改派给另一成员。
	ChannelAttemptReasonReassigned ChannelAttemptReason = "reassigned"
)

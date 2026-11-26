package constants

//go:generate toolx gen enum ChannelEventType

// ChannelEventType 是 Team 持久事件流的稳定事件类型。
type ChannelEventType string

const (
	// ChannelEventTypeUnspecified 表示空或未知事件类型。
	ChannelEventTypeUnspecified ChannelEventType = ""
	// ChannelEventTaskCreated 表示正式团队任务已经持久化。
	ChannelEventTaskCreated ChannelEventType = "task.created"
	// ChannelEventTaskStarted 表示正式团队任务已经关联成员执行。
	ChannelEventTaskStarted ChannelEventType = "task.started"
	// ChannelEventTaskUpdated 表示负责人已修改尚未执行的正式团队任务。
	ChannelEventTaskUpdated ChannelEventType = "task.updated"
	// ChannelEventTaskFinished 表示正式团队任务尝试已经提交终态。
	ChannelEventTaskFinished ChannelEventType = "task.finished"
	// ChannelEventTaskFollowUpQueued 表示用户补充已经进入任务队列。
	ChannelEventTaskFollowUpQueued ChannelEventType = "task.follow_up_queued"
	// ChannelEventTaskRetryQueued 表示失败任务已经由负责人显式重试。
	ChannelEventTaskRetryQueued ChannelEventType = "task.retry_queued"
	// ChannelEventTaskReviewApproved 表示用户已通过任务人工验收。
	ChannelEventTaskReviewApproved ChannelEventType = "task.review_approved"
	// ChannelEventTaskCancelled 表示单项团队任务已经取消。
	ChannelEventTaskCancelled ChannelEventType = "task.cancelled"
	// ChannelEventTaskRecoveryFailed 表示恢复时无法继续旧任务尝试。
	ChannelEventTaskRecoveryFailed ChannelEventType = "task.recovery_failed"
	// ChannelEventTaskDispatchDeferred 表示任务已经持久但本次派发需要稍后重试。
	ChannelEventTaskDispatchDeferred ChannelEventType = "task.dispatch_deferred"
	// ChannelEventThreadCreated 表示团队对话及成员 Chat 已经就绪。
	ChannelEventThreadCreated ChannelEventType = "thread.created"
	// ChannelEventThreadWorkStopping 表示团队对话已经冻结新工作并正在收敛真实执行。
	ChannelEventThreadWorkStopping ChannelEventType = "thread.work_stopping"
	// ChannelEventThreadWorkStopped 表示团队对话当前工作已由用户停止。
	ChannelEventThreadWorkStopped ChannelEventType = "thread.work_stopped"
	// ChannelEventMemberConfigsUpdated 表示团队对话成员执行配置已经更新。
	ChannelEventMemberConfigsUpdated ChannelEventType = "members.config_updated"
	// ChannelEventLeadInputQueued 表示用户输入已进入负责人可靠队列。
	ChannelEventLeadInputQueued ChannelEventType = "lead.input_queued"
	// ChannelEventLeadInputFailed 表示负责人队列已结算未进入 Chat 的输入。
	ChannelEventLeadInputFailed ChannelEventType = "lead.input_failed"
	// ChannelEventLeadInputCancelled 表示指定的负责人队列输入已取消。
	ChannelEventLeadInputCancelled ChannelEventType = "lead.input_cancelled"
	// ChannelEventTeamLeaderProcessing 表示负责人开始或结束处理成员结果批次。
	ChannelEventTeamLeaderProcessing ChannelEventType = "team.leader.processing"
	// ChannelEventMemberDirectDelivered 表示成员报告已直接成为团队主时间线的完整答复。
	ChannelEventMemberDirectDelivered ChannelEventType = "team.member.direct_delivered"
	// ChannelEventApprovalRequested 表示成员 Chat 审批请求已投影到团队事件流。
	ChannelEventApprovalRequested ChannelEventType = "interaction.approval.requested"
	// ChannelEventApprovalResolved 表示成员 Chat 审批结果已投影到团队事件流。
	ChannelEventApprovalResolved ChannelEventType = "interaction.approval.resolved"
	// ChannelEventElicitationRequested 表示成员 Chat 结构化输入请求已投影到团队事件流。
	ChannelEventElicitationRequested ChannelEventType = "interaction.elicitation.requested"
	// ChannelEventElicitationResolved 表示成员 Chat 结构化输入结果已投影到团队事件流。
	ChannelEventElicitationResolved ChannelEventType = "interaction.elicitation.resolved"
)

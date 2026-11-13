package models

import "github.com/JieWaZi/NeuxBot/server/constants"

// WorkRef 定位原工作，无独立持久身份。
type WorkRef struct {
	// Kind 是原工作容器种类。
	Kind constants.WorkOwnerKind `json:"kind"`
	// ID 是该原容器的身份。
	ID string `json:"id"`
}

// AttentionCompleteness 报告查询是否覆盖全部原事实。
type AttentionCompleteness struct {
	// Complete 表示该查询范围的阅读投影完整。
	Complete bool `json:"complete"`
	// IncompleteReason 区分暂时恢复与已隔离的资源。
	IncompleteReason string `json:"incompleteReason,omitempty"`
	// Epoch 标识提供本次结果的 Core 进程。
	Epoch string `json:"epoch"`
}

// AttentionMetrics 保留各自业务口径的独立聚合。
type AttentionMetrics struct {
	// UnreadMessageCount 是可见会话的合格未读消息数。
	UnreadMessageCount int64 `json:"unreadMessageCount"`
	// UnreadChatCount 是有合格未读消息的会话数。
	UnreadChatCount int64 `json:"unreadChatCount"`
	// WaitingWorkCount 是有有效请求的工作容器数。
	WaitingWorkCount int64 `json:"waitingWorkCount"`
	// PendingRequestCount 是有效的原请求数。
	PendingRequestCount int64 `json:"pendingRequestCount"`
	// AwaitingViewWorkCount 是仍有未读消息或未查看结果的工作容器数。
	AwaitingViewWorkCount int64 `json:"awaitingViewWorkCount"`
	// RunningChatCount 是原活动状态为运行中的可见对话数。
	RunningChatCount int64 `json:"runningChatCount"`
}

// WorkspaceAttentionSnapshot 是铃铛所需的最小权威结果。
type WorkspaceAttentionSnapshot struct {
	// AttentionCompleteness 报告查询覆盖范围。
	AttentionCompleteness
	// HasAction 表示至少有一个有效请求。
	HasAction bool `json:"hasAction"`
	// HasUnread 表示至少有一个工作仍待查看。
	HasUnread bool `json:"hasUnread"`
}

// WorkViewTarget 冻结一次用户操作所见的原对象及阅读边界。
type WorkViewTarget struct {
	// Work 是需要重新校验归属的原容器。
	Work WorkRef `json:"work"`
	// ChatID 是实际阅读的公开会话。
	ChatID string `json:"chatId,omitempty"`
	// ThroughMessageID 是操作时已存在的最后一条合格消息。
	ThroughMessageID string `json:"throughMessageID,omitempty"`
	// ObservedUnreadGeneration 是操作时观察的手动未读代次。
	ObservedUnreadGeneration int64 `json:"observedUnreadGeneration"`
	// OwnerKind 是查看最终结果时的原运行种类。
	OwnerKind constants.RunOwnerKind `json:"ownerKind,omitempty"`
	// OwnerID 是查看最终结果时的原运行标识。
	OwnerID string `json:"ownerId,omitempty"`
}

// MarkViewedResult 是一批实际提交的结果。
type MarkViewedResult struct {
	// Applied 是本批已确认的原目标数量。
	Applied int `json:"applied"`
	// SkippedConflict 是代次或原对象状态已经变化的目标数量。
	SkippedConflict int `json:"skippedConflict"`
	// SkippedDeleted 是原工作或边界已被删除的目标数量。
	SkippedDeleted int `json:"skippedDeleted"`
}

// ExecutorAttentionChat 供伙伴和团队侧栏直接定位公开会话和请求。
type ExecutorAttentionChat struct {
	// Work 是公开容器身份。
	Work WorkRef `json:"work"`
	// Title 是公开会话的原名称。
	Title string `json:"title"`
	// Requests 定位这条公开会话归属的有效请求。
	Requests []WorkBoardAttention `json:"requests"`
	// ChatID 是公开会话的阅读对象。
	ChatID string `json:"chatId"`
	// UnreadMessageCount 是会话内待查看消息数。
	UnreadMessageCount int64 `json:"unreadMessageCount"`
	// FirstUnreadMessageID 是公开展示顺序中的第一条未读消息。
	FirstUnreadMessageID string `json:"firstUnreadMessageID,omitempty"`
	// RequestKinds 是当前有效请求的不同操作类型。
	RequestKinds []string `json:"requestKinds"`
	// Activity 是原会话运行状态。
	Activity constants.ChannelActivityState `json:"activity"`
	// LastStatus 是原会话最近一次执行状态。
	LastStatus string `json:"lastStatus,omitempty"`
}

// ExecutorAttentionSummary 是指定伙伴或团队的独立阅读和待办指标。
type ExecutorAttentionSummary struct {
	// ExecutorID 是请求的原伙伴或团队。
	ExecutorID string `json:"executorId"`
	// Kind 是执行对象类型。
	Kind string `json:"kind"`
	// AttentionMetrics 是归属到此执行对象的精确指标。
	AttentionMetrics
	// LatestChatID 是按当前活动时间及稳定身份选出的最近公开对话。
	LatestChatID string `json:"latestChatId,omitempty"`
	// LatestWork 定位最近公开对话所属原工作。
	LatestWork *WorkRef `json:"latestWork,omitempty"`
	// LastStatus 是该最近公开对话的最后一次真实执行结果。
	LastStatus string `json:"lastStatus,omitempty"`
	// Chats 定位具备关注或正在工作的公开会话。
	Chats []ExecutorAttentionChat `json:"chats"`
	// Requests 定位每个仍有效的原操作及真实成员或子运行。
	Requests []WorkBoardAttention `json:"requests"`
}

// WorkBoardLiveEvent 只描述来自本次真实提交的安全提醒身份。
type WorkBoardLiveEvent struct {
	// Operation 区分完成、失败、请求建立和请求结束。
	Operation string `json:"operation"`
	// Work 是事件生产者持有的原身份，展示前核验可见归属。
	Work WorkRef `json:"work"`
	// MessageID 是公开结果的原消息。
	MessageID string `json:"messageId,omitempty"`
	// ChatID 是实际产生结果或等待操作的 Chat。
	ChatID string `json:"chatId,omitempty"`
	// RequestID 是原操作请求或验收任务的真实身份。
	RequestID string `json:"requestId,omitempty"`
	// RequestKind 是审批、输入或验收。
	RequestKind string `json:"requestKind,omitempty"`
	// ExecutionID 是实际执行或工作流子运行。
	ExecutionID string `json:"executionId,omitempty"`
	// RequestFence 是团队验收的原围栏。
	RequestFence int64 `json:"requestFence,omitempty"`
}

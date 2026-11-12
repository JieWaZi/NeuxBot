package models

import "github.com/JieWaZi/NeuxBot/server/constants"

// WorkBoardCounts 是原工作容器的即时聚合，不注册数据库表。
type WorkBoardCounts struct {
	// Total 是活动日期范围内的不同工作容器数。
	Total int64
	// Running 是当前正在执行的工作容器数，不含等待用户操作的工作。
	Running int64
	// Ended 是当前空闲且最近执行已成功、失败或取消的工作容器数。
	Ended int64
}

// WorkBoardExecutor 是原执行对象配置的有界搜索结果，不保存工作历史。
type WorkBoardExecutor struct {
	// ID 是伙伴、团队或流程定义的原标识。
	ID string `json:"id"`
	// Name 是当前名称。
	Name string `json:"name"`
	// Kind 区分伙伴、团队与工作流。
	Kind string `json:"kind"`
}

// WorkBoardEntry 是既有工作容器的窄查询结果，不注册数据库表。
type WorkBoardEntry struct {
	// OwnerKind 是原工作容器种类。
	OwnerKind constants.WorkOwnerKind `json:"ownerKind"`
	// OwnerID 是原容器标识。
	OwnerID string `json:"ownerId"`
	// Title 是原容器当前标题。
	Title string `json:"title"`
	// ExecutorID 是伙伴、团队或工作流的身份，用于统一筛选。
	ExecutorID string `json:"executorId"`
	// ExecutorName 是执行对象当前名称。
	ExecutorName string `json:"executorName"`
	// Source 是对话、团队、工作流、自动任务或 IM 来源。
	Source string `json:"source"`
	// ChatID 定位原独立对话或团队主对话。
	ChatID string `json:"chatId,omitempty"`
	// ChannelID 定位所属团队。
	ChannelID string `json:"channelId,omitempty"`
	// WorkflowID 定位原工作流定义。
	WorkflowID string `json:"workflowId,omitempty"`
	// WakerID 是进入普通对话时所需伙伴标识。
	WakerID string `json:"wakerId,omitempty"`
	// Activity 表达当前空闲、执行或等待，不改写领域状态。
	Activity constants.ChannelActivityState `json:"activity"`
	// LastStatus 是最近结束执行或固定运行的原状态。
	LastStatus string `json:"lastStatus,omitempty"`
	// UpdatedAt 是原工作最近活动时间。
	UpdatedAt int64 `json:"updatedAt"`
	// ApprovalCount 是此工作仍有效的审批数量。
	ApprovalCount int64 `json:"approvalCount"`
	// InputCount 是此工作仍有效的输入请求数量。
	InputCount int64 `json:"inputCount"`
	// ReviewCount 是此工作等待团队人工验收的任务数。
	ReviewCount int64 `json:"reviewCount"`
	// AutomaticTaskID 定位未接纳自动机会的原配置。
	AutomaticTaskID string `json:"automaticTaskId,omitempty"`
	// UnreadMessageCount 是本工作公开会话的未读消息数。
	UnreadMessageCount int64 `gorm:"-" json:"unreadMessageCount"`
	// FirstUnreadMessageID 是最早合格未读消息的原身份。
	FirstUnreadMessageID string `gorm:"-" json:"firstUnreadMessageID,omitempty"`
	// LatestReadableMessageID 是冻结阅读目标的最后消息身份。
	LatestReadableMessageID string `gorm:"-" json:"latestReadableMessageID,omitempty"`
}

// WorkBoardAttention 是一个有效用户交互的只读定位信息。
type WorkBoardAttention struct {
	// ID 在请求所在作用域内稳定，用于事项去重与排序。
	ID string `json:"id"`
	// RequestID 是原请求或原验收任务身份。
	RequestID string `json:"requestId"`
	// ExecutionID 是请求实际等待的原执行或子运行。
	ExecutionID string `json:"executionId,omitempty"`
	// RequestSequence 是同一 Chat 内待答请求的原始到达顺序。
	RequestSequence int64 `json:"requestSequence,omitempty"`
	// RequestFence 是团队当前验收代次。
	RequestFence int64 `json:"requestFence,omitempty"`
	// InstanceID 是人工请求对应的原工作流步骤实例。
	InstanceID string `json:"instanceId,omitempty"`
	// Kind 是审批、输入或验收的操作类别。
	Kind string `json:"kind"`
	// OwnerKind 是该事项归属的原工作种类。
	OwnerKind constants.WorkOwnerKind `json:"ownerKind"`
	// OwnerID 是事项所属的原工作身份。
	OwnerID string `json:"ownerId"`
	// Title 是请求安全标题，不包含工具原始参数。
	Title string `json:"title"`
	// WorkTitle 是原工作标题。
	WorkTitle string `json:"workTitle"`
	// ExecutorID 是原工作执行对象，用于范围筛选。
	ExecutorID string `json:"executorId"`
	// ActorName 是实际等待操作的成员或工作流名称。
	ActorName string `json:"actorName"`
	// Source 是该工作当前来源。
	Source string `json:"source"`
	// ChatID 定位实际等待操作的 Chat。
	ChatID string `json:"chatId,omitempty"`
	// WakerID 是实际成员伙伴身份。
	WakerID string `json:"wakerId,omitempty"`
	// ChannelID 是所属团队身份。
	ChannelID string `json:"channelId,omitempty"`
	// WorkflowID 是对应工作流定义。
	WorkflowID string `json:"workflowId,omitempty"`
	// RunID 是实际等待操作的运行，可能是子运行。
	RunID string `json:"runId,omitempty"`
	// TaskID 是待验收的原团队任务。
	TaskID string `json:"taskId,omitempty"`
	// CreatedAt 是请求开始等待的真实时间。
	CreatedAt int64 `json:"createdAt"`
}

package models

import "github.com/JieWaZi/NeuxBot/server/constants"

// NotificationTarget 是原业务入口的有判别定位，不保存任意可执行地址。
type NotificationTarget struct {
	// 目标类别
	Kind constants.NotificationKind `json:"kind"`
	// 对话标识
	ChatID string `json:"chatId,omitempty"`
	// 伙伴标识
	WakerID string `json:"wakerId,omitempty"`
	// 团队标识
	ChannelID string `json:"channelId,omitempty"`
	// 团队对话
	ThreadID string `json:"threadId,omitempty"`
	// 验收任务
	TaskID string `json:"taskId,omitempty"`
	// 提交围栏
	Fence int64 `json:"fence,omitempty"`
	// 流程标识
	DefinitionID string `json:"definitionId,omitempty"`
	// 运行标识
	RunID string `json:"runId,omitempty"`
	// 原请求标识
	RequestID string `json:"requestId,omitempty"`
	// 容器类别
	OwnerKind constants.WorkResultOwnerKind `json:"ownerKind,omitempty"`
	// 容器标识
	OwnerID string `json:"ownerId,omitempty"`
	// 成果版本
	ResultVersion int64 `json:"resultVersion,omitempty"`
	// 精确消息
	MessageID string `json:"messageId,omitempty"`
	// 成果引用
	Reference *WorkResultReference `json:"reference,omitempty"`
	// 自动运行
	AutomaticRunID string `json:"automaticRunId,omitempty"`
}

// NotificationItem 是由原领域事实派生的安全通知，不注册持久化表。
type NotificationItem struct {
	// 通知标识
	ID string `json:"id"`
	// 通知类型
	Kind constants.NotificationKind `json:"kind"`
	// 关注分桶
	Bucket string `json:"bucket"`
	// 原始来源
	Source string `json:"source"`
	// 安全标题
	Title string `json:"title"`
	// 有界摘要
	Description string `json:"description"`
	// 发生时间
	CreatedAt int64 `json:"createdAt"`
	// 原始版本
	Version int64 `json:"version"`
	// 执行对象
	Executor WorkBoardExecutor `json:"executor"`
	// 原目标引用
	Target NotificationTarget `json:"target"`
	// 提示已收起
	Dismissed bool `json:"dismissed"`
	// 原交付失败
	Failed bool `json:"failed,omitempty"`
}

// NotificationFacts 是同一仓储短读快照内的统计与有界预览。
type NotificationFacts struct {
	// ActionCount 是全部有效待处理请求数。
	ActionCount int64
	// UnreadResultCount 是存在最新未确认交付的原容器数。
	UnreadResultCount int64
	// UnreadFailureCount 是没有原成果覆盖的当前未确认自动失败数。
	UnreadFailureCount int64
	// ActionPreview 是最多五条最早等待的请求。
	ActionPreview []NotificationItem
	// UnreadPreview 是最多五条最新交付或失败。
	UnreadPreview []NotificationItem
	// NextDeadline 是当前最早有效请求期限，用于单次唤醒。
	NextDeadline int64
}

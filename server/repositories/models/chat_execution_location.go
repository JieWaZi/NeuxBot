package models

import (
	"github.com/shrewx/ginx/pkg/dbhelper"
	"slices"
)

func init() {
	dbhelper.RegisterTable(&ChatExecutionLocation{})
	dbhelper.RegisterTable(&ChatExecutionLocationCursor{})
}

// ChatExecutionLocation 只定位原 JSONL 中的执行及消息入口，不保存执行状态或正文。
type ChatExecutionLocation struct {
	// ChatID 是权威日志所属 Chat。
	ChatID string `gorm:"primaryKey;size:32;index:idx_chat_submission_location,priority:1;index:idx_chat_execution_sequence,priority:1"`
	// ExecutionID 是原执行、用户消息或继承消息的稳定身份。
	ExecutionID string `gorm:"primaryKey;size:32"`
	// SubmissionID 是入口原有的可选幂等身份。
	SubmissionID string `gorm:"index:idx_chat_submission_location,priority:2"`
	// Sequence 是原 JSONL 执行或消息入口的一基序号。
	Sequence int64 `gorm:"index:idx_chat_execution_sequence,priority:3"`
	// LifecycleSequences 是 execution.started 和唯一终态在原 JSONL 中的位置。
	LifecycleSequences []int64 `gorm:"serializer:json;type:text"`
	// InvocationBoundarySequence 是最新原生调用身份首次出现的原信封位置。
	InvocationBoundarySequence int64
	// InvocationFinishedSequence 是最近已持久化调用结论的原日志位置。
	InvocationFinishedSequence int64
	// FinishedInvocationBoundarySequence 是最近调用结论对应的原生身份信封位置。
	FinishedInvocationBoundarySequence int64
	// MessageOnly 区分用户或继承消息入口与真实接纳执行。
	MessageOnly bool `gorm:"index:idx_chat_execution_sequence,priority:2"`
}

// TableName 返回执行定位投影表名。
func (ChatExecutionLocation) TableName() string { return "chat_execution_locations" }

// ChatExecutionLocationCursor 只记录执行定位索引已覆盖的原日志位置。
type ChatExecutionLocationCursor struct {
	// ChatID 是需要追平的权威日志身份。
	ChatID string `gorm:"primaryKey;size:32"`
	// SchemaVersion 是当前纯定位投影的格式版本，不是业务版本。
	SchemaVersion int
	// Sequence 是定位条目已完整覆盖的最大连续序号。
	Sequence int64
}

// TableName 返回执行定位覆盖游标表名。
func (ChatExecutionLocationCursor) TableName() string { return "chat_execution_location_cursors" }

// MergeExecutionLocationTail 合并同一原执行的定位尾部，只保留原序号，不保存业务状态。
func MergeExecutionLocationTail(previous *ChatExecutionLocation, next ChatExecutionLocation) ChatExecutionLocation {
	if previous == nil {
		return next
	}
	next.LifecycleSequences = append(append([]int64(nil), previous.LifecycleSequences...), next.LifecycleSequences...)
	slices.Sort(next.LifecycleSequences)
	next.LifecycleSequences = slices.Compact(next.LifecycleSequences)
	if next.InvocationFinishedSequence == 0 {
		next.InvocationFinishedSequence = previous.InvocationFinishedSequence
		next.FinishedInvocationBoundarySequence = previous.FinishedInvocationBoundarySequence
	} else if next.FinishedInvocationBoundarySequence == 0 {
		next.FinishedInvocationBoundarySequence = previous.InvocationBoundarySequence
	}
	if next.InvocationBoundarySequence == 0 {
		next.InvocationBoundarySequence = previous.InvocationBoundarySequence
	}
	return next
}

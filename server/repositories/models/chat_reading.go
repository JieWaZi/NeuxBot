package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&ChatReadableMessage{})
	dbhelper.RegisterTable(&ChatReadCursor{})
	dbhelper.RegisterTable(&ReadingInitialization{})
}

// ChatReadableMessage 是公开可读消息的可重建索引。
type ChatReadableMessage struct {
	// ChatID 是权威消息所属 Chat。
	ChatID string `gorm:"size:32;primaryKey;uniqueIndex:idx_chat_readable_order,priority:1"`
	// MessageID 是原公开消息身份。
	MessageID string `gorm:"size:32;primaryKey"`
	// OrderSequence 是原消息创建序号，也是公开展示顺序。
	OrderSequence int64 `gorm:"not null;uniqueIndex:idx_chat_readable_order,priority:2"`
	// ExecutionID 是产生原消息的执行，可为空。
	ExecutionID string `gorm:"size:32;not null;default:''"`
	// Kind 是安全的 answer、failure 或 member_report 分类。
	Kind string `gorm:"size:16;not null;check:kind IN ('answer','failure','member_report')"`
	// CreatedAt 是原消息的创建时间。
	CreatedAt int64 `gorm:"not null;autoCreateTime:false"`
}

// TableName 返回公开可读消息索引表。
func (ChatReadableMessage) TableName() string { return "chat_readable_messages" }

// ChatReadCursor 保存一个阅读者在指定 Chat 的阅读边界及手动未读代次。
type ChatReadCursor struct {
	// ReaderID 是 Core 固定的本机阅读者身份。
	ReaderID string `gorm:"size:32;primaryKey" json:"-"`
	// ChatID 是阅读事实所属对话。
	ChatID string `gorm:"size:32;primaryKey" json:"chatId"`
	// ThroughSequence 是已确认的公开展示前缀。
	ThroughSequence int64 `gorm:"not null;default:0" json:"throughSequence"`
	// UnreadGeneration 仅在主动标未读后递增。
	UnreadGeneration int64 `gorm:"not null;default:0" json:"unreadGeneration"`
	// HoldUnread 阻止自动读，直到新的明确阅读意图生效。
	HoldUnread bool `gorm:"not null;default:false" json:"holdUnread"`
	// UpdatedAt 是阅读事实最后变更的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"not null;autoUpdateTime:false" json:"updatedAt"`
}

// TableName 返回用户阅读位置表。
func (ChatReadCursor) TableName() string { return "chat_read_cursors" }

// ChatReadingState 返回一个对话的当前阅读位置与消息边界。
type ChatReadingState struct {
	// ChatReadCursor 保存阅读边界与代次。
	ChatReadCursor
	// UnreadMessageCount 是展示顺序位于阅读边界之后的合格消息数。
	UnreadMessageCount int64 `json:"unreadMessageCount"`
	// FirstUnreadMessageID 是第一条合格未读消息的原身份。
	FirstUnreadMessageID string `json:"firstUnreadMessageID,omitempty"`
	// LatestReadableMessageID 是最后一条公开合格消息的原身份。
	LatestReadableMessageID string `json:"latestReadableMessageID,omitempty"`
}

// ReadingInitialization 是首次切换的单行持久阶段标记。
type ReadingInitialization struct {
	// ID 是固定初始化标记身份。
	ID string `gorm:"size:32;primaryKey"`
	// StartedAt 是首次进入新阅读基线流程的时间。
	StartedAt int64 `gorm:"not null"`
	// BaselineCompletedAt 是原运行恢复开始前的持久屏障。
	BaselineCompletedAt *int64
	// CompletedAt 是原运行恢复和旧表清理后开放业务的持久屏障。
	CompletedAt *int64
}

// TableName 返回首次阅读初始化表。
func (ReadingInitialization) TableName() string { return "reading_initialization" }

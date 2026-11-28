package models

import (
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&AutomaticEventCursor{})
	dbhelper.RegisterTable(&AutomaticEventObservation{})
}

// AutomaticEventCursor 保存自动规则对当前来源实例的最近检查状态。
type AutomaticEventCursor struct {
	// TaskID 是授权检查的规则。
	TaskID string `gorm:"size:32;primaryKey" json:"-"`
	// Instance 区分仓库或来源配置变更前后的读取位置。
	Instance string `gorm:"size:64;primaryKey" json:"-"`
	// Revision 防止较早的并发响应覆盖已提交的状态。
	Revision int64 `gorm:"not null" json:"-"`
	// Initialized 表明至少一次成功检查已保存基线。
	Initialized bool `gorm:"not null" json:"initialized"`
	// CheckedAt 是已提交检查的开始时间。
	CheckedAt int64 `gorm:"not null" json:"checkedAt"`
	// Outcome 区分基线、空检查、过滤、命中和失败。
	Outcome string `gorm:"size:24;not null" json:"outcome"`
	// ErrorCode 是检查失败时展示的稳定错误标识。
	ErrorCode string `gorm:"size:120" json:"errorCode,omitempty"`
	// Matched 是这次新增的符合条件的事件数。
	Matched int `gorm:"not null" json:"matched"`
	// Filtered 是这次新增的已过滤事件数。
	Filtered int `gorm:"not null" json:"filtered"`
}

// TableName 返回事件规则游标的表名。
func (AutomaticEventCursor) TableName() string { return "automatic_event_cursors" }

// AutomaticEventObservation 保存一个来源实例对事件的接纳与派工回执。
type AutomaticEventObservation struct {
	// ID 是规则、来源实例和事件共同派生的内部标识。
	ID string `gorm:"size:64;primaryKey" json:"id"`
	// TaskID 是授权接收该观察的规则。
	TaskID string `gorm:"size:32;not null;index:idx_automatic_event_delivery,priority:1" json:"-"`
	// Instance 是首次建立基线时使用的同一来源身份。
	Instance string `gorm:"size:64;not null;index:idx_automatic_event_delivery,priority:2" json:"-"`
	// Event 保留适配器生成的已规范化来源信息。
	Event eventsource.Event `gorm:"serializer:json;type:text;not null" json:"event"`
	// PolicyDigest 固定决定这条观察是否符合规则的来源过滤条件。
	PolicyDigest string `gorm:"size:64;not null;default:''" json:"-"`
	// Disposition 表示基线、已过滤、待派工、已接纳或失败。
	Disposition string `gorm:"size:24;not null;index:idx_automatic_event_delivery,priority:3" json:"disposition"`
	// RunID 是已经接纳的自动任务运行标识。
	RunID string `gorm:"size:32;not null;default:''" json:"runId,omitempty"`
	// ErrorCode 是不可派工的稳定原因或续办回退原因。
	ErrorCode string `gorm:"size:120" json:"errorCode,omitempty"`
	// ObservedAt 是 Core 接纳该事件的 Unix 毫秒时间。
	ObservedAt int64 `gorm:"not null;index:idx_automatic_event_delivery,priority:4" json:"observedAt"`
}

// TableName 返回事件规则观察的表名。
func (AutomaticEventObservation) TableName() string { return "automatic_event_observations" }

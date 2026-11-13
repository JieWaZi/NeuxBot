package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() { dbhelper.RegisterTable(&RunViewMark{}) }

// RunOwnerRef 定位具有独立最终结果的原运行。
type RunOwnerRef struct {
	// OwnerKind 是可查看最终结果的原运行类型。
	OwnerKind constants.RunOwnerKind `json:"ownerKind"`
	// OwnerID 是不可变的原运行标识。
	OwnerID string `json:"ownerId"`
}

// RunViewMark 记录一个阅读者已查看一次原运行的最终结果。
type RunViewMark struct {
	// ReaderID 是 Core 固定的本机阅读者身份。
	ReaderID string `gorm:"size:32;primaryKey"`
	// OwnerKind 与 OwnerID 共同定位原运行。
	OwnerKind constants.RunOwnerKind `gorm:"size:24;primaryKey"`
	// OwnerID 是不可变的原运行标识。
	OwnerID string `gorm:"size:32;primaryKey"`
	// ViewedAt 是用户首次确认查看最终结果的时间。
	ViewedAt int64 `gorm:"not null"`
}

// TableName 返回原运行查看事实表。
func (RunViewMark) TableName() string { return "run_view_marks" }

// RunViewingState 表示指定原运行是否有用户可查看的结果及是否已查看。
type RunViewingState struct {
	// RunOwnerRef 定位原运行。
	RunOwnerRef
	// Viewable 表示已发布可见终态结果。
	Viewable bool `json:"viewable"`
	// Viewed 表示该原运行的可见结果已被查看。
	Viewed bool `json:"viewed"`
}

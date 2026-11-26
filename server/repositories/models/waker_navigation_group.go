package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&WakerNavigationGroup{})
}

// WakerNavigationGroup 保存伙伴导航分组及其插入顺序。
type WakerNavigationGroup struct {
	// ID 是分组的 32 位无连字符 UUID。
	ID string `gorm:"type:varchar(32);primaryKey"`
	// Name 是去掉首尾空白后的用户可见名称。
	Name string `gorm:"type:text;not null;uniqueIndex"`
	// Position 是分组在导航中的稳定插入顺序。
	Position int64 `gorm:"not null;uniqueIndex"`
}

// TableName 返回伙伴导航分组使用的表名。
func (WakerNavigationGroup) TableName() string {
	return "waker_navigation_groups"
}

// 伙伴导航分组成员
type WakerNavigationGroupMembers struct {
	// 分组名称
	Name string `json:"name" validate:"required"`
	// 伙伴标识
	WakerIDs []string `json:"wakerIds" validate:"required,dive,required"`
}

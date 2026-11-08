package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() { dbhelper.RegisterTable(&ResourceCategory{}) }

// ResourceCategory 保存各类型资源可重复选择的用户分类，移除资源标签不会删除分类。
type ResourceCategory struct {
	// ID 是分类的稳定标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Type 区分插件、独立技能和独立连接器。
	Type constants.ResourceCategoryType `gorm:"column:type;not null;uniqueIndex:idx_resource_category,priority:1" json:"type"`
	// Name 是用户设置的分类名称，同类型内唯一。
	Name string `gorm:"column:name;not null;uniqueIndex:idx_resource_category,priority:2" json:"name"`
	// CreatedAt 是首次创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回用户资源分类表名。
func (ResourceCategory) TableName() string { return "resource_categories" }

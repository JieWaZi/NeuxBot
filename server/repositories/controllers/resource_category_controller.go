package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResourceCategoryController 维护同类型资源共享的分类选项。
type ResourceCategoryController interface {
	// List 返回指定类型的全部已有分类。
	List(context.Context, constants.ResourceCategoryType) ([]models.ResourceCategory, error)
	// Ensure 幂等创建已规范化的分类，重复保存不产生重复项。
	Ensure(context.Context, constants.ResourceCategoryType, []string) error
}

// resourceCategoryController 使用单表查询管理用户分类。
type resourceCategoryController struct {
	// db 是默认数据库连接，事务内使用 Context 中的连接。
	db *gorm.DB
}

// NewResourceCategoryController 创建分类持久化接口。
func NewResourceCategoryController(db *gorm.DB) ResourceCategoryController {
	return &resourceCategoryController{db: db}
}

// List 按名称稳定返回指定资源类型的分类。
func (c *resourceCategoryController) List(ctx context.Context, kind constants.ResourceCategoryType) ([]models.ResourceCategory, error) {
	items := []models.ResourceCategory{}
	if err := controllerDB(ctx, c.db).Where("type = ?", kind).Order("name, id").Find(&items).Error; err != nil {
		return nil, databaseError("list_resource_categories", string(kind), err)
	}
	return items, nil
}

// Ensure 使用类型和实际名称的唯一索引处理并发创建。
func (c *resourceCategoryController) Ensure(ctx context.Context, kind constants.ResourceCategoryType, names []string) error {
	if len(names) == 0 {
		return nil
	}
	items := make([]models.ResourceCategory, 0, len(names))
	for _, name := range names {
		items = append(items, models.ResourceCategory{ID: util.New(), Type: kind, Name: name})
	}
	err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "type"}, {Name: "name"}}, DoNothing: true}).Create(&items).Error
	if err != nil {
		return databaseError("ensure_resource_categories", string(kind), err)
	}
	return nil
}

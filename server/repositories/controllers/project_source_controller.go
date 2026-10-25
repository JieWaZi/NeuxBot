package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectSourceController 定义项目来源查询索引的持久化操作。
type ProjectSourceController interface {
	// Ensure 幂等创建或刷新一个由真实路径稳定标识的项目来源。
	Ensure(ctx context.Context, source models.ProjectSource) error
	// Get 按标识返回项目来源。
	Get(ctx context.Context, id string) (models.ProjectSource, error)
}

// projectSourceController 使用 GORM 实现项目来源索引持久化。
type projectSourceController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewProjectSourceController 创建项目来源 Repository Controller。
func NewProjectSourceController(db *gorm.DB) ProjectSourceController {
	return &projectSourceController{db: db}
}

// Ensure 幂等创建或刷新一个由真实路径稳定标识的项目来源。
func (c *projectSourceController) Ensure(
	ctx context.Context,
	source models.ProjectSource,
) error {
	err := controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":       source.Name,
				"path":       source.Path,
				"updated_at": source.UpdatedAt,
			}),
		}).
		Create(&source).Error
	if err != nil {
		return databaseError("ensure_project_source", "project_source_id="+source.ID, err)
	}
	return nil
}

// Get 按标识返回项目来源。
func (c *projectSourceController) Get(
	ctx context.Context,
	id string,
) (models.ProjectSource, error) {
	var source models.ProjectSource
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&source).Error
	if err == nil {
		return source, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ProjectSource{}, status_error.ProjectSourceUnavailable
	}
	return models.ProjectSource{}, databaseError(
		"get_project_source",
		"project_source_id="+id,
		err,
	)
}

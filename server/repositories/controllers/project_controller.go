package controllers

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// ProjectController 定义伙伴项目及其目录关联的持久化操作。
type ProjectController interface {
	// Create 创建项目主体。
	Create(ctx context.Context, project models.Project) error
	// Get 按标识返回项目主体。
	Get(ctx context.Context, id string) (models.Project, error)
	// ListByWaker 按名称或简介搜索并分页返回伙伴拥有的项目。
	ListByWaker(ctx context.Context, wakerID string, search string, limit int, offset int) ([]models.Project, error)
	// CountByWaker 返回伙伴范围内符合搜索条件的项目数量。
	CountByWaker(ctx context.Context, wakerID string, search string) (int64, error)
	// Update 更新项目可编辑配置。
	Update(ctx context.Context, project models.Project) error
	// ListDirectories 按配置顺序返回项目目录及其项目级配置。
	ListDirectories(ctx context.Context, projectID string) ([]models.ProjectDirectory, error)
	// ReplaceDirectories 完整替换项目的有序目录关联及其配置。
	ReplaceDirectories(ctx context.Context, projectID string, directories []models.ProjectDirectory) error
	// Delete 删除项目及其目录关联配置。
	Delete(ctx context.Context, id string) error
	// DeleteByWaker 删除伙伴拥有的全部项目及目录关联配置。
	DeleteByWaker(ctx context.Context, wakerID string) error
}

// projectController 使用 GORM 实现伙伴项目持久化。
type projectController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewProjectController 创建伙伴项目 Repository Controller。
func NewProjectController(db *gorm.DB) ProjectController {
	return &projectController{db: db}
}

// Create 创建项目主体。
func (c *projectController) Create(ctx context.Context, project models.Project) error {
	err := controllerDB(ctx, c.db).Create(&project).Error
	if err == nil {
		return nil
	}
	if dbhelper.IsDuplicateError(err) {
		return status_error.ProjectNameConflict
	}
	return databaseError("create_project", "project_id="+project.ID, err)
}

// Get 按标识返回项目主体。
func (c *projectController) Get(ctx context.Context, id string) (models.Project, error) {
	var project models.Project
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&project).Error
	if err == nil {
		return project, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Project{}, status_error.ProjectNotFound.WithParams(
			map[string]interface{}{"ProjectID": id},
		)
	}
	return models.Project{}, databaseError("get_project", "project_id="+id, err)
}

// ListByWaker 按名称或简介搜索并分页返回伙伴拥有的项目。
func (c *projectController) ListByWaker(
	ctx context.Context,
	wakerID string,
	search string,
	limit int,
	offset int,
) ([]models.Project, error) {
	var projects []models.Project
	err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Scopes(projectSearch(search)).
		Order("updated_at DESC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&projects).Error
	if err != nil {
		return nil, databaseError("list_projects", "waker_id="+wakerID, err)
	}
	return projects, nil
}

// CountByWaker 返回伙伴范围内符合搜索条件的项目数量。
func (c *projectController) CountByWaker(ctx context.Context, wakerID string, search string) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).
		Model(&models.Project{}).
		Where("waker_id = ?", wakerID).
		Scopes(projectSearch(search)).
		Count(&count).Error
	if err != nil {
		return 0, databaseError("count_projects", "waker_id="+wakerID, err)
	}
	return count, nil
}

// Update 更新项目可编辑配置。
func (c *projectController) Update(ctx context.Context, project models.Project) error {
	result := controllerDB(ctx, c.db).
		Model(&models.Project{}).
		Where("id = ?", project.ID).
		Updates(map[string]any{
			"name":       project.Name,
			"brief":      project.Brief,
			"updated_at": project.UpdatedAt,
		})
	if result.Error == nil {
		return nil
	}
	if dbhelper.IsDuplicateError(result.Error) {
		return status_error.ProjectNameConflict
	}
	return databaseError("update_project", "project_id="+project.ID, result.Error)
}

// ListDirectories 按配置顺序返回项目目录及其项目级配置。
func (c *projectController) ListDirectories(
	ctx context.Context,
	projectID string,
) ([]models.ProjectDirectory, error) {
	db := controllerDB(ctx, c.db)
	var bindings []models.ProjectSourceBinding
	if err := db.Where("project_id = ?", projectID).Order("position ASC").Find(&bindings).Error; err != nil {
		return nil, databaseError("list_project_directories", projectID, err)
	}
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.ProjectSourceID)
	}
	sources, err := readRelatedRows[models.ProjectSource](db, "id", "*", ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.ProjectSource, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	directories := make([]models.ProjectDirectory, 0, len(bindings))
	for _, binding := range bindings {
		if source, ok := byID[binding.ProjectSourceID]; ok {
			directories = append(directories, models.ProjectDirectory{Source: source, LoadInstructions: binding.LoadInstructions, IsDefault: binding.IsDefault})
		}
	}
	return directories, nil
}

// ReplaceDirectories 完整替换项目的有序目录关联及其配置。
func (c *projectController) ReplaceDirectories(
	ctx context.Context,
	projectID string,
	directories []models.ProjectDirectory,
) error {
	if len(directories) == 0 {
		return status_error.BadRequest
	}
	defaultCount := 0
	for _, directory := range directories {
		if directory.Source.ID == "" {
			return status_error.BadRequest
		}
		if directory.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		return status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	if err := db.Where("project_id = ?", projectID).Delete(&models.ProjectSourceBinding{}).Error; err != nil {
		return databaseError("delete_project_source_bindings", "project_id="+projectID, err)
	}
	bindings := make([]models.ProjectSourceBinding, 0, len(directories))
	for position, directory := range directories {
		bindings = append(bindings, models.ProjectSourceBinding{
			ProjectID:        projectID,
			ProjectSourceID:  directory.Source.ID,
			Position:         position,
			LoadInstructions: directory.LoadInstructions,
			IsDefault:        directory.IsDefault,
		})
	}
	if err := db.Create(&bindings).Error; err != nil {
		return databaseError("replace_project_source_bindings", "project_id="+projectID, err)
	}
	return nil
}

// Delete 删除项目及其目录关联配置。
func (c *projectController) Delete(ctx context.Context, id string) error {
	db := controllerDB(ctx, c.db)
	if err := db.Where("project_id = ?", id).Delete(&models.ProjectSourceBinding{}).Error; err != nil {
		return databaseError("delete_project_source_bindings", "project_id="+id, err)
	}
	if err := db.Where("id = ?", id).Delete(&models.Project{}).Error; err != nil {
		return databaseError("delete_project", "project_id="+id, err)
	}
	return nil
}

// DeleteByWaker 删除伙伴拥有的全部项目及目录关联配置。
func (c *projectController) DeleteByWaker(ctx context.Context, wakerID string) error {
	db := controllerDB(ctx, c.db)
	var projectIDs []string
	if err := db.Model(&models.Project{}).Where("waker_id = ?", wakerID).Pluck("id", &projectIDs).Error; err != nil {
		return databaseError("list_waker_projects_for_delete", "waker_id="+wakerID, err)
	}
	if len(projectIDs) > 0 {
		if err := db.Where("project_id IN ?", projectIDs).Delete(&models.ProjectSourceBinding{}).Error; err != nil {
			return databaseError("delete_waker_project_source_bindings", "waker_id="+wakerID, err)
		}
	}
	if err := db.Where("waker_id = ?", wakerID).Delete(&models.Project{}).Error; err != nil {
		return databaseError("delete_waker_projects", "waker_id="+wakerID, err)
	}
	return nil
}

// projectSearch 按名称或简介进行字面子串查询，避免通配符扩大匹配。
func projectSearch(search string) func(*gorm.DB) *gorm.DB {
	search = strings.ToLower(strings.TrimSpace(search))
	return func(db *gorm.DB) *gorm.DB {
		if search == "" {
			return db
		}
		return db.Where("instr(lower(name), ?) > 0 OR instr(lower(brief), ?) > 0", search, search)
	}
}

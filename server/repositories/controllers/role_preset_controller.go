// Package controllers 定义持久化接口及其 GORM 实现。
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

// RolePresetController 定义角色预设持久化操作。
type RolePresetController interface {
	// Create 创建角色预设。
	Create(ctx context.Context, preset models.RolePreset) error
	// Get 按标识查询角色预设。
	Get(ctx context.Context, id string) (models.RolePreset, error)
	// NameExists 判断角色名称是否已被其他预设占用。
	NameExists(ctx context.Context, name string, excludeID string) (bool, error)
	// List 分页搜索角色预设。
	List(ctx context.Context, query string, limit int, offset int) ([]models.RolePreset, error)
	// Count 统计符合搜索条件的角色预设数量。
	Count(ctx context.Context, query string) (int64, error)
	// Update 更新用户角色预设的元数据和展示投影。
	Update(ctx context.Context, id string, preset models.RolePreset) error
	// Delete 删除角色预设。
	Delete(ctx context.Context, id string) error
}

// rolePresetController 使用 GORM 实现角色预设持久化。
type rolePresetController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewRolePresetController 创建角色预设 Repository Controller。
func NewRolePresetController(db *gorm.DB) RolePresetController {
	return &rolePresetController{db: db}
}

// Create 创建角色预设。
func (c *rolePresetController) Create(ctx context.Context, preset models.RolePreset) error {
	err := controllerDB(ctx, c.db).Create(&preset).Error
	if err == nil {
		return nil
	}
	if dbhelper.IsDuplicateError(err) {
		return status_error.RolePresetNameConflict
	}
	return databaseError("create_role_preset", "role_preset_id="+preset.ID, err)
}

// Get 按标识查询角色预设。
func (c *rolePresetController) Get(
	ctx context.Context,
	id string,
) (models.RolePreset, error) {
	var preset models.RolePreset
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&preset).Error
	if err == nil {
		return preset, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.RolePreset{}, status_error.RolePresetNotFound.WithParams(
			map[string]interface{}{"RolePresetID": id},
		)
	}
	return models.RolePreset{}, databaseError(
		"get_role_preset",
		"role_preset_id="+id,
		err,
	)
}

// NameExists 判断角色名称是否已被其他预设占用。
func (c *rolePresetController) NameExists(
	ctx context.Context,
	name string,
	excludeID string,
) (bool, error) {
	query := controllerDB(ctx, c.db).
		Model(&models.RolePreset{}).
		Where("role_name = ?", name)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, databaseError("check_role_preset_name", "role_name="+name, err)
	}
	return count > 0, nil
}

// List 分页搜索角色预设。
func (c *rolePresetController) List(
	ctx context.Context,
	query string,
	limit int,
	offset int,
) ([]models.RolePreset, error) {
	var presets []models.RolePreset
	db := rolePresetSearch(controllerDB(ctx, c.db), query)
	err := db.Order("source ASC, updated_at DESC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&presets).Error
	if err != nil {
		return nil, databaseError("list_role_presets", "role_preset", err)
	}
	return presets, nil
}

// Count 统计符合搜索条件的角色预设数量。
func (c *rolePresetController) Count(ctx context.Context, query string) (int64, error) {
	var count int64
	err := rolePresetSearch(
		controllerDB(ctx, c.db).Model(&models.RolePreset{}),
		query,
	).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_role_presets", "role_preset", err)
	}
	return count, nil
}

// Update 更新用户角色预设的元数据和展示投影。
func (c *rolePresetController) Update(
	ctx context.Context,
	id string,
	preset models.RolePreset,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.RolePreset{}).
		Where("id = ?", id).
		Select(
			"role_name",
			"default_avatar",
			"description",
			"expertise",
			"work_style",
			"work_methods",
			"updated_at",
		).
		Updates(&preset)
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.RolePresetNameConflict
		}
		return databaseError("update_role_preset", "role_preset_id="+id, result.Error)
	}
	return nil
}

// Delete 删除角色预设。
func (c *rolePresetController) Delete(ctx context.Context, id string) error {
	result := controllerDB(ctx, c.db).
		Where("id = ?", id).
		Delete(&models.RolePreset{})
	if result.Error != nil {
		return databaseError("delete_role_preset", "role_preset_id="+id, result.Error)
	}
	return nil
}

func rolePresetSearch(db *gorm.DB, query string) *gorm.DB {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return db
	}
	like := "%" + normalized + "%"
	return db.Where(
		"LOWER(role_name) LIKE ? OR LOWER(description) LIKE ?",
		like,
		like,
	)
}

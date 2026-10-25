// Package controllers 定义持久化接口及其 GORM 实现。
package controllers

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// WakerController 定义伙伴持久化操作。
type WakerController interface {
	// Create 创建伙伴。
	Create(ctx context.Context, waker models.Waker) error
	// Get 按标识查询伙伴。
	Get(ctx context.Context, id string) (models.Waker, error)
	// NameExists 判断名称是否已经被其他伙伴占用。
	NameExists(ctx context.Context, name string, excludeID string) (bool, error)
	// List 按搜索条件分页查询伙伴。
	List(ctx context.Context, query string, limit int, offset int) ([]models.Waker, error)
	// Count 统计符合搜索条件的伙伴数量。
	Count(ctx context.Context, query string) (int64, error)
	// Update 更新伙伴可直接编辑的资料。
	Update(ctx context.Context, id string, waker models.Waker) error
	// UpdateProfileProjection 更新从三份档案解析出的展示投影。
	UpdateProfileProjection(
		ctx context.Context,
		id string,
		projection profile.Projection,
		updatedAt int64,
	) error
	// Delete 删除伙伴。
	Delete(ctx context.Context, id string) error
}

// wakerController 使用 GORM 实现伙伴持久化。
type wakerController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewWakerController 创建伙伴 Repository Controller。
func NewWakerController(db *gorm.DB) WakerController {
	return &wakerController{db: db}
}

// Create 创建伙伴。
func (c *wakerController) Create(ctx context.Context, waker models.Waker) error {
	err := controllerDB(ctx, c.db).Create(&waker).Error
	if err == nil {
		publishCommittedChange(ctx, c.db, "entities", eventsResource("waker", waker.ID))
		return nil
	}
	if dbhelper.IsDuplicateError(err) {
		return status_error.WakerNameConflict
	}
	return databaseError("create_waker", "waker_id="+waker.ID, err)
}

// Get 按标识查询伙伴。
func (c *wakerController) Get(ctx context.Context, id string) (models.Waker, error) {
	var waker models.Waker
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&waker).Error
	if err == nil {
		return waker, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Waker{}, status_error.WakerNotFound.WithParams(
			map[string]interface{}{"WakerID": id},
		)
	}
	return models.Waker{}, databaseError("get_waker", "waker_id="+id, err)
}

// NameExists 判断名称是否已经被其他伙伴占用。
func (c *wakerController) NameExists(
	ctx context.Context,
	name string,
	excludeID string,
) (bool, error) {
	query := controllerDB(ctx, c.db).
		Model(&models.Waker{}).
		Where("name = ?", name)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, databaseError("check_waker_name", "name="+name, err)
	}
	return count > 0, nil
}

// List 按搜索条件分页查询伙伴。
func (c *wakerController) List(
	ctx context.Context,
	query string,
	limit int,
	offset int,
) ([]models.Waker, error) {
	var wakers []models.Waker
	db := filterWakers(controllerDB(ctx, c.db), query)
	err := db.Order("updated_at DESC, id ASC").Limit(limit).Offset(offset).Find(&wakers).Error
	if err != nil {
		return nil, databaseError("list_wakers", "waker", err)
	}
	return wakers, nil
}

// Count 统计符合搜索条件的伙伴数量。
func (c *wakerController) Count(
	ctx context.Context,
	query string,
) (int64, error) {
	var count int64
	err := filterWakers(
		controllerDB(ctx, c.db).Model(&models.Waker{}),
		query,
	).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_wakers", "waker", err)
	}
	return count, nil
}

// Update 更新伙伴可直接编辑的资料。
func (c *wakerController) Update(
	ctx context.Context,
	id string,
	waker models.Waker,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.Waker{}).
		Where("id = ?", id).
		Select("name", "role_name", "avatar", "bio", "updated_at").
		Updates(&waker)
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.WakerNameConflict
		}
		return databaseError("update_waker", "waker_id="+id, result.Error)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("waker", id))
	return nil
}

// UpdateProfileProjection 更新从三份档案解析出的展示投影。
func (c *wakerController) UpdateProfileProjection(
	ctx context.Context,
	id string,
	projection profile.Projection,
	updatedAt int64,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.Waker{}).
		Where("id = ?", id).
		Select("expertise", "work_style", "work_methods", "updated_at").
		Updates(&models.Waker{
			Expertise:   projection.Expertise,
			WorkStyle:   projection.WorkStyle,
			WorkMethods: projection.WorkMethods,
			UpdatedAt:   updatedAt,
		})
	if result.Error != nil {
		return databaseError("update_waker_profile", "waker_id="+id, result.Error)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("waker", id))
	return nil
}

// Delete 删除伙伴。
func (c *wakerController) Delete(ctx context.Context, id string) error {
	result := controllerDB(ctx, c.db).Where("id = ?", id).Delete(&models.Waker{})
	if result.Error != nil {
		return databaseError("delete_waker", "waker_id="+id, result.Error)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("waker", id))
	return nil
}

func filterWakers(
	db *gorm.DB,
	query string,
) *gorm.DB {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized != "" {
		like := "%" + normalized + "%"
		db = db.Where(
			"LOWER(name) LIKE ? OR LOWER(role_name) LIKE ? OR LOWER(bio) LIKE ?",
			like,
			like,
			like,
		)
	}
	return db
}

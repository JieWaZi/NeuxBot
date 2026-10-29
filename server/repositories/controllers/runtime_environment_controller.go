package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RuntimeEnvironmentController 定义设备级 Runtime 环境条目的持久化操作。
type RuntimeEnvironmentController interface {
	// Ensure 创建缺失条目，并只对账已有条目的 Registry 身份字段。
	Ensure(ctx context.Context, environment models.RuntimeEnvironment) error
	// Count 返回当前运行时环境条目总数。
	Count(ctx context.Context) (int64, error)
	// List 返回按 Runtime 标识排序的全部环境条目。
	List(ctx context.Context) ([]models.RuntimeEnvironment, error)
	// Get 按 Runtime 标识返回一条环境记录。
	Get(ctx context.Context, runtimeID string) (models.RuntimeEnvironment, error)
	// UpdateFields 更新一条环境记录的明确字段集合。
	UpdateFields(ctx context.Context, runtimeID string, fields map[string]any) error
	// UpdateConfiguration 更新用户可维护的完整启动配置和启用状态。
	UpdateConfiguration(ctx context.Context, environment models.RuntimeEnvironment) error
	// UpdateDetection 原子替换一条环境记录的完整探测快照和完成时间。
	UpdateDetection(ctx context.Context, runtimeID string, detection runtimecontract.DetectionResult, detectedAt int64) error
	// ClearDefault 清除全部条目的全局默认标记。
	ClearDefault(ctx context.Context) error
}

// runtimeEnvironmentController 使用 GORM 实现运行时环境条目持久化。
type runtimeEnvironmentController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewRuntimeEnvironmentController 创建 Runtime 环境 Repository Controller。
func NewRuntimeEnvironmentController(db *gorm.DB) RuntimeEnvironmentController {
	return &runtimeEnvironmentController{db: db}
}

// Ensure 创建缺失条目，并只对账已有条目的 Registry 身份字段。
func (c *runtimeEnvironmentController) Ensure(
	ctx context.Context,
	environment models.RuntimeEnvironment,
) error {
	err := controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "runtime_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"base_runtime_id": environment.BaseRuntimeID,
				"name":            environment.Name,
			}),
		}).
		Create(&environment).Error
	if err != nil {
		return databaseError(
			"ensure_runtime_environment",
			"runtime_id="+environment.RuntimeID,
			err,
		)
	}
	publishCommittedChange(ctx, c.db, "runtimes", eventsResource("global", ""))
	return nil
}

// Count 返回当前运行时环境条目总数。
func (c *runtimeEnvironmentController) Count(ctx context.Context) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).
		Model(&models.RuntimeEnvironment{}).
		Count(&count).Error
	if err != nil {
		return 0, databaseError("count_runtime_environments", "", err)
	}
	return count, nil
}

// List 返回按 Runtime 标识排序的全部环境条目。
func (c *runtimeEnvironmentController) List(
	ctx context.Context,
) ([]models.RuntimeEnvironment, error) {
	var environments []models.RuntimeEnvironment
	err := controllerDB(ctx, c.db).
		Order("runtime_id ASC").
		Find(&environments).Error
	if err != nil {
		return nil, databaseError("list_runtime_environments", "", err)
	}
	return environments, nil
}

// Get 按 Runtime 标识返回一条环境记录。
func (c *runtimeEnvironmentController) Get(
	ctx context.Context,
	runtimeID string,
) (models.RuntimeEnvironment, error) {
	var environment models.RuntimeEnvironment
	err := controllerDB(ctx, c.db).
		Where("runtime_id = ?", runtimeID).
		First(&environment).Error
	if err == nil {
		return environment, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.RuntimeEnvironment{}, status_error.RuntimeUnavailable
	}
	return models.RuntimeEnvironment{}, databaseError(
		"get_runtime_environment",
		"runtime_id="+runtimeID,
		err,
	)
}

// UpdateFields 更新一条环境记录的明确字段集合。
func (c *runtimeEnvironmentController) UpdateFields(
	ctx context.Context,
	runtimeID string,
	fields map[string]any,
) error {
	err := controllerDB(ctx, c.db).
		Model(&models.RuntimeEnvironment{}).
		Where("runtime_id = ?", runtimeID).
		Updates(fields).Error
	if err != nil {
		return databaseError(
			"update_runtime_environment",
			"runtime_id="+runtimeID,
			err,
		)
	}
	publishCommittedChange(ctx, c.db, "runtimes", eventsResource("runtime", runtimeID))
	return nil
}

// UpdateConfiguration 更新用户可维护的完整启动配置和启用状态。
func (c *runtimeEnvironmentController) UpdateConfiguration(
	ctx context.Context,
	environment models.RuntimeEnvironment,
) error {
	err := controllerDB(ctx, c.db).
		Model(&models.RuntimeEnvironment{}).
		Where("runtime_id = ?", environment.RuntimeID).
		Select(
			"Enabled",
			"Binary",
			"PrefixArgs",
			"ConfigHome",
			"CustomModels",
			"DefaultModel",
			"DefaultAccountID",
			"DefaultReasoning",
			"DefaultPermissionLevel",
		).
		Updates(&environment).Error
	if err != nil {
		return databaseError(
			"update_runtime_environment_configuration",
			"runtime_id="+environment.RuntimeID,
			err,
		)
	}
	publishCommittedChange(ctx, c.db, "runtimes", eventsResource("runtime", environment.RuntimeID))
	return nil
}

// UpdateDetection 原子替换一条环境记录的完整探测快照和完成时间。
func (c *runtimeEnvironmentController) UpdateDetection(
	ctx context.Context,
	runtimeID string,
	detection runtimecontract.DetectionResult,
	detectedAt int64,
) error {
	err := controllerDB(ctx, c.db).
		Model(&models.RuntimeEnvironment{}).
		Where("runtime_id = ?", runtimeID).
		Select("Detection", "DetectedAt").
		Updates(&models.RuntimeEnvironment{
			Detection:  detection,
			DetectedAt: detectedAt,
		}).Error
	if err != nil {
		return databaseError(
			"update_runtime_environment_detection",
			"runtime_id="+runtimeID,
			err,
		)
	}
	publishCommittedChange(ctx, c.db, "runtimes", eventsResource("runtime", runtimeID))
	return nil
}

// ClearDefault 清除全部条目的全局默认标记。
func (c *runtimeEnvironmentController) ClearDefault(ctx context.Context) error {
	err := controllerDB(ctx, c.db).
		Model(&models.RuntimeEnvironment{}).
		Where("is_default = ?", true).
		Update("is_default", false).Error
	if err != nil {
		return databaseError("clear_default_runtime_environment", "", err)
	}
	publishCommittedChange(ctx, c.db, "runtimes", eventsResource("global", ""))
	return nil
}

package controllers

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const readingInitializationID = "00000000000000000000000000000001"

// ReadingInitializationController 管理一次性阅读基线切换。
type ReadingInitializationController struct {
	// db 是应用原业务数据库。
	db *gorm.DB
}

// NewReadingInitializationController 使用原应用数据库装配初始化仓储。
func NewReadingInitializationController(db *gorm.DB) *ReadingInitializationController {
	return &ReadingInitializationController{db: db}
}

// Begin 读取或创建首次阅读切换的持久阶段。
func (c *ReadingInitializationController) Begin(ctx context.Context) (models.ReadingInitialization, error) {
	db := controllerDB(ctx, c.db)
	value := models.ReadingInitialization{ID: readingInitializationID, StartedAt: time.Now().UnixMilli()}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&value).Error; err != nil {
		return value, databaseError("begin_reading_initialization", value.ID, err)
	}
	if err := db.Where("id=?", value.ID).Take(&value).Error; err != nil {
		return value, databaseError("read_reading_initialization", value.ID, err)
	}
	return value, nil
}

// MarkHistoricalRuns 分批建立历史可见终态的查看事实。
func (c *ReadingInitializationController) MarkHistoricalRuns(ctx context.Context, at int64) error {
	db := controllerDB(ctx, c.db)
	for _, kind := range []constants.RunOwnerKind{constants.RunOwnerKindWorkflowRun, constants.RunOwnerKindAutomaticRun} {
		last := ""
		for {
			var candidates *gorm.DB
			if kind == constants.RunOwnerKindWorkflowRun {
				candidates = viewableWorkflowRuns(db.Model(&models.WorkflowRun{}))
			} else {
				candidates = viewableAutomaticRuns(db.Model(&models.AutomaticRun{}))
			}
			var ids []string
			if err := candidates.Where("id>?", last).Order("id").Limit(128).Pluck("id", &ids).Error; err != nil {
				return databaseError("list_historical_reading_runs", last, err)
			}
			if len(ids) == 0 {
				break
			}
			marks := make([]models.RunViewMark, len(ids))
			for i, id := range ids {
				marks[i] = models.RunViewMark{ReaderID: constants.LocalReaderID, OwnerKind: kind, OwnerID: id, ViewedAt: at}
			}
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&marks).Error; err != nil {
				return databaseError("baseline_historical_reading_runs", last, err)
			}
			last = ids[len(ids)-1]
		}
	}
	return nil
}

// MarkBaselineCompleted 在恢复写入之前持久完成历史基线。
func (c *ReadingInitializationController) MarkBaselineCompleted(ctx context.Context) error {
	err := controllerDB(ctx, c.db).Model(&models.ReadingInitialization{}).Where("id=? AND baseline_completed_at IS NULL", readingInitializationID).UpdateColumn("baseline_completed_at", time.Now().UnixMilli()).Error
	if err != nil {
		return databaseError("complete_reading_baseline", readingInitializationID, err)
	}
	return nil
}

// Finish 清理三张明确的旧跟踪表并提交完成标记。
func (c *ReadingInitializationController) Finish(ctx context.Context) error {
	db := controllerDB(ctx, c.db)
	for _, table := range []string{"attention_notice_acks", "work_result_receipts", "work_result_tracking"} {
		if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			return databaseError("retire_obsolete_reading_table", table, err)
		}
	}
	if err := db.Model(&models.ReadingInitialization{}).Where("id=? AND baseline_completed_at IS NOT NULL AND completed_at IS NULL", readingInitializationID).UpdateColumn("completed_at", time.Now().UnixMilli()).Error; err != nil {
		return databaseError("finish_reading_initialization", readingInitializationID, err)
	}
	return nil
}

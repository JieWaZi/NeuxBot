package controllers

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RunViewingController 提供独立原运行最终结果的查看事实。
type RunViewingController interface {
	Get(context.Context, string, models.RunOwnerRef) (models.RunViewingState, error)
	MarkViewed(context.Context, string, models.RunOwnerRef) error
	// MarkViewedForBoard 返回确认是否变化，由调用方合并发布看板失效。
	MarkViewedForBoard(context.Context, string, models.RunOwnerRef) (bool, error)
}

type runViewingController struct {
	// db 是仓储默认连接。
	db *gorm.DB
	// transaction 保护最终结果校验与用户确认。
	transaction TransactionController
}

// NewRunViewingController 创建原运行查看仓储。
func NewRunViewingController(db *gorm.DB) RunViewingController {
	return &runViewingController{db: db, transaction: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

const workflowRunResultAvailableSQL = `completed_at IS NOT NULL AND ((status='succeeded' AND result IS NOT NULL AND result <> 'null') OR (status='failed' AND failure IS NOT NULL AND failure <> 'null'))`
const automaticRunResultAvailableSQL = `admission IN ('failed','interrupted') AND error_code <> '' AND chat_id = '' AND thread_id = '' AND workflow_run_id = ''`

// viewableWorkflowRuns 统一独立用户可见运行结果的资格，SQL 只读取必要原字段。
func viewableWorkflowRuns(db *gorm.DB) *gorm.DB {
	return db.Where(`history_deleted_at IS NULL AND (parent_run_id = '' OR NOT joined) AND EXISTS(SELECT 1 FROM workflow_definitions WHERE id=workflow_runs.definition_id) AND ` + workflowRunResultAvailableSQL)
}

func viewableAutomaticRuns(db *gorm.DB) *gorm.DB {
	return db.Where(automaticRunResultAvailableSQL + " AND EXISTS(SELECT 1 FROM automatic_tasks WHERE id=automatic_runs.task_id)")
}

func (c *runViewingController) Get(ctx context.Context, reader string, owner models.RunOwnerRef) (models.RunViewingState, error) {
	value := models.RunViewingState{RunOwnerRef: owner}
	read := func(tx context.Context) error {
		db := controllerDB(tx, c.db)
		if owner.OwnerID == "" {
			return status_error.BadRequest
		}
		var table *gorm.DB
		switch owner.OwnerKind {
		case constants.RunOwnerKindWorkflowRun:
			table = db.Model(&models.WorkflowRun{}).Where("id = ? AND history_deleted_at IS NULL AND (parent_run_id = '' OR NOT joined) AND EXISTS(SELECT 1 FROM workflow_definitions WHERE id=workflow_runs.definition_id)", owner.OwnerID)
		case constants.RunOwnerKindAutomaticRun:
			table = db.Model(&models.AutomaticRun{}).Where("id = ? AND EXISTS(SELECT 1 FROM automatic_tasks WHERE id=automatic_runs.task_id)", owner.OwnerID)
		default:
			return status_error.BadRequest
		}
		var existing int64
		if err := table.Session(&gorm.Session{}).Count(&existing).Error; err != nil {
			return databaseError("resolve_viewable_run", owner.OwnerID, err)
		}
		if existing == 0 {
			if owner.OwnerKind == constants.RunOwnerKindAutomaticRun {
				return status_error.AutomaticRunNotFound
			}
			return status_error.WorkflowNotFound
		}
		if owner.OwnerKind == constants.RunOwnerKindAutomaticRun {
			table = viewableAutomaticRuns(table)
		} else {
			table = viewableWorkflowRuns(table)
		}
		var viewable int64
		if err := table.Count(&viewable).Error; err != nil {
			return databaseError("check_viewable_run", owner.OwnerID, err)
		}
		value.Viewable = viewable > 0
		if !value.Viewable {
			return nil
		}
		var viewed int64
		if err := db.Model(&models.RunViewMark{}).Where("reader_id = ? AND owner_kind = ? AND owner_id = ?", reader, owner.OwnerKind, owner.OwnerID).Count(&viewed).Error; err != nil {
			return databaseError("read_run_view_mark", owner.OwnerID, err)
		}
		value.Viewed = viewed > 0
		return nil
	}
	if ctx.Value(databaseTransactionKey{}) != nil {
		err := read(ctx)
		return value, err
	}
	err := c.transaction.Transaction(ctx, read)
	return value, err
}

func (c *runViewingController) MarkViewed(ctx context.Context, reader string, owner models.RunOwnerRef) error {
	_, err := c.markViewed(ctx, reader, owner, false)
	return err
}

// MarkViewedForBoard 确认看板冻结的原运行结果，并只发布查看详情失效。
func (c *runViewingController) MarkViewedForBoard(ctx context.Context, reader string, owner models.RunOwnerRef) (bool, error) {
	return c.markViewed(ctx, reader, owner, true)
}

func (c *runViewingController) markViewed(ctx context.Context, reader string, owner models.RunOwnerRef, boardBatch bool) (bool, error) {
	changed := false
	apply := func(tx context.Context) error {
		changed = false
		value, err := c.Get(tx, reader, owner)
		if err != nil {
			return err
		}
		if !value.Viewable {
			return status_error.BadRequest
		}
		if value.Viewed {
			return nil
		}
		mark := models.RunViewMark{ReaderID: reader, OwnerKind: owner.OwnerKind, OwnerID: owner.OwnerID, ViewedAt: time.Now().UnixMilli()}
		result := controllerDB(tx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&mark)
		if result.Error != nil {
			return databaseError("mark_run_viewed", owner.OwnerID, result.Error)
		}
		if result.RowsAffected > 0 {
			resource := eventsResource(string(owner.OwnerKind), owner.OwnerID)
			publishCommittedChange(tx, c.db, "reading", resource)
			if !boardBatch {
				publishCommittedChange(tx, c.db, "work-board", resource)
			}
			changed = true
		}
		return nil
	}
	if ctx.Value(databaseTransactionKey{}) != nil {
		err := apply(ctx)
		return changed, err
	}
	err := c.transaction.Transaction(ctx, apply)
	return changed && err == nil, err
}

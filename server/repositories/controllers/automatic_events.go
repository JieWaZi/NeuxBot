package controllers

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AutomaticEventController 管理事件检查游标与规则消费的同一提交边界。
type AutomaticEventController struct {
	// db 提供规则消费持久连接及事务环境。
	db *gorm.DB
	// tx 提供可重试的游标推进和事件接纳事务。
	tx TransactionController
}

// NewAutomaticEventController 创建持久事件账本。
func NewAutomaticEventController(db *gorm.DB) *AutomaticEventController {
	return &AutomaticEventController{db: db, tx: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

// Cursor 返回当前实例已经提交的读取位置。
func (c *AutomaticEventController) Cursor(ctx context.Context, taskID, instance string) (models.AutomaticEventCursor, error) {
	var value models.AutomaticEventCursor
	res := controllerDB(ctx, c.db).Where("task_id = ? AND instance = ?", taskID, instance).Limit(1).Find(&value)
	value.TaskID, value.Instance = taskID, instance
	if res.Error != nil {
		return value, databaseError("read_automatic_event_cursor", taskID, res.Error)
	}
	return value, nil
}

// Observation 返回规则曾经接纳的指定事件观察。
func (c *AutomaticEventController) Observation(ctx context.Context, taskID, id string) (models.AutomaticEventObservation, error) {
	return workTicketRow[models.AutomaticEventObservation](controllerDB(ctx, c.db).Where("task_id = ? AND id = ?", taskID, id),
		"read_automatic_event_observation", taskID)
}

// Commit 仅允许基于仍有效游标提交一批观察，消费回执和游标对外原子可见。
func (c *AutomaticEventController) Commit(ctx context.Context, value models.AutomaticEventCursor, observations []models.AutomaticEventObservation) (bool, error) {
	won := false
	err := c.tx.Transaction(ctx, func(tx context.Context) error {
		won = false
		db := controllerDB(tx, c.db)
		initial := models.AutomaticEventCursor{TaskID: value.TaskID, Instance: value.Instance}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		current := db.Model(&models.AutomaticEventCursor{}).Where("task_id = ? AND instance = ? AND revision = ?", value.TaskID, value.Instance, value.Revision)
		claimed := current.Update("revision", value.Revision+1)
		if claimed.Error != nil || claimed.RowsAffected == 0 {
			return claimed.Error
		}
		matched, filtered := 0, 0
		for _, row := range observations {
			row.TaskID, row.Instance, row.ObservedAt = value.TaskID, value.Instance, value.CheckedAt
			row.ID = eventObservationID(row.TaskID, row.Instance, row.Event.ID)
			res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				switch row.Disposition {
				case "pending":
					matched++
				case "filtered":
					filtered++
				}
			}
		}
		outcome := value.Outcome
		if outcome == "observed" {
			switch {
			case matched > 0:
				outcome = "matched"
			case filtered > 0:
				outcome = "filtered"
			default:
				outcome = "empty"
			}
		}
		if err := db.Model(&models.AutomaticEventCursor{}).Where("task_id = ? AND instance = ?", value.TaskID, value.Instance).Updates(map[string]any{
			"initialized": value.Initialized, "checked_at": value.CheckedAt, "outcome": outcome, "error_code": value.ErrorCode,
			"matched": matched, "filtered": filtered,
		}).Error; err != nil {
			return err
		}
		won = true
		publishCommittedChange(tx, c.db, "automatic-tasks", eventsResource("global", ""))
		return nil
	})
	return won, err
}

func eventObservationID(taskID, instance, eventID string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(taskID+"\x00"+instance+"\x00"+eventID)))
}

// Known identifies already committed observations in the supplied adapter window.
func (c *AutomaticEventController) Known(ctx context.Context, taskID, instance string, eventIDs []string) (map[string]bool, error) {
	known := make(map[string]bool, len(eventIDs))
	for start := 0; start < len(eventIDs); start += 200 {
		reverse := map[string]string{}
		rowIDs := make([]string, 0, 200)
		for _, eventID := range eventIDs[start:min(start+200, len(eventIDs))] {
			rowID := eventObservationID(taskID, instance, eventID)
			rowIDs = append(rowIDs, rowID)
			reverse[rowID] = eventID
		}
		var observed []string
		err := controllerDB(ctx, c.db).Model(&models.AutomaticEventObservation{}).Where("id IN ?", rowIDs).Pluck("id", &observed).Error
		if err != nil {
			return nil, databaseError("read_known_automatic_events", taskID, err)
		}
		for _, id := range observed {
			known[reverse[id]] = true
		}
	}
	return known, nil
}

// List 读取某规则待接纳的最早事件，或其最近观察。
func (c *AutomaticEventController) List(ctx context.Context, taskID, instance, disposition string, limit int) ([]models.AutomaticEventObservation, error) {
	var values []models.AutomaticEventObservation
	query := controllerDB(ctx, c.db).Where("task_id = ? AND instance = ?", taskID, instance)
	if disposition != "" {
		query = query.Where("disposition = ?", disposition).Order("observed_at ASC, rowid ASC")
	} else {
		query = query.Order("observed_at DESC, rowid DESC")
	}
	if err := query.Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_automatic_events", taskID, err)
	}
	return values, nil
}

// Settle 保留原事件的终态收据。重试无法将已结算事件恢复为待处理。
func (c *AutomaticEventController) Settle(ctx context.Context, id, disposition, runID, code string) error {
	return c.tx.Transaction(ctx, func(tx context.Context) error {
		res := controllerDB(tx, c.db).Model(&models.AutomaticEventObservation{}).Where("id = ? AND disposition = ?", id, "pending").
			Updates(map[string]any{"disposition": disposition, "run_id": runID, "error_code": code})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			publishCommittedChange(tx, c.db, "automatic-tasks", eventsResource("global", ""))
		}
		return nil
	})
}

func deleteAutomaticEventData(db *gorm.DB, taskIDs []string) error {
	if err := db.Where("task_id IN ?", taskIDs).Delete(&models.AutomaticEventObservation{}).Error; err != nil {
		return err
	}
	return db.Where("task_id IN ?", taskIDs).Delete(&models.AutomaticEventCursor{}).Error
}

// RecordPendingError retains a follow-up rejection while the observation waits for its rule slot.
func (c *AutomaticEventController) RecordPendingError(ctx context.Context, id, code string) error {
	res := controllerDB(ctx, c.db).Model(&models.AutomaticEventObservation{}).Where("id = ? AND disposition = ?", id, "pending").
		UpdateColumn("error_code", code)
	if res.Error != nil {
		return databaseError("record_pending_automatic_event_error", "id="+id, res.Error)
	}
	return nil
}

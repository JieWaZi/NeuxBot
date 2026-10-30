package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm/clause"
)

const chatExecutionLocationSchemaVersion = 2

// ReadExecutionLocation 通过原执行或输入身份定位日志入口，均为空时读取首个真实执行。
func (c *chatController) ReadExecutionLocation(ctx context.Context, chatID, executionID, submissionID string) (*models.ChatExecutionLocation, error) {
	query := controllerDB(ctx, c.db).Where("chat_id = ?", chatID)
	switch {
	case executionID != "":
		query = query.Where("execution_id = ?", executionID)
	case submissionID != "":
		query = query.Where("submission_id = ?", submissionID)
	default:
		query = query.Where("message_only = ?", false).Order("sequence ASC")
	}
	rows := []models.ChatExecutionLocation{}
	if err := query.Limit(1).Find(&rows).Error; err != nil {
		return nil, databaseError("read_chat_execution_location", "chat_id="+chatID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ReadExecutionLocationCursor 读取纯定位索引的覆盖游标，尚未建立时为零。
func (c *chatController) ReadExecutionLocationCursor(ctx context.Context, chatID string) (int64, error) {
	var row models.ChatExecutionLocationCursor
	if err := controllerDB(ctx, c.db).Where("chat_id = ?", chatID).Find(&row).Error; err != nil {
		return 0, databaseError("read_chat_execution_location_cursor", "chat_id="+chatID, err)
	}
	if row.SchemaVersion != chatExecutionLocationSchemaVersion {
		return 0, nil
	}
	return row.Sequence, nil
}

// AdvanceExecutionLocations 在调用方事务中提交尾部定位与覆盖游标；冲突由调用方重新取尾部。
func (c *chatController) AdvanceExecutionLocations(ctx context.Context, chatID string, after, latest int64, locations []models.ChatExecutionLocation) error {
	db := controllerDB(ctx, c.db)
	if after == 0 {
		var current models.ChatExecutionLocationCursor
		if err := db.Where("chat_id = ?", chatID).Find(&current).Error; err != nil {
			return databaseError("check_chat_execution_location_cursor", "chat_id="+chatID, err)
		}
		if current.ChatID != "" && current.Sequence != 0 && current.SchemaVersion == chatExecutionLocationSchemaVersion {
			return status_error.Conflict
		}
		if err := db.Where("chat_id = ?", chatID).Delete(&models.ChatExecutionLocation{}).Error; err != nil {
			return databaseError("reset_chat_execution_locations", "chat_id="+chatID, err)
		}
		if current.ChatID != "" && current.SchemaVersion != chatExecutionLocationSchemaVersion {
			if err := db.Where("chat_id = ?", chatID).Delete(&models.ChatExecutionLocationCursor{}).Error; err != nil {
				return databaseError("reset_chat_execution_location_format", "chat_id="+chatID, err)
			}
			current.ChatID = ""
		}
		if current.ChatID == "" {
			if err := db.Create(&models.ChatExecutionLocationCursor{ChatID: chatID, SchemaVersion: chatExecutionLocationSchemaVersion}).Error; err != nil {
				return databaseError("create_chat_execution_location_cursor", "chat_id="+chatID, err)
			}
		}
	}
	if after > latest {
		result := db.Where("chat_id = ? AND sequence = ?", chatID, after).Delete(&models.ChatExecutionLocationCursor{})
		if result.Error != nil {
			return databaseError("reset_chat_execution_location_cursor", "chat_id="+chatID, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.Conflict
		}
		if err := db.Where("chat_id = ?", chatID).Delete(&models.ChatExecutionLocation{}).Error; err != nil {
			return databaseError("reset_chat_execution_locations", "chat_id="+chatID, err)
		}
		if err := db.Create(&models.ChatExecutionLocationCursor{ChatID: chatID, SchemaVersion: chatExecutionLocationSchemaVersion}).Error; err != nil {
			return databaseError("create_chat_execution_location_cursor", "chat_id="+chatID, err)
		}
		after = 0
	}

	result := db.Model(&models.ChatExecutionLocationCursor{}).Where("chat_id = ? AND sequence = ?", chatID, after).Update("sequence", latest)
	if result.Error != nil {
		return databaseError("advance_chat_execution_location_cursor", "chat_id="+chatID, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.Conflict
	}
	if len(locations) > 0 {
		if after > 0 {
			for first := 0; first < len(locations); first += 100 {
				last := min(first+100, len(locations))
				ids := make([]string, 0, last-first)
				for _, location := range locations[first:last] {
					ids = append(ids, location.ExecutionID)
				}
				var existing []models.ChatExecutionLocation
				if err := db.Where("chat_id = ? AND execution_id IN ?", chatID, ids).Find(&existing).Error; err != nil {
					return databaseError("read_execution_location_tail_base", "chat_id="+chatID, err)
				}
				byID := make(map[string]*models.ChatExecutionLocation, len(existing))
				for i := range existing {
					byID[existing[i].ExecutionID] = &existing[i]
				}
				for i := first; i < last; i++ {
					locations[i] = models.MergeExecutionLocationTail(byID[locations[i].ExecutionID], locations[i])
				}
			}
		}
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "chat_id"}, {Name: "execution_id"}}, DoUpdates: clause.AssignmentColumns([]string{"submission_id", "sequence", "message_only", "lifecycle_sequences", "invocation_boundary_sequence", "invocation_finished_sequence", "finished_invocation_boundary_sequence"})}).CreateInBatches(locations, 100).Error; err != nil {
			return databaseError("index_chat_execution_locations", "chat_id="+chatID, err)
		}
	}
	return nil
}

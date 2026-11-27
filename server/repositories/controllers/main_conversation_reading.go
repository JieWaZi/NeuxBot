package controllers

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// InitializeReadingBaseline 仅为升级前尚无阅读记录的会话确认历史公开前缀。
func (c *MainConversationController) InitializeReadingBaseline(ctx context.Context, reader string) error {
	err := controllerDB(ctx, c.db).Exec(`
INSERT INTO main_conversation_read_cursors
  (reader_id, conversation_id, through_sequence, unread_generation, hold_unread, updated_at)
SELECT ?, id, COALESCE((
  SELECT MAX(sequence) FROM main_conversation_messages INDEXED BY idx_main_conversation_readable
  WHERE conversation_id = main_conversations.id AND role = 'assistant' AND removed_at = 0
), 0), 0, false, ?
FROM main_conversations
WHERE NOT EXISTS (
  SELECT 1 FROM main_conversation_read_cursors
  WHERE reader_id = ? AND conversation_id = main_conversations.id
)`, reader, time.Now().UnixMilli(), reader).Error
	if err != nil {
		return databaseError("initialize_main_conversation_reading", "", err)
	}
	return nil
}

func (c *MainConversationController) createReadingCursor(ctx context.Context, reader, conversationID string) error {
	row := models.MainConversationReadCursor{
		ReaderID: reader, ConversationID: conversationID, UpdatedAt: time.Now().UnixMilli(),
	}
	if err := controllerDB(ctx, c.db).Create(&row).Error; err != nil {
		return databaseError("create_main_read_cursor", conversationID, err)
	}
	return nil
}

// ReadingCursor 按阅读者和公开会话加载持久位置，用于冻结用户实际观察到的阅读代次。
func (c *MainConversationController) ReadingCursor(ctx context.Context, reader, conversationID string) (models.MainConversationReadCursor, error) {
	var cursor models.MainConversationReadCursor
	result := controllerDB(ctx, c.db).Where("reader_id = ? AND conversation_id = ?", reader, conversationID).Limit(1).Find(&cursor)
	if result.Error != nil {
		return cursor, databaseError("read_main_cursor", conversationID, result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := c.Get(ctx, conversationID); err != nil {
			return cursor, err
		}
		return cursor, status_error.ReadingProjectionIncomplete
	}
	return cursor, nil
}

func mainReadableMessages(db *gorm.DB, conversationID string) *gorm.DB {
	// 冷库的查询计划可能偏向原顺序索引；阅读范围只需要已公开的发言。
	return db.Table("main_conversation_messages INDEXED BY idx_main_conversation_readable").Where("conversation_id = ? AND role = 'assistant' AND removed_at = 0", conversationID)
}

// Reading 读取同一快照中的持久游标、合格消息数和真实消息边界。
func (c *MainConversationController) Reading(ctx context.Context, reader, conversationID string) (value models.MainConversationReadingState, err error) {
	err = c.transaction.Transaction(ctx, func(tx context.Context) error {
		cursor, err := c.ReadingCursor(tx, reader, conversationID)
		if err != nil {
			return err
		}
		value = models.MainConversationReadingState{MainConversationReadCursor: cursor}
		db := controllerDB(tx, c.db)
		var latest models.MainConversationMessage
		if err := mainReadableMessages(db, conversationID).Select("id", "sequence").Order("sequence DESC").Limit(1).Find(&latest).Error; err != nil {
			return databaseError("read_main_latest_message", conversationID, err)
		}
		value.LatestReadableMessageID = latest.ID
		if latest.Sequence <= cursor.ThroughSequence {
			return nil
		}
		if err := mainReadableMessages(db, conversationID).Where("sequence > ?", cursor.ThroughSequence).Count(&value.UnreadMessageCount).Error; err != nil {
			return databaseError("count_main_unread_messages", conversationID, err)
		}
		var first models.MainConversationMessage
		if err := mainReadableMessages(db, conversationID).Select("id").Where("sequence > ?", cursor.ThroughSequence).Order("sequence ASC").Limit(1).Find(&first).Error; err != nil {
			return databaseError("read_main_first_unread_message", conversationID, err)
		}
		value.FirstUnreadMessageID = first.ID
		return nil
	})
	return value, err
}

// MarkRead 确认实际呈现的主会话合格消息前缀。
func (c *MainConversationController) MarkRead(ctx context.Context, reader, conversationID, messageID string, generation int64, intent constants.ReadingIntent) error {
	if intent != constants.ReadingIntentAutomatic && intent != constants.ReadingIntentExplicit {
		return status_error.BadRequest
	}
	_, err := c.moveReadingCursor(ctx, reader, conversationID, messageID, generation, intent, false)
	return err
}

// MarkReadForBoard 确认看板冻结的边界；批次所有者统一发布看板失效。
func (c *MainConversationController) MarkReadForBoard(ctx context.Context, reader, conversationID, messageID string, generation int64) (bool, error) {
	return c.moveReadingCursor(ctx, reader, conversationID, messageID, generation, constants.ReadingIntentExplicit, true)
}

// MarkUnread 设置主会话持久手动未读边界，阻断其它窗口的旧阅读命令。
func (c *MainConversationController) MarkUnread(ctx context.Context, reader, conversationID, messageID string, generation int64) error {
	_, err := c.moveReadingCursor(ctx, reader, conversationID, messageID, generation, "", false)
	return err
}

func (c *MainConversationController) moveReadingCursor(ctx context.Context, reader, conversationID, messageID string, generation int64, intent constants.ReadingIntent, boardBatch bool) (bool, error) {
	if conversationID == "" || messageID == "" || generation < 0 {
		return false, status_error.BadRequest
	}
	var changed bool
	apply := func(tx context.Context) error {
		changed = false
		db := controllerDB(tx, c.db)
		var boundary models.MainConversationMessage
		result := db.Select("sequence").Where("id = ? AND conversation_id = ? AND role = 'assistant' AND removed_at = 0", messageID, conversationID).Limit(1).Find(&boundary)
		if result.Error != nil {
			return databaseError("resolve_main_read_boundary", conversationID, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.BadRequest
		}
		cursor, err := c.ReadingCursor(tx, reader, conversationID)
		if err != nil {
			return err
		}
		previous := readingPosition{cursor.ThroughSequence, cursor.UnreadGeneration, cursor.HoldUnread}
		next, err := previous.move(boundary.Sequence, generation, intent)
		if err != nil || next == previous {
			return err
		}
		condition := db.Model(&models.MainConversationReadCursor{}).Where("reader_id = ? AND conversation_id = ? AND unread_generation = ?", reader, conversationID, generation)
		if intent == constants.ReadingIntentAutomatic {
			condition = condition.Where("hold_unread = ?", false)
		}
		result = condition.Updates(map[string]any{"through_sequence": next.through, "hold_unread": next.hold, "unread_generation": next.generation, "updated_at": time.Now().UnixMilli()})
		if result.Error != nil {
			return databaseError("update_main_read_cursor", conversationID, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.ReadingIntentConflict
		}
		resource := eventsResource(string(constants.WorkOwnerKindMainConversation), conversationID)
		publishCommittedChange(tx, c.db, "reading", resource)
		if !boardBatch {
			owner, err := c.Get(tx, conversationID)
			if err != nil {
				return err
			}
			c.publishReadingAttention(tx, conversationID, owner.SubjectType, owner.SubjectID)
		}
		changed = true
		return nil
	}
	if ctx.Value(databaseTransactionKey{}) != nil {
		err := apply(ctx)
		return changed, err
	}
	err := c.transaction.Transaction(ctx, apply)
	return changed && err == nil, err
}

func (c *MainConversationController) publishReadingAttention(ctx context.Context, conversationID, subjectKind, subjectID string) {
	if subjectKind == "team" {
		subjectKind = "channel"
	}
	resource := eventsResource(string(constants.WorkOwnerKindMainConversation), conversationID)
	publishCommittedChange(ctx, c.db, "work-board", resource, eventsResource(subjectKind, subjectID))
}

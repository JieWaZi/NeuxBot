package controllers

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatReadingController 持久化派生消息索引及用户阅读位置。
type ChatReadingController interface {
	Get(context.Context, string, string) (models.ChatReadingState, error)
	// GetCursor 只读取持久阅读位置，独立于可重建的消息索引。
	GetCursor(context.Context, string, string) (models.ChatReadCursor, error)
	MarkRead(context.Context, string, string, string, int64, constants.ReadingIntent) error
	// MarkReadForBoard 返回游标是否变化，由调用方合并发布看板失效。
	MarkReadForBoard(context.Context, string, string, string, int64) (bool, error)
	MarkUnread(context.Context, string, string, string, int64) error
	EnsureCursor(context.Context, string, string, int64) error
	Project(context.Context, string, []string, []models.ChatReadableMessage, bool) (int64, error)
	ProjectWithChange(context.Context, string, []string, []models.ChatReadableMessage, bool) (int64, bool, error)
	DeleteReading(context.Context, string) error
	Readings(context.Context, string, []models.Chat) (map[string]models.ChatReadingState, error)
	ProjectionChanged(context.Context, string)
	HasMessage(context.Context, string, string, string, bool) (bool, error)
	HasCursor(context.Context, string, string) (bool, error)
	ProjectionRecovered(context.Context, string)
	ProjectionUnavailable(context.Context, string)
}

type chatReadingController struct {
	// db 是仓储默认连接。
	db *gorm.DB
	// transaction 保护边界读取和游标写入的原子性。
	transaction TransactionController
}

// NewChatReadingController 创建阅读事实仓储。
func NewChatReadingController(db *gorm.DB) ChatReadingController {
	return &chatReadingController{db: db, transaction: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

func (c *chatReadingController) EnsureCursor(ctx context.Context, reader, chat string, through int64) error {
	row := models.ChatReadCursor{ReaderID: reader, ChatID: chat, ThroughSequence: through, UpdatedAt: time.Now().UnixMilli()}
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return databaseError("initialize_chat_read_cursor", chat, err)
	}
	return nil
}

// Project 应在原 Chat 投影事务中调用；调用者将返回的最大序号写入同一行原投影。
func (c *chatReadingController) Project(ctx context.Context, chat string, remove []string, values []models.ChatReadableMessage, replace bool) (int64, error) {
	latest, _, err := c.ProjectWithChange(ctx, chat, remove, values, replace)
	return latest, err
}

func (c *chatReadingController) ProjectWithChange(ctx context.Context, chat string, remove []string, values []models.ChatReadableMessage, replace bool) (int64, bool, error) {
	db := controllerDB(ctx, c.db)
	changed := false
	if replace {
		result := db.Where("chat_id = ?", chat).Delete(&models.ChatReadableMessage{})
		if result.Error != nil {
			return 0, false, databaseError("reset_chat_readable_messages", chat, result.Error)
		}
		changed = result.RowsAffected > 0
	} else {
		for start := 0; start < len(remove); start += 128 {
			result := db.Where("chat_id=? AND message_id IN ?", chat, remove[start:min(start+128, len(remove))]).Delete(&models.ChatReadableMessage{})
			if result.Error != nil {
				return 0, false, databaseError("remove_chat_readable_messages", chat, result.Error)
			}
			changed = changed || result.RowsAffected > 0
		}
	}
	if len(values) > 0 {
		result := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "chat_id"}, {Name: "message_id"}}, DoUpdates: clause.AssignmentColumns([]string{"order_sequence", "execution_id", "kind", "created_at"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "excluded.order_sequence <> chat_readable_messages.order_sequence OR excluded.kind <> chat_readable_messages.kind OR excluded.execution_id <> chat_readable_messages.execution_id"}}}}).CreateInBatches(values, 128)
		if result.Error != nil {
			return 0, false, databaseError("project_chat_readable_messages", chat, result.Error)
		}
		changed = changed || result.RowsAffected > 0
	}
	var latest int64
	if err := db.Model(&models.ChatReadableMessage{}).Where("chat_id=?", chat).Select("COALESCE(MAX(order_sequence), 0)").Scan(&latest).Error; err != nil {
		return 0, false, databaseError("read_latest_readable_sequence", chat, err)
	}
	return latest, changed, nil
}

func (c *chatReadingController) DeleteReading(ctx context.Context, chat string) error {
	db := controllerDB(ctx, c.db)
	if err := db.Where("chat_id = ?", chat).Delete(&models.ChatReadableMessage{}).Error; err != nil {
		return databaseError("delete_chat_readable_messages", chat, err)
	}
	if err := db.Where("chat_id = ?", chat).Delete(&models.ChatReadCursor{}).Error; err != nil {
		return databaseError("delete_chat_read_cursor", chat, err)
	}
	return nil
}

// GetCursor 只读取持久阅读位置，独立于可重建的消息索引。
func (c *chatReadingController) GetCursor(ctx context.Context, reader, chat string) (models.ChatReadCursor, error) {
	var cursor models.ChatReadCursor
	result := controllerDB(ctx, c.db).Where("reader_id = ? AND chat_id = ?", reader, chat).Limit(1).Find(&cursor)
	if result.Error != nil {
		return models.ChatReadCursor{}, databaseError("read_chat_cursor", chat, result.Error)
	}
	if result.RowsAffected == 0 {
		return models.ChatReadCursor{}, status_error.ReadingProjectionIncomplete
	}
	return cursor, nil
}

func (c *chatReadingController) Get(ctx context.Context, reader, chat string) (models.ChatReadingState, error) {
	var value models.ChatReadingState
	err := c.transaction.Transaction(ctx, func(tx context.Context) error {
		cursor, err := c.GetCursor(tx, reader, chat)
		if err != nil {
			return err
		}
		value = models.ChatReadingState{ChatReadCursor: cursor}
		var latest models.ChatReadableMessage
		result := controllerDB(tx, c.db).Select("message_id").Where("chat_id = ?", chat).Order("order_sequence DESC").Limit(1).Find(&latest)
		if result.Error != nil {
			return databaseError("read_latest_readable_message", chat, result.Error)
		}
		value.LatestReadableMessageID = latest.MessageID
		if latest.MessageID == "" {
			return nil
		}
		return c.readUnread(tx, []models.ChatReadingState{value}, func(unread unreadReading) {
			value.UnreadMessageCount = unread.Count
			value.FirstUnreadMessageID = unread.First
		})
	})
	return value, err
}

// unreadReading 保存当前索引范围的计数和两端原消息身份。
type unreadReading struct {
	// ChatID 是本次范围所属会话。
	ChatID string
	// Count 是阅读边界之后的精确合格消息数。
	Count int64
	// First 是阅读边界之后的最早消息身份。
	First string
	// Latest 是阅读边界之后的最后消息身份。
	Latest string
}

func (c *chatReadingController) readUnread(ctx context.Context, candidates []models.ChatReadingState, collect func(unreadReading)) error {
	const rangeSQL = `SELECT ? AS chat_id,
 (SELECT COUNT(*) FROM chat_readable_messages WHERE chat_id=? AND order_sequence>?) AS count,
 COALESCE((SELECT message_id FROM chat_readable_messages WHERE chat_id=? AND order_sequence>? ORDER BY order_sequence LIMIT 1),'') AS first,
 COALESCE((SELECT message_id FROM chat_readable_messages WHERE chat_id=? AND order_sequence>? ORDER BY order_sequence DESC LIMIT 1),'') AS latest`
	for start := 0; start < len(candidates); start += 128 {
		batch := candidates[start:min(start+128, len(candidates))]
		sql := make([]string, len(batch))
		args := make([]any, 0, len(batch)*7)
		for i, item := range batch {
			sql[i] = rangeSQL
			args = append(args, item.ChatID, item.ChatID, item.ThroughSequence, item.ChatID, item.ThroughSequence, item.ChatID, item.ThroughSequence)
		}
		var rows []unreadReading
		if err := controllerDB(ctx, c.db).Raw(strings.Join(sql, " UNION ALL "), args...).Scan(&rows).Error; err != nil {
			return databaseError("count_unread_messages", "", err)
		}
		for _, row := range rows {
			collect(row)
		}
	}
	return nil
}

// Readings 批量读取可见对话的游标，只为确有候选的 Chat 计算未读范围。
func (c *chatReadingController) Readings(ctx context.Context, reader string, chats []models.Chat) (map[string]models.ChatReadingState, error) {
	result := make(map[string]models.ChatReadingState, len(chats))
	candidates := []models.ChatReadingState{}
	for start := 0; start < len(chats); start += 128 {
		batch := chats[start:min(start+128, len(chats))]
		ids := make([]string, len(batch))
		latest := make(map[string]int64, len(batch))
		for i, v := range batch {
			ids[i] = v.ID
			latest[v.ID] = v.LatestReadableSequence
		}
		var rows []models.ChatReadCursor
		if err := controllerDB(ctx, c.db).Where("reader_id=? AND chat_id IN ?", reader, ids).Find(&rows).Error; err != nil {
			return nil, databaseError("read_chat_cursors", "", err)
		}
		for _, row := range rows {
			item := models.ChatReadingState{ChatReadCursor: row}
			result[row.ChatID] = item
			if latest[row.ChatID] > row.ThroughSequence {
				candidates = append(candidates, item)
			}
		}
	}
	err := c.readUnread(ctx, candidates, func(row unreadReading) {
		item := result[row.ChatID]
		item.UnreadMessageCount = row.Count
		item.FirstUnreadMessageID = row.First
		item.LatestReadableMessageID = row.Latest
		result[row.ChatID] = item
	})
	return result, err
}

func (c *chatReadingController) MarkRead(ctx context.Context, reader, chat, message string, generation int64, intent constants.ReadingIntent) error {
	if intent != constants.ReadingIntentAutomatic && intent != constants.ReadingIntentExplicit {
		return status_error.BadRequest
	}
	_, err := c.moveCursor(ctx, reader, chat, message, generation, intent, false)
	return err
}

// MarkReadForBoard 确认看板冻结的会话阅读边界，并只发布阅读详情失效。
func (c *chatReadingController) MarkReadForBoard(ctx context.Context, reader, chat, message string, generation int64) (bool, error) {
	return c.moveCursor(ctx, reader, chat, message, generation, constants.ReadingIntentExplicit, true)
}

func (c *chatReadingController) MarkUnread(ctx context.Context, reader, chat, message string, generation int64) error {
	_, err := c.moveCursor(ctx, reader, chat, message, generation, "", false)
	return err
}

func (c *chatReadingController) moveCursor(ctx context.Context, reader, chat, message string, generation int64, intent constants.ReadingIntent, boardBatch bool) (bool, error) {
	if chat == "" || message == "" || generation < 0 {
		return false, status_error.BadRequest
	}
	changed := false
	apply := func(tx context.Context) error {
		changed = false
		db := controllerDB(tx, c.db)
		var boundary models.ChatReadableMessage
		result := db.Select("order_sequence").Where("chat_id = ? AND message_id = ?", chat, message).Limit(1).Find(&boundary)
		if result.Error != nil {
			return databaseError("resolve_reading_boundary", chat, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.BadRequest
		}
		var row models.ChatReadCursor
		result = db.Where("reader_id = ? AND chat_id = ?", reader, chat).Limit(1).Find(&row)
		if result.Error != nil {
			return databaseError("read_chat_cursor_for_update", chat, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.ReadingProjectionIncomplete
		}
		if row.UnreadGeneration != generation || (intent == constants.ReadingIntentAutomatic && row.HoldUnread) {
			return status_error.ReadingIntentConflict
		}
		next := max(row.ThroughSequence, boundary.OrderSequence)
		hold := false
		nextGeneration := generation
		if intent == "" {
			next = boundary.OrderSequence - 1
			hold = true
			nextGeneration++
		}
		if next == row.ThroughSequence && hold == row.HoldUnread && nextGeneration == row.UnreadGeneration {
			return nil
		}
		condition := db.Model(&models.ChatReadCursor{}).Where("reader_id = ? AND chat_id = ? AND unread_generation = ?", reader, chat, generation)
		if intent == constants.ReadingIntentAutomatic {
			condition = condition.Where("hold_unread = ?", false)
		}
		result = condition.Updates(map[string]any{"through_sequence": next, "hold_unread": hold, "unread_generation": nextGeneration, "updated_at": time.Now().UnixMilli()})
		if result.Error != nil {
			return databaseError("update_chat_read_cursor", chat, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.ReadingIntentConflict
		}
		resource := eventsResource("chat", chat)
		publishCommittedChange(tx, c.db, "reading", resource)
		if !boardBatch {
			publishCommittedChange(tx, c.db, "work-board", resource)
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

// ProjectionChanged 使阅读详情与工作聚合查询失效。
func (c *chatReadingController) ProjectionChanged(ctx context.Context, chat string) {
	resource := eventsResource("chat", chat)
	if chat == "" {
		resource = eventsResource("global", "")
	}
	publishCommittedChange(ctx, c.db, "reading", resource)
	publishCommittedChange(ctx, c.db, "work-board", resource)
}

// HasMessage 只校验公开消息及原执行身份。
func (c *chatReadingController) HasMessage(ctx context.Context, chatID, messageID, executionID string, failed bool) (bool, error) {
	query := controllerDB(ctx, c.db).Model(&models.ChatReadableMessage{}).Where("chat_id=? AND message_id=? AND execution_id=?", chatID, messageID, executionID)
	if failed {
		query = query.Where("kind='failure'")
	} else {
		query = query.Where("kind<>'failure'")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, databaseError("validate_readable_message", chatID, err)
	}
	return count > 0, nil
}

// HasCursor 校验原会话是否已有阅读基线。
func (c *chatReadingController) HasCursor(ctx context.Context, reader, chat string) (bool, error) {
	var count int64
	if err := controllerDB(ctx, c.db).Model(&models.ChatReadCursor{}).Where("reader_id=? AND chat_id=?", reader, chat).Count(&count).Error; err != nil {
		return false, databaseError("check_reading_cursor", chat, err)
	}
	return count > 0, nil
}

// ProjectionRecovered 刷新查询并触发桌面核对已显示的原待办。
func (c *chatReadingController) ProjectionRecovered(ctx context.Context, chat string) {
	resource := eventsResource("chat", chat)
	if chat == "" {
		resource = eventsResource("global", "")
	}
	publishCommittedChange(ctx, c.db, "reading", resource)
	publishCommittedWorkBoardChange(ctx, c.db, resource, "reading-recovered")
}

// ProjectionUnavailable 在数据库故障时发布固定范围失效。
func (c *chatReadingController) ProjectionUnavailable(ctx context.Context, chat string) {
	resource := eventsResource("chat", chat)
	if chat == "" {
		resource = eventsResource("global", "")
	}
	configured, _ := c.db.Get(eventPublisherSetting)
	publisher, ok := configured.(events.Publisher)
	if !ok {
		return
	}
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, "reading", "reading.changed", resource, resource)
		_ = events.Emit(publisher, "work-board", "work-board.changed", resource, resource)
		_ = events.Emit(publisher, "entities", constants.EntityActivityChangedEventType, eventsResource("global", ""), resource)
	})
}

package controllers

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Message 按会话与消息双重定位，跨会话的引用不可见。
func (c *MainConversationController) Message(ctx context.Context, convID, msgID string) (models.MainConversationMessage, error) {
	row, found, err := c.FindMessage(ctx, convID, msgID)
	if err == nil && !found {
		err = status_error.ChatMessageNotFound
	}
	return row, err
}

// FindMessage 使用原公开消息身份查找已有正文，区分尚未发布和查询失败。
func (c *MainConversationController) FindMessage(ctx context.Context, convID, msgID string) (models.MainConversationMessage, bool, error) {
	var row models.MainConversationMessage
	if err := controllerDB(ctx, c.db).Where("id = ? AND conversation_id = ?", msgID, convID).Limit(1).Find(&row).Error; err != nil {
		return row, false, databaseError("find_main_message", msgID, err)
	}
	return row, row.ID != "", nil
}

// Append 在事务内保存发言或返回原发言；同键不得改变来源、引用或正文。
func (c *MainConversationController) Append(ctx context.Context, msg models.MainConversationMessage, kind, id string) (models.MainConversationMessage, bool, error) {
	if msg.ID == "" {
		return models.MainConversationMessage{}, false, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	// 当前事务读取会话最后位置。并发写者由业务事务的 SQLite busy 重试重新取位。
	var last models.MainConversationMessage
	if err := db.Select("sequence").Where("conversation_id = ?", msg.ConversationID).Order("sequence DESC").Limit(1).Find(&last).Error; err != nil {
		return models.MainConversationMessage{}, false, databaseError("main_message_position", msg.ConversationID, err)
	}
	msg.Sequence = last.Sequence + 1
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&msg)
	if res.Error != nil {
		return models.MainConversationMessage{}, false, databaseError("append_main_message", msg.ConversationID, res.Error)
	}
	if res.RowsAffected != 0 {
		if !msg.AdmissionPending {
			publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource(kind, id))
		}
		if msg.IsReadablePublication() {
			ref := models.WorkRef{Kind: constants.WorkOwnerKindMainConversation, ID: msg.ConversationID}
			publishCommittedChange(ctx, c.db, "reading", eventsResource(string(ref.Kind), ref.ID))
			c.publishReadingAttention(ctx, ref.ID, kind, id)
			publishWorkLive(ctx, c.db, models.WorkBoardLiveEvent{
				Operation: constants.WorkLiveOperationPublished,
				Work:      ref,
				MessageID: msg.ID,
			})
		}
		return msg, false, nil
	}
	prev, found, err := c.FindMessage(ctx, msg.ConversationID, msg.ID)
	if err != nil {
		return models.MainConversationMessage{}, false, err
	}
	if !found || prev.Content != msg.Content || prev.Role != msg.Role ||
		prev.SourceWakerID != msg.SourceWakerID || prev.SourceChatID != msg.SourceChatID || prev.SourceMessageID != msg.SourceMessageID ||
		(prev.SourceExecutionID != "" && prev.SourceExecutionID != msg.SourceExecutionID) || prev.ReplyToMessageID != msg.ReplyToMessageID || prev.RequestedDirectory != msg.RequestedDirectory || prev.InputDigest != msg.InputDigest {
		return models.MainConversationMessage{}, false, status_error.CreateRequestConflict
	}
	return prev, true, nil
}

// RemoveMessage 在调用方事务内移除公开时间线上的一条消息，同时保留原执行及收据。
func (c *MainConversationController) RemoveMessage(ctx context.Context, conversation models.MainConversation, messageID string) error {
	row, err := c.Message(ctx, conversation.ID, messageID)
	if err != nil || row.RemovedAt != 0 {
		return err
	}
	if row.AdmissionPending {
		return status_error.ChatMessageNotFound
	}
	result := controllerDB(ctx, c.db).Model(&models.MainConversationMessage{}).
		Where("id = ? AND conversation_id = ? AND removed_at = 0", messageID, conversation.ID).
		Update("removed_at", time.Now().UnixMilli())
	if result.Error != nil {
		return databaseError("remove_main_message", messageID, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}
	publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource(conversation.SubjectType, conversation.SubjectID))
	if row.IsReadablePublication() {
		publishCommittedChange(ctx, c.db, "reading", eventsResource(string(constants.WorkOwnerKindMainConversation), conversation.ID))
		c.publishReadingAttention(ctx, conversation.ID, conversation.SubjectType, conversation.SubjectID)
	}
	return nil
}

// ConfirmUserAdmission 公开已由来源日志确认接纳的交办，并在提交后发送事件。
func (c *MainConversationController) ConfirmUserAdmission(ctx context.Context, conversationID, messageID string) (row models.MainConversationMessage, err error) {
	err = c.transaction.Transaction(ctx, func(tx context.Context) error {
		var err error
		row, err = c.Message(tx, conversationID, messageID)
		if err != nil || !row.AdmissionPending {
			return err
		}
		if row.Role != string(constants.MessageRoleUser) {
			return status_error.BadRequest
		}
		owner, err := c.Get(tx, conversationID)
		if err != nil {
			return err
		}
		db := controllerDB(tx, c.db)
		// 更晚的公开消息可能已进入历史页。延迟接纳的交办从最新一页进入时间线。
		visible := db.Model(&models.MainConversationMessage{}).Select("1").
			Where("conversation_id = ? AND sequence > ? AND removed_at = 0 AND admission_pending = ?", conversationID, row.Sequence, false)
		var later bool
		if err := db.Raw("SELECT EXISTS (?)", visible).Scan(&later).Error; err != nil {
			return databaseError("check_later_main_messages", conversationID, err)
		}
		if later {
			var last models.MainConversationMessage
			if err := db.Select("sequence").Where("conversation_id = ?", conversationID).Order("sequence DESC").Limit(1).Find(&last).Error; err != nil {
				return databaseError("read_main_admission_position", conversationID, err)
			}
			row.Sequence = last.Sequence + 1
		}
		if err := db.Model(&models.MainConversationMessage{}).Where("id = ? AND conversation_id = ?", messageID, conversationID).
			Updates(map[string]any{"admission_pending": false, "sequence": row.Sequence}).Error; err != nil {
			return databaseError("confirm_main_admission", messageID, err)
		}
		row.AdmissionPending = false
		publishCommittedChange(tx, c.db, constants.MainConversationEventTopic, eventsResource(owner.SubjectType, owner.SubjectID))
		return nil
	})
	return row, err
}

// PendingUserAdmissions 有界读取仍需核对来源日志的交办。
func (c *MainConversationController) PendingUserAdmissions(ctx context.Context, conversationID, after string, limit int) ([]models.MainConversationMessage, error) {
	var rows []models.MainConversationMessage
	query := controllerDB(ctx, c.db).Select("id", "conversation_id", "source_chat_id", "source_execution_id", "admission_pending", "admission_submission_id").
		Where("admission_pending = 1 AND removed_at = 0 AND id > ?", after)
	if conversationID != "" {
		// 前台只补齐当前主体已活动或刚结算的工作。启动恢复核对全部未确认记录。
		query = query.Where("conversation_id = ? AND EXISTS (SELECT 1 FROM chats WHERE chats.id = main_conversation_messages.source_chat_id AND "+
			"(chats.active_execution_id = main_conversation_messages.source_execution_id OR chats.last_ended_execution_id = main_conversation_messages.source_execution_id))", conversationID)
	}
	if err := query.Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, databaseError("list_pending_main_admissions", conversationID, err)
	}
	return rows, nil
}

// Messages 从最近窗口向前分页，结果按序号从小到大展示。
func (c *MainConversationController) Messages(ctx context.Context, id string, before int64, limit int) ([]models.MainConversationMessage, bool, error) {
	rows := []models.MainConversationMessage{}
	query := controllerDB(ctx, c.db).Where("conversation_id = ? AND removed_at = 0 AND admission_pending = ?", id, false)
	if before > 0 {
		query = query.Where("sequence < ?", before)
	}
	if err := query.Order("sequence DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, databaseError("main_conversation_messages", id, err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	slices.Reverse(rows)
	return rows, more, nil
}

// WorkPublications 按来源和当前页执行身份读取已发布的真实发言。
func (c *MainConversationController) WorkPublications(ctx context.Context, work models.MainConversationWork, executionIDs []string) ([]models.MainConversationMessage, error) {
	if len(executionIDs) == 0 {
		return nil, nil
	}
	db := controllerDB(ctx, c.db)
	publications := func(indexedTable string) *gorm.DB {
		return db.Table(indexedTable).
			Select("id", "content", "sequence", "source_execution_id", "reply_to_message_id").
			Where("conversation_id = ? AND source_chat_id = ? AND role = ?", work.ConversationID, work.WorkChatID, constants.MessageRoleAssistant)
	}
	var rows, legacy []models.MainConversationMessage
	// 未运行 ANALYZE 的桌面库可能优先扫描整条会话时间线，因此明确当前页使用的定位索引。
	if err := publications("main_conversation_messages INDEXED BY idx_main_conversation_messages_source_execution_id").
		Where("source_execution_id IN ?", executionIDs).Find(&rows).Error; err != nil {
		return nil, databaseError("find_work_publications", work.WorkChatID, err)
	}
	// 早期主会话以公开用户交办的 ID 作为执行 ID。仅当回复目标确实是
	// 同一来源且使用该身份的用户交办时，才为未记录执行身份的发言补齐归属。
	origins := db.Table("main_conversation_messages INDEXED BY sqlite_autoindex_main_conversation_messages_1").Select("id").
		Where("conversation_id = ? AND source_chat_id = ? AND role = ?", work.ConversationID, work.WorkChatID, constants.MessageRoleUser).
		Where("id IN ? AND (source_execution_id = '' OR source_execution_id = id)", executionIDs)
	if err := publications("main_conversation_messages INDEXED BY idx_main_conversation_messages_reply_to_message_id").
		Where("source_execution_id = '' AND reply_to_message_id IN (?)", origins).Find(&legacy).Error; err != nil {
		return nil, databaseError("find_legacy_work_publications", work.WorkChatID, err)
	}
	for i := range legacy {
		legacy[i].SourceExecutionID = legacy[i].ReplyToMessageID
	}
	rows = append(rows, legacy...)
	slices.SortFunc(rows, func(first, second models.MainConversationMessage) int {
		return cmp.Compare(first.Sequence, second.Sequence)
	})
	return rows, nil
}

// MainConversationFailureCandidate 定位尚未公开答复的交办及待核验的权威终态。
type MainConversationFailureCandidate struct {
	// ChatID 是实际工作的来源会话。
	ChatID string
	// ExecutionID 是本轮交办对应的执行身份。
	ExecutionID string
	// WakerID 是接纳时绑定到该来源的实际执行伙伴。
	WakerID string
}

// UnansweredFailures 只对本页未获公开答复的用户交办批量匹配来源失败索引及归属。
func (c *MainConversationController) UnansweredFailures(ctx context.Context, conversationID string, page []models.MainConversationMessage) (map[string]MainConversationFailureCandidate, error) {
	var inputIDs []string
	var replyKeys [][]any
	inputs := make(map[string]models.MainConversationMessage, len(page))
	for _, row := range page {
		if row.Role != string(constants.MessageRoleUser) || row.SourceChatID == "" {
			continue
		}
		if row.SourceExecutionID == "" {
			row.SourceExecutionID = row.ID
		}
		inputIDs = append(inputIDs, row.ID)
		// 历史发言未保存执行身份，但发布时已校验来源。
		replyKeys = append(replyKeys, []any{row.ID, row.SourceChatID, row.SourceExecutionID}, []any{row.ID, row.SourceChatID, ""})
		inputs[row.ID] = row
	}
	if len(inputIDs) == 0 {
		return nil, nil
	}
	db := controllerDB(ctx, c.db)
	var answered []string
	// SQLite 否则可能沿会话时间线扫描全部旧消息，而当前页的回复目标已有索引。
	if err := db.Table("main_conversation_messages INDEXED BY idx_main_conversation_messages_reply_to_message_id").Distinct("reply_to_message_id").
		Where("conversation_id = ? AND role = ? AND reply_to_message_id IN ?", conversationID, constants.MessageRoleAssistant, inputIDs).
		Where("(reply_to_message_id, source_chat_id, source_execution_id) IN ?", replyKeys).Find(&answered).Error; err != nil {
		return nil, databaseError("find_main_conversation_replies", conversationID, err)
	}
	for _, id := range answered {
		delete(inputs, id)
	}
	var pairs [][]any
	for _, row := range inputs {
		pairs = append(pairs, []any{row.SourceChatID, row.SourceExecutionID})
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	var failures []models.ChatReadableMessage
	if err := db.Select("chat_id", "execution_id").Where("kind = 'failure' AND execution_id = message_id AND (chat_id, message_id) IN ?", pairs).Find(&failures).Error; err != nil {
		return nil, databaseError("find_main_conversation_failures", conversationID, err)
	}
	if len(failures) == 0 {
		return nil, nil
	}
	failed := make(map[[2]string]bool, len(failures))
	chatIDs := make([]string, 0, len(failures))
	owners := make(map[string]string, len(failures))
	for _, failure := range failures {
		failed[[2]string{failure.ChatID, failure.ExecutionID}] = true
		if _, seen := owners[failure.ChatID]; !seen {
			owners[failure.ChatID] = ""
			chatIDs = append(chatIDs, failure.ChatID)
		}
	}
	var works []models.MainConversationWork
	if err := db.Select("conversation_id", "work_chat_id", "waker_id").Where("work_chat_id IN ?", chatIDs).Find(&works).Error; err != nil {
		return nil, databaseError("find_main_conversation_failure_sources", conversationID, err)
	}
	for _, work := range works {
		if work.ConversationID == conversationID {
			owners[work.WorkChatID] = work.WakerID
		}
	}
	result := make(map[string]MainConversationFailureCandidate, len(failures))
	for id, input := range inputs {
		if wakerID := owners[input.SourceChatID]; wakerID != "" && failed[[2]string{input.SourceChatID, input.SourceExecutionID}] {
			result[id] = MainConversationFailureCandidate{ChatID: input.SourceChatID, ExecutionID: input.SourceExecutionID, WakerID: wakerID}
		}
	}
	return result, nil
}

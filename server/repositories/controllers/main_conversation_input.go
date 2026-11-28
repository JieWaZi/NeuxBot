package controllers

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const openMainInputs = "status != 'accepted' AND status != 'failed'"

// MainConversationInputController 保存主会话接纳顺序和基于原状态的派发转换。
type MainConversationInputController struct {
	// db 提供主会话业务库默认连接。
	db *gorm.DB
	// tx 将来源核对与接纳顺序写入置于同一事务。
	tx TransactionController
}

// NewMainConversationInputController 构造主会话持久输入队列。
func NewMainConversationInputController(db *gorm.DB) *MainConversationInputController {
	return &MainConversationInputController{db: db, tx: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

// Get 按公开消息标识查找该会话的原回执。
func (c *MainConversationInputController) Get(ctx context.Context, conversationID, messageID string) (models.MainConversationInput, bool, error) {
	var row models.MainConversationInput
	result := controllerDB(ctx, c.db).Where("conversation_id = ? AND public_message_id = ?", conversationID, messageID).Limit(1).Find(&row)
	return row, result.RowsAffected == 1, mainInputDatabaseError("read_main_input_receipt", messageID, result.Error)
}

// HasCommittedPrivateInput 核对这次来源执行确实从私有输入队列经过原生提交门。
func (c *MainConversationController) HasCommittedPrivateInput(ctx context.Context, work models.MainConversationWork, executionID string) (bool, error) {
	var row models.MainConversationInput
	result := controllerDB(ctx, c.db).Select("sequence").
		Where("execution_id = ? AND conversation_id = ? AND work_chat_id = ? AND private_source = ? AND commit_started = ? AND status <> ?",
			executionID, work.ConversationID, work.WorkChatID, true, true, constants.MainConversationInputFailed).
		Limit(1).Find(&row)
	return result.RowsAffected == 1, mainInputDatabaseError("read_private_main_input_execution", executionID, result.Error)
}

// Insert 核对原会话或来源身份后，原子写入接纳位置。
func (c *MainConversationInputController) Insert(ctx context.Context, row models.MainConversationInput) (models.MainConversationInput, error) {
	var stored models.MainConversationInput
	err := c.tx.Transaction(ctx, func(tx context.Context) error {
		db := controllerDB(tx, c.db)
		if row.WorkChatID == "" {
			var conversation models.MainConversation
			if err := db.Select("id").Where("id = ?", row.ConversationID).Limit(1).Find(&conversation).Error; err != nil {
				return mainInputDatabaseError("authorize_main_routing_input", row.ConversationID, err)
			}
			if conversation.ID == "" || row.RouterWakerID == "" {
				return status_error.ChatNotFound
			}
		} else if row.EntryPoint == models.MainConversationTransferredReply {
			if row.StartWork || row.TicketReplyID == "" || row.TicketReplyID != row.PublicReplyID {
				return status_error.BadRequest
			}
			tickets, err := NewWorkTicketController(c.db).TicketsForMessages(tx, constants.WorkTicketOriginMain, row.ConversationID, []string{row.TicketReplyID})
			if err != nil {
				return err
			}
			if len(tickets) != 1 || !tickets[0].Ticket.Transferred || tickets[0].Ticket.WorkChatID != row.WorkChatID || tickets[0].Ticket.Status == constants.WorkTicketStatusStopped {
				return status_error.WorkTicketCannotContinue
			}
		} else {
			var work models.MainConversationWork
			filter := db.Select("work_chat_id").Where("work_chat_id = ? AND conversation_id = ?", row.WorkChatID, row.ConversationID)
			if err := filter.Limit(1).Find(&work).Error; err != nil {
				return mainInputDatabaseError("authorize_main_input_source", row.WorkChatID, err)
			}
			if work.WorkChatID == "" {
				return status_error.ChatNotFound.WithField("chatId", row.WorkChatID)
			}
		}
		queued := row
		queued.Status = constants.MainConversationInputQueued
		result := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "public_message_id"}}, DoNothing: true}).Create(&queued)
		if result.Error != nil {
			return mainInputDatabaseError("enqueue_main_input", row.PublicMessageID, result.Error)
		}
		if result.RowsAffected == 1 {
			stored = queued
			return c.publishChange(tx, row.ConversationID)
		}
		var found bool
		var err error
		stored, found, err = c.Get(tx, row.ConversationID, row.PublicMessageID)
		if err == nil && !found {
			err = status_error.CreateRequestConflict
		}
		return err
	})
	return stored, err
}

// RoutingHead 读取该主会话最早的待路由输入。
func (c *MainConversationInputController) RoutingHead(ctx context.Context, conversationID string) (models.MainConversationInput, bool, error) {
	var row models.MainConversationInput
	result := controllerDB(ctx, c.db).Where("conversation_id = ? AND work_chat_id = ''", conversationID).Where(openMainInputs).
		Order("sequence ASC").Limit(1).Find(&row)
	return row, result.RowsAffected == 1, mainInputDatabaseError("read_main_routing_head", conversationID, result.Error)
}

// ActiveRoutingConversations 分页读取尚有待路由输入的主会话。
func (c *MainConversationInputController) ActiveRoutingConversations(ctx context.Context, after string, limit int) ([]string, error) {
	var ids []string
	err := controllerDB(ctx, c.db).Model(&models.MainConversationInput{}).
		Where("conversation_id > ? AND work_chat_id = ''", after).Where(openMainInputs).
		Distinct("conversation_id").Order("conversation_id ASC").Limit(limit).Pluck("conversation_id", &ids).Error
	return ids, mainInputDatabaseError("list_main_routing_lanes", after, err)
}

// PromoteRoute 将已领取的原回执交给核准的工作来源。
func (c *MainConversationInputController) PromoteRoute(ctx context.Context, row models.MainConversationInput) (bool, error) {
	input, err := json.Marshal(row.Input)
	if err != nil {
		return false, status_error.InternalServerError
	}
	scope, err := json.Marshal(row.RoutedSource)
	if err != nil {
		return false, status_error.InternalServerError
	}
	result := controllerDB(ctx, c.db).Model(&models.MainConversationInput{}).
		Where("sequence = ? AND conversation_id = ? AND work_chat_id = '' AND status = ?", row.Sequence, row.ConversationID, constants.MainConversationInputDispatching).
		Updates(map[string]any{
			"status": constants.MainConversationInputQueued, "work_chat_id": row.WorkChatID, "commit_started": false,
			"input": string(input), "input_digest": row.InputDigest, "routed_source": string(scope),
			"public_reply_id": row.PublicReplyID, "start_work": row.StartWork,
		})
	return result.RowsAffected == 1, mainInputDatabaseError("promote_main_routing_input", row.PublicMessageID, result.Error)
}

// Head 读取原工作 Chat 最早的未交接输入。
func (c *MainConversationInputController) Head(ctx context.Context, chatID string) (models.MainConversationInput, bool, error) {
	var row models.MainConversationInput
	result := controllerDB(ctx, c.db).Where("work_chat_id = ?", chatID).Where(openMainInputs).
		Order("sequence ASC").Limit(1).Find(&row)
	return row, result.RowsAffected == 1, mainInputDatabaseError("read_main_input_head", chatID, result.Error)
}

// ActiveChats 分页读取尚有未交接输入的工作 Chat。
func (c *MainConversationInputController) ActiveChats(ctx context.Context, after string, limit int) ([]string, error) {
	var ids []string
	err := controllerDB(ctx, c.db).Model(&models.MainConversationInput{}).
		Where("work_chat_id > ?", after).Where(openMainInputs).
		Distinct("work_chat_id").Order("work_chat_id ASC").Limit(limit).Pluck("work_chat_id", &ids).Error
	return ids, mainInputDatabaseError("list_main_input_lanes", after, err)
}

// HoldsAttachments 核对待处理的原来源输入是否仍持有暂存附件。
func (c *MainConversationInputController) HoldsAttachments(ctx context.Context, chatID, messageID string) (bool, error) {
	var row models.MainConversationInput
	result := controllerDB(ctx, c.db).Select("sequence").Where("work_chat_id = ? AND public_message_id = ?", chatID, messageID).
		Where(openMainInputs).Limit(1).Find(&row)
	return result.RowsAffected == 1, mainInputDatabaseError("check_main_input_attachments", messageID, result.Error)
}

// MarkedInputs 分页读取已登记原生提交门的未交接来源输入。
func (c *MainConversationInputController) MarkedInputs(ctx context.Context, after int64, limit int) ([]models.MainConversationInput, error) {
	var rows []models.MainConversationInput
	err := controllerDB(ctx, c.db).Where(openMainInputs).
		Where("sequence > ? AND commit_started = 1 AND work_chat_id != ''", after).
		Order("sequence ASC").Limit(limit).Find(&rows).Error
	return rows, mainInputDatabaseError("list_main_input_recovery_claims", "", err)
}

// List 分页读取主会话或指定工作来源的待处理或失败输入。
func (c *MainConversationInputController) List(ctx context.Context, conversationID, workChatID string, after int64, limit int, failed bool) ([]models.MainConversationInput, error) {
	var rows []models.MainConversationInput
	query := controllerDB(ctx, c.db).Where("conversation_id = ?", conversationID)
	if failed {
		query = query.Where("status = 'failed'").Order("sequence DESC")
		if after != 0 {
			query = query.Where("sequence < ?", after)
		}
	} else {
		query = query.Where(openMainInputs).Where("sequence > ?", after).Order("sequence ASC")
	}
	if workChatID != "" {
		query = query.Where("work_chat_id = ?", workChatID)
	}
	err := query.Limit(limit).Find(&rows).Error
	return rows, mainInputDatabaseError("list_main_input_receipts", conversationID, err)
}

// Claim 持久领取队首输入，保证每个序列同一时间只有一个派发者。
func (c *MainConversationInputController) Claim(ctx context.Context, sequence int64) (bool, error) {
	db := controllerDB(ctx, c.db)
	earlier := db.Table("main_conversation_inputs AS earlier").Select("1").
		Where("earlier.work_chat_id = main_conversation_inputs.work_chat_id AND earlier.sequence < main_conversation_inputs.sequence").
		Where("earlier.work_chat_id != '' OR earlier.conversation_id = main_conversation_inputs.conversation_id").
		Where(openMainInputs)
	// 尚未选定来源的主会话输入，也可能进入同一伙伴的默认来源。
	defaultWaker := db.Model(&models.MainConversationWork{}).Select("waker_id").
		Where("work_chat_id = main_conversation_inputs.work_chat_id AND scope_key = ?", models.MainConversationDefaultScope)
	earlierRouting := db.Table("main_conversation_inputs AS routing INDEXED BY idx_main_input_conversation_open").Select("1").
		Where("routing.conversation_id = main_conversation_inputs.conversation_id AND routing.work_chat_id = ''").
		Where("routing.sequence < main_conversation_inputs.sequence AND routing.router_waker_id IN (?)", defaultWaker).
		Where(openMainInputs)
	result := db.Model(&models.MainConversationInput{}).
		Where("sequence = ? AND status = ?", sequence, constants.MainConversationInputQueued).
		Where("NOT EXISTS (?)", earlier).
		Where("NOT EXISTS (?)", earlierRouting).
		Updates(map[string]any{"status": constants.MainConversationInputDispatching, "error_code": ""})
	return result.RowsAffected == 1, mainInputDatabaseError("claim_main_input", "", result.Error)
}

// DefaultSourceForRouting 读取可能因更早的主会话输入等待派发的默认来源。
func (c *MainConversationInputController) DefaultSourceForRouting(ctx context.Context, conversationID, wakerID string) (string, error) {
	var work models.MainConversationWork
	result := controllerDB(ctx, c.db).Select("work_chat_id").
		Where("conversation_id = ? AND waker_id = ? AND scope_key = ?", conversationID, wakerID, models.MainConversationDefaultScope).
		Limit(1).Find(&work)
	return work.WorkChatID, mainInputDatabaseError("read_default_source_for_routing", conversationID, result.Error)
}

// MarkCommitting 持久登记当前领取已到达原生提交门。
func (c *MainConversationInputController) MarkCommitting(ctx context.Context, sequence int64) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.MainConversationInput{}).
		Where("sequence = ? AND status = ? AND commit_started = ?", sequence, constants.MainConversationInputDispatching, false).
		Update("commit_started", true)
	return result.RowsAffected == 1, mainInputDatabaseError("mark_main_input_commit", "", result.Error)
}

// Settle 根据本次已观测状态登记交接结果，并清除已结算输入的内部参数。
func (c *MainConversationInputController) Settle(ctx context.Context, row models.MainConversationInput, status constants.MainConversationInputStatus, code string) (bool, error) {
	return c.settle(ctx, row, status, code, nil)
}

// SettleRouting 同时读取已解除路由阻塞的默认来源，覆盖路由开始后新建的来源。
func (c *MainConversationInputController) SettleRouting(ctx context.Context, row models.MainConversationInput, status constants.MainConversationInputStatus, code string) (bool, string, error) {
	var defaultSource string
	applied, err := c.settle(ctx, row, status, code, &defaultSource)
	return applied, defaultSource, err
}

func (c *MainConversationInputController) settle(ctx context.Context, row models.MainConversationInput, status constants.MainConversationInputStatus, code string, defaultSource *string) (bool, error) {
	updates := map[string]any{"status": status, "error_code": code}
	switch status {
	case constants.MainConversationInputQueued:
		updates["commit_started"] = false
	case constants.MainConversationInputAccepted, constants.MainConversationInputFailed:
		// Keep the original key and public receipt. Runtime selections and local references belong
		// only to the outstanding delivery, while accepted text is owned by the public timeline.
		compact, err := json.Marshal(workinput.Input{SubmissionID: row.Input.SubmissionID})
		if err != nil {
			return false, status_error.InternalServerError
		}
		updates["input"], updates["options"] = string(compact), "{}"
		updates["requested_directory"], updates["ticket_reply_id"] = "", ""
		if row.EntryPoint != models.MainConversationTransferredReply {
			updates["entry_point"] = ""
		}
		updates["routed_source"], updates["input_digest"] = nil, ""
		if status == constants.MainConversationInputAccepted {
			updates["public_content"] = ""
		}
	}
	var applied bool
	update := func(ctx context.Context) error {
		result := controllerDB(ctx, c.db).Model(&models.MainConversationInput{}).
			Where("sequence = ? AND status = ? AND commit_started = ?", row.Sequence, row.Status, row.CommitStarted).
			Updates(updates)
		applied = result.RowsAffected == 1
		if result.Error != nil {
			return databaseError("settle_main_input", row.PublicMessageID, result.Error)
		}
		if applied && status != constants.MainConversationInputQueued {
			if defaultSource != nil {
				var err error
				*defaultSource, err = c.DefaultSourceForRouting(ctx, row.ConversationID, row.RouterWakerID)
				if err != nil {
					return err
				}
			}
			return c.publishChange(ctx, row.ConversationID)
		}
		return nil
	}
	if status == constants.MainConversationInputQueued {
		err := update(ctx)
		return applied, err
	}
	err := c.tx.Transaction(ctx, update)
	return applied, err
}

func (c *MainConversationInputController) publishChange(ctx context.Context, conversationID string) error {
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nil
	}
	var owner models.MainConversation
	err := controllerDB(ctx, c.db).Select("id", "subject_type", "subject_id").Where("id = ?", conversationID).Limit(1).Find(&owner).Error
	if err != nil {
		return databaseError("find_main_input_subject", conversationID, err)
	}
	if owner.ID != "" {
		publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource(owner.SubjectType, owner.SubjectID))
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	}
	return nil
}

func mainInputDatabaseError(operation, resource string, err error) error {
	if err == nil {
		return nil
	}
	return databaseError(operation, resource, err)
}

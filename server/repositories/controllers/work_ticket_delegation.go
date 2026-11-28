package controllers

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (c *workTicketController) Delegations(ctx context.Context, parentID string, generation int64, limit int) ([]models.WorkTicket, error) {
	var rows []models.WorkTicket
	err := controllerDB(ctx, c.db).Where("parent_ticket_id = ? AND parent_generation = ?", parentID, generation).Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_work_ticket_delegations", parentID, err)
	}
	return rows, err
}

func (c *workTicketController) LiveDelegationOwners(ctx context.Context, parents []models.WorkTicket) ([]models.WorkTicket, error) {
	keys := make([][]any, 0, len(parents))
	ids := make([]string, 0, len(parents))
	for _, parent := range parents {
		keys = append(keys, []any{parent.ID, parent.Generation})
		ids = append(ids, parent.ID)
	}
	var rows []models.WorkTicket
	err := controllerDB(ctx, c.db).Select("id", "generation", "owner_kind", "owner_id", "owner_waker_id", "work_chat_id").
		Where("parent_ticket_id IN ? AND (parent_ticket_id, parent_generation) IN ? AND status NOT IN ?",
			ids, keys, []constants.WorkTicketStatus{constants.WorkTicketStatusCompleted, constants.WorkTicketStatusStopped}).
		Find(&rows).Error
	if err != nil {
		err = databaseError("read_live_work_ticket_children", "", err)
	}
	return rows, err
}

func (c *workTicketController) TransferDelegations(ctx context.Context, parentID string, previous, next int64) error {
	err := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("parent_ticket_id = ? AND parent_generation = ?", parentID, previous).
		UpdateColumn("parent_generation", next).Error
	if err != nil {
		err = databaseError("transfer_work_ticket_child_return_authority", parentID, err)
	}
	return err
}

func (c *workTicketController) PendingDelegations(ctx context.Context, after string, limit int) ([]models.WorkTicket, error) {
	var rows []models.WorkTicket
	err := controllerDB(ctx, c.db).Where("parent_ticket_id <> '' AND generation = 0 AND status = ? AND id > ?", constants.WorkTicketStatusActive, after).
		Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_delegations", "", err)
	}
	return rows, err
}

func (c *workTicketController) WaitingDelegationParents(ctx context.Context, after string, limit int) ([]models.WorkTicket, error) {
	var rows []models.WorkTicket
	err := controllerDB(ctx, c.db).Table("work_tickets INDEXED BY idx_work_ticket_awaiting_returns").
		Where("awaiting_delegation_returns = 1 AND id > ?", after).Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_work_ticket_delegation_parents", "", err)
	}
	return rows, err
}

func (c *workTicketController) DelegationReports(ctx context.Context, ids []string, limit int) ([]models.WorkTicketReport, error) {
	var rows []models.WorkTicketReport
	err := controllerDB(ctx, c.db).Where("ticket_id IN ? AND message_id = id AND reply_to_report_id = ''", ids).Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_direct_work_ticket_returns", "", err)
	}
	return rows, err
}

func (c *workTicketController) DelegationTurnReports(ctx context.Context, executions []string, statuses []constants.WorkTicketStatus, after string, limit int) ([]models.WorkTicketReport, error) {
	var rows []models.WorkTicketReport
	err := controllerDB(ctx, c.db).Where("execution_id IN ? AND status IN ? AND message_id = id AND reply_to_report_id = '' AND id > ?", executions, statuses, after).
		Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_work_ticket_delegation_turn_reports", "", err)
	}
	return rows, err
}

func (c *workTicketController) DelegationReplyForQuestion(ctx context.Context, reportID string) (models.WorkTicketReport, error) {
	var row models.WorkTicketReport
	err := controllerDB(ctx, c.db).Where("reply_to_report_id <> '' AND reply_to_report_id = ?", reportID).Limit(1).Find(&row).Error
	if err != nil {
		err = databaseError("read_work_ticket_delegation_reply", reportID, err)
	}
	return row, err
}

func (c *workTicketController) PendingDelegationReplies(ctx context.Context, after string, limit int) ([]models.WorkTicketReport, error) {
	var rows []models.WorkTicketReport
	err := controllerDB(ctx, c.db).Table("work_ticket_reports INDEXED BY idx_work_ticket_undelivered").
		Where("message_id = '' AND reply_to_report_id <> '' AND id > ?", after).Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_delegation_replies", "", err)
	}
	return rows, err
}

func (c *workTicketController) ReserveDelegationExecution(ctx context.Context, previous, next models.WorkTicket) error {
	directories, _ := json.Marshal(next.AllowedDirectories)
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("id = ? AND generation = 0 AND pending_execution_id = '' AND parent_ticket_id = ? AND status = ?",
			previous.ID, previous.ParentTicketID, constants.WorkTicketStatusActive).
		UpdateColumns(map[string]any{"pending_execution_id": next.PendingExecutionID, "working_directory": next.WorkingDirectory,
			"allowed_directories": string(directories), "updated_at": next.UpdatedAt})
	return workTicketChanged(result, "reserve_work_ticket_delegation", previous.ID)
}

func (c *workTicketController) MarkDelegationFailed(ctx context.Context, ticket models.WorkTicket) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("id = ? AND current_execution_id = ? AND generation = ? AND parent_ticket_id <> ''", ticket.ID, ticket.CurrentExecutionID, ticket.Generation).
		UpdateColumn("delegation_failed", true)
	if err := workTicketChanged(result, "mark_work_ticket_delegation_failed", ticket.ID); err != nil {
		return err
	}
	c.publishActivityChange(ctx, ticket)
	return nil
}

// ActivityCandidates 读取主会话工单树与受托子单，调用方必须校验读取和跳转范围。
func (c *workTicketController) ActivityCandidates(ctx context.Context, conversationID, subjectType, subjectID string) ([]models.WorkTicket, error) {
	db := controllerDB(ctx, c.db)
	const limit = 100
	var rows []models.WorkTicket
	if err := db.Where("origin_kind = ? AND origin_id = ? AND origin_id <> '' AND parent_ticket_id = ''", constants.WorkTicketOriginMain, conversationID).
		Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, databaseError("read_work_activity_roots", conversationID, err)
	}
	seen := make(map[string]bool)
	for _, row := range rows {
		seen[row.ID] = true
	}
	frontier := append([]models.WorkTicket(nil), rows...)
	for depth := 0; len(frontier) > 0 && depth < 12; depth++ {
		ids := make([]string, 0, len(frontier))
		for _, row := range frontier {
			ids = append(ids, row.ID)
		}
		frontier = nil
		if err := db.Where("parent_ticket_id IN ?", ids).Order("created_at DESC, id DESC").Limit(limit).Find(&frontier).Error; err != nil {
			return nil, databaseError("read_work_activity_children", conversationID, err)
		}
		for _, row := range frontier {
			if !seen[row.ID] {
				seen[row.ID] = true
				rows = append(rows, row)
			}
		}
	}
	if subjectType == "waker" {
		var incoming []models.WorkTicket
		if err := db.Where("owner_waker_id = ? AND parent_ticket_id <> ''", subjectID).Order("created_at DESC, id DESC").Limit(limit).Find(&incoming).Error; err != nil {
			return nil, databaseError("read_work_activity_received", subjectID, err)
		}
		for _, row := range incoming {
			if !seen[row.ID] {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// publishActivityChange 用无内容的事件更新发起会话和接收伙伴的活动投影。
func (c *workTicketController) publishActivityChange(ctx context.Context, ticket models.WorkTicket) {
	root := ticket
	for depth := 0; root.ParentTicketID != "" && depth < 12; depth++ {
		parent, err := c.Ticket(ctx, root.ParentTicketID)
		if err != nil || parent.ID == "" {
			return
		}
		root = parent
	}
	if root.OriginKind != constants.WorkTicketOriginMain || root.ParentTicketID != "" {
		return
	}
	var owner models.MainConversation
	if err := controllerDB(ctx, c.db).Select("subject_type", "subject_id").Where("id = ?", root.OriginID).Take(&owner).Error; err != nil {
		return
	}
	publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource(owner.SubjectType, owner.SubjectID))
	if ticket.ParentTicketID != "" && ticket.OwnerWakerID != "" && (owner.SubjectType != "waker" || owner.SubjectID != ticket.OwnerWakerID) {
		publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource("waker", ticket.OwnerWakerID))
	}
}

// AuthorizesTeamDelegate 查询指定团队目前有效的实际执行成员，不读取其他团队。
func (c *workTicketController) AuthorizesTeamDelegate(ctx context.Context, teamID, wakerID string) (bool, error) {
	db := controllerDB(ctx, c.db)
	var team models.Channel
	if err := db.Where("id = ?", teamID).Limit(1).Find(&team).Error; err != nil {
		return false, databaseError("work_ticket_delegate_team", teamID, err)
	}
	if team.ID == "" {
		return false, nil
	}
	if team.AuthorizesWorkActor(wakerID, nil) {
		return true, nil
	}
	var member models.ChannelMember
	if err := db.Where("channel_id = ? AND waker_id = ?", teamID, wakerID).Limit(1).Find(&member).Error; err != nil {
		return false, databaseError("work_ticket_delegate_member", teamID, err)
	}
	return team.AuthorizesWorkActor(wakerID, []models.ChannelMember{member}), nil
}

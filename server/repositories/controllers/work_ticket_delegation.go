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
	return workTicketChanged(result, "mark_work_ticket_delegation_failed", ticket.ID)
}

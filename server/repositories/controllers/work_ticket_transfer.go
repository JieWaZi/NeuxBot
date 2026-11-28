package controllers

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (c *workTicketController) TransferAttempt(ctx context.Context, ticketID, requestID string) (models.WorkTicketAttempt, error) {
	return workTicketRow[models.WorkTicketAttempt](controllerDB(ctx, c.db).Where("ticket_id = ? AND transfer_request_id = ?", ticketID, requestID), "read_work_ticket_transfer", ticketID)
}

func (c *workTicketController) PendingTransfers(ctx context.Context, afterID string, limit int) ([]models.WorkTicket, error) {
	var rows []models.WorkTicket
	err := controllerDB(ctx, c.db).Where("id > ? AND transferred = 1 AND status = 'active' AND current_execution_id = '' AND pending_execution_id <> ''", afterID).
		Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_transfers", "", err)
	}
	return rows, err
}

func (c *workTicketController) PendingTransferAttempts(ctx context.Context, tickets []models.WorkTicket) ([]models.WorkTicketAttempt, error) {
	if len(tickets) == 0 {
		return nil, nil
	}
	keys := make([][]any, 0, len(tickets))
	ids := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		keys = append(keys, []any{ticket.ID, ticket.Generation})
		ids = append(ids, ticket.ID)
	}
	var rows []models.WorkTicketAttempt
	err := controllerDB(ctx, c.db).Where("ticket_id IN ? AND (ticket_id, generation) IN ? AND status = ? AND transfer_request_id <> ''",
		ids, keys, constants.WorkTicketAttemptStatusPending).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_transfer_attempts", "", err)
	}
	return rows, err
}

func (c *workTicketController) TransferOwner(ctx context.Context, previous, next models.WorkTicket) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("id = ? AND generation = ? AND current_execution_id = ? AND pending_execution_id = '' AND status = ? AND updated_at = ?",
			previous.ID, previous.Generation, previous.CurrentExecutionID, previous.Status, previous.UpdatedAt).
		UpdateColumns(map[string]any{
			"owner_kind": next.OwnerKind, "owner_id": next.OwnerID, "owner_waker_id": next.OwnerWakerID, "work_chat_id": next.WorkChatID,
			"transferred": true, "project_id": "", "project_scope": "", "working_directory": "", "allowed_directories": nil, "permission_level": next.PermissionLevel,
			"generation": next.Generation, "status": next.Status, "current_execution_id": "", "pending_execution_id": next.PendingExecutionID,
			"awaiting_delegation_returns": next.AwaitingDelegationReturns, "delegation_failed": next.DelegationFailed, "summary": next.Summary, "updated_at": next.UpdatedAt,
		})
	return workTicketChanged(result, "transfer_work_ticket_owner", previous.ID)
}

func (c *workTicketController) FreezeTransferDirectory(ctx context.Context, ticket models.WorkTicket) error {
	directories, _ := json.Marshal(ticket.AllowedDirectories)
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).Where("id = ? AND generation = ? AND pending_execution_id = ? AND working_directory = ''", ticket.ID, ticket.Generation, ticket.PendingExecutionID).
		UpdateColumns(map[string]any{"working_directory": ticket.WorkingDirectory, "allowed_directories": string(directories)})
	return workTicketChanged(result, "freeze_transfer_directory", ticket.ID)
}

func (c *workTicketController) PendingOriginReplies(ctx context.Context, afterID string, limit int) ([]models.WorkTicketInput, error) {
	var rows []models.WorkTicketInput
	err := controllerDB(ctx, c.db).Table("work_ticket_inputs INDEXED BY idx_work_ticket_origin_replies").
		Where("origin_reply IS NOT NULL AND execution_id > ?", afterID).
		Order("execution_id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_origin_replies", "", err)
	}
	return rows, err
}

func (c *workTicketController) ConfirmOriginReply(ctx context.Context, executionID string) error {
	err := controllerDB(ctx, c.db).Model(&models.WorkTicketInput{}).
		Where("execution_id = ? AND confirmed_at IS NOT NULL AND origin_reply IS NOT NULL", executionID).
		UpdateColumn("origin_reply", nil).Error
	if err != nil {
		err = databaseError("confirm_work_ticket_origin_reply", executionID, err)
	}
	return err
}

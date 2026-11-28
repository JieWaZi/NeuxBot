package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (c *workTicketController) EventSubscription(ctx context.Context, ticketID string) (models.WorkTicketEventSubscription, error) {
	return workTicketRow[models.WorkTicketEventSubscription](controllerDB(ctx, c.db).Where("ticket_id = ?", ticketID), "read_work_ticket_event_subscription", ticketID)
}

func (c *workTicketController) EventTopicSubscriptions(ctx context.Context, ruleID, topic string) ([]models.WorkTicketEventSubscription, error) {
	var rows []models.WorkTicketEventSubscription
	err := controllerDB(ctx, c.db).Where("rule_id = ? AND topic = ?", ruleID, topic).Order("ticket_id").Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_work_ticket_event_subscriptions", ruleID, err)
	}
	return rows, nil
}

func (c *workTicketController) CreateEventSubscription(ctx context.Context, subscription models.WorkTicketEventSubscription) error {
	return c.create(ctx, subscription.TicketID, &subscription)
}

func (c *workTicketController) EventDelivery(ctx context.Context, id string) (models.WorkTicketEventDelivery, error) {
	return workTicketRow[models.WorkTicketEventDelivery](controllerDB(ctx, c.db).Where("id = ?", id), "read_work_ticket_event_delivery", id)
}

func (c *workTicketController) CreateEventDelivery(ctx context.Context, delivery models.WorkTicketEventDelivery) error {
	return c.create(ctx, delivery.ID, &delivery)
}

func (c *workTicketController) SettleEventDelivery(ctx context.Context, id, executionID, code string) error {
	res := controllerDB(ctx, c.db).Model(&models.WorkTicketEventDelivery{}).Where("id = ? AND execution_id = '' AND error_code = ''", id).
		Updates(map[string]any{"execution_id": executionID, "error_code": code})
	if res.Error != nil {
		return databaseError("settle_work_ticket_event_delivery", id, res.Error)
	}
	return nil
}

func (c *workTicketController) MarkEventInputRead(ctx context.Context, executionID string, at int64) error {
	res := controllerDB(ctx, c.db).Model(&models.WorkTicketEventDelivery{}).Where("execution_id = ? AND execution_id <> '' AND read_at IS NULL", executionID).Update("read_at", at)
	if res.Error != nil {
		return databaseError("mark_work_ticket_event_input_read", executionID, res.Error)
	}
	return nil
}

func (c *workTicketController) RecoverEventDelivery(ctx context.Context, delivery models.WorkTicketEventDelivery, retry bool, code string) error {
	fields := map[string]any{"error_code": code}
	if retry {
		fields["execution_id"], fields["retry"] = "", delivery.Retry+1
		fields["recovery_after_execution_id"] = delivery.ExecutionID
	}
	res := controllerDB(ctx, c.db).Model(&models.WorkTicketEventDelivery{}).
		Where("id = ? AND execution_id = ? AND retry = ? AND read_at IS NULL AND error_code = ''", delivery.ID, delivery.ExecutionID, delivery.Retry).Updates(fields)
	if res.Error != nil {
		return databaseError("recover_work_ticket_event_delivery", delivery.ID, res.Error)
	}
	return nil
}

func (c *workTicketController) EventWait(ctx context.Context, id string) (models.WorkTicketEventWait, error) {
	return workTicketRow[models.WorkTicketEventWait](controllerDB(ctx, c.db).Where("id = ?", id), "read_work_ticket_event_wait", id)
}

func (c *workTicketController) ActiveEventWait(ctx context.Context, ticket models.WorkTicket) (models.WorkTicketEventWait, error) {
	filter := controllerDB(ctx, c.db).Where("ticket_id = ? AND generation = ? AND execution_id = ?", ticket.ID, ticket.Generation, ticket.CurrentExecutionID)
	return workTicketRow[models.WorkTicketEventWait](filter.Order("wake_delivery_id ASC, id ASC"), "read_current_work_ticket_event_wait", ticket.ID)
}

func (c *workTicketController) CreateEventWait(ctx context.Context, wait models.WorkTicketEventWait) error {
	return c.create(ctx, wait.ID, &wait)
}

func (c *workTicketController) WakeEventWait(ctx context.Context, id, deliveryID string) error {
	res := controllerDB(ctx, c.db).Model(&models.WorkTicketEventWait{}).Where("id = ? AND wake_delivery_id = ''", id).Update("wake_delivery_id", deliveryID)
	if res.Error != nil {
		return databaseError("wake_work_ticket_event_wait", id, res.Error)
	}
	if res.RowsAffected != 1 {
		return status_error.WorkTicketAttemptUnavailable
	}
	return nil
}

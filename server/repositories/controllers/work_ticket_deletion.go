package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func (c *workTicketController) PendingConclusionsForChats(ctx context.Context, chatIDs []string) ([]models.WorkTicketReport, error) {
	var rows []models.WorkTicketReport
	db := controllerDB(ctx, c.db)
	inputs := db.Model(&models.WorkTicketInput{}).Select("execution_id").
		Where("origin_kind = ? AND origin_id IN ? AND confirmed_at IS NOT NULL", constants.WorkTicketOriginChat, chatIDs)
	if err := db.Where("execution_id IN (?) AND message_id = '' AND status = ?", inputs, constants.WorkTicketStatusCompleted).
		Order("id").Find(&rows).Error; err != nil {
		return nil, databaseError("list_conclusions_for_origin_deletion", "", err)
	}
	return rows, nil
}

func (c *workTicketController) PreserveDeletedOriginReceipt(ctx context.Context, report models.WorkTicketReport) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicketReport{}).
		Where("id = ? AND (message_id = '' OR message_id = ?) AND delivery_conversation_id = ''", report.ID, report.MessageID).
		UpdateColumns(map[string]any{"message_id": report.MessageID, "delivery_conversation_id": report.DeliveryConversationID})
	return workTicketChanged(result, "preserve_deleted_origin_report_receipt", report.ID)
}

func (c *workTicketController) MarkOrdinaryTicketOriginDeleted(ctx context.Context, origin models.Chat) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("origin_kind = ? AND origin_id = ? AND transferred = ? AND parent_ticket_id = '' AND deleted_origin IS NULL",
			constants.WorkTicketOriginChat, origin.ID, true).
		Select("deleted_origin").UpdateColumns(&models.WorkTicket{
		DeletedOrigin: &models.WorkTicketDeletedOrigin{WakerID: origin.WakerID, Title: origin.Title},
	})
	if result.Error != nil {
		return databaseError("mark_work_ticket_origin_deleted", origin.ID, result.Error)
	}
	return nil
}

// deleteChatWorkTickets 保留删除事务已核实的结案回执及其关联，其余工单事实随原记录清理。
func deleteChatWorkTickets(db *gorm.DB, chatID string) error {
	tickets := db.Model(&models.WorkTicket{}).Select("id").Where("work_chat_id = ?", chatID).
		Where("NOT (transferred = 1 AND current_execution_id = '' AND status IN ?)",
			[]constants.WorkTicketStatus{constants.WorkTicketStatusActive, constants.WorkTicketStatusBlocked})
	if err := db.Where("ticket_id IN (?) AND delivery_conversation_id = ''", tickets).Delete(&models.WorkTicketReport{}).Error; err != nil {
		return databaseError("delete_work_ticket_reports", chatID, err)
	}
	retainedReports := db.Model(&models.WorkTicketReport{}).Where("ticket_id IN (?) AND delivery_conversation_id <> ''", tickets)
	if err := db.Where("ticket_id IN (?) AND execution_id NOT IN (?)", tickets, retainedReports.Select("execution_id")).Delete(&models.WorkTicketInput{}).Error; err != nil {
		return databaseError("delete_work_ticket_inputs", chatID, err)
	}
	inputs := db.Model(&models.WorkTicketInput{}).Select("1").Where("ticket_id = work_ticket_attempts.ticket_id AND generation = work_ticket_attempts.generation")
	if err := db.Where("ticket_id IN (?) AND NOT EXISTS (?)", tickets, inputs).Delete(&models.WorkTicketAttempt{}).Error; err != nil {
		return databaseError("delete_work_ticket_attempts", chatID, err)
	}
	if err := db.Where("id IN (?) AND id NOT IN (?)", tickets, retainedReports.Select("ticket_id")).Delete(&models.WorkTicket{}).Error; err != nil {
		return databaseError("delete_chat_work_tickets", chatID, err)
	}
	return nil
}

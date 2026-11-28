package mainconversation

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// PublishDeletedOriginReport 写入同一伙伴主会话，并保留原工单的只读来源引用。
func (s *Service) PublishDeletedOriginReport(ctx context.Context, owner string, ticket models.WorkTicket, report models.WorkTicketReport, origin models.MainConversationOriginalWork) (row models.MainConversationMessage, err error) {
	if owner == "" || ticket.OwnerWakerID == "" || report.ID == "" || report.Content == "" || ticket.OriginKind != constants.WorkTicketOriginChat ||
		origin.ChatID == "" || origin.ChatID != ticket.OriginID || origin.TicketID != ticket.ID {
		return row, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		conv, err := s.Ensure(tx, "waker", owner)
		if err != nil {
			return err
		}
		message := models.MainConversationMessage{
			ID: report.ID, ConversationID: conv.ID, Role: string(constants.MessageRoleAssistant),
			Content: report.Content, SourceWakerID: ticket.OwnerWakerID, OriginalWork: &origin,
		}
		if ticket.Transferred {
			message.SourceChatID, message.SourceExecutionID, message.SourceReadOnly = ticket.WorkChatID, report.ExecutionID, true
		}
		row, _, err = s.append(tx, message, conv.SubjectType, conv.SubjectID)
		return err
	})
	return
}

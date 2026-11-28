package mainconversation

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// AdmitTransferredUser preserves the original public reply while the receiving partner executes privately.
func (s *Service) AdmitTransferredUser(ctx context.Context, origin models.MainConversation, ticket models.WorkTicket, msg models.MainConversationMessage, commit func(context.Context) error) (models.MainConversationMessage, error) {
	return s.admitUser(ctx, origin.SubjectType, origin.SubjectID, msg, func(tx context.Context) (string, error) {
		if !ticket.Transferred || ticket.OriginKind != constants.WorkTicketOriginMain || ticket.OriginID != origin.ID ||
			msg.SourceChatID != ticket.WorkChatID || !msg.SourceReadOnly {
			return "", status_error.WorkTicketCannotContinue
		}
		conversation, err := s.Ensure(tx, origin.SubjectType, origin.SubjectID)
		if err != nil {
			return "", err
		}
		quote, err := s.store.Message(tx, conversation.ID, msg.ReplyToMessageID)
		if err != nil {
			return "", err
		}
		if conversation.ID != origin.ID || quote.AdmissionPending || quote.RemovedAt != 0 {
			return "", status_error.ChatReplyTargetUnavailable
		}
		work, owner, err := s.authorizedWork(tx, ticket.WorkChatID)
		if err != nil {
			return "", err
		}
		if owner.SubjectType != ticket.OwnerKind || owner.SubjectID != ticket.OwnerID || work.WakerID != ticket.OwnerWakerID {
			return "", status_error.WorkTicketCannotContinue
		}
		return origin.ID, nil
	}, commit)
}

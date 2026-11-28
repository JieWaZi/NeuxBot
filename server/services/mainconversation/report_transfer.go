package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ReadOnlyReportSource 核对原会话中的精确回件和实际来源绑定，不授予对来源的普通读写权限。
func (s *Service) ReadOnlyReportSource(ctx context.Context, kind, id, chatID, publicationID string) (models.MainConversationWork, models.MainConversationMessage, error) {
	var empty models.MainConversationWork
	conv, err := s.Ensure(ctx, kind, id)
	if err != nil {
		return empty, models.MainConversationMessage{}, err
	}
	publication, found, err := s.store.FindMessage(ctx, conv.ID, publicationID)
	if err != nil {
		return empty, publication, err
	}
	notFound := status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": chatID})
	if !found || !publication.IsReadablePublication() || !publication.SourceReadOnly || publication.AdmissionPending ||
		publication.SourceChatID != chatID || publication.SourceExecutionID == "" || publication.SourceWakerID == "" {
		return empty, publication, notFound
	}
	work, err := s.store.Work(ctx, chatID)
	if err != nil {
		return empty, publication, err
	}
	if work.WakerID != publication.SourceWakerID {
		return empty, publication, notFound
	}
	return work, publication, nil
}

// PublishTransferredWorkReport 将当前工单回件送回已验证的原主会话。
func (s *Service) PublishTransferredWorkReport(ctx context.Context, ticket models.WorkTicket, executionID, reportID, content string) (msg models.MainConversationMessage, replay bool, err error) {
	if !ticket.Transferred || ticket.OriginKind != constants.WorkTicketOriginMain || ticket.CurrentExecutionID != executionID ||
		executionID == "" || reportID == "" || strings.TrimSpace(content) == "" {
		return msg, false, status_error.WorkTicketAttemptUnavailable
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		work, target, err := s.authorizedWork(tx, ticket.WorkChatID)
		if err != nil || target.SubjectType != ticket.OwnerKind || target.SubjectID != ticket.OwnerID || work.WakerID != ticket.OwnerWakerID {
			if err == nil {
				err = status_error.WorkTicketAttemptUnavailable
			}
			return err
		}
		origin, err := s.store.Get(tx, ticket.OriginID)
		if err != nil {
			return err
		}
		if _, err := s.subjectActor(tx, origin.SubjectType, origin.SubjectID); err != nil {
			return err
		}
		input, err := s.store.Message(tx, origin.ID, ticket.ID)
		if err != nil || input.Role != string(constants.MessageRoleUser) || input.AdmissionPending {
			if err == nil {
				err = status_error.ChatReplyTargetUnavailable
			}
			return err
		}
		msg, replay, err = s.store.Append(tx, models.MainConversationMessage{
			ID: reportID, ConversationID: origin.ID,
			Role: string(constants.MessageRoleAssistant), Content: content, ReplyToMessageID: ticket.ID, SourceWakerID: ticket.OwnerWakerID,
			SourceChatID: ticket.WorkChatID, SourceExecutionID: executionID, SourceReadOnly: true,
		}, origin.SubjectType, origin.SubjectID)
		return err
	})
	return msg, replay, err
}

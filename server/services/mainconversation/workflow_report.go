package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ResolveWorkflowOrigin 返回实际工作回合在主会话中的稳定公开地址。
func (s *Service) ResolveWorkflowOrigin(ctx context.Context, chatID, executionID string) (string, string, error) {
	work, found, err := s.store.FindWork(ctx, chatID)
	if err != nil || !found {
		return "", "", err
	}
	row, err := s.store.FindWorkflowInput(ctx, work.ConversationID, chatID, executionID)
	if util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable) {
		private, proofErr := s.store.HasCommittedPrivateInput(ctx, work, executionID)
		if proofErr != nil || private {
			return "", "", proofErr
		}
	}
	return work.ConversationID, row.ID, err
}

// PublishWorkflowConclusion 将已结算的工作流结论追加到接纳时冻结的主会话。
func (s *Service) PublishWorkflowConclusion(ctx context.Context, run models.WorkflowRun, content string) error {
	if (run.ConclusionMainID == "" && run.ConclusionRecipient == nil) || run.ID == "" || run.DefinitionID == "" || strings.TrimSpace(content) == "" {
		return status_error.BadRequest
	}
	return s.tx.Transaction(ctx, func(ctx context.Context) error {
		var conv models.MainConversation
		var err error
		if run.ConclusionRecipient != nil {
			conv, err = s.Ensure(ctx, run.ConclusionRecipient.SubjectType, run.ConclusionRecipient.SubjectID)
		} else {
			conv, err = s.store.Get(ctx, run.ConclusionMainID)
			if err == nil {
				_, err = s.subjectActor(ctx, conv.SubjectType, conv.SubjectID)
			}
		}
		if err != nil {
			return err
		}
		if run.ConclusionReplyID != "" {
			origin, err := s.store.Message(ctx, conv.ID, run.ConclusionReplyID)
			if err != nil {
				return err
			}
			if origin.Role != string(constants.MessageRoleUser) || origin.SourceChatID != run.SourceChatID || origin.SourceExecutionID != run.SourceExecutionID {
				return status_error.ChatReplyTargetUnavailable
			}
		}
		_, _, err = s.append(ctx, models.MainConversationMessage{
			ID: publicMessageID("workflow-conclusion", run.ID, conv.ID), ConversationID: conv.ID,
			Role: string(constants.MessageRoleAssistant), Content: content, ReplyToMessageID: run.ConclusionReplyID,
			QuoteReply:   run.ConclusionReplyID != "",
			SourceChatID: run.SourceChatID, SourceExecutionID: run.SourceExecutionID, SourceWakerID: run.SourceWakerID,
			WorkflowRun: &models.MainConversationWorkflowRun{DefinitionID: run.DefinitionID, RunID: run.ID},
		}, conv.SubjectType, conv.SubjectID)
		return err
	})
}

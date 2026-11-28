package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// PublishAutomaticReport 以原运行身份提交可见报告，重投返回同一已提交消息。
func (s *Service) PublishAutomaticReport(ctx context.Context, kind, subjectID, wakerID, runID, content string) (models.MainConversationMessage, error) {
	return s.publishAutomaticReport(ctx, kind, subjectID, wakerID, runID, content, "", "")
}

// PublishTrackedEventReport 保留首个追踪报告与同一工单来源的引用。
func (s *Service) PublishTrackedEventReport(ctx context.Context, run models.AutomaticRun, content string) (models.MainConversationMessage, error) {
	return s.publishAutomaticReport(ctx, "waker", run.WakerID, run.WakerID, run.ID, content, run.ChatID, run.ExecutionID)
}

func (s *Service) publishAutomaticReport(ctx context.Context, kind, subjectID, wakerID, runID, content, chatID, executionID string) (models.MainConversationMessage, error) {
	var result models.MainConversationMessage
	if wakerID == "" || runID == "" || strings.TrimSpace(content) == "" {
		return result, status_error.BadRequest
	}
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		conv, err := s.Ensure(tx, kind, subjectID)
		if err != nil {
			return err
		}
		if chatID != "" {
			source, err := s.Source(tx, kind, subjectID, chatID)
			if err != nil {
				return err
			}
			if source.WakerID != wakerID || source.ConversationID != conv.ID || executionID == "" {
				return status_error.AgentOperationForbidden
			}
		}
		result, _, err = s.append(tx, models.MainConversationMessage{
			ID: AutomaticReportMessageID(runID), ConversationID: conv.ID,
			Role: string(constants.MessageRoleAssistant), Content: content, SourceWakerID: wakerID,
			SourceChatID: chatID, SourceExecutionID: executionID,
		}, kind, subjectID)
		return err
	})
	return result, err
}

// AutomaticReportMessageID 按原自动运行身份确定唯一公开报告。
func AutomaticReportMessageID(runID string) string {
	return publicMessageID("automatic-report", runID)
}

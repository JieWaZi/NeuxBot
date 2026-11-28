package schedule

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *Service) validateWorkflowReportRecipient(ctx context.Context, scopeWakerID string, task models.AutomaticTask, config models.AutomaticTaskConfiguration, validate bool) error {
	recipient := config.WorkflowReportRecipient
	if recipient == nil {
		return nil
	}
	invalid := status_error.BadRequest.WithField("field", "workflowReportRecipient")
	if config.TargetKind != constants.AutomaticTaskTargetKindWorkflow || config.Trigger != constants.AutomaticTaskTriggerSchedule {
		return invalid
	}
	if scopeWakerID != "" && (task.WorkflowReportRecipient == nil || *task.WorkflowReportRecipient != *recipient) {
		return invalid
	}
	if recipient.SubjectID == "" || len(recipient.SubjectID) > 32 || strings.TrimSpace(recipient.SubjectID) != recipient.SubjectID {
		return invalid
	}
	// 原配置仍可停用或维护。重新选定收件人时才验证可用性。
	changed := task.WorkflowReportRecipient == nil || *task.WorkflowReportRecipient != *recipient
	switch recipient.SubjectType {
	case "waker":
		if validate && changed {
			_, err := s.config.Wakers.Get(ctx, recipient.SubjectID)
			return err
		}
	case "team":
		if validate && changed {
			_, err := s.config.Channels.Get(ctx, recipient.SubjectID)
			return err
		}
	default:
		return invalid
	}
	return nil
}

func scheduledWorkflowReportRecipient(run models.AutomaticRun) *models.MainConversationRecipient {
	if run.Source != constants.AutomaticRunSourceSchedule {
		return nil
	}
	return run.Configuration.WorkflowReportRecipient
}

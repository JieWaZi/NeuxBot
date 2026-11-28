package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (c *workflowRunController) ListPendingConclusions(ctx context.Context, after string, limit int) ([]models.WorkflowRun, error) {
	rows := []models.WorkflowRun{}
	statuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	err := controllerDB(ctx, c.db).Select("id").
		Where("conclusion_requested = true AND conclusion_finished_at IS NULL AND completed_at IS NOT NULL").
		Where("id > ? AND status IN ?", after, statuses).Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_workflow_conclusions", "", err)
	}
	return rows, nil
}

func (c *workflowRunController) FinishConclusion(ctx context.Context, runID, errorCode string, at int64) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).
		Where("id = ? AND conclusion_requested = true AND completed_at IS NOT NULL", runID).
		Where("conclusion_finished_at IS NULL OR conclusion_delivery_error <> ''").
		UpdateColumns(map[string]any{"conclusion_finished_at": at, "conclusion_delivery_error": errorCode})
	if result.Error != nil {
		return databaseError("finish_workflow_conclusion", runID, result.Error)
	}
	return nil
}

func (c *workflowRunController) ListMainWorkflowRuns(ctx context.Context, conversationID string, messageIDs []string) ([]models.WorkflowRun, error) {
	rows := []models.WorkflowRun{}
	if len(messageIDs) == 0 {
		return rows, nil
	}
	err := controllerDB(ctx, c.db).Select("id", "definition_id", "conclusion_reply_id").
		Where("conclusion_main_id = ? AND conclusion_reply_id <> '' AND conclusion_reply_id IN ? AND history_deleted_at IS NULL", conversationID, messageIDs).
		Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_main_workflow_runs", conversationID, err)
	}
	return rows, nil
}

func (c *workflowRunController) publishMainWorkflowAdmission(ctx context.Context, run models.WorkflowRun) error {
	if run.ConclusionReplyID == "" {
		return nil
	}
	var subject models.MainConversation
	if err := controllerDB(ctx, c.db).Select("subject_type", "subject_id").Where("id = ?", run.ConclusionMainID).Limit(1).Find(&subject).Error; err != nil {
		return databaseError("find_workflow_main_origin", run.ConclusionMainID, err)
	}
	if subject.SubjectID != "" {
		publishCommittedChange(ctx, c.db, constants.MainConversationEventTopic, eventsResource(subject.SubjectType, subject.SubjectID))
	}
	return nil
}

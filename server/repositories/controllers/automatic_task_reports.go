package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (c *automaticTaskController) PendingReports(ctx context.Context, after string, limit int) ([]models.AutomaticRun, error) {
	var rows []models.AutomaticRun
	err := controllerDB(ctx, c.db).Where("report_state = 'pending' AND id > ?", after).Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_pending_automatic_reports", "", err)
	}
	return rows, nil
}

func (c *automaticTaskController) ReserveAgentReport(ctx context.Context, id, key, body string) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).
		Where("id = ? AND report_by_agent = ? AND report_state = ? AND agent_report_key = ''", id, true, constants.AutomaticReportStatePending).
		UpdateColumns(map[string]any{"agent_report_key": key, "agent_report_body": body})
	if result.Error != nil {
		return false, databaseError("reserve_automatic_agent_report", id, result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (c *automaticTaskController) FinishReport(ctx context.Context, id string, state constants.AutomaticReportState, href string) error {
	db := controllerDB(ctx, c.db)
	result := db.Model(&models.AutomaticRun{}).Where("id = ? AND report_state = 'pending'", id).
		UpdateColumns(map[string]any{"report_state": state, "report_href": href})
	if result.Error != nil {
		return databaseError("finish_automatic_report", "run_id="+id, result.Error)
	}
	if result.RowsAffected > 0 {
		publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	}
	return nil
}

func (c *automaticTaskController) QueuedReportRuns(ctx context.Context, afterTime int64, afterID string, limit int) ([]models.AutomaticRun, error) {
	var rows []models.AutomaticRun
	err := controllerDB(ctx, c.db).Where("awaiting_dispatch = 1 AND (created_at, id) > (?, ?)", afterTime, afterID).
		Order("created_at ASC, id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_queued_report_runs", "", err)
	}
	return rows, nil
}

func (c *automaticTaskController) ClearReportRunQueue(ctx context.Context, id string) error {
	result := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).Where("id = ? AND awaiting_dispatch = 1", id).
		UpdateColumn("awaiting_dispatch", false)
	if result.Error != nil {
		return databaseError("consume_queued_report_run", "run_id="+id, result.Error)
	}
	return nil
}

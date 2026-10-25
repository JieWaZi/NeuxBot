package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func (c *automaticTaskController) FindWorkflowTriggerReceipt(ctx context.Context, workflowID, requestID string) (models.WorkflowTriggerReceipt, bool, error) {
	var value models.WorkflowTriggerReceipt
	err := controllerDB(ctx, c.db).Where("workflow_id = ? AND request_id = ?", workflowID, requestID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, false, nil
	}
	if err != nil {
		return value, false, databaseError("get_workflow_trigger_receipt", workflowID, err)
	}
	return value, true, nil
}

func (c *automaticTaskController) CreateWorkflowTriggerReceipt(ctx context.Context, value models.WorkflowTriggerReceipt) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_trigger_receipt", value.WorkflowID, err)
	}
	return nil
}

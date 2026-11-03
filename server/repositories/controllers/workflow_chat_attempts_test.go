package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestWorkflowChatAttemptsKeepOriginalExecutionIdentity(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkflowRunController(db)
	ctx := context.Background()
	for _, value := range []models.WorkflowEffect{{ID: "original", ChatID: "worker-chat", RunID: "run-a", InstanceID: "instance-000"}, {ID: "retry", ChatID: "new-worker-chat", RunID: "run-a", InstanceID: "instance-000"}} {
		if err := db.Create(&value).Error; err != nil {
			t.Fatal(err)
		}
	}
	effects, err := controller.ListChatAttempts(ctx, "worker-chat")
	if err != nil || len(effects) != 1 || effects[0].ID != "original" {
		t.Fatal("retry replaced original chat identity", effects, err)
	}
}

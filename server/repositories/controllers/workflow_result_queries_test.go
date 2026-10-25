package controllers

import (
	"context"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestWorkflowResultQueriesKeepNodePagingAndChatAttemptIdentity(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkflowRunController(db)
	ctx := context.Background()
	for index := 0; index < 205; index++ {
		value := models.WorkflowNodeInstance{ID: fmt.Sprintf("instance-%03d", index), RunID: "run-a", NodeID: "worker-a", CreatedAt: int64(index + 1)}
		if err := db.Create(&value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []models.WorkflowNodeInstance{{ID: "other-node", RunID: "run-a", NodeID: "worker-b"}, {ID: "other-run", RunID: "run-b", NodeID: "worker-a"}} {
		if err := db.Create(&value).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := controller.ListNodeInstances(ctx, "run-a", "worker-a", 20, 200)
	if err != nil || total != 205 || len(rows) != 5 || rows[4].ID != "instance-204" {
		t.Fatalf("truncated or cross-scope instances: %+v %d %v", rows, total, err)
	}
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

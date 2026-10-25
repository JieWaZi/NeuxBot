package controllers

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWorkflowCreationDeleteRelationshipContract(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkflowCreationController(db)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	ctx := context.Background()
	for _, creation := range []models.WorkflowCreationSession{{ID: "creation-a", DefinitionID: "workflow-a", ChatID: "chat-a"}, {ID: "creation-b", DefinitionID: "workflow-a", ChatID: "chat-b"}, {ID: "creation-c", DefinitionID: "workflow-b", ChatID: "chat-c"}} {
		if err := controller.CreateCreationSession(ctx, creation); err != nil {
			t.Fatal(err)
		}
	}
	err := transactions.Transaction(ctx, func(ctx context.Context) error {
		if err := controller.DeleteCreationSessionsByChatID(ctx, "chat-a"); err != nil {
			return err
		}
		return status_error.WorkflowStateConflict
	})
	if err == nil {
		t.Fatal("expected deletion rollback")
	}
	if _, err := controller.GetCreationSession(ctx, "creation-a"); err != nil {
		t.Fatalf("rollback removed relationship: %v", err)
	}
	if err := controller.DeleteCreationSessionsByChatID(ctx, "chat-a"); err != nil {
		t.Fatal(err)
	}
	creationSessions, err := controller.ListCreationSessions(ctx, "workflow-a")
	if err != nil || len(creationSessions) != 1 || creationSessions[0].ID != "creation-b" {
		t.Fatalf("scoped deletion: %+v %v", creationSessions, err)
	}
	if _, err := controller.GetCreationSession(ctx, "creation-c"); err != nil {
		t.Fatalf("another workflow was modified: %v", err)
	}
	if err := controller.DeleteCreationSessionsByChatID(ctx, "chat-a"); err != nil {
		t.Fatalf("repeated cleanup: %v", err)
	}
}

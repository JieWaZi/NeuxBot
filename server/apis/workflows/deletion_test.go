package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestDeleteProtectsActiveRunAndRetainsHistoricalRevision(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	ctx := context.Background()
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	if err := deleteDefinition(ctx, workflowAPIDependencies(s), definition.ID, definition.CurrentRevisionID); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("active definition deleted", err)
	}
	if err := s.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	if err := deleteDefinition(ctx, workflowAPIDependencies(s), definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal(err)
	}
	if err := deleteDefinition(ctx, workflowAPIDependencies(s), definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal("repeat delete failed", err)
	}
	if _, err := s.Store.GetDefinition(ctx, definition.ID); workflowstate.ErrorKey(err) != status_error.WorkflowNotFound.Key() {
		t.Fatal("resource remains", err)
	}
	if _, err := s.Store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal("historical evidence deleted", err)
	}
	if _, err := s.Runs.GetRun(ctx, run.ID); err != nil {
		t.Fatal("old run deleted", err)
	}
}

func TestDeleteRevisionRemovesHistoryButPreservesPinnedRun(t *testing.T) {
	s, store := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	ctx := context.Background()
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "pinned"})
	if err != nil {
		t.Fatal(err)
	}
	newVersion, _, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "second", ExpectedRevisionID: definition.CurrentRevisionID, Source: strings.Replace(humanSource, "人工关卡", "新版人工关卡", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteRevision(ctx, workflowAPIDependencies(s), definition.ID, newVersion.RevisionID); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("current version deleted", err)
	}
	if err := deleteRevision(ctx, workflowAPIDependencies(s), definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal(err)
	}
	if err := deleteRevision(ctx, workflowAPIDependencies(s), definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal("repeat deletion failed", err)
	}
	history, err := store.ListRevisions(ctx, definition.ID, 0, 50)
	if err != nil || len(history) != 1 || history[0].ID != newVersion.RevisionID {
		t.Fatal("deleted history remains", history, err)
	}
	old, err := store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
	if err != nil || old.Source != humanSource || old.DeletedAt == nil {
		t.Fatal("pinned program lost", err)
	}
	runTicks(t, s, run.ID, 2)
	pinned, err := s.Runs.GetRun(ctx, run.ID)
	if err != nil || pinned.RevisionID != old.ID || pinned.Status == constants.WorkflowRunStatusFailed {
		t.Fatal("old run lost version", pinned, err)
	}
	other, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "other-definition", Title: "另一个工作流"})
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteRevision(ctx, workflowAPIDependencies(s), other.ID, old.ID); workflowstate.ErrorKey(err) != status_error.WorkflowNotFound.Key() {
		t.Fatal("cross workflow deletion accepted", err)
	}
}

func TestDeleteRunHistoryProtectsActiveRunAndPreservesIdempotency(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	ctx := context.Background()
	d := workflowAPIDependencies(s)
	command := workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run-history"}
	run, err := s.StartRun(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	if err := deleteRunHistory(ctx, d, definition.ID, run.ID); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("active run removed", err)
	}
	if err := s.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	if err := deleteRunHistory(ctx, d, "another-workflow", run.ID); workflowstate.ErrorKey(err) != status_error.WorkflowNotFound.Key() {
		t.Fatal("cross workflow deletion accepted", err)
	}
	before, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := deleteRunHistory(ctx, d, definition.ID, run.ID); err != nil {
			t.Fatal("idempotent history removal failed", err)
		}
	}
	history, err := s.Runs.ListRuns(ctx, definition.ID, 0, "", 50)
	if err != nil || len(history) != 0 {
		t.Fatal("removed history still visible", history, err)
	}
	after, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil || after.Run.HistoryDeletedAt == nil || after.Run.Status != before.Run.Status || after.Run.Version != before.Run.Version || len(after.Fibers) != len(before.Fibers) || len(after.Humans) != len(before.Humans) {
		t.Fatal("execution facts lost", after, err)
	}
	repeated, err := s.StartRun(ctx, command)
	if err != nil || repeated.ID != run.ID {
		t.Fatal("removed history caused duplicate admission", repeated, err)
	}
	events, err := s.Store.ListEvents(ctx, definition.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	deleted := 0
	for _, event := range events {
		if event.Type == constants.WorkflowRunHistoryDeletedEvent {
			deleted++
		}
	}
	if deleted != 1 {
		t.Fatal("repeated deletion emitted duplicate events", deleted)
	}
}

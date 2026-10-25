package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestSourceSaveReplayConflictAndImmutableRestore(t *testing.T) {
	s, store := workflowTestService(t)
	ctx := context.Background()
	command := CreateDefinitionCommand{RequestID: "create-1", Title: "待创作生成"}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := createDefinition(ctx, workflowAPIDependencies(s), command)
	if err != nil || replay.ID != definition.ID {
		t.Fatalf("create replay: %+v %v", replay, err)
	}
	command.Title = "不同名称"
	if _, err := createDefinition(ctx, workflowAPIDependencies(s), command); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatalf("create conflict: %v", err)
	}
	save := workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save-1", Source: workflowTestSource(t), SDKVersion: constants.WorkflowSDKVersion}
	first, diagnostics, err := s.SaveSource(ctx, save)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	same, _, err := s.SaveSource(ctx, save)
	if err != nil || same.RevisionID != first.RevisionID || !same.Replayed {
		t.Fatalf("save replay: %+v %v", same, err)
	}
	save.Source = strings.Replace(save.Source, "纯计算验收", "修改名称", 1)
	if _, _, err := s.SaveSource(ctx, save); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatalf("same request with different payload: %v", err)
	}
	save.RequestID = "save-conflict"
	if _, _, err := s.SaveSource(ctx, save); !errors.Is(err, status_error.WorkflowRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	save.RequestID = "save-2"
	save.ExpectedRevisionID = first.RevisionID
	second, _, err := s.SaveSource(ctx, save)
	if err != nil {
		t.Fatal(err)
	}
	if second.Ordinal != 2 {
		t.Fatalf("ordinal %+v", second)
	}
	old, err := store.GetRevision(ctx, definition.ID, first.RevisionID)
	if err != nil || strings.Contains(old.Source, "修改名称") || old.Manifest.Meta.Title != "纯计算验收" {
		t.Fatal("history was mutated")
	}
	restored, _, err := restoreSource(ctx, workflowAPIDependencies(s), definition.ID, first.RevisionID, second.RevisionID, "restore-1")
	if err != nil || restored.Ordinal != 3 || restored.RevisionID == first.RevisionID {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	revision, err := store.GetRevision(ctx, definition.ID, restored.RevisionID)
	if err != nil || revision.RestoredFromRevisionID != first.RevisionID || revision.SourceDigest != first.SourceDigest {
		t.Fatal("restore lost source evidence")
	}
	events, err := store.ListEvents(ctx, definition.ID, 0, 100)
	if err != nil || len(events) != 4 {
		t.Fatalf("events: %+v %v", events, err)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatal("event stream has a gap")
		}
	}
}

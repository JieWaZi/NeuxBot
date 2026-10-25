package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

func TestUserConfigurationEditCreatesSourceVersionAndRejectsStaleForm(t *testing.T) {
	s, store := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	ctx := context.Background()
	before, err := store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	command := EditConfigurationCommand{DefinitionID: definition.ID, RequestID: "rename-manually", ExpectedRevisionID: before.ID, Edit: workflowdsl.SourceEdit{Target: "meta", Fields: map[string]any{"title": "用户自己修改的标题", "description": "直接修改生成内容"}}}
	receipt, diagnostics, err := editConfiguration(ctx, workflowAPIDependencies(s), command)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	after, err := store.GetRevision(ctx, definition.ID, receipt.RevisionID)
	if err != nil || after.Manifest.Meta.Title != "用户自己修改的标题" || after.Source == before.Source || after.Ordinal != before.Ordinal+1 {
		t.Fatalf("configuration was not saved in a source revision: %+v %v", after, err)
	}
	head, err := store.GetDefinition(ctx, definition.ID)
	if err != nil || head.Title != after.Manifest.Meta.Title {
		t.Fatalf("title projection did not follow source: %+v %v", head, err)
	}
	replayed, _, err := editConfiguration(ctx, workflowAPIDependencies(s), command)
	if err != nil || !replayed.Replayed || replayed.RevisionID != receipt.RevisionID {
		t.Fatalf("edit retry duplicated a revision: %+v %v", replayed, err)
	}
	command.RequestID = "stale-form"
	command.Edit.Fields["title"] = "旧窗口覆盖"
	if _, _, err := editConfiguration(ctx, workflowAPIDependencies(s), command); !errors.Is(err, status_error.WorkflowRevisionConflict) {
		t.Fatalf("accepted stale configuration: %v", err)
	}
	immutable, err := store.GetRevision(ctx, definition.ID, before.ID)
	if err != nil || immutable.Source != before.Source {
		t.Fatal("editing changed the original revision")
	}
}

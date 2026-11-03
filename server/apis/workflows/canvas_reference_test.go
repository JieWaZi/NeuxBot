package workflows

import (
	"context"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"testing"
)

func TestCanvasReferencesResolveAuthoritativeRevisionLabels(t *testing.T) {
	s, store := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	ctx := context.Background()
	current, _ := store.GetDefinition(ctx, definition.ID)
	revision, err := store.GetRevision(ctx, definition.ID, current.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	phase := revision.Manifest.Meta.Phases[0]
	node := revision.Manifest.Nodes[0]
	references := []CanvasReference{{RevisionID: revision.ID, Kind: "phase", ID: phase.ID}, {RevisionID: revision.ID, Kind: "node", ID: node.ID}}
	resolved, err := resolveCanvasReferences(ctx, workflowAPIDependencies(s), definition.ID, references)
	if err != nil || len(resolved) != 2 || resolved[0]["label"] != phase.Title || resolved[1]["label"] != node.Label {
		t.Fatal(resolved, err)
	}
	references[0].ID = "missing"
	if _, err = resolveCanvasReferences(ctx, workflowAPIDependencies(s), definition.ID, references); err == nil {
		t.Fatal("nonexistent target accepted")
	}
	references[0].ID = phase.ID
	another, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "other-definition", Title: "其他"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolveCanvasReferences(ctx, workflowAPIDependencies(s), another.ID, references); err == nil {
		t.Fatal("cross-definition reference accepted")
	}
	before := SendCreationMessageCommand{Content: "修改", CanvasReferences: references}
	after := before
	after.CanvasReferences = nil
	if workflowstate.Digest(before) == workflowstate.Digest(after) {
		t.Fatal("removing references did not change idempotency payload")
	}
}

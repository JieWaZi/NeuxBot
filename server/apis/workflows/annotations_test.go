package workflows

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestAnnotationReplayAndSourceReceiptResolveExactlyOnce(t *testing.T) {
	s, store := workflowTestService(t)
	ctx := context.Background()
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	head, err := store.GetDefinition(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.GetRevision(ctx, definition.ID, head.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	command := CreateAnnotationCommand{DefinitionID: definition.ID, RequestID: "annotation-request", BaseRevisionID: revision.ID, NodeID: revision.Manifest.Meta.Phases[0].ID, Text: "保留本阶段职责，补充说明"}
	first, err := createAnnotation(ctx, workflowAPIDependencies(s), command)
	if err != nil || first.CreatedAt == 0 {
		t.Fatalf("create annotation: %+v %v", first, err)
	}
	second, err := createAnnotation(ctx, workflowAPIDependencies(s), command)
	if err != nil || first.ID != second.ID {
		t.Fatalf("replayed annotation changed identity: %+v %v", second, err)
	}
	command.Text = "另一要求"
	if _, err := createAnnotation(ctx, workflowAPIDependencies(s), command); err == nil {
		t.Fatal("request identity accepted changed content")
	}
	command.RequestID, command.NodeID = "missing-target", "nonexistent"
	if _, err := createAnnotation(ctx, workflowAPIDependencies(s), command); err == nil {
		t.Fatal("missing source target accepted")
	}
	annotations, err := s.CreationSessions.ListAnnotations(ctx, definition.ID)
	if err != nil || len(annotations) != 1 || annotations[0].ResolvedRevisionID != "" {
		t.Fatalf("invalid creation changed annotations: %+v %v", annotations, err)
	}
	saved, _, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "resolve-unchanged", ExpectedRevisionID: revision.ID, Source: revision.Source, SDKVersion: constants.WorkflowSDKVersion, ResolvedAnnotationIDs: []string{first.ID}})
	if err != nil || !saved.Unchanged || saved.RevisionID != revision.ID {
		t.Fatalf("unchanged source failed to record resolution: %+v %v", saved, err)
	}
	annotations, err = s.CreationSessions.ListAnnotations(ctx, definition.ID)
	if err != nil || annotations[0].ResolvedRevisionID != revision.ID {
		t.Fatalf("save did not resolve bound annotation: %+v %v", annotations, err)
	}
	events, err := store.ListEvents(ctx, definition.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Type == constants.WorkflowAnnotationChangedEvent {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("annotation create/resolve events missing or duplicated: %+v", events)
	}
}

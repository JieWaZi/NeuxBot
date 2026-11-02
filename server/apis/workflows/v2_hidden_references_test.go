package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestV2CreationReferencesStayHiddenAndPersistForReplay(t *testing.T) {
	runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 3)}
	s, close := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	defer close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	revision, err := s.Store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "reference-run", Args: map[string]any{"text": "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "referenced-turn", Content: "请调整选中的步骤", CanvasReferences: []CanvasReference{{RevisionID: revision.ID, Kind: "node", ID: revision.Manifest.Nodes[0].ID}}, RunReference: &RunReference{RunID: run.ID, RevisionID: revision.ID}, Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture"}}
	session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil {
		t.Fatal(err)
	}
	var request fixtureInvocation
	select {
	case request = <-runtime.requests:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	persisted, err := s.CreationSessions.GetCreationSession(ctx, session.ID)
	if err != nil || persisted.Content != command.Content || len(persisted.ContextInstructions) != 2 {
		t.Fatalf("reference context polluted visible content or was not persisted: %+v %v", persisted, err)
	}
	for _, instruction := range persisted.ContextInstructions {
		if !strings.Contains(instruction.Content, revision.ID) || !strings.Contains(request.Message.Text, instruction.Name) {
			t.Fatalf("model lost current-turn reference: %+v", instruction)
		}
		for _, actual := range request.Instructions {
			if actual.Name == instruction.Name {
				t.Fatal("per-turn reference changed the stable native session plan")
			}
		}
	}
	if !strings.Contains(request.Message.Text, "<workflow-reference-context>") || !strings.Contains(request.Message.Text, revision.ID) || !strings.Contains(request.Message.Text, run.ID) || !strings.Contains(request.Message.Text, revision.Manifest.Nodes[0].ID) {
		t.Fatal("actual runtime prompt lost current-turn reference targets")
	}
	history, err := s.Chats.ReadMessageHistory(ctx, session.ChatID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundUser := false
	for _, message := range history.Resources {
		if message.Role == "user" {
			foundUser = true
			if message.Content != command.Content {
				t.Fatalf("visible user message contains internal references: %q", message.Content)
			}
		}
	}
	if !foundUser {
		t.Fatal("missing authoritative user message")
	}
	public, _ := json.Marshal(persisted)
	if strings.Contains(string(public), "contextInstructions") || strings.Contains(string(public), "WORKFLOW_RUN_REFERENCE") {
		t.Fatal("hidden context exposed through session DTO")
	}
	replayed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || replayed.ActiveExecutionID != session.ActiveExecutionID || len(replayed.ContextInstructions) != 2 {
		t.Fatal("replay lost frozen context", err)
	}
	for {
		completed, err := s.Chats.ReadExecution(ctx, session.ChatID, session.AcceptedExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if completed.CompletedAt != nil {
			break
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	command.SessionID, command.RequestID, command.Content = session.ID, "without-reference", "继续调整"
	command.CanvasReferences, command.RunReference = nil, nil
	next, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || len(next.ContextInstructions) != 0 || next.Content != command.Content {
		t.Fatal("removed references survived into next turn", err)
	}
	select {
	case nextRequest := <-runtime.requests:
		if !strings.Contains(nextRequest.Message.Text, "<workflow-reference-context>") || !strings.Contains(nextRequest.Message.Text, "\n[]\n</workflow-reference-context>") {
			t.Fatal("native continuation did not explicitly clear prior-turn references")
		}
		for _, instruction := range nextRequest.Instructions {
			if instruction.Name == "WORKFLOW_CANVAS_REFERENCES.md" || instruction.Name == "WORKFLOW_RUN_REFERENCE.md" {
				t.Fatal("old references reinjected after removal")
			}
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

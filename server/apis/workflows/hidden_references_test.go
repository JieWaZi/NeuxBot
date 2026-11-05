package workflows

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestCreationReferencesStayHiddenAndPersistForReplay(t *testing.T) {
	runtime := &fixtureRuntime{}
	s, close := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	defer close()
	ctx := t.Context()
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
	boundary := &creationInputBoundary{Service: s.Executions, err: status_error.RuntimePolicyUnsupported}
	s.Executions = boundary
	session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatal(err)
	}
	persisted, err := s.CreationSessions.GetCreationSession(ctx, session.ID)
	if err != nil || persisted.Content != command.Content || len(persisted.ContextInstructions) != 2 {
		t.Fatalf("reference context polluted visible content or was not persisted: %+v %v", persisted, err)
	}
	if boundary.intent.Input.Content != command.Content || len(boundary.intent.Input.ContextInstructions) != 3 {
		t.Fatalf("reference context was not isolated from visible input: %+v", boundary.intent.Input)
	}
	for index, instruction := range persisted.ContextInstructions {
		if !strings.Contains(instruction.Content, revision.ID) || boundary.intent.Input.ContextInstructions[index] != instruction {
			t.Fatalf("execution intent lost a current-turn reference: %+v", instruction)
		}
	}
	public, _ := json.Marshal(persisted)
	if strings.Contains(string(public), "contextInstructions") || strings.Contains(string(public), "WORKFLOW_RUN_REFERENCE") {
		t.Fatal("hidden context exposed through session DTO")
	}
	replayed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || replayed.ActiveExecutionID != session.ActiveExecutionID || len(replayed.ContextInstructions) != 2 {
		t.Fatal("replay lost frozen context", err)
	}
	command.SessionID, command.RequestID, command.Content = session.ID, "without-reference", "继续调整"
	command.CanvasReferences, command.RunReference = nil, nil
	next, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || len(next.ContextInstructions) != 0 || next.Content != command.Content {
		t.Fatal("removed references survived into next turn", err)
	}
	for _, instruction := range boundary.intent.Input.ContextInstructions {
		if strings.Contains(instruction.Name, "WORKFLOW_CANVAS_REFERENCES") || strings.Contains(instruction.Name, "WORKFLOW_RUN_REFERENCE") {
			t.Fatal("old references reinjected after removal")
		}
	}
}

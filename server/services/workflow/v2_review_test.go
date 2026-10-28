package workflow

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"path/filepath"
	"testing"
)

func TestV2RepairFailureCannotRedispatchBusiness(t *testing.T) {
	effect := models.WorkflowEffect{Status: constants.WorkflowStepStatusFailed, ExecutionStatus: constants.ExecutionStatusCompleted, AttemptGeneration: 1, Boundary: workflowdsl.EffectBoundary{Primitive: "worker", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("read"), workflowdsl.JSONValue(map[string]any{"schema": map[string]any{"type": "object"}, "retry": map[string]any{"maxAttempts": float64(4), "safety": "pure"}})}}}
	state := controllers.WorkflowRunState{Run: models.WorkflowRun{Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: true}}}
	if prepareWorkerRetry(&state, 0, effect) {
		t.Fatal("format failure retried business execution")
	}
}
func TestV2CandidateFileFreezesAndRejectsEscapes(t *testing.T) {
	directory, _ := filepath.EvalSymlinks(t.TempDir())
	path := filepath.Join(directory, "choices.json")
	if err := os.WriteFile(path, []byte(`[{"value":"a","label":"甲"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	options := map[string]any{"kind": "select", "choices": map[string]any{"fileRef": path}}
	if err := freezeHumanChoices(directory, options); err != nil {
		t.Fatal(err)
	}
	if err := validateHumanSnapshot(options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if len(options["choices"].([]any)) != 1 || options["choicesFile"] == nil {
		t.Fatal("snapshot changed")
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if freezeHumanChoices(directory, map[string]any{"choices": map[string]any{"fileRef": outside}}) == nil {
		t.Fatal("outside file accepted")
	}
}

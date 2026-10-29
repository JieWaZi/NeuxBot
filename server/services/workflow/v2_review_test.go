package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
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

func TestHumanCandidateFilesReadInternalWorkspaceWithExternalBusinessScope(t *testing.T) {
	for _, location := range []string{"internal", "business", "outside", "symlink"} {
		t.Run(location, func(t *testing.T) {
			s, _ := workflowTestService(t)
			business, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.json")
			path := filepath.Join(business, "choices.json")
			expression := "flowContext.workspace.runDirectory+'/choices.json'"
			if location == "business" || location == "outside" {
				if location == "outside" {
					path = outside
				}
				encoded, _ := json.Marshal(path)
				expression = string(encoded)
			}
			source := runtimeV2Meta + `return await askUser('选择候选',{id:'review',label:'选择候选',phase:'work',kind:'select',choices:{fileRef:` + expression + `}});`
			definition := scopedProcessDefinition(t, s, source, business)
			run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "candidate"})
			if err != nil {
				t.Fatal(err)
			}
			workspace, err := runWorkspace(run)
			if err != nil {
				t.Fatal(err)
			}
			if location == "internal" || location == "symlink" {
				path = filepath.Join(workspace.RunDirectory, "choices.json")
			}
			if location == "symlink" {
				if err := os.WriteFile(outside, []byte(`[{"value":"a","label":"甲"}]`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(`[{"value":"a","label":"甲"}]`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
				return state.Run.Status == constants.WorkflowRunStatusWaiting || workflowstate.TerminalRun(state.Run.Status)
			})
			if location == "outside" || location == "symlink" {
				if state.Run.Status != constants.WorkflowRunStatusFailed || len(state.Humans) != 0 {
					t.Fatalf("candidate path expanded the frozen scope: %+v", state.Run)
				}
				return
			}
			if state.Run.Status != constants.WorkflowRunStatusWaiting || len(state.Humans) != 1 {
				t.Fatalf("authorized candidate did not create a question: %+v", state.Run)
			}
			choices := state.Humans[0].Options["choices"].([]any)
			if len(choices) != 1 || choices[0].(map[string]any)["value"] != "a" || choices[0].(map[string]any)["label"] != "甲" {
				t.Fatalf("question did not freeze actual candidate bytes: %+v", choices)
			}
		})
	}
}

func TestLocalOperationDescriptorUsesActualRunDirectory(t *testing.T) {
	s, _ := workflowTestService(t)
	business, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	definition := scopedProcessDefinition(t, s, scriptSource(t, "cwd.txt", false), business)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "operation-cwd"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := runWorkspace(run)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := s.store.GetRevision(context.Background(), run.DefinitionID, run.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]any{"args": []any{"-test.run=^TestWorkflowLocalProcessHelper$", "--", "cwd.txt"}, "env": map[string]any{"ALLY_WORKFLOW_PROCESS_TEST": "1"}}
	effect := models.WorkflowEffect{Boundary: workflowdsl.EffectBoundary{Primitive: "action", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("write"), workflowdsl.JSONValue(options)}}}
	_, descriptor, required, err := operationDescription(revision.Manifest, run, effect)
	if err != nil || !required || descriptor["workingDirectory"] != workspace.RunDirectory {
		t.Fatalf("local approval describes a different process directory: %+v %v", descriptor, err)
	}
	if _, err := s.executeScript(context.Background(), run, revision.Manifest.Meta.Actions[0], options); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(descriptor["workingDirectory"].(string), "cwd.txt")); err != nil || string(body) != "ALLY ALLY!" {
		t.Fatalf("actual local process did not use its approved cwd: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(business, "cwd.txt")); !os.IsNotExist(err) {
		t.Fatal("local process used the business scope as its cwd")
	}
	python := models.WorkflowEffect{Boundary: workflowdsl.EffectBoundary{Primitive: "functionTask", Arguments: []workflowdsl.Value{workflowdsl.JSONValue(map[string]any{"language": "python", "executionMode": "trusted_local", "code": "def main(input): return input"})}}}
	_, descriptor, required, err = operationDescription(revision.Manifest, run, python)
	if err != nil || !required || descriptor["workingDirectory"] != workspace.RunDirectory || run.ScopeBinding.WorkingDirectory != business {
		t.Fatalf("Python approval lost the actual cwd or original business authorization: %+v %v", descriptor, err)
	}
	for _, kind := range []string{"http", "builtin"} {
		declaration := workflowdsl.ActionDeclaration{ID: kind, Type: kind, Risk: "write"}
		manifest := workflowdsl.Manifest{Meta: workflowdsl.Meta{Actions: []workflowdsl.ActionDeclaration{declaration}}}
		nonlocal := models.WorkflowEffect{Boundary: workflowdsl.EffectBoundary{Primitive: "action", Arguments: []workflowdsl.Value{workflowdsl.JSONValue(kind), workflowdsl.JSONValue(map[string]any{})}}}
		_, descriptor, _, err := operationDescription(manifest, run, nonlocal)
		if err != nil || descriptor["workingDirectory"] != business {
			t.Fatalf("non-process authorization scope was relabeled as process cwd: %+v %v", descriptor, err)
		}
	}
	run.Context = nil
	for _, local := range []models.WorkflowEffect{effect, python} {
		_, _, _, err := operationDescription(revision.Manifest, run, local)
		var failure interface{ Key() string }
		if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
			t.Fatalf("local approval accepted missing frozen workspace: %v", err)
		}
	}
}
func TestV2CandidateFileFreezesAndRejectsEscapes(t *testing.T) {
	directory, _ := filepath.EvalSymlinks(t.TempDir())
	s, _ := workflowTestService(t)
	definition := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, directory)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "candidate-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "choices.json")
	if err := os.WriteFile(path, []byte(`[{"value":"a","label":"甲"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	options := map[string]any{"kind": "select", "choices": map[string]any{"fileRef": path}}
	if err := freezeHumanChoices(run, options); err != nil {
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
	if freezeHumanChoices(run, map[string]any{"choices": map[string]any{"fileRef": outside}}) == nil {
		t.Fatal("outside file accepted")
	}
	run.Context = nil
	err = freezeHumanChoices(run, map[string]any{"choices": map[string]any{"fileRef": path}})
	var failure interface{ Key() string }
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
		t.Fatalf("candidate reader fell back to business scope without a workspace: %v", err)
	}
}

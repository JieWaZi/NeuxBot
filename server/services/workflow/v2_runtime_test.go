package workflow

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const runtimeV2Meta = `export const meta={sdk:'ally.workflow/2',title:'V2',description:'runtime',phases:[{id:'work',title:'执行',detail:'流程'}]};`

func TestV2SourcePatchCASAndReplaySQLite(t *testing.T) {
	s, store := workflowTestService(t)
	ctx := context.Background()
	definition := savedTestWorkflow(t, s, runtimeV2Meta+`return 'first';`)
	base, _ := store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
	command := SaveSourceCommand{DefinitionID: definition.ID, RequestID: "patch", ExpectedRevisionID: base.ID, BaseSourceDigest: base.SourceDigest, Patches: []SourcePatch{{Search: "'first'", Replace: "'second'"}}, SDKVersion: constants.WorkflowSDKVersion}
	first, _, err := s.SaveSource(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := s.SaveSource(ctx, command)
	if err != nil || replay.RevisionID != first.RevisionID || !replay.Replayed {
		t.Fatal(replay, err)
	}
	command.RequestID = "stale"
	if _, _, err = s.SaveSource(ctx, command); err == nil {
		t.Fatal("stale CAS accepted")
	}
	revision, _ := store.GetRevision(ctx, definition.ID, first.RevisionID)
	command = SaveSourceCommand{DefinitionID: definition.ID, RequestID: "same", ExpectedRevisionID: first.RevisionID, Source: revision.Source, SDKVersion: constants.WorkflowSDKVersion}
	next, _, err := s.SaveSource(ctx, command)
	if err != nil || next.RevisionID == first.RevisionID {
		t.Fatal(next, err)
	}
}
func TestV2PythonJSONStdinAndStrictProtocol(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	s, _ := workflowTestService(t)
	s.pythonExecutable = python
	directory, _ := filepath.EvalSymlinks(t.TempDir())
	for _, input := range []any{nil, false, float64(0), "", map[string]any{"值": "中文"}} {
		code := "import json,sys\njson.dump(json.load(sys.stdin),sys.stdout)\n"
		bundle := workflowdsl.ProgramBundle{Scripts: map[string]workflowdsl.ScriptDefinition{"echo": {Language: "python", Protocol: "json", Code: code, Digest: workflowdsl.Digest([]byte(code))}}}
		value, err := s.executeScript(context.Background(), models.WorkflowRun{ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}}, workflowdsl.ActionDeclaration{ScriptID: "echo"}, map[string]any{"input": input}, bundle)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value.(map[string]any)["data"], input) {
			t.Fatalf("stdin changed: %#v", value)
		}
	}
	code := "print('true false')\n"
	bundle := workflowdsl.ProgramBundle{Scripts: map[string]workflowdsl.ScriptDefinition{"bad": {Language: "python", Protocol: "json", Code: code, Digest: workflowdsl.Digest([]byte(code))}}}
	if _, err := s.executeScript(context.Background(), models.WorkflowRun{ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}}, workflowdsl.ActionDeclaration{ScriptID: "bad"}, map[string]any{}, bundle); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}
func TestV2TypedHumanAnswers(t *testing.T) {
	options := map[string]any{"kind": "select", "choices": []any{map[string]any{"value": "a", "label": "A"}}}
	if ValidateHumanAnswer(options, "a") != nil || ValidateHumanAnswer(options, "A") == nil {
		t.Fatal("selection did not use stable key")
	}
	options["kind"] = "multiselect"
	for _, value := range []any{[]any{}, []any{"a", "a"}, []any{"unknown"}} {
		if ValidateHumanAnswer(options, value) == nil {
			t.Fatal("invalid selection accepted", value)
		}
	}
	options["minSelections"] = float64(0)
	if err := ValidateHumanAnswer(options, []any{}); err != nil {
		t.Fatal(err)
	}
	options["kind"] = "confirm"
	options["onTimeout"] = "default"
	options["defaultValue"] = true
	if validateHumanSnapshot(options) == nil {
		t.Fatal("automatic confirmation accepted")
	}
}

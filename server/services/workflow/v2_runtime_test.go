package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const runtimeV2Meta = `export const meta={sdk:'ally.workflow/2',title:'V2',description:'runtime',phases:[{id:'work',title:'执行',detail:'流程'}]};`

func TestRunWorkspaceUsesWorkflowIdentityAndPrivateRunDirectory(t *testing.T) {
	s, _ := workflowTestService(t)
	s.dataDirectory = t.TempDir()
	definition := savedTestWorkflow(t, s, runtimeV2Meta+`return true;`)
	first, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(s.dataDirectory)
	want := filepath.Join(root, "workflow-workspaces", definition.ID)
	if first.ScopeBinding.WorkingDirectory != want || second.ScopeBinding.WorkingDirectory != want {
		t.Fatalf("runs did not share workflow directory: %s %s", first.ScopeBinding.WorkingDirectory, second.ScopeBinding.WorkingDirectory)
	}
	for _, run := range []models.WorkflowRun{first, second} {
		workspace, _ := run.Context["workspace"].(map[string]any)
		if workspace["rootDirectory"] != want || workspace["runDirectory"] != filepath.Join(want, "runs", run.ID) {
			t.Fatalf("run workspace missing: %#v", workspace)
		}
	}
}

func TestRelativeOutputsUseRunDirectoryWithoutNameFallback(t *testing.T) {
	s, _ := workflowTestService(t)
	business, _ := filepath.EvalSymlinks(t.TempDir())
	definition := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, business)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "relative-output"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := run.Context["workspace"].(map[string]any)
	actual := filepath.Join(workspace["runDirectory"].(string), "report.txt")
	for path, content := range map[string]string{
		actual:                                "current run report",
		filepath.Join(business, "report.txt"): "old business report",
		filepath.Join(workspace["rootDirectory"].(string), "report.txt"): "old workflow report",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	schema := map[string]any{"type": "string", "format": "file"}
	files := InspectOutputFiles(run, schema, "report.txt")
	if len(files) != 1 || !files[0].Available {
		t.Fatalf("current run output unavailable: %+v", files)
	}
	file, _, err := OpenDeclaredOutputFile(run, schema, "report.txt", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(content) != "current run report" {
		t.Fatalf("relative output resolved against another directory: %q %v", content, err)
	}
	if err := os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	if files := InspectOutputFiles(run, schema, "report.txt"); len(files) != 1 || files[0].Available {
		t.Fatalf("missing run output fell back to a same-name file: %+v", files)
	}
	if file, _, err := OpenDeclaredOutputFile(run, schema, "report.txt", files[0].ID); err == nil {
		file.Close()
		t.Fatal("missing run output downloaded a same-name file")
	}
}

func TestRelativeChildResultsFreezeChildRunDirectory(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx := context.Background()
	business, _ := filepath.EvalSymlinks(t.TempDir())
	parent := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, business)
	child := savedTestWorkflow(t, s, strings.Replace(runtimeV2Meta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1)+`return 'report.txt';`)
	parentRun, err := s.StartRun(ctx, StartRunCommand{DefinitionID: parent.ID, RequestID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	childRun, err := s.StartRun(ctx, StartRunCommand{DefinitionID: child.ID, RequestID: "child", ParentRunID: parentRun.ID, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	childRun.Result = "report.txt"
	workspace := childRun.Context["workspace"].(map[string]any)
	actual := filepath.Join(workspace["runDirectory"].(string), "report.txt")
	for path, content := range map[string]string{
		actual:                                "child run report",
		filepath.Join(business, "report.txt"): "old business report",
		filepath.Join(workspace["rootDirectory"].(string), "report.txt"): "old workflow report",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	effect := models.WorkflowEffect{ID: "relative-child-result"}
	result, err := s.freezeChildResult(ctx, parentRun, effect, childRun)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(result.(string))
	if err != nil || string(content) != "child run report" {
		t.Fatalf("child result froze bytes from another directory: %q %v", content, err)
	}
	if err := os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	if _, err := s.freezeChildResult(ctx, parentRun, effect, childRun); err == nil {
		t.Fatal("missing child output fell back to a same-name file")
	}
}

func TestChildRunRelativeFileInputsUseParentRunDirectory(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx := context.Background()
	business, _ := filepath.EvalSymlinks(t.TempDir())
	parent := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, business)
	child := savedTestWorkflow(t, s, strings.Replace(runtimeV2Meta, "phases:", "inputSchema:{type:'string',format:'file'},phases:", 1)+`return args;`)
	parentRun, err := s.StartRun(ctx, StartRunCommand{DefinitionID: parent.ID, RequestID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := parentRun.Context["workspace"].(map[string]any)
	actual := filepath.Join(workspace["runDirectory"].(string), "report.txt")
	for path, content := range map[string]string{
		actual:                                "parent run report",
		filepath.Join(business, "report.txt"): "old business report",
		filepath.Join(workspace["rootDirectory"].(string), "report.txt"): "old workflow report",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := StartRunCommand{DefinitionID: child.ID, RequestID: "child", Args: "report.txt", ParentRunID: parentRun.ID, Depth: 1}
	childRun, err := s.StartRun(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(childRun.Args.(string))
	if err != nil || string(content) != "parent run report" {
		t.Fatalf("child input froze bytes from another directory: %q %v", content, err)
	}
	if err := os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	command.RequestID = "missing-parent-output"
	if _, err := s.StartRun(ctx, command); err == nil {
		t.Fatal("missing parent output fell back to a same-name file")
	}
}

func TestChildRunFreezesParentInternalFileWithExplicitBusinessScope(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx := context.Background()
	business, _ := filepath.EvalSymlinks(t.TempDir())
	source := strings.Replace(runtimeV2Meta, "phases:", "inputSchema:{type:'string',format:'file'},phases:", 1) + `return args;`
	parent := scopedProcessDefinition(t, s, source, business)
	child := savedTestWorkflow(t, s, source)
	upload, err := s.UploadFile(ctx, parent.ID, "", "input.txt", bytes.NewBufferString("frozen parent input"))
	if err != nil {
		t.Fatal(err)
	}
	parentRun, err := s.StartRun(ctx, StartRunCommand{DefinitionID: parent.ID, RequestID: "parent", Args: upload.Reference})
	if err != nil {
		t.Fatal(err)
	}
	childRun, err := s.StartRun(ctx, StartRunCommand{DefinitionID: child.ID, RequestID: "child", Args: parentRun.Args, ParentRunID: parentRun.ID, Depth: 1})
	if err != nil {
		t.Fatalf("child lost access to the parent's frozen input: %v", err)
	}
	if childRun.Args == parentRun.Args {
		t.Fatal("child reused a mutable parent input instead of freezing its own copy")
	}
	workspace := childRun.Context["workspace"].(map[string]any)
	if !strings.HasPrefix(childRun.Args.(string), workspace["rootDirectory"].(string)+string(filepath.Separator)) {
		t.Fatalf("child input escaped its internal directory: %#v", childRun.Args)
	}
	content, err := os.ReadFile(childRun.Args.(string))
	if err != nil || string(content) != "frozen parent input" {
		t.Fatalf("child input bytes changed: %q %v", content, err)
	}
}

func TestParentDownloadsFrozenChildWorkflowFileResults(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	for _, mode := range []string{"absolute", "relative", "nested-relative"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := workflowTestService(t)
			s.pythonExecutable = python
			nested, relative := mode == "nested-relative", mode != "absolute"
			schema := `{type:'string',format:'file'}`
			result := `output.data`
			if nested {
				schema = `{type:'object',properties:{files:{type:'array',items:{type:'string',format:'file'}},note:{type:'string'},label:{type:'string'}},required:['files','note','label']}`
				result = `{files:[output.data],note:output.data,label:'child'}`
			}
			path := "flowContext.workspace.runDirectory+'/report.txt'"
			if relative {
				path = "'report.txt'"
			}
			code, _ := json.Marshal("import json,pathlib,sys\np=pathlib.Path(json.load(sys.stdin)['path'])\np.write_bytes(b'child report bytes')\njson.dump(str(p),sys.stdout)")
			childSource := strings.Replace(runtimeV2Meta, "phases:", "outputSchema:"+schema+",actions:[{id:'write',type:'script',scriptId:'write',label:'生成成果',risk:'write'}],phases:", 1) + `export const scripts={write:{language:'python',protocol:'json',code:` + string(code) + `}};const output=await action('write',{id:'write-file',label:'生成成果',phase:'work',input:{path:` + path + `}});return ` + result + `;`
			child := savedTestWorkflow(t, s, childSource)
			parentSource := strings.Replace(runtimeV2Meta, "phases:", "outputSchema:"+schema+",phases:", 1) + `const child=await workflow('` + child.ID + `',{id:'child-call',label:'调用子流程',phase:'work'});return child.result;`
			parent := savedTestWorkflow(t, s, parentSource)
			run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: parent.ID, RequestID: "parent", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			final := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
			if final.Run.Status != constants.WorkflowRunStatusSucceeded {
				t.Fatalf("parent did not complete: %+v", final.Run)
			}
			files, err := s.OutputFiles(context.Background(), final.Run)
			if err != nil || len(files) != 1 || !files[0].Available {
				t.Fatalf("child file not available through parent: %+v %v", files, err)
			}
			file, _, err := s.OpenOutputFile(context.Background(), parent.ID, run.ID, files[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(file)
			file.Close()
			if err != nil || string(content) != "child report bytes" {
				t.Fatalf("child file bytes changed: %q %v", content, err)
			}
			childRun, err := s.runs.GetRun(context.Background(), final.Effects[0].ChildRunID)
			if err != nil {
				t.Fatal(err)
			}
			parentPath, childPath := final.Run.Result, childRun.Result
			if nested {
				parentObject, childObject := final.Run.Result.(map[string]any), childRun.Result.(map[string]any)
				parentPath, childPath = parentObject["files"].([]any)[0], childObject["files"].([]any)[0]
				if parentObject["note"] != childObject["note"] || parentObject["label"] != "child" {
					t.Fatalf("ordinary business fields were rewritten: %#v", parentObject)
				}
			}
			parentRoot := final.Run.Context["workspace"].(map[string]any)["rootDirectory"].(string)
			if parentPath == childPath || !strings.HasPrefix(parentPath.(string), parentRoot+string(filepath.Separator)) {
				t.Fatalf("parent retained child directory authority: parent=%v child=%v", parentPath, childPath)
			}
			childActual := childPath.(string)
			if relative {
				childActual = filepath.Join(childRun.Context["workspace"].(map[string]any)["runDirectory"].(string), childActual)
			}
			if file, err := openRunFile(final.Run, childActual); err == nil {
				file.Close()
				t.Fatal("receiving a child file expanded the parent's directory authorization")
			}
			if err := os.WriteFile(childActual, []byte("changed after completion"), 0o600); err != nil {
				t.Fatal(err)
			}
			content, err = os.ReadFile(parentPath.(string))
			if err != nil || string(content) != "child report bytes" {
				t.Fatalf("parent output still aliases child bytes: %q %v", content, err)
			}
		})
	}
}

func TestChildFileResultDoesNotAuthorizeOutsideDirectories(t *testing.T) {
	s, _ := workflowTestService(t)
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, _ := json.Marshal(outside)
	child := savedTestWorkflow(t, s, strings.Replace(runtimeV2Meta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1)+`return `+string(path)+`;`)
	parent := savedTestWorkflow(t, s, strings.Replace(runtimeV2Meta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1)+`const child=await workflow('`+child.ID+`',{id:'child-call',label:'调用子流程',phase:'work'});return child.result;`)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: parent.ID, RequestID: "outside"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	final := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	if final.Run.Status != constants.WorkflowRunStatusFailed || final.Run.Failure == nil {
		t.Fatalf("child result authorized an outside file: %+v", final.Run)
	}
	if code, _ := final.Run.Failure.Data.(map[string]any)["code"].(string); code != status_error.WorkflowFileInvalid.Key() {
		t.Fatalf("outside file was rejected for an unrelated reason: %+v", final.Run.Failure)
	}
}

func TestDeclaredOutputsUseOnlyBusinessScopeAndInternalWorkspace(t *testing.T) {
	s, _ := workflowTestService(t)
	business, _ := filepath.EvalSymlinks(t.TempDir())
	definition := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, business)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "outputs"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := run.Context["workspace"].(map[string]any)
	internal := filepath.Join(workspace["runDirectory"].(string), "report.txt")
	external := filepath.Join(business, "report.txt")
	outside := filepath.Join(t.TempDir(), "private.txt")
	for _, path := range []string{internal, external, outside} {
		if err := os.WriteFile(path, []byte("report"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	schema := map[string]any{"type": "string", "format": "file"}
	for _, path := range []string{internal, external} {
		files := InspectOutputFiles(run, schema, path)
		if len(files) != 1 || !files[0].Available {
			t.Fatalf("declared output unavailable: %s %+v", path, files)
		}
		file, _, err := OpenDeclaredOutputFile(run, schema, path, files[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	files := InspectOutputFiles(run, schema, outside)
	if len(files) != 1 || files[0].Available {
		t.Fatal("result path expanded file authorization")
	}
	if file, _, err := OpenDeclaredOutputFile(run, schema, outside, files[0].ID); err == nil {
		file.Close()
		t.Fatal("outside file downloaded")
	}
}

func TestDeclaredOutputsRejectMissingMalformedAndMismatchedWorkspace(t *testing.T) {
	s, _ := workflowTestService(t)
	business, _ := filepath.EvalSymlinks(t.TempDir())
	definition := scopedProcessDefinition(t, s, runtimeV2Meta+`return true;`, business)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "invalid-output-workspace"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(business, "report.txt")
	if err := os.WriteFile(path, []byte("business report"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := run.Context["workspace"].(map[string]any)
	mismatched := make(map[string]any, len(workspace))
	for key, value := range workspace {
		mismatched[key] = value
	}
	mismatched["scriptsDirectory"] = business
	schema := map[string]any{"type": "string", "format": "file"}
	for _, test := range []struct {
		// name 标识缺少或损坏的冻结目录事实。
		name string
		// workspace 是该用例提供的非可信目录记录。
		workspace any
		// failure 是必须明确返回的状态错误。
		failure status_error.StatusError
	}{
		{name: "missing", failure: status_error.WorkflowStateInvalid},
		{name: "malformed", workspace: "invalid", failure: status_error.WorkflowStateInvalid},
		{name: "mismatched", workspace: mismatched, failure: status_error.WorkflowStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := run
			invalid.Context = map[string]any{"workspace": test.workspace}
			files := InspectOutputFiles(invalid, schema, path)
			if len(files) != 1 || files[0].Available {
				t.Errorf("invalid workspace retained business-scope download: %+v", files)
			}
			file, _, err := OpenDeclaredOutputFile(invalid, schema, path, outputFileID(""))
			if file != nil {
				file.Close()
			}
			var failure interface{ Key() string }
			if !errors.As(err, &failure) || failure.Key() != test.failure.Key() {
				t.Fatalf("invalid workspace download error = %v, want %v", err, test.failure)
			}
		})
	}
}

func TestPythonScriptUsesManagedSourceCwdAndTemporaryFiles(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	s, _ := workflowTestService(t)
	s.dataDirectory, s.pythonExecutable = t.TempDir(), python
	definition := savedTestWorkflow(t, s, runtimeV2Meta+`return true;`)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "script"})
	if err != nil {
		t.Fatal(err)
	}
	code := "import json,os,tempfile\njson.dump({'source':__file__,'cwd':os.getcwd(),'tmp':tempfile.gettempdir(),'cache':os.environ.get('XDG_CACHE_HOME')},__import__('sys').stdout)"
	bundle := workflowdsl.ProgramBundle{Scripts: map[string]workflowdsl.ScriptDefinition{"paths": {Language: "python", Protocol: "json", Code: code, Digest: workflowdsl.Digest([]byte(code))}}}
	value, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "paths"}, map[string]any{}, bundle)
	if err != nil {
		t.Fatal(err)
	}
	data := value.(map[string]any)["data"].(map[string]any)
	root, _ := filepath.EvalSymlinks(s.dataDirectory)
	root = filepath.Join(root, "workflow-workspaces", definition.ID)
	if !strings.HasPrefix(data["source"].(string), filepath.Join(root, "scripts")+string(filepath.Separator)) || data["cwd"] != filepath.Join(root, "runs", run.ID) || data["tmp"] != filepath.Join(root, "tmp") || data["cache"] != filepath.Join(root, "cache") {
		t.Fatalf("Python used unmanaged paths: %#v", data)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "XDG_CACHE_HOME", "ALLY_WORKFLOW_ROOT", "ALLY_WORKFLOW_SCRIPTS_DIR", "ALLY_WORKFLOW_STATE_DIR", "ALLY_WORKFLOW_CACHE_DIR", "ALLY_WORKFLOW_TEMP_DIR", "ALLY_WORKFLOW_RUN_DIR"} {
		if _, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "paths"}, map[string]any{"env": map[string]any{key: t.TempDir()}}, bundle); err == nil {
			t.Fatalf("reserved directory %s was overridable", key)
		}
		if _, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "paths"}, map[string]any{"secretEnv": map[string]any{key: map[string]any{"secretRef": "invalid"}}}, bundle); !errors.Is(err, status_error.WorkflowSchemaInvalid) {
			t.Fatalf("secret environment did not reject reserved directory %s before resolving credentials: %v", key, err)
		}
	}
	tampered := bundle.Scripts["paths"]
	tampered.Code = "print('modified')"
	bundle.Scripts["paths"] = tampered
	if _, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "paths"}, map[string]any{}, bundle); !errors.Is(err, status_error.WorkflowStateInvalid) {
		t.Fatalf("script bytes did not match their frozen digest: %v", err)
	}
}

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
	definition := savedTestWorkflow(t, s, runtimeV2Meta+`return true;`)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "protocol"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{nil, false, float64(0), "", map[string]any{"值": "中文"}} {
		code := "import json,sys\njson.dump(json.load(sys.stdin),sys.stdout)\n"
		bundle := workflowdsl.ProgramBundle{Scripts: map[string]workflowdsl.ScriptDefinition{"echo": {Language: "python", Protocol: "json", Code: code, Digest: workflowdsl.Digest([]byte(code))}}}
		value, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "echo"}, map[string]any{"input": input}, bundle)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value.(map[string]any)["data"], input) {
			t.Fatalf("stdin changed: %#v", value)
		}
	}
	code := "print('true false')\n"
	bundle := workflowdsl.ProgramBundle{Scripts: map[string]workflowdsl.ScriptDefinition{"bad": {Language: "python", Protocol: "json", Code: code, Digest: workflowdsl.Digest([]byte(code))}}}
	if _, err := s.executeScript(context.Background(), run, workflowdsl.ActionDeclaration{ScriptID: "bad"}, map[string]any{}, bundle); err == nil {
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

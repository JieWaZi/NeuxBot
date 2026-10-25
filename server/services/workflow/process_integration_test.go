package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowfunction "github.com/JieWaZi/NeuxBot/server/internal/workflow/function"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestMain(m *testing.M) {
	if workflowfunction.IsChildMode() {
		os.Exit(workflowfunction.RunChild(os.Stdin))
	}
	os.Exit(m.Run())
}
func TestWorkflowLocalProcessHelper(t *testing.T) {
	if os.Getenv("ALLY_WORKFLOW_PROCESS_TEST") != "1" {
		return
	}
	target := os.Args[len(os.Args)-1]
	if err := os.WriteFile(target+".started", []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ALLY_WORKFLOW_PROCESS_WRITE_FIRST") == "1" {
		file, err := os.OpenFile(target+".calls", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("call\n"); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("ALLY ALLY!"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("ALLY_WORKFLOW_PROCESS_DELAY") == "1" {
		time.Sleep(3 * time.Second)
	}
	if os.Getenv("ALLY_WORKFLOW_PROCESS_FAIL") == "1" {
		t.Error("process produced a real nonzero exit")
		return
	}
	if err := os.WriteFile(target, []byte("ALLY ALLY!"), 0600); err != nil {
		t.Fatal(err)
	}
}
func processWorkflowSource(code string) string {
	return `export const meta={sdk:'ally.workflow/1',title:'进程验收',description:'真实隔离进程',phases:[{id:'compute',title:'计算',detail:'真实执行',canExit:true}]};phase('compute');` + code
}
func waitWorkflow(t *testing.T, s *Service, id string, condition func(controllers.WorkflowRunState) bool) controllers.WorkflowRunState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, err := s.runs.LoadRunState(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if condition(state) {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, err := s.runs.LoadRunState(context.Background(), id)
	t.Fatalf("workflow wait expired: %+v %v", state, err)
	return state
}
func TestFunctionWorkflowRunsARealChildProcess(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, processWorkflowSource(`const value=await functionTask({id:'js',label:'JS计算',phase:'compute',language:'javascript',input:{text:'ally'},code:'function main(input){const message=(input.text+" "+input.text+"!").toUpperCase();return {message,length:message.length}}',outputSchema:{type:'object',required:['message','length']}});return value;`))
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	data, ok := state.Run.Result.(map[string]any)
	if state.Run.Status != constants.WorkflowRunStatusSucceeded || !ok || data["message"] != "ALLY ALLY!" || data["length"] != float64(10) {
		t.Fatalf("real function: %+v %+v", state.Run, state.Instances)
	}
}

func TestFunctionFailureRemainsCatchableWithSourceAndReason(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, processWorkflowSource(`try { await functionTask({id:'failed-js',label:'失败计算',phase:'compute',language:'javascript',input:{},code:'function main(input){throw new Error("真实失败原因") }'}); } catch(error) { return {code:error.code,kind:error.kind,reason:error.reason,line:error.sourceRange.start.line}; }`))
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	data, ok := state.Run.Result.(map[string]any)
	if !ok || state.Run.Status != constants.WorkflowRunStatusSucceeded || data["code"] != "FUNCTION_FAILED" || !strings.Contains(data["reason"].(string), "真实失败原因") || data["line"] == float64(0) {
		t.Fatalf("catch lost actual failure: %+v", state.Run)
	}
	if len(state.Instances) != 1 || state.Instances[0].Status != constants.WorkflowStepStatusFailed {
		t.Fatalf("handled failure falsely marked successful: %+v", state.Instances)
	}
}

func TestScriptFailureKeepsActualProcessReceipt(t *testing.T) {
	s, _ := workflowTestService(t)
	directory := t.TempDir()
	target := filepath.Join(directory, "must-not-exist.txt")
	source := strings.Replace(scriptSource(t, target, false), "ALLY_WORKFLOW_PROCESS_TEST:'1'", "ALLY_WORKFLOW_PROCESS_TEST:'1',ALLY_WORKFLOW_PROCESS_FAIL:'1'", 1)
	source = strings.Replace(source, "const result=await action(", "try { const result=await action(", 1)
	source = strings.Replace(source, "return result.process.exitCode;", `return result.process.exitCode; } catch(error) { return {code:error.code,exitCode:error.process.exitCode,stdout:error.process.stdout,line:error.sourceRange.start.line}; }`, 1)
	definition := scopedProcessDefinition(t, s, source, directory)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	data, ok := state.Run.Result.(map[string]any)
	if !ok || state.Run.Status != constants.WorkflowRunStatusSucceeded || data["code"] != "PROCESS_FAILED" || data["exitCode"] == float64(0) || !strings.Contains(data["stdout"].(string), "nonzero exit") {
		t.Fatalf("actual process receipt missing: %+v", state.Run)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed process wrote the target")
	}
}
func scriptSource(t *testing.T, target string, delay bool) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	action, _ := json.Marshal(map[string]any{"id": "write", "label": "真实写入", "description": "写入临时验收目录", "type": "script", "risk": "write", "executable": executable, "workingDirectory": "workflow_scope"})
	args, _ := json.Marshal([]string{"-test.run=^TestWorkflowLocalProcessHelper$", "--", target})
	environment := `{ALLY_WORKFLOW_PROCESS_TEST:'1'}`
	if delay {
		environment = `{ALLY_WORKFLOW_PROCESS_TEST:'1',ALLY_WORKFLOW_PROCESS_DELAY:'1'}`
	}
	return strings.Replace(processWorkflowSource(`const result=await action('write',{id:'write-node',label:'写文件',phase:'compute',args:`+string(args)+`,env:`+environment+`});return result.process.exitCode;`), "phases:", "actions:["+string(action)+"],phases:", 1)
}
func scopedProcessDefinition(t *testing.T, s *Service, source, directory string) models.WorkflowDefinition {
	t.Helper()
	definition, err := fixtureDefinition(context.Background(), s.store, definitionFixture{RequestID: "create-scope", Title: "进程", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := s.SaveSource(context.Background(), SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save-scope", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	return definition
}

func TestCancellationWaitsForActualProcessExit(t *testing.T) {
	s, _ := workflowTestService(t)
	directory := t.TempDir()
	target := filepath.Join(directory, "cancel.txt")
	definition := scopedProcessDefinition(t, s, scriptSource(t, target, true), directory)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		_, err := os.Stat(target + ".started")
		return err == nil
	})
	started := time.Now()
	if err = s.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	if state.Run.Status != constants.WorkflowRunStatusCancelled || time.Since(started) > 2*time.Second {
		t.Fatalf("real cancellation: %+v %v", state.Run, time.Since(started))
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled process completed write")
	}
}

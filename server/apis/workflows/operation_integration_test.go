package workflows

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
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestOperationApprovalIsIndependentAndFirstAnswerWins(t *testing.T) {
	s, _ := workflowTestService(t)
	directory := t.TempDir()
	target := filepath.Join(directory, "result.txt")
	definition := scopedProcessDefinition(t, s, scriptSource(t, target, false), directory)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Operations) == 1 })
	operation := state.Operations[0]
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("operation executed without permission")
	}
	if err = answerHuman(context.Background(), workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: operation.ID, RequestID: "business-answer", Value: "approve"}); err == nil {
		t.Fatal("business answer authorized operation")
	}
	command := AnswerOperationCommand{RunID: run.ID, OperationID: operation.ID, RequestID: "allow", DescriptorDigest: operation.DescriptorDigest, Allowed: true}
	if err = answerOperation(context.Background(), workflowAPIDependencies(s), command); err != nil {
		t.Fatal(err)
	}
	if err = answerOperation(context.Background(), workflowAPIDependencies(s), command); err != nil {
		t.Fatal("same answer replay", err)
	}
	command.RequestID = "late-deny"
	command.Allowed = false
	if err = answerOperation(context.Background(), workflowAPIDependencies(s), command); err == nil {
		t.Fatal("late answer changed permission")
	}
	state = waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "ALLY ALLY!" || state.Run.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("real authorized write: %s %v %+v", content, err, state.Run)
	}
}

func TestMain(m *testing.M) {
	if workflowfunction.IsChildMode() {
		os.Exit(workflowfunction.RunChild(os.Stdin))
	}
	if os.Getenv("ALLY_WORKFLOW_PROCESS_TEST") == "1" {
		os.Exit(m.Run())
	}
	os.Exit(runCLITestSuite(m))
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

func waitWorkflow(t *testing.T, s *workflowFixture, id string, condition func(controllers.WorkflowRunState) bool) controllers.WorkflowRunState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, err := s.Runs.LoadRunState(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if condition(state) {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, err := s.Runs.LoadRunState(context.Background(), id)
	t.Fatalf("workflow wait expired: %+v %v", state, err)
	return state
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

func scopedProcessDefinition(t *testing.T, s *workflowFixture, source, directory string) models.WorkflowDefinition {
	t.Helper()
	definition, err := createDefinition(context.Background(), workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create-scope", Title: "进程", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := s.SaveSource(context.Background(), workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save-scope", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	return definition
}

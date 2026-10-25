package workflows

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func openRecoveryWorkflow(t *testing.T, databasePath string) (*workflowFixture, func()) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: databasePath, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	close := func() {
		once.Do(func() {
			if err := connection.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(close)
	return workflowTestNew(workflow.Config{Store: controllers.NewWorkflowController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}), close
}

func TestRealSideEffectRestartFailsAndPreservesEvidenceWithoutResend(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "workflow.db")
	target := filepath.Join(directory, "result.txt")
	s, closeDB := openRecoveryWorkflow(t, databasePath)
	source := strings.Replace(scriptSource(t, target, true), "ALLY_WORKFLOW_PROCESS_TEST:'1'", "ALLY_WORKFLOW_PROCESS_TEST:'1',ALLY_WORKFLOW_PROCESS_WRITE_FIRST:'1'", 1)
	definition := scopedProcessDefinition(t, s, source, directory)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { _, err := os.Stat(target); return err == nil })
	// 真实进程已经写入外部文件，关闭服务丢弃尚未提交的返回收据，然后关闭并重开 SQLite。
	s.Close()
	closeDB()
	restored, _ := openRecoveryWorkflow(t, databasePath)
	if err = restored.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	state := waitWorkflow(t, restored, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	if len(state.Effects) != 1 || state.Effects[0].Status != constants.WorkflowStepStatusUnknown || state.Instances[0].Status != constants.WorkflowStepStatusFailed {
		t.Fatalf("missing unknown evidence: %+v", state)
	}
	if err := restored.CancelRun(ctx, run.ID); err != nil {
		t.Fatalf("cancellation erased unknown effect: %v", err)
	}
	if _, err := retryRun(ctx, workflowAPIDependencies(restored), RetryRunCommand{RunID: run.ID, RequestID: "unsafe-retry", ExpectedVersion: state.Run.Version}); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatalf("unknown effect blindly retried: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "ALLY ALLY!" {
		t.Fatalf("actual file missing: %q %v", data, err)
	}
	final := runTicks(t, restored, run.ID, 3)
	if final.Status != constants.WorkflowRunStatusFailed || final.Failure == nil || final.CompletedAt == nil {
		t.Fatalf("failure evidence missing: %+v", final)
	}
	calls, err := os.ReadFile(target + ".calls")
	if err != nil || string(calls) != "call\n" {
		t.Fatalf("non-idempotent side effect resent: %q %v", calls, err)
	}
}

func TestRetryCreatesNewRunWithOriginalVersionAndEvidence(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx := context.Background()
	source := `export const meta={sdk:'ally.workflow/1',title:'重跑',description:'冻结原版本',phases:[{id:'one',title:'one',detail:'one',canExit:true}]};throw new Error('original failure');`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Args: map[string]any{"unchanged": true}})
	if err != nil {
		t.Fatal(err)
	}
	failed := runTicks(t, s, run.ID, 3)
	if failed.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("expected failed source: %+v", failed)
	}
	_, _, err = s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: run.RevisionID, RequestID: "update", Source: strings.Replace(source, "throw new Error('original failure');", "return 'new version';", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatal(err)
	}
	command := RetryRunCommand{RunID: run.ID, RequestID: "retry", ExpectedVersion: failed.Version}
	next, err := retryRun(ctx, workflowAPIDependencies(s), command)
	if err != nil || next.ID == run.ID || next.RevisionID != run.RevisionID || next.Context["retrySourceRunId"] != run.ID {
		t.Fatalf("retry lost original evidence: %+v %v", next, err)
	}
	again, err := retryRun(ctx, workflowAPIDependencies(s), command)
	if err != nil || again.ID != next.ID {
		t.Fatal("retry replay duplicated run", err)
	}
	result := runTicks(t, s, next.ID, 3)
	if result.Status != constants.WorkflowRunStatusFailed {
		t.Fatal("retry silently used latest source")
	}
	old, _ := s.Runs.GetRun(ctx, run.ID)
	if old.Version != failed.Version || old.Status != failed.Status {
		t.Fatal("retry overwrote original run")
	}
}

func TestSkippableWorkerStopsActualDispatchBeforeNullResult(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	worker := &holdingWorker{changed: make(chan struct{}, 8)}
	s.Worker = worker
	s.Service = workflow.New(s.Config)
	source := `export const meta={sdk:'ally.workflow/1',title:'跳过',description:'取消收敛',nodeResultPageOmissions:{held:'只返回步骤跳过与取消测试的控制值'},phases:[{id:'one',title:'one',detail:'one',canExit:true}]};const value=await worker('等待',{id:'held',label:'可跳过步骤',phase:'one',resolve:{kind:'waker',wakerId:'fixture'},skippable:true});return {value};`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitActiveWorkers(t, worker, 1)
	state, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := SkipWorkerCommand{RunID: run.ID, InstanceID: state.Instances[0].ID, RequestID: "skip", AttemptGeneration: 1}
	if err = skipWorker(ctx, workflowAPIDependencies(s), command); err != nil {
		t.Fatal(err)
	}
	if err = skipWorker(ctx, workflowAPIDependencies(s), command); err != nil {
		t.Fatal("skip replay failed", err)
	}
	result := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	worker.mutex.Lock()
	active := worker.active
	worker.mutex.Unlock()
	if active != 0 || result.Run.Status != constants.WorkflowRunStatusSucceeded || result.Instances[0].Status != constants.WorkflowStepStatusSkipped || result.Run.Result.(map[string]any)["value"] != nil {
		t.Fatalf("skip formed a false terminal result: active=%d run=%+v instances=%+v", active, result.Run, result.Instances)
	}
}

func TestHumanFallbackKeepsSkipAndTimeoutSources(t *testing.T) {
	for _, source := range []string{"skip", "timeout"} {
		t.Run(source, func(t *testing.T) {
			s, _ := workflowTestService(t)
			text := strings.Replace(humanSource, "allowSkip:false", "allowSkip:true", 1)
			text = strings.Replace(text, "source:answer.source", "source:answer.source,value:answer.value,skipped:answer.skipped", 1)
			if source == "timeout" {
				text = strings.Replace(text, "allowSkip:true", "allowSkip:true,timeoutMs:1", 1)
			}
			definition := savedTestWorkflow(t, s, text)
			run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, _ := s.Runs.LoadRunState(context.Background(), run.ID)
			if source == "skip" {
				if err := answerHuman(context.Background(), workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "skip", Skip: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				time.Sleep(2 * time.Millisecond)
			}
			result := runTicks(t, s, run.ID, 3)
			data, _ := result.Result.(map[string]any)
			if result.Status != constants.WorkflowRunStatusSucceeded || data["value"] != "reject" || data["source"] != source || data["skipped"] != true || data["approved"] != false {
				t.Fatalf("fallback changed approval meaning: %+v", result)
			}
		})
	}
}

package workflow

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestV2OutputRepairBudgetPersistsWithoutBusinessRerun(t *testing.T) {
	s, _ := workflowTestService(t)
	worker := &retryWorker{}
	s.worker = worker
	definition := savedTestWorkflow(t, s, runtimeV2Meta+`return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}});`)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, _ := s.runs.LoadRunState(context.Background(), run.ID)
	effect := state.Effects[0]
	calls := 0
	s.repairOutput = func(context.Context, models.WorkflowExecutionDefaults, map[string]any, string) (string, error) {
		calls++
		return "invalid", nil
	}
	receipt := WorkerReceipt{ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, Text: "bad", FinalPresent: true}
	if _, err = s.repairWorkerOutput(context.Background(), run, effect, receipt); err == nil {
		t.Fatal("invalid output succeeded")
	}
	current, _ := s.runs.GetEffect(context.Background(), effect.ID)
	if calls != 2 || current.OutputRepairs != 2 || len(worker.attempts) != 0 {
		t.Fatal("budget/business dispatch", calls, current.OutputRepairs, worker.attempts)
	}
	s.repairOutput = func(context.Context, models.WorkflowExecutionDefaults, map[string]any, string) (string, error) {
		t.Fatal("restarted budget was reset")
		return "{}", nil
	}
	if _, err = s.repairWorkerOutput(context.Background(), run, effect, receipt); err == nil {
		t.Fatal("exhausted repair succeeded")
	}
}

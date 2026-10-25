package workflow

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// retryWorker 控制真实派发边界的终态，实际身份与候选仍通过持久服务处理。
type retryWorker struct {
	// attempts 保存收到的独立尝试身份。
	attempts []WorkerDispatch
}

func (*retryWorker) ResolveAdmission(_ context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, _ bool) (models.WorkflowExecutionDefaults, error) {
	return workerConfiguration(options, defaults), nil
}

func (w *retryWorker) Dispatch(_ context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	w.attempts = append(w.attempts, input)
	status := constants.ExecutionStatusFailed
	if len(w.attempts) == 2 {
		status = constants.ExecutionStatusCompleted
	}
	return WorkerReceipt{ChatID: "chat-" + input.Effect.ID, ExecutionID: input.Effect.ExecutionID, Status: status, ErrorCode: "FIRST_ATTEMPT_FAILED"}, nil
}
func (w *retryWorker) Inspect(context.Context, models.WorkflowEffect) (WorkerReceipt, error) {
	return WorkerReceipt{}, status_error.WorkflowNotFound
}
func (w *retryWorker) Cancel(context.Context, models.WorkflowEffect) error { return nil }

func TestWorkerRetryKeepsAttemptsAndRevokesOldResultToken(t *testing.T) {
	s, _ := workflowTestService(t)
	w := &retryWorker{}
	s.worker = w
	source := `export const meta={sdk:'ally.workflow/1',title:'重试',description:'保留尝试',nodeResultPageOmissions:{read:'只返回重试协议测试的控制值'},phases:[{id:'work',title:'工作',detail:'只读',canExit:true}]}; return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'},retry:{maxAttempts:2,safety:'pure'}});`
	definition := savedTestWorkflow(t, s, source)
	ctx := context.Background()
	if _, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "unsafe"}); workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
		t.Fatal("writable pure retry was admitted", err)
	}
	run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "safe", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: true, RuntimeID: "fixture-runtime"}})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	state, _ := s.runs.LoadRunState(ctx, run.ID)
	first := state.Effects[0]
	if first.DeadlineAt != 0 {
		t.Fatal("worker admission created a node deadline")
	}
	resultScope := workflowtool.Scope{Key: first.ID, Token: first.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}
	query := workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryResultSchema}
	if result, err := s.handleTool(ctx, resultScope, query); err != nil || result["schema"].(map[string]any)["type"] != "string" {
		t.Fatalf("current attempt schema unavailable: %+v %v", result, err)
	}
	if current, _ := s.runs.GetEffect(ctx, first.ID); current.Candidate != nil {
		t.Fatal("schema query wrote a result candidate")
	}
	wrongToken := resultScope
	wrongToken.Token = "other-attempt-token"
	if _, err := s.handleTool(ctx, wrongToken, query); err == nil {
		t.Fatal("schema query accepted another attempt's token")
	}
	if _, err := s.handleTool(ctx, resultScope, workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQuerySDK}); err == nil {
		t.Fatal("result query acquired creation guidance")
	}
	if _, err := s.handleTool(ctx, workflowtool.Scope{Key: first.ID, Token: first.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "uncommitted"}); err != nil {
		t.Fatal(err)
	}
	dispatchPrepared(t, s, run.ID)
	runTicks(t, s, run.ID, 1)
	state, _ = s.runs.LoadRunState(ctx, run.ID)
	firstAfter, _ := s.runs.GetEffect(ctx, first.ID)
	if len(state.Effects) != 2 || firstAfter.Status != constants.WorkflowStepStatusFailed || state.Instances[0].AttemptGeneration != 2 {
		t.Fatalf("attempts overwritten: %+v", state)
	}
	second, _ := s.runs.GetEffect(ctx, state.Instances[0].EffectID)
	if second.DeadlineAt != 0 {
		t.Fatal("worker retry created a node deadline")
	}
	if second.ExecutionID == first.ExecutionID || second.AuthToken == first.AuthToken || second.Candidate != nil {
		t.Fatal("retry reused identity or candidate")
	}
	if _, err := s.handleTool(ctx, workflowtool.Scope{Key: first.ID, Token: first.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "late"}); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("old token accepted", err)
	}
	if _, err := s.handleTool(ctx, resultScope, query); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("old attempt could still query result schema", err)
	}
	if _, err := s.handleTool(ctx, workflowtool.Scope{Key: second.ID, Token: second.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "verified"}); err != nil {
		t.Fatal(err)
	}
	dispatchPrepared(t, s, run.ID)
	result := runTicks(t, s, run.ID, 3)
	if result.Status != constants.WorkflowRunStatusSucceeded || result.Result != "verified" || len(w.attempts) != 2 {
		t.Fatalf("retry result: %+v", result)
	}
}

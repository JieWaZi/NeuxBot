package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentcapability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
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
	source := `export const meta={sdk:'ally.workflow',title:'重试',description:'保留尝试',phases:[{id:'work',title:'工作',detail:'只读',canExit:true}]}; return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'},retry:{maxAttempts:2,safety:'pure'}});`
	definition := savedTestWorkflow(t, s, source)
	ctx := context.Background()
	if _, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "unsafe"}); workflowstate.ErrorKey(err) != "WorkflowRetryRequiresReadOnly" {
		t.Fatal("writable pure retry was admitted", err)
	}
	run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "safe", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: false, RuntimeID: "fixture-runtime"}, WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{"read": {WorkspaceReadOnly: true, RuntimeID: "fixture-runtime"}}})
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
	result, err := s.handleTool(ctx, resultScope, query)
	if err != nil || result["schema"].(map[string]any)["type"] != "string" {
		t.Fatalf("current attempt schema unavailable: %+v %v", result, err)
	}
	guidance, _ := result["guidance"].(string)
	if !strings.Contains(guidance, "workflow_results_submit") || strings.Contains(guidance, "allycli") {
		t.Fatalf("result schema directs MCP workers to CLI: %s", guidance)
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
	completed := runTicks(t, s, run.ID, 3)
	if completed.Status != constants.WorkflowRunStatusSucceeded || completed.Result != "verified" || len(w.attempts) != 2 {
		t.Fatalf("retry result: %+v", completed)
	}
}

func retryPolicySource(safety string) string {
	return `export const meta={sdk:'ally.workflow',title:'多源采集',description:'按渠道检索',phases:[{id:'collect',title:'采集',detail:'保留各渠道结果',canExit:true}]};
 const RETRY={maxAttempts:2,safety:'` + safety + `',idempotencyKey:'channel'};
 return await parallel([
 async()=>await worker('查询官方资讯',{id:'vendors',label:'厂商发布',phase:'collect',resolve:{kind:'waker',wakerId:'fixture'},retry:RETRY}),
 async()=>await worker('查询媒体资讯',{id:'media',label:'媒体报道',phase:'collect',resolve:{kind:'waker',wakerId:'fixture'},retry:RETRY}),
 async()=>await worker('查询研究资讯',{id:'research',label:'研究动态',phase:'collect',resolve:{kind:'waker',wakerId:'fixture'},retry:RETRY})
 ],{id:'collect-all',failureMode:'collect-null'});`
}

func TestAuthoringRetryPreflightRejectsBeforeSaving(t *testing.T) {
	for _, tc := range []struct {
		// name 区分可写冲突和未实现的幂等重试。
		name string
		// safety 是源码申请的重试策略。
		safety string
		// readonly 是当前创作会话的真实读取约束。
		readonly bool
		// code 是各入口应共同返回的稳定业务错误。
		code string
	}{
		{"writable-pure", "pure", false, "WorkflowRetryRequiresReadOnly"},
		{"unsupported-idempotent", "idempotent", true, "WorkflowRetryIdempotencyUnsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, _ := workflowTestService(t)
			definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "retry-preflight", Title: "采集"})
			if err != nil {
				t.Fatal(err)
			}
			call := authoringTestCLI(t, s, models.WorkflowCreationSession{ID: "creation", DefinitionID: definition.ID, ActiveExecutionID: "authoring", AuthToken: "private", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: tc.readonly}})
			source := retryPolicySource(tc.safety)
			code, checked := call([]string{"workflow", "source", "validate", "--input", "-"}, map[string]any{"source": source})
			if code != 0 {
				t.Fatalf("validation transport failed: %+v", checked)
			}
			op, _ := agentcapability.Find("workflow.source.validate")
			if err := workflowdsl.ValidateJSON(op.OutputSchema, checked.Data); err != nil {
				t.Fatalf("validation diagnostics cannot be returned through MCP: %v", err)
			}
			data := checked.Data.(map[string]any)
			if data["valid"] != false || data["errorCode"] != tc.code {
				t.Fatalf("incompatible source reported valid: %+v", data)
			}
			diagnostics, _ := data["diagnostics"].([]any)
			if len(diagnostics) != 3 {
				t.Fatalf("expected all three conflicting nodes: %+v", diagnostics)
			}
			for index, id := range []string{"vendors", "media", "research"} {
				d := diagnostics[index].(map[string]any)
				if d["nodeId"] != id || d["field"] != "retry" || d["hint"] == "" || d["range"] == nil {
					t.Fatalf("node conflict is not actionable: %+v", d)
				}
			}
			code, rejected := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "rejected"}, map[string]any{"source": source, "expectedRevisionId": ""})
			if code == 0 || rejected.Error == nil || rejected.Error.Code != tc.code {
				t.Fatalf("incompatible source saved: %+v", rejected)
			}
			if got, _ := rejected.Error.Details["diagnostics"].([]any); len(got) != 3 {
				t.Fatalf("save lost diagnostics: %+v", rejected.Error)
			}
			head, err := s.store.GetDefinition(ctx, definition.ID)
			if err != nil || head.CurrentRevisionID != "" || head.Ordinal != 0 {
				t.Fatalf("rejected source changed head: %+v %v", head, err)
			}
			if code, response := call([]string{"receipts", "get", "--operation", "workflow.source.save", "--request-id", "rejected"}, nil); code == 0 || response.Error.Code != status_error.AgentReceiptNotFound.Key() {
				t.Fatalf("rejection created success receipt: %+v", response)
			}
		})
	}
}

func TestAuthoringPreparationReportsRetryCompatibility(t *testing.T) {
	for _, readonly := range []bool{false, true} {
		t.Run(fmt.Sprint(readonly), func(t *testing.T) {
			s, _ := workflowTestService(t)
			definition, err := fixtureDefinition(context.Background(), s.store, definitionFixture{RequestID: "prepare-retry", Title: "采集"})
			if err != nil {
				t.Fatal(err)
			}
			call := authoringTestCLI(t, s, models.WorkflowCreationSession{ID: "creation", DefinitionID: definition.ID, ActiveExecutionID: "authoring", AuthToken: "private", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: readonly}})
			code, response := call([]string{"workflow", "authoring", "prepare", "--primitives", "worker"}, nil)
			if code != 0 {
				t.Fatalf("prepare failed: %+v", response)
			}
			op, _ := agentcapability.Find("workflow.authoring.prepare")
			if err := workflowdsl.ValidateJSON(op.OutputSchema, response.Data); err != nil {
				t.Fatalf("execution constraints cannot be returned through MCP: %v", err)
			}
			execution, ok := response.Data.(map[string]any)["execution"].(map[string]any)
			if !ok {
				t.Fatal("prepare omitted execution constraints")
			}
			defaults := execution["defaults"].(map[string]any)
			policies := execution["workerRetry"].(map[string]any)
			pure := policies["pure"].(map[string]any)
			idempotent := policies["idempotent"].(map[string]any)
			if defaults["workspaceReadOnly"] != readonly || pure["supported"] != readonly || idempotent["supported"] != false {
				t.Fatalf("retry availability disagrees with current configuration: %+v", execution)
			}
			if !readonly && pure["errorCode"] != "WorkflowRetryRequiresReadOnly" {
				t.Fatalf("missing precise retry restriction: %+v", pure)
			}
			if idempotent["errorCode"] != "WorkflowRetryIdempotencyUnsupported" {
				t.Fatalf("unsupported idempotency advertised: %+v", idempotent)
			}
		})
	}
}

func TestRetryTargetPreflightMatchesRunAdmission(t *testing.T) {
	for _, readonly := range []bool{false, true} {
		t.Run(fmt.Sprint(readonly), func(t *testing.T) {
			s, _ := workflowTestService(t)
			s.worker = &controlledWorker{}
			definition := savedTestWorkflow(t, s, retryPolicySource("pure"))
			defaults := models.WorkflowExecutionDefaults{WorkspaceReadOnly: readonly, PermissionLevel: constants.PermissionLevelFullAccess, RuntimeID: "fixture-runtime"}
			_, _, targetErr := s.ValidateTargetConfiguration(context.Background(), definition.ID, "", defaults)
			run, startErr := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "admission", Defaults: defaults})
			if readonly {
				if targetErr != nil || startErr != nil {
					t.Fatalf("compatible configuration rejected: target=%v start=%v", targetErr, startErr)
				}
			} else if workflowstate.ErrorKey(targetErr) != "WorkflowRetryRequiresReadOnly" || workflowstate.ErrorKey(startErr) != "WorkflowRetryRequiresReadOnly" {
				t.Fatalf("preflight/start diverged: target=%v start=%v", targetErr, startErr)
			}
			if !readonly && run.ID != "" {
				t.Fatalf("rejected configuration created a run: %+v", run)
			}
		})
	}
}

func TestRetryAdmissionChecksFrozenNodeConfiguration(t *testing.T) {
	s, _ := workflowTestService(t)
	s.worker = &controlledWorker{}
	definition := savedTestWorkflow(t, s, retryPolicySource("pure"))
	_, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "frozen-writable", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: true, RuntimeID: "fixture-runtime"}, WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{
		"vendors":  {WorkspaceReadOnly: false, RuntimeID: "fixture-runtime"},
		"media":    {WorkspaceReadOnly: true, RuntimeID: "fixture-runtime"},
		"research": {WorkspaceReadOnly: true, RuntimeID: "fixture-runtime"},
	}})
	if workflowstate.ErrorKey(err) != "WorkflowRetryRequiresReadOnly" {
		t.Fatalf("global readonly hid writable node configuration: %v", err)
	}
}

func TestAuthoringRetrySaveReplaySurvivesConfigurationChange(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "retry-replay", Title: "采集"})
	if err != nil {
		t.Fatal(err)
	}
	authoringTestCLI(t, s, models.WorkflowCreationSession{ID: "creation", DefinitionID: definition.ID, ActiveExecutionID: "authoring", AuthToken: "private", Defaults: models.WorkflowExecutionDefaults{WorkspaceReadOnly: true}})
	command := SaveSourceCommand{DefinitionID: definition.ID, RequestID: "saved", Source: retryPolicySource("pure"), SDK: constants.WorkflowSDKIdentifier, CreationSessionID: "creation", CreationExecutionID: "authoring", CreationToken: "private"}
	saved, _, err := s.SaveSource(ctx, command)
	if err != nil {
		t.Fatalf("compatible source did not save: %v", err)
	}
	session, err := s.creationSessions.GetCreationSession(ctx, "creation")
	if err != nil {
		t.Fatal(err)
	}
	session.Defaults.WorkspaceReadOnly = false
	if err := s.creationSessions.UpdateCreationSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	replay, _, err := s.SaveSource(ctx, command)
	if err != nil || !replay.Replayed || replay.RevisionID != saved.RevisionID {
		t.Fatalf("changed configuration rewrote original receipt: %+v %v", replay, err)
	}
	command.ExpectedRevisionID = saved.RevisionID
	command.RequestID = "changed"
	_, _, err = s.SaveSource(ctx, command)
	if workflowstate.ErrorKey(err) != "WorkflowRetryRequiresReadOnly" {
		t.Fatalf("new save ignored current configuration: %v", err)
	}
}

func TestSourceImportRejectsUnsupportedRetryWithoutExecutionConfiguration(t *testing.T) {
	s, _ := workflowTestService(t)
	definition, err := fixtureDefinition(context.Background(), s.store, definitionFixture{RequestID: "import-retry", Title: "采集"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, diagnostics, err := s.SaveSource(context.Background(), SaveSourceCommand{DefinitionID: definition.ID, RequestID: "unsupported", Source: retryPolicySource("idempotent"), SDK: constants.WorkflowSDKIdentifier})
	if workflowstate.ErrorKey(err) != "WorkflowRetryIdempotencyUnsupported" || len(diagnostics) != 3 || receipt.RevisionID != "" {
		t.Fatalf("import ignored unsupported host retry policy: %+v %+v %v", receipt, diagnostics, err)
	}
}

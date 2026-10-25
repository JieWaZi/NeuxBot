package workflow

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestCreationRunsGetIncludesPersistedToolFailureAndOriginalIdentity(t *testing.T) {
	s, _ := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/1',title:'发送诊断',description:'保留原调用失败',phases:[{id:'delivery-phase',title:'发送',detail:'发送',canExit:true}],actions:[{id:'send',label:'发送',description:'发送微信',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'original-call',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'target',content:'正文'}})};return await action('send',{id:'deliver',label:'发送微信',phase:'delivery-phase'});`
	d := savedTestWorkflow(t, s, source)
	s.imValidate = func(context.Context, string) error { return nil }
	calls := 0
	s.imCall = func(context.Context, models.WorkflowRun, models.WorkflowActionCall) (any, error) {
		calls++
		return nil, status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED").WithField("reason", status_error.IMWorkflowDeliveryNotAllowed.Key()).WithParams(map[string]any{"targetId": "target", "cause": map[string]any{"code": status_error.IMWorkflowDeliveryNotAllowed.Key(), "hint": constants.IMRecoveryEnableWorkflowDelivery}})
	}
	ctx := context.Background()
	run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: d.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	if calls != 1 || len(state.ActionCalls) != 1 || state.Run.Failure == nil {
		t.Fatalf("未持久化真实调用链: %+v", state)
	}
	s.chats = &consumerChatBoundary{
		actual:  models.Execution{ID: "authoring", ChatID: "authoring-chat", Status: constants.ExecutionStatusRunning},
		summary: chat.ChatSummary{Chat: models.Chat{ID: "authoring-chat", ActiveExecutionID: "authoring"}},
	}
	if err := s.creationSessions.CreateCreationSession(ctx, models.WorkflowCreationSession{ID: "creation", ChatID: "authoring-chat", ActiveExecutionID: "authoring", DefinitionID: d.ID, AuthToken: "private"}); err != nil {
		t.Fatal(err)
	}
	data, err := s.handleCreationTool(ctx, workflowtool.Scope{Key: "creation", Token: "private"}, workflowtool.Input{Action: constants.WorkflowActionRunsGet, RunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	receipts := data["actionCalls"].([]models.WorkflowActionCall)
	if len(receipts) != 1 || receipts[0].ID != state.ActionCalls[0].ID || receipts[0].CallID != "original-call" {
		t.Fatalf("优化读取丢失原调用身份: %+v", data)
	}
	failure := receipts[0].Failure.Data.(map[string]any)
	if failure["reason"] != status_error.IMWorkflowDeliveryNotAllowed.Key() || failure["targetId"] != "target" || failure["cause"] == nil || failure["sourceRange"] == nil {
		t.Fatalf("优化读取丢失完整原因: %+v", failure)
	}
}

func TestToolFailureKeepsCauseAndDeliveryEvidence(t *testing.T) {
	err := status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED").WithParams(map[string]any{
		"reason": "IMChatNotAuthorized", "targetId": "target", "hint": "enable_workflow_delivery",
		"delivery": map[string]any{"deliveryId": "delivery", "state": "unknown", "partsSent": 1},
	})
	failure := failureFromError(err, nil, nil).Data.(map[string]any)
	if failure["reason"] != "IMChatNotAuthorized" || failure["targetId"] != "target" || failure["hint"] != "enable_workflow_delivery" || failure["delivery"] == nil {
		t.Fatalf("具体原因及投递证据丢失: %+v", failure)
	}
}

func TestLateDeterministicReceiptCannotChangeConfirmedTerminalResult(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/1',title:'终态收据',description:'终态不可覆盖',phases:[{id:'compute',title:'计算',detail:'计算',canExit:true}]};const value=await functionTask({id:'once',label:'计算结果',phase:'compute',language:'javascript',input:{},code:'function main(){return 0;}'});return value;`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, err := s.runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("prepare: %+v %v", state, err)
	}
	effect := state.Effects[0]
	// 夹具模拟已经确认并保存的成功效果；真实 Runner 从原检查点形成运行终态。
	err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		confirmed := workflowdsl.JSONValue(float64(0))
		current.Status = constants.WorkflowStepStatusSucceeded
		current.Result, current.Failure = &confirmed, nil
		state.Run.Status = constants.WorkflowRunStatusRecovering
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmed := runTicks(t, s, run.ID, 3)
	if confirmed.Status != constants.WorkflowRunStatusSucceeded || confirmed.Result != float64(0) {
		t.Fatalf("confirmed receipt did not complete original checkpoint: %+v", confirmed)
	}
	// 使用原效果身份交付迟到失败，不能改写已有成功收据或运行终态。
	s.completeDeterministic(ctx, effect, nil, status_error.WorkflowEffectFailed)
	final, err := s.runs.LoadRunState(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Run.Status != constants.WorkflowRunStatusSucceeded || final.Run.Version != confirmed.Version || final.Run.Result != confirmed.Result {
		t.Fatalf("late receipt changed final result: %+v", final.Run)
	}
	if len(final.Effects) != 1 || final.Effects[0].Status != constants.WorkflowStepStatusSucceeded || final.Effects[0].Failure != nil || final.Effects[0].Result == nil || final.Effects[0].Result.Data != float64(0) {
		t.Fatalf("late receipt overwrote confirmed effect: %+v", final.Effects)
	}
}

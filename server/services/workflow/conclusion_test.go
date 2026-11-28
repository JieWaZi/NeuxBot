package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestWorkflowConclusionReturnsOnceToOrdinaryChatAndNeverToCanvas(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		source string
		status constants.WorkflowRunStatus
		want   string
	}{
		{"succeeded", "return 'verified result';", constants.WorkflowRunStatusSucceeded, "verified result"},
		{"failed", "throw new Error('step failed');", constants.WorkflowRunStatusFailed, "运行失败"},
		{"cancelled", "return 'unused';", constants.WorkflowRunStatusCancelled, "运行已停止"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := t.Context()
			s, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, s, runtimeTestMeta+scenario.source)
			conversation := consumerFixtureChat(t, "original-chat")
			run, actual := startChatDeliveryFixture(t, s, conversation, "original-execution", definition)
			canvas, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "canvas"})
			if err != nil {
				t.Fatal(err)
			}
			if scenario.status == constants.WorkflowRunStatusCancelled {
				for _, id := range []string{run.ID, canvas.ID} {
					if err := s.CancelRun(ctx, id); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, id := range []string{run.ID, canvas.ID} {
				if final := runTicks(t, s, id, 8); final.Status != scenario.status {
					t.Fatalf("run %s finished as %s, want %s", id, final.Status, scenario.status)
				}
			}
			at := int64(100)
			actual.Status, actual.CompletedAt = constants.ExecutionStatusCompleted, &at
			boundary := &conclusionChatRecorder{deliveryChatBoundary: &deliveryChatBoundary{actual: actual}}
			s.chats = boundary
			for range 2 {
				s.drainConclusions(ctx, "")
			}
			if len(boundary.conclusions) != 1 {
				t.Fatalf("expected one conclusion in the original chat, got %+v", boundary.conclusions)
			}
			got := boundary.conclusions[0]
			if got.ChatID != conversation.ID || got.ExecutionID != actual.ID || got.SourceID != run.ID || !strings.Contains(got.Summary, scenario.want) {
				t.Fatalf("wrong original destination or result: %+v", got)
			}
		})
	}
}

func TestWorkflowRestartReconcilesTheFrozenWorkerBeforeOpeningApproval(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "workflow.db")
	before, closeBefore := openRecoveryWorkflow(t, path)
	before.worker = &controlledWorker{}
	source := runtimeTestMeta + `const result=await worker('B',{id:'facts',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'},schema:{type:'string'}});await askUser('批准？',{id:'review',label:'审批',phase:'work',kind:'confirm'});return result;`
	definition := savedTestWorkflow(t, before, source)
	run, err := before.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, before, run.ID, 2)
	dispatchPrepared(t, before, run.ID)
	state, err := before.runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("worker step was not persisted: %+v %v", state, err)
	}
	effect := state.Effects[0]
	if effect.Status != constants.WorkflowStepStatusRunning || effect.ChatID == "" || effect.ExecutionID == "" {
		t.Fatalf("first dispatch did not bind the worker: %+v", effect)
	}
	scope := workflowtool.Scope{Key: effect.ID, Token: effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}
	if _, err := before.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "confirmed original result"}); err != nil {
		t.Fatal(err)
	}
	// 原执行的终态已可读取，Runner 尚未消费它。
	if err := before.runs.ReleaseLeases(ctx, before.state.Owner, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	closeBefore()
	after, _ := openRecoveryWorkflow(t, path)
	boundary := &restartWorkerReceipt{testing: t, original: effect}
	after.worker = boundary
	restored, err := after.tick(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	after.dispatchEffect(ctx, restored.Run, effect)
	after.dispatchEffect(ctx, restored.Run, effect)
	runTicks(t, after, run.ID, 4)
	state, err = after.runs.LoadRunState(ctx, run.ID)
	if err != nil || state.Run.Status != constants.WorkflowRunStatusWaiting || len(state.Humans) != 1 || state.Humans[0].Status != constants.WorkflowStepStatusWaiting {
		t.Fatalf("recovered worker did not open the original downstream approval: %+v %v", state, err)
	}
	verified, err := after.runs.GetEffect(ctx, effect.ID)
	if err != nil || boundary.reads != 1 || verified.Status != constants.WorkflowStepStatusSucceeded || verified.Result == nil || verified.Result.Data != "confirmed original result" {
		t.Fatalf("worker was not settled once: reads=%d, verified=%+v, error=%v", boundary.reads, verified, err)
	}
}

func TestWorkflowRestartDeliversAnAlreadySettledScheduledRunOnce(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "workflow.db")
	before, closeBefore := openRecoveryWorkflow(t, path)
	definition := savedTestWorkflow(t, before, runtimeTestMeta+"return 'confirmed report';")
	recipient := &models.MainConversationRecipient{SubjectType: "team", SubjectID: "original-team"}
	run, err := before.StartRun(ctx, StartRunCommand{
		DefinitionID: definition.ID, RequestID: "fixed-occurrence", AutomaticRunID: "automatic-run",
		TriggerSource: string(constants.AutomaticRunSourceSchedule), ConclusionRecipient: recipient,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := runTicks(t, before, run.ID, 5)
	if finished.Status != constants.WorkflowRunStatusSucceeded || finished.ConclusionFinishedAt != nil {
		t.Fatalf("unexpected state before publication: %+v", finished)
	}
	closeBefore()
	after, _ := openRecoveryWorkflow(t, path)
	publications := 0
	after.publishMainConclusion = func(_ context.Context, confirmed models.WorkflowRun, content string) error {
		if confirmed.ID != run.ID || confirmed.ConclusionRecipient == nil || *confirmed.ConclusionRecipient != *recipient || !strings.Contains(content, "confirmed report") {
			t.Fatalf("restart changed the report or its frozen destination: %+v %q", confirmed, content)
		}
		publications++
		return nil
	}
	for range 2 {
		after.drainConclusions(ctx, "")
	}
	if err := after.RetryConclusion(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	receipt, err := after.runs.GetRun(ctx, run.ID)
	if err != nil || publications != 1 || receipt.ConclusionDeliveryError != "" || receipt.ConclusionFinishedAt == nil {
		t.Fatalf("restart did not settle one durable publication: count=%d receipt=%+v error=%v", publications, receipt, err)
	}
}

func TestScheduledWorkflowQuietlySettlesNoChangesInMainConversation(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		policy       constants.AutomaticReportPolicy
		want         int
	}{
		{"no-change-always", "return '[NEUX_NO_CHANGES]';", constants.AutomaticReportPolicyAlways, 0},
		{"no-change-changes-only", "return '[NEUX_NO_CHANGES]';", constants.AutomaticReportPolicyChangesOnly, 0},
		{"empty", "return null;", constants.AutomaticReportPolicyAlways, 0},
		{"blank", "return '  ';", constants.AutomaticReportPolicyAlways, 0},
		{"changed", "return 'New item #2';", constants.AutomaticReportPolicyAlways, 1},
		{"failed", "throw new Error('check failed');", constants.AutomaticReportPolicyChangesOnly, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "monitor.db")
			before, closeBefore := openRecoveryWorkflow(t, path)
			definition := savedTestWorkflow(t, before, runtimeTestMeta+tc.source)
			run, err := before.StartRun(ctx, StartRunCommand{
				DefinitionID: definition.ID, RequestID: "fixed-occurrence", AutomaticRunID: "automatic-run",
				TriggerSource:       string(constants.AutomaticRunSourceSchedule),
				ConclusionRecipient: &models.MainConversationRecipient{SubjectType: "team", SubjectID: "recipient"},
				ConclusionPolicy:    tc.policy,
			})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, before, run.ID, 5)
			closeBefore()
			after, _ := openRecoveryWorkflow(t, path)
			publications := 0
			after.publishMainConclusion = func(_ context.Context, _ models.WorkflowRun, content string) error {
				if tc.name == "changed" && !strings.Contains(content, "New item #2") {
					t.Fatalf("missing report content: %q", content)
				}
				publications++
				return nil
			}
			after.drainConclusions(ctx, "")
			if err := after.RetryConclusion(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			receipt, err := after.runs.GetRun(ctx, run.ID)
			if err != nil || receipt.ConclusionFinishedAt == nil || publications != tc.want {
				t.Fatalf("publications=%d want=%d, receipt=%+v error=%v", publications, tc.want, receipt, err)
			}
		})
	}
}

func TestWorkflowStepIMDeliveryKeepsItsDestinationWithOrWithoutMainReport(t *testing.T) {
	ctx := t.Context()
	service, _ := workflowTestService(t)
	source := `export const meta={sdk:'bot.workflow',title:'发送步骤',phases:[{id:'work',title:'发送',detail:'独立步骤产出',canExit:true}],actions:[{id:'send',label:'步骤通知',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};
const actions={send:async(ctx,input)=>await ctx.callTool({callId:'step-result',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'step-destination',content:'步骤产出'}})};
await action('send',{id:'deliver',label:'交付步骤产出',phase:'work'});return '整图结论';`
	definition := savedTestWorkflow(t, service, source)
	service.imValidate = func(context.Context, string) error { return nil }
	stepSends := make(chan models.WorkflowActionCall, 2)
	service.imCall = func(_ context.Context, _ models.WorkflowRun, call models.WorkflowActionCall) (any, error) {
		stepSends <- call
		return map[string]any{"deliveryId": "im-receipt"}, nil
	}
	conclusions := make(chan string, 2)
	service.publishMainConclusion = func(_ context.Context, run models.WorkflowRun, content string) error {
		conclusions <- run.ID + ":" + content
		return nil
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	for _, key := range []string{"off", "on"} {
		cmd := StartRunCommand{DefinitionID: definition.ID, RequestID: key, AutomaticRunID: key, TriggerSource: "schedule", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}}
		if key == "on" {
			cmd.ConclusionRecipient = &models.MainConversationRecipient{SubjectType: "team", SubjectID: "main-destination"}
		}
		run, err := service.StartRun(ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		state := waitWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool {
			return state.Run.Status == constants.WorkflowRunStatusSucceeded
		})
		if len(state.ActionCalls) != 1 || state.ActionCalls[0].Status != constants.WorkflowStepStatusSucceeded {
			t.Fatalf("step delivery receipt was lost: %+v", state.ActionCalls)
		}
		var step models.WorkflowActionCall
		select {
		case step = <-stepSends:
		default:
			t.Fatal("step delivery was missing")
		}
		input, _ := step.Input.(map[string]any)
		if step.RunID != run.ID || step.CallID != "step-result" || input["targetId"] != "step-destination" {
			t.Fatalf("step was sent to the wrong audience: %+v", step)
		}
		if key == "on" {
			if err := service.RetryConclusion(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case conclusion := <-conclusions:
				if !strings.HasPrefix(conclusion, run.ID+":") || !strings.Contains(conclusion, "整图结论") {
					t.Fatalf("wrong terminal report: %s", conclusion)
				}
			default:
				t.Fatal("terminal conclusion was missing")
			}
		}
	}
	if len(stepSends) != 0 || len(conclusions) != 0 {
		t.Fatal("the step or terminal conclusion was delivered more than once")
	}
}

type restartWorkerReceipt struct {
	controlledWorker
	testing  *testing.T
	original models.WorkflowEffect
	reads    int
}

func (w *restartWorkerReceipt) Dispatch(context.Context, WorkerDispatch) (WorkerReceipt, error) {
	w.testing.Fatal("restart dispatched a new worker for the bound step")
	return WorkerReceipt{}, nil
}

func (w *restartWorkerReceipt) Inspect(_ context.Context, effect models.WorkflowEffect) (WorkerReceipt, error) {
	if effect.ID != w.original.ID || effect.ChatID != w.original.ChatID || effect.ExecutionID != w.original.ExecutionID {
		w.testing.Fatalf("restart inspected the wrong receipt: %+v", effect)
	}
	w.reads++
	return WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true}, nil
}

type conclusionChatRecorder struct {
	*deliveryChatBoundary
	conclusions []chat.ExecutionDelivery
}

func (b *conclusionChatRecorder) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	if chatID != b.actual.ChatID || executionID != b.actual.ID {
		panic("conclusion attempted to read another execution")
	}
	return b.actual, nil
}

func (b *conclusionChatRecorder) PublishExecutionDelivery(_ context.Context, delivery chat.ExecutionDelivery) error {
	if delivery.Summary != "" {
		b.conclusions = append(b.conclusions, delivery)
	}
	return nil
}

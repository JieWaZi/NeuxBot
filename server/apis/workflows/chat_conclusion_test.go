package workflows

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

func TestWorkflowAgentResultStillWaitsForOriginalApprovalBeforeChatConclusion(t *testing.T) {
	const source = `export const meta={sdk:'bot.workflow',title:'审核流程',phases:[{id:'work',title:'执行',detail:'分析和审批',canExit:true}]};
const result = await worker('整理',{id:'facts',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object'}});
const approval = await askUser('批准？',{id:'review',label:'审批',phase:'work',kind:'confirm',allowSkip:false});
if (!approval.value) { throw new Error('user rejected review'); }
return result;`
	for _, approve := range []bool{true, false} {
		name := "approve"
		expectedStatus, expectedText := constants.WorkflowRunStatusSucceeded, "完成导出"
		if !approve {
			name, expectedStatus, expectedText = "reject", constants.WorkflowRunStatusFailed, "运行失败"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			runtime := &fixtureRuntime{calls: map[string]int{}}
			service, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{}, true)
			definition := savedTestWorkflow(t, service, source)
			sourceChat, execution, runID := startWaitingWorkflowFromChat(t, service, definition)
			state := awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
				return state.Run.Status == constants.WorkflowRunStatusWaiting && len(state.Humans) == 1
			})
			if state.Humans[0].Status != constants.WorkflowStepStatusWaiting || state.Run.CompletedAt != nil || state.Run.ConclusionFinishedAt != nil {
				t.Fatalf("agent completion prematurely settled the workflow: %+v", state.Run)
			}
			if items := workflowExecutionConclusions(t, service, sourceChat.ID, execution.ID, runID); len(items) != 0 {
				t.Fatalf("approval wait published a terminal conclusion: %+v", items)
			}
			answer := AnswerHumanCommand{RunID: runID, HumanID: state.Humans[0].ID, RequestID: "answer", Value: approve}
			wrong := answer
			wrong.HumanID = "different-step"
			if err := answerHumanFixture(ctx, workflowAPIDependencies(service), wrong); !errors.Is(err, status_error.WorkflowNotFound) {
				t.Fatalf("wrong step answered original approval: %v", err)
			}
			if err := answerHumanFixture(ctx, workflowAPIDependencies(service), answer); err != nil {
				t.Fatal(err)
			}
			awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
				return state.Run.Status == expectedStatus
			})
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
				run, err := service.Runs.GetRun(ctx, runID)
				if err != nil {
					t.Fatal(err)
				}
				if run.ConclusionFinishedAt != nil {
					if run.ConclusionDeliveryError != "" {
						t.Fatalf("automatic conclusion failed: %s", run.ConclusionDeliveryError)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("terminal workflow never delivered its conclusion")
				}
			}
			for range 2 {
				if err := service.RetryConclusion(ctx, runID); err != nil {
					t.Fatal(err)
				}
			}
			items := workflowExecutionConclusions(t, service, sourceChat.ID, execution.ID, runID)
			if len(items) != 1 || !strings.Contains(items[0].Summary, expectedText) {
				t.Fatalf("wrong terminal conclusion on the original execution: %+v", items)
			}
			runtime.mutex.Lock()
			defer runtime.mutex.Unlock()
			if runtime.calls["facts"] != 1 {
				t.Fatalf("the worker's already reconciled result was dispatched again: %+v", runtime.calls)
			}
		})
	}
}

func TestWorkflowApprovalReturnsToTheOriginalPublicInput(t *testing.T) {
	ctx := t.Context()
	service, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{calls: map[string]int{}}, fixtureEnvironment{}, true)
	definition := savedTestWorkflow(t, service, humanSource)
	chat, execution, runID := startWaitingWorkflowFromChat(t, service, definition, "approval-work")
	public, err := service.main.Ensure(ctx, "waker", chat.WakerID)
	if err != nil {
		t.Fatal(err)
	}
	inputID := mainconversation.UserMessageID(public.ID, execution.SubmissionID)
	state := awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusWaiting && len(state.Humans) == 1
	})
	if state.Run.ConclusionMainID != public.ID || state.Run.ConclusionReplyID != inputID {
		t.Fatalf("main admission did not preserve the public input: %+v", state.Run)
	}
	if err := answerHumanFixture(ctx, workflowAPIDependencies(service), AnswerHumanCommand{RunID: runID, HumanID: state.Humans[0].ID, RequestID: "answer", Value: "approve"}); err != nil {
		t.Fatal(err)
	}
	awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusSucceeded
	})
	for range 2 {
		if err := service.RetryConclusion(ctx, runID); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := service.main.Messages(ctx, public.ID, 0, 10)
	if err != nil || len(rows) != 2 || rows[1].ReplyToMessageID != inputID || rows[1].WorkflowRun == nil || rows[1].WorkflowRun.RunID != runID {
		t.Fatalf("main timeline lost its original address or run: %+v, %v", rows, err)
	}
	if other := workflowExecutionConclusions(t, service, chat.ID, execution.ID, runID); len(other) != 0 {
		t.Fatalf("hidden chat also received a public conclusion: %+v", other)
	}
}

func workflowExecutionConclusions(t *testing.T, service *workflowFixture, chatID, executionID, runID string) []eventlog.ExecutionDeliveryData {
	t.Helper()
	records, err := service.Chats.ReadExecutionRecords(chatID, executionID)
	if err != nil {
		t.Fatal(err)
	}
	var conclusions []eventlog.ExecutionDeliveryData
	for _, record := range records {
		if record.Type != constants.ChatEventTypeExecutionDelivery {
			continue
		}
		var delivery eventlog.ExecutionDeliveryData
		if err := json.Unmarshal(record.Data, &delivery); err != nil {
			t.Fatal(err)
		}
		if delivery.SourceID == runID && delivery.Summary != "" {
			conclusions = append(conclusions, delivery)
		}
	}
	return conclusions
}

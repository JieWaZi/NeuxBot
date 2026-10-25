package workflows

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func saveChildFixture(t *testing.T, s *workflowFixture, directory, source string) models.WorkflowDefinition {
	t.Helper()
	ctx := context.Background()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: util.New(), Title: "子流程验收", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: util.New(), Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("save child fixture: %v diagnostics=%v", err, diagnostics)
	}
	definition.CurrentRevisionID = receipt.RevisionID
	return definition
}

func TestChildHandleRestartKeepsSingleFrozenChildAndActualResult(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	runtime := &fixtureRuntime{calls: map[string]int{}}
	s, close := openCoreWorkflowFixture(t, directory, runtime)
	child := saveChildFixture(t, s, directory, strings.Replace(humanSource, "return {approved:", "return {input:args.message,parent:flowContext.parentRunId,approved:", 1))
	source := `export const meta={sdk:'ally.workflow/1',title:'先启动再等待',description:'冻结与恢复',phases:[{id:'main',title:'子任务',detail:'独立持久句柄',canExit:true}]}; const handle=startWorkflow(` + strconv.Quote(child.ID) + `,{id:'spawn',label:'启动子任务',phase:'main',mode:'joined',revisionId:` + strconv.Quote(child.CurrentRevisionID) + `,args:{message:'frozen-input'}});const result=await joinWorkflow(handle);return result;`
	parent := saveChildFixture(t, s, directory, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: parent.ID, RequestID: "parent-run"})
	if err != nil {
		t.Fatal(err)
	}
	var childRun models.WorkflowRun
	awaitFixtureState(t, s, run.ID, func(_ controllers.WorkflowRunState) bool {
		children, err := s.Runs.ListChildren(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(children) == 1 {
			childRun = children[0]
		}
		return childRun.Status == constants.WorkflowRunStatusWaiting
	})
	state, err := s.Runs.LoadRunState(ctx, childRun.ID)
	if err != nil || len(state.Humans) != 1 {
		t.Fatal("missing actual child request", err)
	}
	humanID := state.Humans[0].ID
	if _, _, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: child.ID, ExpectedRevisionID: child.CurrentRevisionID, RequestID: "new-child-version", Source: strings.Replace(humanSource, "人工关卡", "新版人工关卡", 1), SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatal(err)
	}
	close()
	s, _ = openCoreWorkflowFixture(t, directory, runtime)
	state = awaitFixtureState(t, s, childRun.ID, func(state controllers.WorkflowRunState) bool {
		return len(state.Humans) == 1 && state.Humans[0].Status == constants.WorkflowStepStatusWaiting
	})
	if state.Humans[0].ID != humanID || state.Run.RevisionID != child.CurrentRevisionID || state.Run.ScopeBinding.WorkingDirectory != run.ScopeBinding.WorkingDirectory {
		t.Fatal("child identity, revision or approved scope changed")
	}
	if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: childRun.ID, HumanID: humanID, RequestID: "explicit-child-answer", Value: "approve"}); err != nil {
		t.Fatal(err)
	}
	state = awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result, _ := state.Run.Result.(map[string]any)
	childResult, _ := result["result"].(map[string]any)
	children, err := s.Runs.ListChildren(ctx, run.ID)
	if err != nil || len(children) != 1 || state.Run.Status != constants.WorkflowRunStatusSucceeded || childResult["approved"] != true || childResult["input"] != "frozen-input" || childResult["parent"] != run.ID || result["revisionId"] != child.CurrentRevisionID {
		t.Fatalf("child result: status=%s result=%v children=%d error=%v", state.Run.Status, result, len(children), err)
	}
}

func TestJoinedChildCancellationLeavesDetachedChildAvailable(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, _ := openCoreWorkflowFixture(t, directory, &fixtureRuntime{calls: map[string]int{}})
	child := saveChildFixture(t, s, directory, humanSource)
	source := `export const meta={sdk:'ally.workflow/1',title:'后台与跟随子任务',description:'取消边界',phases:[{id:'main',title:'子任务',detail:'生命周期',canExit:true}]};startWorkflow(` + strconv.Quote(child.ID) + `,{id:'joined',label:'跟随子任务',phase:'main',mode:'joined'});startWorkflow(` + strconv.Quote(child.ID) + `,{id:'detached',label:'后台子任务',phase:'main',mode:'detached'});return {started:true};`
	parent := saveChildFixture(t, s, directory, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: parent.ID, RequestID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	awaitFixtureState(t, s, run.ID, func(_ controllers.WorkflowRunState) bool {
		children, err := s.Runs.ListChildren(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(children) == 2 && children[0].Status == constants.WorkflowRunStatusWaiting && children[1].Status == constants.WorkflowRunStatusWaiting
	})
	if err := s.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	if state.Run.Status != constants.WorkflowRunStatusCancelled {
		t.Fatal("parent cancellation did not settle")
	}
	children, err := s.Runs.ListChildren(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, childRun := range children {
		if childRun.Joined {
			if childRun.Status != constants.WorkflowRunStatusCancelled {
				t.Fatal("joined child survived cancellation")
			}
			continue
		}
		childState, err := s.Runs.LoadRunState(ctx, childRun.ID)
		if err != nil || childRun.Status != constants.WorkflowRunStatusWaiting || len(childState.Humans) != 1 {
			t.Fatal("detached child lost its durable request", err)
		}
		if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: childRun.ID, HumanID: childState.Humans[0].ID, RequestID: "detached-answer", Value: "approve"}); err != nil {
			t.Fatal(err)
		}
		finished := awaitFixtureState(t, s, childRun.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
		if finished.Run.Status != constants.WorkflowRunStatusSucceeded {
			t.Fatal("detached child could not complete after parent stopped")
		}
	}
}

func TestRecursiveWorkflowIsRejectedBeforeCreatingSecondRun(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, _ := openCoreWorkflowFixture(t, directory, &fixtureRuntime{calls: map[string]int{}})
	definition := saveChildFixture(t, s, directory, humanSource)
	source := `export const meta={sdk:'ally.workflow/1',title:'递归拒绝',description:'循环调用',phases:[{id:'main',title:'调用',detail:'拒绝递归',canExit:true}]};return await workflow(` + strconv.Quote(definition.ID) + `,{id:'recursive',label:'递归',phase:'main'});`
	if _, _, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "recursive-source", Source: source, SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "recursive-run"})
	if err != nil {
		t.Fatal(err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	children, err := s.Runs.ListChildren(ctx, run.ID)
	if err != nil || len(children) != 0 || state.Run.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("recursive child created: status=%s count=%d error=%v", state.Run.Status, len(children), err)
	}
}

func TestJoinedChildTimeoutSettlesChildBeforeCatchReturns(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, _ := openCoreWorkflowFixture(t, directory, &fixtureRuntime{calls: map[string]int{}})
	child := saveChildFixture(t, s, directory, humanSource)
	source := `export const meta={sdk:'ally.workflow/1',title:'子任务超时',description:'捕获前停止子任务',phases:[{id:'main',title:'子任务',detail:'超时治理',canExit:true}]};try { await workflow(` + strconv.Quote(child.ID) + `,{id:'child',label:'等待子任务',phase:'main'}); } catch(failure) { return {caught:true}; } return {caught:false};`
	parent := saveChildFixture(t, s, directory, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: parent.ID, RequestID: "timeout-parent"})
	if err != nil {
		t.Fatal(err)
	}
	var childID, effectID string
	awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		children, err := s.Runs.ListChildren(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(children) == 1 && children[0].Status == constants.WorkflowRunStatusWaiting && len(state.Effects) == 1 {
			childID, effectID = children[0].ID, state.Effects[0].ID
			return true
		}
		return false
	})
	if err := s.State.MutateEffect(ctx, run.ID, effectID, func(_ context.Context, _ *controllers.WorkflowRunState, effect *models.WorkflowEffect) error {
		effect.DeadlineAt = time.Now().UnixMilli() - 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	finished, err := s.Runs.GetRun(ctx, childID)
	result, _ := state.Run.Result.(map[string]any)
	if err != nil || finished.Status != constants.WorkflowRunStatusCancelled || state.Run.Status != constants.WorkflowRunStatusSucceeded || result["caught"] != true {
		t.Fatalf("timeout did not settle: parent=%s child=%s result=%v error=%v", state.Run.Status, finished.Status, result, err)
	}
}

func TestUnobservedJoinedChildFailureFailsParent(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, _ := openCoreWorkflowFixture(t, directory, &fixtureRuntime{calls: map[string]int{}})
	childSource := `export const meta={sdk:'ally.workflow/1',title:'失败子任务',description:'未等待的失败仍传播',phases:[{id:'main',title:'子任务',detail:'失败传播',canExit:true}]};throw {code:'CHILD_FAILED'};`
	child := saveChildFixture(t, s, directory, childSource)
	source := `export const meta={sdk:'ally.workflow/1',title:'未等待的跟随任务',description:'不能隐藏子任务失败',phases:[{id:'main',title:'子任务',detail:'失败传播',canExit:true}]};startWorkflow(` + strconv.Quote(child.ID) + `,{id:'child',label:'跟随任务',phase:'main',mode:'joined'});return {started:true};`
	parent := saveChildFixture(t, s, directory, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: parent.ID, RequestID: "unobserved-child-parent"})
	if err != nil {
		t.Fatal(err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	children, err := s.Runs.ListChildren(ctx, run.ID)
	if err != nil || len(children) != 1 || children[0].Status != constants.WorkflowRunStatusFailed || state.Run.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("unobserved failure did not propagate: parent=%s children=%d error=%v", state.Run.Status, len(children), err)
	}
}

package workflows

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
)

func TestPublicRunQueriesKeepBusinessOriginAndInternalRecovery(t *testing.T) {
	s, store := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "public-run", TriggerSource: "automatic_task"})
	if err != nil {
		t.Fatal(err)
	}
	previousStore, previousRuns := global.WorkflowControllerManager, global.WorkflowRunControllerManager
	global.WorkflowControllerManager, global.WorkflowRunControllerManager = store, s.Runs
	t.Cleanup(func() {
		global.WorkflowControllerManager, global.WorkflowRunControllerManager = previousStore, previousRuns
	})
	child := run
	child.ID, child.ParentRunID, child.Depth = "child-run", run.ID, 1
	if err := s.Runs.CreateRun(context.Background(), child, models.WorkflowFiber{ID: "child-fiber", RunID: child.ID, Status: constants.WorkflowStepStatusReady}); err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	listed, err := (&ListWorkflowRuns{ID: definition.ID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := (&GetWorkflowRun{ID: definition.ID, RunID: run.ID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listRun map[string]any
	for _, item := range publicJSON(t, listed)["items"].([]any) {
		value := item.(map[string]any)
		if value["id"] == run.ID {
			listRun = value
		}
	}
	if listRun == nil {
		t.Fatal("parent run missing from history")
	}
	detailRun := publicJSON(t, detail)["run"].(map[string]any)
	children := publicJSON(t, detail)["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("missing child run: %+v", children)
	}
	childRun := children[0].(map[string]any)
	if childRun["parentRunId"] != run.ID {
		t.Fatalf("child business linkage lost: %+v", childRun)
	}
	for _, value := range []map[string]any{listRun, detailRun, childRun} {
		if value["triggerSource"] != "automatic_task" || value["id"] == "" || value["revisionId"] != run.RevisionID {
			t.Fatalf("lost run business identity/origin: %+v", value)
		}
		for _, field := range []string{"flowContext", "rootFiberId", "workspace"} {
			if _, found := value[field]; found {
				t.Errorf("public run leaks %s", field)
			}
		}
	}
	if !reflect.DeepEqual(listRun, detailRun) {
		t.Fatal("list/detail disagree on run business facts")
	}
	retained, err := s.Runs.GetRun(context.Background(), run.ID)
	if err != nil || retained.RootFiberID == "" || retained.Context["workspace"] == nil || retained.Context["triggerSource"] != "automatic_task" {
		t.Fatalf("internal recovery context lost: %+v %v", retained, err)
	}
	state, err := s.Runs.LoadRunState(context.Background(), run.ID)
	if err != nil || len(state.Fibers) != 1 || state.Fibers[0].ID != retained.RootFiberID {
		t.Fatalf("root execution frame lost: %+v %v", state, err)
	}
}

func TestPublicAttemptHistoryPreservesFailureAndCurrentControls(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "attempts"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, err := s.State.OwnedRun(context.Background(), run.ID)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("prepare effect: %+v %v", state, err)
	}
	failure := workflowdsl.Value{Kind: "json", Data: map[string]any{"code": "WORKER_FAILED", "message": "retry evidence"}}
	current := &state.Effects[0]
	current.OutputRepairs, current.FinalMessageID = 1, "internal-final"
	current.Candidate, current.SkipRequested = &failure, true
	old := *current
	old.Status, old.ChatID = constants.WorkflowStepStatusFailed, "old-chat"
	current.ID, current.AttemptGeneration = "current-attempt", 2
	state.Instances[0].EffectID = current.ID
	old.Failure = &failure
	state.Effects = append(state.Effects, old)
	if err := s.State.CommitRun(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	previousRuns, previousChats := global.WorkflowRunControllerManager, global.ChatService
	global.WorkflowRunControllerManager, global.ChatService = s.Runs, nil
	t.Cleanup(func() { global.WorkflowRunControllerManager, global.ChatService = previousRuns, previousChats })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	response, err := (&GetWorkflowInstance{ID: definition.ID, RunID: run.ID, InstanceID: state.Instances[0].ID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value := publicJSON(t, response)
	attempts := value["attempts"].([]any)
	if len(attempts) != 2 {
		t.Fatalf("lost attempts: %+v", attempts)
	}
	first := attempts[0].(map[string]any)
	if first["id"] != old.ID || first["chatId"] != "old-chat" || first["status"] != "failed" || first["failure"] == nil {
		t.Fatalf("lost historical evidence: %+v", first)
	}
	for _, item := range attempts {
		for field := range item.(map[string]any) {
			switch field {
			case "id", "attemptGeneration", "status", "chatId", "failure":
			default:
				t.Errorf("attempt leaks %s", field)
			}
		}
	}
	effect := value["effect"].(map[string]any)
	if effect["candidateSubmitted"] != true || effect["skipRequested"] != true || effect["attemptGeneration"] != float64(2) {
		t.Fatalf("current controls lost: %+v", effect)
	}
	retained, err := s.Runs.GetEffect(context.Background(), state.Effects[0].ID)
	if err != nil || retained.OutputRepairs != 1 || retained.FinalMessageID != "internal-final" || retained.FiberID == "" {
		t.Fatalf("internal recovery facts lost: %+v %v", retained, err)
	}
}

func publicJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPublicRunAcceptanceAndRetryKeepIndependentRunEvidence(t *testing.T) {
	s, store := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow',title:'失败证据',description:'独立重试',phases:[{id:'one',title:'阶段',detail:'保留旧结果',canExit:true}]};throw new Error('original failure');`
	definition := savedTestWorkflow(t, s, source)
	previousStore, previousRuns, previousService := global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowService
	global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowService = store, s.Runs, s.Service
	t.Cleanup(func() {
		global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowService = previousStore, previousRuns, previousService
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	accepted, err := (&StartWorkflowRun{ID: definition.ID, Body: workflow.StartRunCommand{RequestID: "accept-public"}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := publicJSON(t, accepted)
	runID, _ := first["id"].(string)
	if runID == "" || first["triggerSource"] != "manual" {
		t.Fatalf("acceptance lost run origin: %+v", first)
	}
	failed := runTicks(t, s, runID, 3)
	retried, err := (&RetryWorkflowRun{ID: definition.ID, RunID: runID, Body: RetryRunCommand{RequestID: "retry-public", ExpectedVersion: failed.Version}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second := publicJSON(t, retried)
	if second["id"] == runID || second["id"] == "" || second["triggerSource"] != "retry" || second["revisionId"] != first["revisionId"] {
		t.Fatalf("retry lost independent frozen evidence: %+v", second)
	}
	for _, value := range []map[string]any{first, second} {
		for _, field := range []string{"flowContext", "rootFiberId"} {
			if _, found := value[field]; found {
				t.Errorf("acceptance/retry leaks %s", field)
			}
		}
	}
	explicit, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RevisionID: failed.RevisionID, RequestID: "explicit-source", RetrySourceRunID: runID, TriggerSource: "api"})
	if err != nil || explicit.Context["triggerSource"] != "api" {
		t.Fatalf("retry replaced an explicit origin: %+v %v", explicit, err)
	}
	original, err := s.Runs.GetRun(context.Background(), runID)
	if err != nil || original.Status != constants.WorkflowRunStatusFailed || original.Version != failed.Version {
		t.Fatalf("retry replaced original evidence: %+v %v", original, err)
	}
}

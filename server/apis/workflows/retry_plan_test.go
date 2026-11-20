package workflows

import (
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
)

func TestRetryPlanUsesUnknownEffectEvenWhenInstanceFailed(t *testing.T) {
	state := controllers.WorkflowRunState{Run: models.WorkflowRun{ID: "run", RevisionID: "rev", Status: constants.WorkflowRunStatusFailed}, Instances: []models.WorkflowNodeInstance{{ID: "instance", NodeID: "node", Status: constants.WorkflowStepStatusFailed}}, Effects: []models.WorkflowEffect{{InstanceID: "instance", Status: constants.WorkflowStepStatusUnknown}}}
	plan := buildWorkflowRetryPlan(state, nil)
	if plan.Allowed || len(plan.Items) != 1 || plan.Items[0].Action != "reconcile" {
		t.Fatal(plan)
	}
}

func TestRetryPlanRejectsUncertainJoinedChildOutcome(t *testing.T) {
	failure := workflowdsl.JSONValue(map[string]any{"code": constants.WorkflowFailureEffectOutcomeUnknown})
	state := controllers.WorkflowRunState{
		Run:       models.WorkflowRun{ID: "parent", RevisionID: "rev", Status: constants.WorkflowRunStatusFailed},
		Instances: []models.WorkflowNodeInstance{{ID: "join", NodeID: "await", Status: constants.WorkflowStepStatusFailed}},
		Effects:   []models.WorkflowEffect{{InstanceID: "join", ChildRunID: "child", Status: constants.WorkflowStepStatusFailed, Failure: &failure}},
	}
	plan := buildWorkflowRetryPlan(state, nil)
	if plan.Allowed || len(plan.Items) != 1 || plan.Items[0].Action != "reconcile" {
		t.Fatalf("uncertain child effects could be duplicated: %+v", plan)
	}
}

func TestRetryPlanFollowsInstanceCreationOrder(t *testing.T) {
	state := controllers.WorkflowRunState{
		Run: models.WorkflowRun{ID: "run", RevisionID: "rev", Status: constants.WorkflowRunStatusFailed},
		Instances: []models.WorkflowNodeInstance{
			{ID: "a", NodeID: "third-a", CreatedAt: 300, Status: constants.WorkflowStepStatusFailed},
			{ID: "b", NodeID: "third-b", CreatedAt: 300, Status: constants.WorkflowStepStatusFailed},
			{ID: "m", NodeID: "second", CreatedAt: 200, Kind: "worker", Status: constants.WorkflowStepStatusSucceeded},
			{ID: "z", NodeID: "first", CreatedAt: 100, Status: constants.WorkflowStepStatusSucceeded},
		},
	}
	plan := buildWorkflowRetryPlan(state, nil)
	expected := []string{"first", "second", "third-a", "third-b"}
	if !plan.Allowed || len(plan.Items) != len(expected) {
		t.Fatal(plan)
	}
	for i, nodeID := range expected {
		if plan.Items[i].NodeID != nodeID {
			t.Fatalf("plan position %d: got %s, want %s", i, plan.Items[i].NodeID, nodeID)
		}
	}
	if plan.Items[1].Action != "evaluate_reuse" {
		t.Fatal("ordering changed reuse policy", plan.Items[1])
	}
	if state.Instances[0].ID != "a" || state.Instances[3].ID != "z" {
		t.Fatal("plan sorting mutated the caller's state")
	}
}

func TestRetryPlanAndAdmissionAgreeOnDeepJoinedUnknownEffect(t *testing.T) {
	for _, joined := range []bool{true, false} {
		name := "joined"
		if !joined {
			name = "detached"
		}
		t.Run(name, func(t *testing.T) {
			fixture, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, fixture, workflowTestSource(t))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/retry-plan", nil)
			runs := []models.WorkflowRun{
				{ID: "parent", RootRunID: "parent", DefinitionID: definition.ID, RevisionID: definition.CurrentRevisionID, Status: constants.WorkflowRunStatusCancelled, Version: 2, Args: map[string]any{"text": "keep"}},
				{ID: "child", RootRunID: "parent", ParentRunID: "parent", Joined: joined, Status: constants.WorkflowRunStatusCancelled},
				{ID: "grandchild", RootRunID: "parent", ParentRunID: "child", Joined: true, Status: constants.WorkflowRunStatusFailed},
			}
			if err := fixture.db.Create(&runs).Error; err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.Create(&models.WorkflowNodeInstance{ID: "join", NodeID: "await", RunID: "parent", Status: constants.WorkflowStepStatusFailed}).Error; err != nil {
				t.Fatal(err)
			}
			for _, effect := range []models.WorkflowEffect{
				{ID: "join-effect", RunID: "parent", InstanceID: "join", ChildRunID: "child", Status: constants.WorkflowStepStatusFailed},
				{ID: "unknown-effect", RunID: "grandchild", Status: constants.WorkflowStepStatusUnknown},
			} {
				if err := fixture.db.Create(&effect).Error; err != nil {
					t.Fatal(err)
				}
			}
			previousRuns, previousTransactions := global.WorkflowRunControllerManager, global.TransactionControllerManager
			global.WorkflowRunControllerManager, global.TransactionControllerManager = fixture.Runs, fixture.Transactions
			t.Cleanup(func() {
				global.WorkflowRunControllerManager, global.TransactionControllerManager = previousRuns, previousTransactions
			})
			response, err := (&GetWorkflowRetryPlan{ID: definition.ID, RunID: "parent"}).Output(ctx)
			if err != nil {
				t.Fatal(err)
			}
			plan := response.(WorkflowRetryPlan)
			if plan.Allowed == joined || joined && (len(plan.Items) != 1 || plan.Items[0].Action != "reconcile") {
				t.Fatalf("incorrect retry preview: %+v", plan)
			}
			started, err := retryRun(ctx, workflowAPIDependencies(fixture), RetryRunCommand{RunID: "parent", RequestID: "retry", ExpectedVersion: 2})
			if joined && (!util.HasStatusCode(err, status_error.WorkflowStateConflict) || started.ID != "") || !joined && (err != nil || started.ID == "") {
				t.Fatalf("admission disagreed with preview: %+v %v", started, err)
			}
		})
	}
}

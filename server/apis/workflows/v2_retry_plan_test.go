package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestV2RetryPlanUsesUnknownEffectEvenWhenInstanceFailed(t *testing.T) {
	state := controllers.WorkflowRunState{Run: models.WorkflowRun{ID: "run", RevisionID: "rev", Status: constants.WorkflowRunStatusFailed}, Instances: []models.WorkflowNodeInstance{{ID: "instance", NodeID: "node", Status: constants.WorkflowStepStatusFailed}}, Effects: []models.WorkflowEffect{{InstanceID: "instance", Status: constants.WorkflowStepStatusUnknown}}}
	plan := buildWorkflowRetryPlan(state)
	if plan.Allowed || len(plan.Items) != 1 || plan.Items[0].Action != "reconcile" {
		t.Fatal(plan)
	}
}

func TestV2RetryPlanFollowsInstanceCreationOrder(t *testing.T) {
	state := controllers.WorkflowRunState{
		Run: models.WorkflowRun{ID: "run", RevisionID: "rev", Status: constants.WorkflowRunStatusFailed},
		Instances: []models.WorkflowNodeInstance{
			{ID: "a", NodeID: "third-a", CreatedAt: 300, Status: constants.WorkflowStepStatusFailed},
			{ID: "b", NodeID: "third-b", CreatedAt: 300, Status: constants.WorkflowStepStatusFailed},
			{ID: "m", NodeID: "second", CreatedAt: 200, Kind: "worker", Status: constants.WorkflowStepStatusSucceeded},
			{ID: "z", NodeID: "first", CreatedAt: 100, Status: constants.WorkflowStepStatusSucceeded},
		},
	}
	plan := buildWorkflowRetryPlan(state)
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

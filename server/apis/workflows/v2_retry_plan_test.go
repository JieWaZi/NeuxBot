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

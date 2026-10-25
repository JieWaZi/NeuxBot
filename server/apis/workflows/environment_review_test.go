package workflows

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestF2PipelineIndependentApprovalAndReportFacts(t *testing.T) {
	s, _ := workflowTestService(t)
	worker := &controlledWorker{}
	s.Worker = worker
	s.Service = workflow.New(s.Config)
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/environment-review.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition := savedTestWorkflow(t, s, string(source))
	ctx := context.Background()
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
	if len(state.Humans) != 1 || !strings.Contains(state.Humans[0].Prompt, "A") {
		t.Fatal("A waited for B before requesting missing input")
	}
	if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "config", Value: "配置已确认"}); err != nil {
		t.Fatal(err)
	}
	state = waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 2 })
	var approvalID string
	for _, human := range state.Humans {
		if human.Status == constants.WorkflowStepStatusWaiting {
			approvalID = human.ID
		}
	}
	if len(state.Humans) != 2 || approvalID == "" {
		t.Fatal("A did not reach approval independently")
	}
	if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: approvalID, RequestID: "approve-A", Value: "approve"}); err != nil {
		t.Fatal(err)
	}
	state = waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return len(state.Groups) > 0 && len(state.Groups[0].Finished) == 3 && state.Groups[0].Finished[0] && state.Groups[0].Finished[2]
	})
	if !state.Groups[0].Finished[0] || state.Groups[0].Finished[1] || !state.Groups[0].Finished[2] {
		t.Fatal("A did not finish while B inspection remained active")
	}
	worker.completeB()
	state = waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting && strings.Contains(human.Prompt, "B") {
				return true
			}
		}
		return false
	})
	for _, human := range state.Humans {
		if human.Status == constants.WorkflowStepStatusWaiting {
			if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "skip-B", Skip: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	completed := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result := completed.Run
	facts, _ := result.Result.(map[string]any)
	items, _ := facts["environments"].([]any)
	if result.Status != constants.WorkflowRunStatusSucceeded || len(items) != 3 || items[2] != nil || facts["recommendationCount"] != float64(2) || facts["executedCount"] != float64(1) || facts["verifiedCount"] != float64(1) {
		t.Fatalf("report mismatched actual execution: %+v", result)
	}
	b, _ := items[1].(map[string]any)
	if b["executed"] != false || b["verified"] != false {
		t.Fatal("skipped approval became execution")
	}
	for _, prompt := range worker.recordedPrompts() {
		if prompt == "B-execute" || prompt == "B-verify" || prompt == "C-recommend" {
			t.Fatal("unauthorized or failed environment continued", prompt)
		}
	}
	notification, _ := facts["notificationDraft"].(map[string]any)
	if notification["sent"] != false || notification["text"] != facts["reportMarkdown"] {
		t.Fatal("notification/report facts inconsistent")
	}
}

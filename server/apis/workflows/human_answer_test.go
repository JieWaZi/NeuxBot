package workflows

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestHumanAnswerRequiresOriginalSnapshotCAS(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, _ := s.Runs.LoadRunState(context.Background(), run.ID)
	human := state.Humans[0]
	command := AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "answer", Value: "approve"}
	d := workflowAPIDependencies(s)
	if err = answerHuman(context.Background(), d, command); err == nil {
		t.Fatal("missing snapshot CAS accepted")
	}
	command.SnapshotDigest = "stale"
	if err = answerHuman(context.Background(), d, command); err == nil {
		t.Fatal("stale candidate snapshot accepted")
	}
	command.SnapshotDigest = human.SnapshotDigest
	command.Value = "批准"
	if err = answerHuman(context.Background(), d, command); err == nil {
		t.Fatal("label accepted as business key")
	}
	command.Value = "approve"
	if err = answerHuman(context.Background(), d, command); err != nil {
		t.Fatal(err)
	}
	if err = answerHuman(context.Background(), d, command); err != nil {
		t.Fatal("receipt replay rejected", err)
	}
	final := runTicks(t, s, run.ID, 3)
	if final.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatal(final)
	}
}

func TestExpiredJoinedParentInvalidatesWorkflowHumanRequest(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.Humans) != 1 {
		t.Fatalf("workflow is not waiting for input: %+v %v", state.Humans, err)
	}
	parent := run
	parent.ID = "0123456789abcdef0123456789abcdef"
	parent.RootRunID = parent.ID
	parent.DeadlineAt = time.Now().UnixMilli() - 1
	if err := s.db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&models.WorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{"parent_run_id": parent.ID, "root_run_id": parent.ID, "joined": true}).Error; err != nil {
		t.Fatal(err)
	}
	human := state.Humans[0]
	board := controllers.NewWorkBoardController(s.db)
	requests, count, _, err := board.Attention(ctx, controllers.WorkBoardQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	command := AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "late-answer", Value: "approve", SnapshotDigest: human.SnapshotDigest}
	answerErr := answerHuman(ctx, workflowAPIDependencies(s), command)
	if count != 0 || !errors.Is(answerErr, status_error.WorkflowStateConflict) {
		t.Fatalf("joined request remained actionable past the parent's deadline: requests=%+v answer=%v", requests, answerErr)
	}
	if err := s.db.Model(&models.WorkflowRun{}).Where("id = ?", run.ID).UpdateColumn("joined", false).Error; err != nil {
		t.Fatal(err)
	}
	requests, count, _, err = board.Attention(ctx, controllers.WorkBoardQuery{Limit: 5})
	if err != nil || count != 1 || requests[0].OwnerID != run.ID {
		t.Fatalf("detached request did not retain independent authority: requests=%+v error=%v", requests, err)
	}
	if err := answerHuman(ctx, workflowAPIDependencies(s), command); err != nil {
		t.Fatalf("detached request answer rejected: %v", err)
	}
}

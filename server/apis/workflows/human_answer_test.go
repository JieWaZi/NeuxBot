package workflows

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"testing"
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
	command.QuestionVersion = human.QuestionVersion
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

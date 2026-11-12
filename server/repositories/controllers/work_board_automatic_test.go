package controllers

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestStandaloneAutomaticFailureKeepsFrozenExecutor(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	task := automaticTaskFixture("current-waker")
	tasks := NewAutomaticTaskController(db)
	if err := tasks.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	board := NewWorkBoardController(db)
	for _, scenario := range []struct {
		kind       constants.AutomaticTaskTargetKind
		channelID  string
		workflowID string
		executorID string
	}{
		{kind: constants.AutomaticTaskTargetKindWorkflow, workflowID: "original-workflow", executorID: "original-workflow"},
		{kind: constants.AutomaticTaskTargetKindTeam, channelID: "original-team", executorID: "original-team"},
		{executorID: "original-waker"},
	} {
		t.Run(scenario.executorID, func(t *testing.T) {
			config := task.AutomaticTaskConfiguration
			config.TargetKind, config.WorkflowID, config.ChannelID = scenario.kind, scenario.workflowID, scenario.channelID
			run := models.AutomaticRun{
				ID: "run-" + scenario.executorID, TaskID: task.ID, WakerID: "original-waker",
				OccurrenceKey: scenario.executorID, Source: constants.AutomaticRunSourceManual, Configuration: config,
				Admission: constants.AutomaticRunAdmissionFailed, ErrorCode: "RuntimeUnavailable",
			}
			if err := tasks.CreateRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			if scenario.kind == "" {
				if err := db.Model(&run).UpdateColumn("configuration", "{}").Error; err != nil {
					t.Fatal(err)
				}
			}
			query := WorkBoardQuery{ExecutorID: scenario.executorID, Source: "automatic", Limit: 10}
			counts, err := board.Counts(ctx, query)
			if err != nil || counts.Total != 1 || counts.Running != 0 || counts.Ended != 1 {
				t.Errorf("filtered counts = %+v, %v", counts, err)
			}
			check := func(row models.WorkBoardEntry) {
				t.Helper()
				if row.OwnerKind != constants.WorkOwnerKindAutomaticRun || row.OwnerID != run.ID || row.AutomaticTaskID != task.ID ||
					row.Source != "automatic" || row.WakerID != run.WakerID || row.ExecutorID != scenario.executorID ||
					row.WorkflowID != scenario.workflowID || row.ChannelID != scenario.channelID {
					t.Fatalf("standalone failed opportunity = %+v", row)
				}
			}
			row, err := board.ResolveWorkRef(ctx, models.WorkRef{Kind: constants.WorkOwnerKindAutomaticRun, ID: run.ID})
			if err != nil {
				t.Fatal(err)
			}
			check(row)
			rows, err := board.List(ctx, query)
			if err != nil || len(rows) != 1 {
				t.Fatalf("filtered work board = %+v, %v", rows, err)
			}
			check(rows[0])
			awaiting, err := board.AwaitingView(ctx, constants.LocalReaderID, query)
			if err != nil || len(awaiting) != 1 {
				t.Fatalf("filtered attention = %+v, %v", awaiting, err)
			}
			check(awaiting[0])
		})
	}
}

package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVisibleChatRunSourcePageUsesTheSameIndexForTotalAndRows(t *testing.T) {
	for _, count := range []int{20, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db := newTestDB(t)
			removedAt := int64(10000)
			runs := make([]models.WorkflowRun, 0, count*3)
			for i := range count {
				for _, scope := range []string{"visible", "removed", "foreign"} {
					id := fmt.Sprintf("%s-%04d", scope, i)
					run := models.WorkflowRun{ID: id, SourceChatID: "chat", SourceExecutionID: id, CreatedAt: int64(i + 1)}
					if scope == "foreign" {
						run.SourceChatID = "other-chat"
					}
					if scope == "removed" {
						run.HistoryDeletedAt = &removedAt
					}
					runs = append(runs, run)
				}
			}
			if err := db.CreateInBatches(runs, 30).Error; err != nil {
				t.Fatal(err)
			}
			log := &repositoryQueryLog{Interface: logger.Discard}
			observed := db.Session(&gorm.Session{Logger: log})
			transactions := NewTransactionController(dbhelper.NewGormTransactionManager(observed))
			controller := NewWorkflowRunController(observed)
			if err := transactions.Transaction(t.Context(), func(tx context.Context) error {
				rows, total, err := controller.ListVisibleChatRunSources(tx, "chat", count-10, 5)
				if err == nil && (total != int64(count) || len(rows) != 5 || rows[0].ID != "visible-0009" || rows[4].ID != "visible-0005") {
					return fmt.Errorf("visible runs=%+v total=%d", rows, total)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			assertWorkflowSafetyQueryPlans(t, db, log.Statements, []string{"idx_workflow_chat_visible_runs", "idx_workflow_chat_visible_runs"})
		})
	}
}

func TestChatWorkflowRunsKeepSourcesAndJoinedRequestIdentity(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkflowRunController(db)
	ctx := t.Context()
	runs := []models.WorkflowRun{
		{ID: "old", RootRunID: "old", SourceChatID: "chat", SourceExecutionID: "one", SourceMessageID: "input", SourceName: "评审", Status: constants.WorkflowRunStatusFailed},
		{ID: "new", RootRunID: "new", SourceChatID: "chat", SourceExecutionID: "two", SourceMessageID: "again", SourceRunID: "old", SourceName: "评审", Status: constants.WorkflowRunStatusRunning},
		{ID: "other", RootRunID: "other", SourceChatID: "other-chat", SourceExecutionID: "foreign", Status: constants.WorkflowRunStatusRunning},
		{ID: "child", DefinitionID: "child-definition", RootRunID: "new", ParentRunID: "new", Joined: true, Status: constants.WorkflowRunStatusWaiting},
		{ID: "independent", RootRunID: "new", ParentRunID: "new", Joined: false, Status: constants.WorkflowRunStatusWaiting},
	}
	for i := range runs {
		runs[i].WorkerConfigurations = map[string]models.WorkflowExecutionDefaults{}
		runs[i].Context = map[string]any{}
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"child", "independent", "other", "old"} {
		status := constants.WorkflowStepStatusWaiting
		if id == "old" {
			status = constants.WorkflowStepStatusAnswered
		}
		q := models.WorkflowHumanRequest{ID: "q-" + id, RunID: id, InstanceID: "node", Prompt: "选择报告范围", Options: map[string]any{}, Value: "范围", Status: status}
		if err := db.Create(&q).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := controller.ListChatRunSources(ctx, "chat", []string{"one", "two", "foreign"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("source rows: %#v %v", rows, err)
	}
	requests, err := controller.ListChatRunRequests(ctx, rows, 1)
	if err != nil || len(requests["new"]) != 1 || requests["new"][0].RequestID != "q-child" || requests["new"][0].DefinitionID != "child-definition" || !requests["new"][0].Active || len(requests["old"]) != 1 || requests["old"][0].Active {
		t.Fatalf("requests: %#v %v", requests, err)
	}
}

func TestChatWorkflowWorkerWaitWakesOriginOnCommit(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	db := WithEventPublisher(newTestDB(t), hub)
	ctx := t.Context()
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	runs := []models.WorkflowRun{
		{ID: "origin", SourceChatID: "chat", SourceExecutionID: "parent-execution", Status: constants.WorkflowRunStatusRunning, RootRunID: "origin"},
		{ID: "joined", Joined: true, RootRunID: "origin", ParentRunID: "origin", Status: constants.WorkflowRunStatusRunning},
	}
	for i := range runs {
		runs[i].Context = map[string]any{}
		runs[i].WorkerConfigurations = map[string]models.WorkflowExecutionDefaults{}
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	effect := models.WorkflowEffect{ID: "effect", RunID: "joined", InstanceID: "worker-node", ExecutionID: "worker-execution"}
	effect.Boundary.Primitive = "worker"
	if err := db.Create(&effect).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Chat{ID: "worker-chat", ActiveExecutionID: "worker-execution"}).Error; err != nil {
		t.Fatal(err)
	}
	sub := hub.Subscribe("workflow", eventsResource("chat", "chat"))
	defer sub.Close()
	worker := NewChatController(db)
	fields := map[string]any{"waiting_approval": true, "pending_approvals": `[{"approvalId":"permit","requestedAt":3}]`, "latest_sequence": 1}
	sentinel := errors.New("rollback")
	err := transactions.Transaction(ctx, func(tx context.Context) error {
		if err := worker.AdvanceProjection(tx, "worker-chat", 0, fields); err != nil {
			return err
		}
		if len(sub.Events) != 0 {
			t.Fatal("received an uncommitted request")
		}
		return sentinel
	})
	if err == nil || len(sub.Events) != 0 {
		t.Fatal("unexpected rollback notification", err)
	}
	if err := transactions.Transaction(ctx, func(tx context.Context) error { return worker.AdvanceProjection(tx, "worker-chat", 0, fields) }); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-sub.Events:
		if change.Type != "workflow.changed" || change.Resource.ID != "chat" {
			t.Fatal(change)
		}
	default:
		t.Fatal("missing committed change for origin chat")
	}
	requests, err := NewWorkflowRunController(db).ListChatRunRequests(ctx, runs[:1], 1)
	if err != nil || len(requests["origin"]) != 1 {
		t.Fatalf("pending worker: %#v %v", requests, err)
	}
	q := requests["origin"][0]
	if q.RunID != "joined" || q.Kind != "approval" || q.InstanceID != "worker-node" || q.RequestID != "permit" || q.ChatID != "worker-chat" {
		t.Fatal(q)
	}
}

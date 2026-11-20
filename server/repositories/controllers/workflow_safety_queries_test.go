package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWorkflowSafetyQueriesUseIndexesAsHistoryGrows(t *testing.T) {
	for _, count := range []int{3, constants.WorkflowMaxChildren} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db := newTestDB(t)
			root := models.WorkflowRun{ID: "root", RootRunID: "root", SourceThreadID: "target-team", SourceChatID: "target-chat", SourceExecutionID: "root-execution", Status: constants.WorkflowRunStatusWaiting}
			runs := []models.WorkflowRun{root}
			for i := range count {
				id := fmt.Sprintf("child-%03d", i)
				parent := root.ID
				if i == count-1 {
					parent = "child-000"
				}
				runs = append(runs, models.WorkflowRun{ID: id, RootRunID: root.ID, ParentRunID: parent, Joined: true, Status: constants.WorkflowRunStatusCancelled})
			}
			for i := range count * 6 {
				runs = append(runs, models.WorkflowRun{ID: fmt.Sprintf("history-%03d", i), RootRunID: "other", SourceThreadID: root.SourceThreadID, SourceChatID: root.SourceChatID,
					SourceExecutionID: fmt.Sprintf("historical-execution-%03d", i), Status: constants.WorkflowRunStatusSucceeded})
			}
			if err := db.CreateInBatches(runs, 50).Error; err != nil {
				t.Fatal(err)
			}
			uncertain := fmt.Sprintf("child-%03d", count-1)
			if err := db.Create(&models.WorkflowEffect{ID: "unknown-effect", RunID: "child-001", Status: constants.WorkflowStepStatusUnknown}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.WorkflowActionCall{ID: "unknown-call", RunID: uncertain, Status: constants.WorkflowStepStatusUnknown}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.WorkflowConsumerLease{ID: "pending", ThreadID: root.SourceThreadID, ChatID: root.SourceChatID, DeadlineAt: 20}).Error; err != nil {
				t.Fatal(err)
			}
			log := &repositoryQueryLog{Interface: logger.Discard}
			observed := db.Session(&gorm.Session{Logger: log})
			transactions := NewTransactionController(dbhelper.NewGormTransactionManager(observed))
			controller := NewWorkflowRunController(observed)
			if err := transactions.Transaction(t.Context(), func(tx context.Context) error {
				branches, err := controller.ListJoinedRetryConflicts(tx, root)
				if err == nil && (len(branches) != 2 || !slices.Contains(branches, "child-000") || !slices.Contains(branches, "child-001")) {
					return fmt.Errorf("blocked retry branches = %v", branches)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			assertWorkflowSafetyQueryPlans(t, db, log.Statements, []string{"idx_workflow_runs_root_run_id", "idx_workflow_effects_run_id", "idx_workflow_action_calls_run_id"})
			log.Statements = nil
			if err := transactions.Transaction(t.Context(), func(tx context.Context) error {
				chats := []string{root.SourceChatID}
				if err := NewWorkflowConsumerController(observed).StopThreadConsumers(tx, root.SourceThreadID, chats, 10); err != nil {
					return err
				}
				stopped, err := controller.RequestTeamChatRunStop(tx, root.SourceThreadID, chats, 10)
				if err == nil && (len(stopped) != 1 || stopped[0].ID != root.ID) {
					return fmt.Errorf("stopped workflow runs = %v", stopped)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			assertWorkflowSafetyQueryPlans(t, db, log.Statements, []string{"idx_workflow_consumer_thread_stop", "idx_workflow_thread_stop", ""})
		})
	}
}

func assertWorkflowSafetyQueryPlans(t *testing.T, db *gorm.DB, statements, indexes []string) {
	t.Helper()
	if len(statements) != len(indexes) {
		t.Fatalf("expected %d SQL operations, got %v", len(indexes), statements)
	}
	for i, index := range indexes {
		if index == "" {
			continue
		}
		var plan []struct {
			// Detail 是 SQLite 的访问路径和使用的索引。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + statements[i]).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		for _, step := range plan {
			if strings.HasPrefix(step.Detail, "SEARCH ") && strings.Contains(step.Detail, index) {
				index = ""
				break
			}
		}
		if index != "" {
			t.Fatalf("query %q must use index %q: %+v", statements[i], index, plan)
		}
	}
}

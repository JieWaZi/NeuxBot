package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// seedRunHistory 用真实 SQLite 行构造确定的历史顺序，不绕过待验证的清理入口。
func seedRunHistory(t *testing.T, db *gorm.DB, definition string, count int, status constants.WorkflowRunStatus) {
	t.Helper()
	for index := 0; index < count; index++ {
		run := models.WorkflowRun{ID: fmt.Sprintf("%s-%03d", definition, index), DefinitionID: definition, Status: status, CreatedAt: 100, UpdatedAt: 100, Version: 1, LeaseOwner: "worker", Fence: 1, LeaseUntil: 1000}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunHistoryPrunesOldestHalfOnlyAboveLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runs := NewWorkflowRunController(db)
	seedRunHistory(t, db, "flow", 29, constants.WorkflowRunStatusSucceeded)
	seedRunHistory(t, db, "other", 2, constants.WorkflowRunStatusSucceeded)
	create := func(id string) {
		t.Helper()
		err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
			return runs.CreateRun(ctx, models.WorkflowRun{ID: id, DefinitionID: "flow", Status: constants.WorkflowRunStatusQueued, CreatedAt: 200}, models.WorkflowFiber{ID: id + "-fiber", RunID: id})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	create("flow-new-1")
	values, err := runs.ListRuns(ctx, "flow", 0, "", 100)
	if err != nil || len(values) != 30 {
		t.Fatalf("30 条时不得清理: %d %v", len(values), err)
	}
	create("flow-new-2")
	values, err = runs.ListRuns(ctx, "flow", 0, "", 100)
	if err != nil || len(values) != 16 || values[len(values)-1].ID != "flow-015" {
		t.Fatalf("31 条应清理最早 15 条，同时间按身份稳定排序: %+v %v", values, err)
	}
	old, err := runs.GetRun(ctx, "flow-000")
	if err != nil || old.HistoryDeletedAt == nil || old.Version != 1 || old.UpdatedAt != 100 {
		t.Fatalf("清理不得改写运行事实或活动时间: %+v %v", old, err)
	}
	other, err := runs.ListRuns(ctx, "other", 0, "", 100)
	if err != nil || len(other) != 2 {
		t.Fatalf("不能影响其他工作流: %+v %v", other, err)
	}
	var events []models.WorkflowEvent
	if err := db.Where("type = ?", constants.WorkflowRunHistoryPrunedEvent).Find(&events).Error; err != nil || len(events) != 1 || events[0].StreamID != "flow" {
		t.Fatalf("清理必须在定义事件流通知列表: %+v %v", events, err)
	}
}

func TestRunHistoryProtectsActiveRunsAndPrunesWhenTheyEnd(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runs := NewWorkflowRunController(db)
	seedRunHistory(t, db, "flow", 31, constants.WorkflowRunStatusWaiting)
	// 终态提交前仍超限，授权等待期间不可删除。
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", "flow-001").UpdateColumn("status", constants.WorkflowRunStatusWaiting).Error; err != nil {
		t.Fatal(err)
	}
	if err := runs.DeleteRunHistory(ctx, "flow", "flow-001", 100); err != nil {
		t.Fatal(err)
	}
	active, err := runs.GetRun(ctx, "flow-001")
	if err != nil || active.HistoryDeletedAt != nil {
		t.Fatalf("等待中的运行仍是活跃事实: %+v %v", active, err)
	}
	run, err := runs.GetRun(ctx, "flow-000")
	if err != nil {
		t.Fatal(err)
	}
	ended := int64(101)
	run.Status, run.CompletedAt, run.Result = constants.WorkflowRunStatusSucceeded, &ended, map[string]any{"report": "完成"}
	results := NewWorkResultController(db)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
		return runs.CommitRunState(ctx, WorkflowRunState{Run: run}, 1, 101)
	}); err != nil {
		t.Fatal(err)
	}
	visible, err := runs.ListRuns(ctx, "flow", 0, "", 100)
	if err != nil || len(visible) != 30 {
		t.Fatalf("活跃记录应保留，结束后自动继续清理: %d %v", len(visible), err)
	}
	completed, err := runs.GetRun(ctx, run.ID)
	if err != nil || completed.HistoryDeletedAt == nil || completed.Result == nil {
		t.Fatalf("隐藏入口不得删除真实完成结果: %+v %v", completed, err)
	}
	if _, found, err := results.Get(ctx, constants.WorkResultOwnerKindWorkflowRun, run.ID); err != nil || !found {
		t.Fatalf("清理不得跳过成果回执提交: %v %v", found, err)
	}
}

func TestRunHistoryPrunesBacklogButKeepsChildFacts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runs := NewWorkflowRunController(db)
	seedRunHistory(t, db, "flow", 100, constants.WorkflowRunStatusFailed)
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", "flow-000").UpdateColumn("parent_run_id", "flow-001").Error; err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
		return runs.CreateRun(ctx, models.WorkflowRun{ID: "new", DefinitionID: "flow", Status: constants.WorkflowRunStatusQueued, CreatedAt: 200}, models.WorkflowFiber{ID: "new-fiber", RunID: "new"})
	}); err != nil {
		t.Fatal(err)
	}
	visible, err := runs.ListRuns(ctx, "flow", 0, "", 100)
	if err != nil || len(visible) != 26 {
		t.Fatalf("积压历史按同一减半规则收敛: %d %v", len(visible), err)
	}
	children, err := runs.ListChildren(ctx, "flow-001")
	if err != nil || len(children) != 1 || children[0].ID != "flow-000" || children[0].HistoryDeletedAt == nil {
		t.Fatalf("历史清理不能切断父子运行证据: %+v %v", children, err)
	}
}

func TestRunHistoryPruningRollsBackWithAdmission(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runs := NewWorkflowRunController(db)
	seedRunHistory(t, db, "flow", 30, constants.WorkflowRunStatusCancelled)
	err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
		if err := runs.CreateRun(ctx, models.WorkflowRun{ID: "new", DefinitionID: "flow", Status: constants.WorkflowRunStatusQueued}, models.WorkflowFiber{ID: "new-fiber", RunID: "new"}); err != nil {
			return err
		}
		return status_error.WorkflowStateConflict
	})
	if err != status_error.WorkflowStateConflict {
		t.Fatal(err)
	}
	values, err := runs.ListRuns(ctx, "flow", 0, "", 100)
	if err != nil || len(values) != 30 {
		t.Fatalf("接纳回滚必须同时回滚历史清理: %d %v", len(values), err)
	}
	var count int64
	if err := db.Model(&models.WorkflowEvent{}).Where("type = ?", constants.WorkflowRunHistoryPrunedEvent).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("不得留下未提交的清理事件: %d %v", count, err)
	}
}

func TestRemovedRunHistoryDoesNotAppearInWorkBoardOrUnreadCounts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.Create(&models.WorkflowDefinition{ID: "flow", Title: "历史清理"}).Error; err != nil {
		t.Fatal(err)
	}
	seedRunHistory(t, db, "flow", 2, constants.WorkflowRunStatusSucceeded)
	results := NewWorkResultController(db)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"flow-000", "flow-001"} {
		if err := results.Publish(ctx, models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindWorkflowRun, OwnerID: id, LatestVersion: 1, AvailableAt: 200, Reference: models.WorkResultReference{WorkflowRunID: id}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewWorkflowRunController(db).DeleteRunHistory(ctx, "flow", "flow-000", 300); err != nil {
		t.Fatal(err)
	}
	board := NewWorkBoardController(db)
	for _, query := range []WorkBoardQuery{{}, {Source: "workflow"}, {ExecutorID: "flow"}, {Query: "历史"}} {
		count, err := board.ResultCount(ctx, query)
		if err != nil || count != 1 {
			t.Fatalf("所有未读计数路径应排除删除的历史: %+v %d %v", query, count, err)
		}
	}
	for _, method := range []func(context.Context, WorkBoardQuery) ([]models.WorkBoardEntry, error){board.List, board.Results} {
		values, err := method(ctx, WorkBoardQuery{Limit: 50})
		if err != nil || len(values) != 1 || values[0].OwnerID != "flow-001" {
			t.Fatalf("工作看板不得继续出现已移除的记录: %+v %v", values, err)
		}
	}
	if _, found, err := results.Get(ctx, constants.WorkResultOwnerKindWorkflowRun, "flow-000"); err != nil || !found {
		t.Fatalf("成果凭据必须保留: %v %v", found, err)
	}
}

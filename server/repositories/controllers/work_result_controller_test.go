package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowTerminalPublishesOnlyFencedTopLevelResult(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	results := NewWorkResultController(db)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	runs := NewWorkflowRunController(db)
	for _, id := range []string{"top-result", "failed-result", "child-result", "lost-lease", "empty-result"} {
		run := models.WorkflowRun{ID: id, DefinitionID: "definition", Status: constants.WorkflowRunStatusRunning, LeaseOwner: "worker", LeaseUntil: 1000, Fence: 1, Version: 1}
		if id == "child-result" {
			run.ParentRunID = "top-result"
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		ended := int64(100)
		run.Status, run.CompletedAt = constants.WorkflowRunStatusSucceeded, &ended
		if id != "empty-result" {
			run.Result = map[string]any{"text": "已提交成果"}
		}
		if id == "failed-result" {
			run.Status = constants.WorkflowRunStatusFailed
			run.Result = nil
			failure := workflowdsl.JSONValue(map[string]any{"message": "运行失败"})
			run.Failure = &failure
		}
		if id == "lost-lease" {
			run.Fence++
		}
		err := runs.CommitRunState(ctx, WorkflowRunState{Run: run}, 1, 100)
		if id == "lost-lease" {
			if err != status_error.WorkflowFenceConflict {
				t.Fatalf("失效租约提交被接纳: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		value, found, err := results.Get(ctx, constants.WorkResultOwnerKindWorkflowRun, id)
		if err != nil || found != (id == "top-result" || id == "failed-result") {
			t.Fatalf("只有租约提交成功的顶层成果或失败结果可以发布: %s %+v %v", id, value, err)
		}
		if found && (value.Reference.WorkflowRunID != id || value.AvailableAt != ended || value.ViewedVersion != 0) {
			t.Fatalf("结果引用未对应原运行: %+v", value)
		}
	}
}

func TestWorkResultReadingKeepsNewerDeliveryAndNeverRegresses(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkResultController(db)
	ctx := context.Background()
	id := "11111111111111111111111111111111"
	if err := controller.EnableTracking(ctx, 100); err != nil {
		t.Fatal(err)
	}
	publish := func(version int64) {
		t.Helper()
		if err := controller.Publish(ctx, models.WorkResultReceipt{
			OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: id,
			LatestVersion: version, AvailableAt: 100 + version,
			Reference: models.WorkResultReference{ChatID: id, ExecutionID: strings.Repeat(string(rune('a'+version)), 32)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	publish(1)
	publish(2)
	if ok, err := controller.MarkViewed(ctx, constants.WorkResultOwnerKindChat, id, 1, 200); err != nil || !ok {
		t.Fatalf("view old result: %v %v", ok, err)
	}
	value, found, err := controller.Get(ctx, constants.WorkResultOwnerKindChat, id)
	if err != nil || !found || value.LatestVersion != 2 || value.ViewedVersion != 1 {
		t.Fatalf("new result must remain unread: %+v %v", value, err)
	}
	if ok, err := controller.MarkViewed(ctx, constants.WorkResultOwnerKindChat, id, 2, 210); err != nil || !ok {
		t.Fatal(err)
	}
	publish(1)
	if _, err := controller.MarkViewed(ctx, constants.WorkResultOwnerKindChat, id, 1, 220); err != nil {
		t.Fatal(err)
	}
	value, _, _ = controller.Get(ctx, constants.WorkResultOwnerKindChat, id)
	if value.ViewedVersion != 2 || value.LatestVersion != 2 || value.ViewedAt == nil || *value.ViewedAt != 210 {
		t.Fatalf("late read/publication must not regress: %+v", value)
	}
	if ok, err := controller.MarkViewed(ctx, constants.WorkResultOwnerKindChat, id, 3, 230); err != nil || ok {
		t.Fatalf("future result accepted: %v %v", ok, err)
	}
}

func TestWorkResultTrackingRetainsFirstEnableBoundary(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	controller := NewWorkResultController(db)
	if err := controller.EnableTracking(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := controller.EnableTracking(ctx, 200); err != nil {
		t.Fatal(err)
	}
	for index, at := range []int64{99, 150} {
		if err := controller.Publish(ctx, models.WorkResultReceipt{
			OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: strings.Repeat(string(rune('a'+index)), 32),
			LatestVersion: 1, AvailableAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&models.WorkResultReceipt{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("tracking boundary changed: %d %v", count, err)
	}
}

func TestClearUnreadWorkResultsPreservesDeliveryAndNewVersions(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	controller := NewWorkResultController(db)
	if err := controller.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	kinds := []constants.WorkResultOwnerKind{
		constants.WorkResultOwnerKindChat,
		constants.WorkResultOwnerKindTeamThread,
		constants.WorkResultOwnerKindWorkflowRun,
	}
	for _, kind := range kinds {
		if err := controller.Publish(ctx, models.WorkResultReceipt{
			OwnerKind: kind, OwnerID: string(kind), LatestVersion: 1, AvailableAt: 100,
			Reference: models.WorkResultReference{MessageID: "original-result"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := controller.MarkViewed(ctx, kinds[0], string(kinds[0]), 1, 150); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{200, 300} {
		if err := controller.ClearUnread(ctx, at); err != nil {
			t.Fatal(err)
		}
		for index, kind := range kinds {
			value, found, err := controller.Get(ctx, kind, string(kind))
			if err != nil || !found || value.ViewedVersion != 1 || value.LatestVersion != 1 {
				t.Fatalf("清空后交付及确认版本不正确: %+v %v", value, err)
			}
			confirmedAt := int64(200)
			if index == 0 {
				confirmedAt = 150
			}
			if value.ViewedAt == nil || *value.ViewedAt != confirmedAt || value.AvailableAt != 100 || value.Reference.MessageID != "original-result" {
				t.Fatalf("清空或重试改写了原结果或已有确认时间: %+v", value)
			}
		}
	}
	if err := controller.Publish(ctx, models.WorkResultReceipt{
		OwnerKind: kinds[0], OwnerID: string(kinds[0]), LatestVersion: 2, AvailableAt: 400,
		Reference: models.WorkResultReference{MessageID: "new-result"},
	}); err != nil {
		t.Fatal(err)
	}
	value, found, err := controller.Get(ctx, kinds[0], string(kinds[0]))
	if err != nil || !found || value.LatestVersion != 2 || value.ViewedVersion != 1 || value.Reference.MessageID != "new-result" {
		t.Fatalf("清空后发布的新结果没有保留待查看: %+v %v", value, err)
	}
}

func TestWorkResultUnreadQueryUsesPartialIndex(t *testing.T) {
	db := newTestDB(t)
	// details 是 SQLite 公共查询计划，用于证明查询不扫描全部已读历史。
	var details []struct {
		// Detail 是 SQLite 对当前查询步骤的说明。
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT COUNT(*) FROM work_result_receipts WHERE latest_version > viewed_version").Scan(&details).Error; err != nil {
		t.Fatal(err)
	}
	for _, detail := range details {
		if strings.Contains(detail.Detail, "idx_work_result_unread") {
			return
		}
	}
	t.Fatalf("unread count does not use its partial index: %+v", details)
}

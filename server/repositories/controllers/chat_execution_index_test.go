package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestExecutionLocationsUseIdentityIndexesAndAdvanceOnlyTail 验证执行、缺失输入及首执行读取使用数据库定位索引。
func TestExecutionLocationsUseIdentityIndexesAndAdvanceOnlyTail(t *testing.T) {
	db := newTestDB(t)
	controller := NewChatController(db)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	ctx := context.Background()
	locations := make([]models.ChatExecutionLocation, 0, 1024)
	for i := 0; i < 1024; i++ {
		locations = append(locations, models.ChatExecutionLocation{ChatID: "chat", ExecutionID: fmt.Sprintf("exec-%d", i), SubmissionID: fmt.Sprintf("submission-%d", i), Sequence: int64(i + 1)})
	}
	if err := transactions.Transaction(ctx, func(tx context.Context) error {
		return controller.AdvanceExecutionLocations(tx, "chat", 0, 1024, locations)
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		// sql 是生产定位使用的单表过滤和顺序。
		sql string
		// args 是对应的原业务身份。
		args []any
	}{
		{"SELECT * FROM chat_execution_locations WHERE chat_id = ? AND execution_id = ? LIMIT 1", []any{"chat", "exec-1023"}},
		{"SELECT * FROM chat_execution_locations WHERE chat_id = ? AND submission_id = ? LIMIT 1", []any{"chat", "missing"}},
		{"SELECT * FROM chat_execution_locations WHERE chat_id = ? AND message_only = ? ORDER BY sequence ASC LIMIT 1", []any{"chat", false}},
	} {
		var plan []struct {
			// Detail 是 SQLite 返回的访问路径。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN "+query.sql, query.args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range plan {
			if !strings.Contains(row.Detail, "SEARCH") || strings.Contains(row.Detail, "TEMP B-TREE") {
				t.Fatalf("unbounded locator plan: %s", row.Detail)
			}
			t.Log(row.Detail)
		}
	}
	if location, err := controller.ReadExecutionLocation(ctx, "chat", "", "missing"); err != nil || location != nil {
		t.Fatalf("missing submission=%+v %v", location, err)
	}
	first, err := controller.ReadExecutionLocation(ctx, "chat", "", "")
	if err != nil || first.Sequence != 1 {
		t.Fatalf("initial=%+v %v", first, err)
	}
	last, err := controller.ReadExecutionLocation(ctx, "chat", "exec-1023", "")
	if err != nil || last.Sequence != 1024 {
		t.Fatalf("last=%+v %v", last, err)
	}
	if err := transactions.Transaction(ctx, func(tx context.Context) error {
		return controller.AdvanceExecutionLocations(tx, "chat", 1024, 1025, []models.ChatExecutionLocation{{ChatID: "chat", ExecutionID: "latest", Sequence: 1025}})
	}); err != nil {
		t.Fatal(err)
	}
	cursor, err := controller.ReadExecutionLocationCursor(ctx, "chat")
	if err != nil || cursor != 1025 {
		t.Fatalf("cursor=%d %v", cursor, err)
	}
}

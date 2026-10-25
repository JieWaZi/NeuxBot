package controllers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCommandReceiptSharesDomainTransactionAndRejectsPayloadChanges(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "command.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	defer connection.Close()
	store := NewAgentCommandController(database.DB)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	wakers := NewWakerController(database.DB)
	request := agentcapability.Request{Operation: "team.tasks.create", RequestID: "same", Params: map[string]any{"subject": "task"}}
	apply := func(ctx context.Context) error {
		return transactions.Transaction(ctx, func(ctx context.Context) error {
			if err := wakers.Create(ctx, models.Waker{ID: "atomic", Name: "原子变更"}); err != nil {
				return err
			}
			return agentcommand.Commit(ctx, map[string]any{"taskId": "atomic"})
		})
	}
	if err := database.DB.Exec("CREATE TRIGGER reject_receipt BEFORE INSERT ON agent_command_receipts BEGIN SELECT RAISE(ABORT, 'receipt denied'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := agentcommand.Execute(context.Background(), store, "thread", request, apply); err == nil {
		t.Fatal("accepted without receipt")
	}
	if _, err := wakers.Get(context.Background(), "atomic"); err == nil {
		t.Fatal("receipt failure left domain change committed")
	}
	if err := database.DB.Exec("DROP TRIGGER reject_receipt").Error; err != nil {
		t.Fatal(err)
	}
	first, err := agentcommand.Execute(context.Background(), store, "thread", request, apply)
	if err != nil {
		t.Fatal(err)
	}
	second, err := agentcommand.Execute(context.Background(), store, "thread", request, func(context.Context) error { t.Fatal("duplicate executed domain command"); return nil })
	if err != nil || !second.Replayed || first.CommandID != second.CommandID {
		t.Fatalf("receipt replay: %+v %v", second, err)
	}
	request.Params = map[string]any{"subject": "different"}
	if _, err := agentcommand.Execute(context.Background(), store, "thread", request, apply); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("changed content: %v", err)
	}
	if _, err := agentcommand.Lookup(context.Background(), store, "other-thread", request.Operation, request.RequestID); !errors.Is(err, status_error.AgentReceiptNotFound) {
		t.Fatalf("cross-thread receipt: %v", err)
	}
}

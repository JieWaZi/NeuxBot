package workspaceattention

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestReadingInitializationResumesAfterHistoricalBarrier(t *testing.T) {
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reading.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.DB.Exec("CREATE TABLE work_result_receipts (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.WorkflowDefinition{ID: "flow", Title: "Flow"}).Error; err != nil {
		t.Fatal(err)
	}
	now := int64(100)
	run := models.WorkflowRun{ID: "history", DefinitionID: "flow", Status: constants.WorkflowRunStatusSucceeded, Result: map[string]any{"ok": true}, CompletedAt: &now}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	controller := controllers.NewReadingInitializationController(db.DB)
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	interrupted := errors.New("stopped during baseline")
	if err := InitializeBaseline(ctx, controller, func(context.Context) error { return interrupted }); !errors.Is(err, interrupted) {
		t.Fatalf("baseline must fail: %v", err)
	}
	attempts := 0
	baseline := func(context.Context) error { attempts++; return nil }
	if err := InitializeBaseline(ctx, controller, baseline); err != nil {
		t.Fatal(err)
	}
	run.ID = "recovery-result"
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := InitializeBaseline(ctx, controller, baseline); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("baseline reran after durable barrier: %d", attempts)
	}
	if err := FinishInitialization(ctx, controller, tx); err != nil {
		t.Fatal(err)
	}
	if db.DB.Migrator().HasTable("work_result_receipts") || !db.DB.Migrator().HasTable(&models.WorkflowRun{}) {
		t.Fatal("legacy cleanup changed wrong tables")
	}
	viewing := controllers.NewRunViewingController(db.DB)
	for id, wantViewed := range map[string]bool{"history": true, "recovery-result": false} {
		state, err := viewing.Get(ctx, constants.LocalReaderID, models.RunOwnerRef{OwnerKind: constants.RunOwnerKindWorkflowRun, OwnerID: id})
		if err != nil || !state.Viewable || state.Viewed != wantViewed {
			t.Fatalf("%s: %+v %v", id, state, err)
		}
	}
}

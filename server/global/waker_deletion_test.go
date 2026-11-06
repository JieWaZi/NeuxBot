package global

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestStartupResolvesStagedAgentUsingWakerFactWithoutChats(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	defer pool.Close()
	previous := WakerControllerManager
	defer func() { WakerControllerManager = previous }()
	WakerControllerManager = controllers.NewWakerController(database.DB)
	ctx := context.Background()
	if err = WakerControllerManager.Create(ctx, models.Waker{ID: "waker", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := layout.AgentWorkspace("waker")
	if err != nil {
		t.Fatal(err)
	}
	if err = workspace.EnsureAgentWorkspace(cwd, "waker"); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(cwd, "report.txt")
	if err = os.WriteFile(report, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	coordinator, err := chatstorage.NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.StageWaker("before-db", "waker"); err != nil {
		t.Fatal(err)
	}
	if err = recoverWakerDeletions(ctx, layout.Root()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(report); err != nil || string(content) != "preserve" {
		t.Fatalf("live Waker did not roll back: %q %v", content, err)
	}
	if _, err = coordinator.StageWaker("after-db", "waker"); err != nil {
		t.Fatal(err)
	}
	if err = WakerControllerManager.Delete(ctx, "waker"); err != nil {
		t.Fatal(err)
	}
	if err = recoverWakerDeletions(ctx, layout.Root()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cwd); !os.IsNotExist(err) {
		t.Fatalf("deleted Waker restored: %v", err)
	}
	if err = recoverWakerDeletions(ctx, layout.Root()); err != nil {
		t.Fatal(err)
	}
}

package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestRecentProjectScansBeyondFirstBatch(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "recent.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	dir := t.TempDir()
	source, err := projectsource.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []interface{}{&models.Project{ID: "valid", WakerID: "waker", Name: "valid"}, &source, &models.ProjectSourceBinding{ProjectID: "valid", ProjectSourceID: source.ID}} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 102; i++ {
		project := "missing"
		if i == 0 {
			project = "valid"
		}
		if err := db.DB.Create(&models.Chat{ID: fmt.Sprintf("%032d", i), WakerID: "waker", ProjectID: project, UpdatedAt: int64(i + 1)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestService(t, Config{StorageDirectory: t.TempDir(), Projects: controllers.NewProjectController(db.DB), QueryIndexes: QueryIndexDependencies{Chats: controllers.NewChatController(db.DB)}})
	got, err := svc.RecentAvailableProject(context.Background(), "waker")
	if err != nil || got != "valid" {
		t.Fatalf("recent project = %q, %v; want valid", got, err)
	}
}

func TestListChatsDoesNotReadProjects(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "list.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.DB.Create(&models.Chat{ID: "chat", WakerID: "waker", ProjectID: "project"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, Config{StorageDirectory: t.TempDir(), QueryIndexes: QueryIndexDependencies{Chats: controllers.NewChatController(db.DB)}})
	if _, err := svc.ListChats(context.Background(), ListChatsInput{WakerID: "waker", Limit: 10}); err != nil {
		t.Fatalf("list chats: %v", err)
	}
}

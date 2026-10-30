package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestRecentProjectCandidatesUseStableBoundedCursor(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "cursor.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for i := 0; i < 101; i++ {
		if err := db.DB.Create(&models.Chat{ID: fmt.Sprintf("%032d", i), WakerID: "waker", ProjectID: "project", UpdatedAt: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	controller := NewChatController(db.DB)
	first, err := controller.ListRecentProjectCandidates(context.Background(), "waker", 0, "", 100)
	if err != nil || len(first) != 100 {
		t.Fatalf("first page: %d, %v", len(first), err)
	}
	last := first[len(first)-1]
	second, err := controller.ListRecentProjectCandidates(context.Background(), "waker", last.UpdatedAt, last.ID, 100)
	if err != nil || len(second) != 1 || second[0].ID != fmt.Sprintf("%032d", 0) {
		t.Fatalf("second page: %+v, %v", second, err)
	}
}

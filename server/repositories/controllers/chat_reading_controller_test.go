package controllers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func readingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reading.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db.DB
}

func TestChatReadingGenerationAndHold(t *testing.T) {
	ctx := context.Background()
	db := readingDB(t)
	c := NewChatReadingController(db)
	if err := c.EnsureCursor(ctx, constants.LocalReaderID, "chat", 0); err != nil {
		t.Fatal(err)
	}
	messages := []models.ChatReadableMessage{
		{ChatID: "chat", MessageID: "earlier", OrderSequence: 3, Kind: "answer", CreatedAt: 1},
		{ChatID: "chat", MessageID: "later", OrderSequence: 9, Kind: "member_report", CreatedAt: 2},
	}
	if _, err := c.Project(ctx, "chat", nil, messages, false); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkRead(ctx, constants.LocalReaderID, "chat", "later", 0, constants.ReadingIntentAutomatic); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkRead(ctx, constants.LocalReaderID, "chat", "earlier", 0, constants.ReadingIntentAutomatic); err != nil {
		t.Fatal(err)
	}
	state, err := c.Get(ctx, constants.LocalReaderID, "chat")
	if err != nil || state.ThroughSequence != 9 || state.UnreadGeneration != 0 || state.UnreadMessageCount != 0 {
		t.Fatalf("normal read: %+v, %v", state, err)
	}
	if err := c.MarkUnread(ctx, constants.LocalReaderID, "chat", "earlier", 0); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		// generation 是窗口看到的代次。
		generation int64
		// intent 是该窗口发出的阅读意图。
		intent constants.ReadingIntent
	}{{0, constants.ReadingIntentExplicit}, {1, constants.ReadingIntentAutomatic}} {
		if err := c.MarkRead(ctx, constants.LocalReaderID, "chat", "later", attempt.generation, attempt.intent); !errors.Is(err, status_error.ReadingIntentConflict) {
			t.Fatalf("stale or automatic write accepted: %v", err)
		}
	}
	state, err = c.Get(ctx, constants.LocalReaderID, "chat")
	if err != nil || state.ThroughSequence != 2 || state.UnreadGeneration != 1 || !state.HoldUnread || state.UnreadMessageCount != 2 || state.FirstUnreadMessageID != "earlier" {
		t.Fatalf("unread hold: %+v, %v", state, err)
	}
	if err := c.MarkRead(ctx, constants.LocalReaderID, "chat", "earlier", 1, constants.ReadingIntentExplicit); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Project(ctx, "chat", []string{"earlier"}, messages[1:], false); err != nil {
		t.Fatal(err)
	}
	state, err = c.Get(ctx, constants.LocalReaderID, "chat")
	if err != nil || state.ThroughSequence != 3 || state.UnreadGeneration != 1 || state.HoldUnread || state.UnreadMessageCount != 1 || state.FirstUnreadMessageID != "later" {
		t.Fatalf("deleted boundary and repeated message: %+v, %v", state, err)
	}
	if err := c.MarkRead(ctx, constants.LocalReaderID, "another-chat", "later", 0, constants.ReadingIntentExplicit); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("foreign boundary: %v", err)
	}
}

func TestRunViewMarkImmutableRun(t *testing.T) {
	ctx := context.Background()
	db := readingDB(t)
	c := NewRunViewingController(db)
	at := int64(12)
	if err := db.Create(&models.WorkflowDefinition{ID: "flow", Title: "Flow"}).Error; err != nil {
		t.Fatal(err)
	}
	old := models.WorkflowRun{ID: "old-run", DefinitionID: "flow", Status: constants.WorkflowRunStatusSucceeded, Result: map[string]any{"answer": "ok"}, CompletedAt: &at}
	fresh := old
	fresh.ID = "new-run"
	if err := db.Create(&[]models.WorkflowRun{old, fresh}).Error; err != nil {
		t.Fatal(err)
	}
	ref := models.RunOwnerRef{OwnerKind: "workflow_run", OwnerID: old.ID}
	if err := c.MarkViewed(ctx, constants.LocalReaderID, ref); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", old.ID).Update("version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.MarkViewed(ctx, constants.LocalReaderID, ref); err != nil {
		t.Fatal(err)
	}
	state, err := c.Get(ctx, constants.LocalReaderID, ref)
	if err != nil || !state.Viewable || !state.Viewed {
		t.Fatalf("old run: %+v %v", state, err)
	}
	ref.OwnerID = fresh.ID
	state, err = c.Get(ctx, constants.LocalReaderID, ref)
	if err != nil || !state.Viewable || state.Viewed {
		t.Fatalf("new run: %+v %v", state, err)
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", fresh.ID).Update("status", constants.WorkflowRunStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.MarkViewed(ctx, constants.LocalReaderID, ref); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("cancelled run accepted: %v", err)
	}
}

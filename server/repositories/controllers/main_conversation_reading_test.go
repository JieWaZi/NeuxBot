package controllers

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMainReadingBaselineDoesNotResetExistingManualUnread(t *testing.T) {
	db := newTestDB(t)
	store := NewMainConversationController(db)
	ctx := t.Context()
	const reader = constants.LocalReaderID
	if err := db.Migrator().DropTable(&models.MainConversationReadCursor{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex(&models.MainConversationMessage{}, "idx_main_conversation_readable"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MainConversation{ID: "historical", SubjectType: "waker", SubjectID: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []models.MainConversationMessage{
		{ID: "old-report", ConversationID: "historical", Sequence: 1, Role: "assistant"},
		{ID: "old-input", ConversationID: "historical", Sequence: 2, Role: "user"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&models.MainConversationMessage{}, &models.MainConversationReadCursor{}); err != nil {
		t.Fatal(err)
	}
	known, err := store.Ensure(ctx, "team", "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MainConversationMessage{ID: "kept-report", ConversationID: known.ID, Sequence: 1, Role: "assistant"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUnread(ctx, reader, known.ID, "kept-report", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeReadingBaseline(ctx, reader); err != nil {
		t.Fatal(err)
	}
	historical, err := store.Reading(ctx, reader, "historical")
	if err != nil || historical.UnreadMessageCount != 0 || historical.ThroughSequence != 1 || historical.LatestReadableMessageID != "old-report" {
		t.Fatalf("legacy baseline = %+v, err = %v", historical, err)
	}
	post := models.MainConversationMessage{ID: "new-report", ConversationID: "historical", Role: "assistant", Content: "new"}
	if _, _, err := store.Append(ctx, post, "waker", "a"); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeReadingBaseline(ctx, reader); err != nil {
		t.Fatal(err)
	}
	historical, err = store.Reading(ctx, reader, "historical")
	if err != nil || historical.UnreadMessageCount != 1 || historical.FirstUnreadMessageID != "new-report" {
		t.Fatalf("startup reset newly published unread report: %+v, %v", historical, err)
	}
	held, err := store.Reading(ctx, reader, known.ID)
	if err != nil || !held.HoldUnread || held.UnreadGeneration != 1 || held.UnreadMessageCount != 1 {
		t.Fatalf("startup reset existing manual unread: %+v, %v", held, err)
	}
	if err := deleteMainConversation(db, "team", "b"); err != nil {
		t.Fatal(err)
	}
	var cursors int64
	if err := db.Model(&models.MainConversationReadCursor{}).Where("conversation_id = ?", known.ID).Count(&cursors).Error; err != nil || cursors != 0 {
		t.Fatalf("deleted conversation retained %d cursor(s), err = %v", cursors, err)
	}
}

func TestMainReadingSuffixQueriesUsePublicationIndex(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	const reader = constants.LocalReaderID
	conversation, err := NewMainConversationController(db).Ensure(ctx, "waker", "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MainConversationMessage{ID: "report", ConversationID: conversation.ID, Sequence: 1, Role: "assistant"}).Error; err != nil {
		t.Fatal(err)
	}
	log := &repositoryQueryLog{Interface: logger.Discard}
	observed := db.Session(&gorm.Session{Logger: log})
	if _, err := NewMainConversationController(observed).Reading(ctx, reader, conversation.ID); err != nil {
		t.Fatal(err)
	}
	queries := 0
	for _, sql := range log.Statements {
		if !strings.Contains(sql, "sequence >") {
			continue
		}
		queries++
		var plan []struct {
			// Detail 是 SQLite 查询计划的具体扫描步骤。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + sql).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if len(plan) != 1 || !strings.Contains(plan[0].Detail, "SEARCH main_conversation_messages USING") ||
			!strings.Contains(plan[0].Detail, "idx_main_conversation_readable (conversation_id=? AND sequence>?)") {
			t.Fatalf("public reading did not use the unread suffix index: %+v", plan)
		}
	}
	if queries != 2 {
		t.Fatalf("expected count and first-unread lookup, got %v", log.Statements)
	}
}

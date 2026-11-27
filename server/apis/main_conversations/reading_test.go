package main_conversations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMainReadingOperatorsScopePublicBoundaries(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reading.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	oldMain, oldStore, oldTransactions, oldHub := global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager, global.EventHub
	oldChats := global.ChatService
	t.Cleanup(func() {
		global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager, global.EventHub = oldMain, oldStore, oldTransactions, oldHub
		global.ChatService = oldChats
		hub.Close()
		_ = connection.Close()
	})
	wakers := controllers.NewWakerController(db.DB)
	store := controllers.NewMainConversationController(db.DB)
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	service := mainconversation.New(store, transactions, wakers, controllers.NewChannelController(db.DB))
	global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager, global.EventHub = service, store, transactions, hub
	global.ChatService = capturedReceipts{runner: &capturedTurns{}}
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	work, err := service.ReserveWork(ctx, "waker", "a", "a", "managed")
	if err != nil {
		t.Fatal(err)
	}
	input, err := service.AdmitUser(ctx, "waker", "a", "submit", "own input", "", work.WorkChatID, "execution", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	publication, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "call", "public result", input.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	state := func() MainConversationReadingResponse {
		t.Helper()
		result, err := (&GetMainConversationReading{SubjectType: "waker", SubjectID: "a"}).Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return result.(MainConversationReadingResponse)
	}
	initial := state()
	if !initial.Complete || initial.Epoch != hub.Epoch() || initial.ConversationID != work.ConversationID ||
		initial.UnreadMessageCount != 1 || initial.FirstUnreadMessageID != publication.ID {
		t.Fatalf("initial read state = %+v", initial)
	}
	raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "a"}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	timeline := raw.(MainConversationResponse).Messages
	if len(timeline) != 2 || timeline[0].Readable || !timeline[1].Readable {
		t.Fatalf("public eligibility = %+v", timeline)
	}
	mark := &MarkMainConversationRead{SubjectType: "waker", SubjectID: "b"}
	body := `{"throughMessageID":"` + publication.ID + `","observedUnreadGeneration":0,"intent":"explicit"}`
	if err := json.Unmarshal([]byte(body), &mark.Body); err != nil {
		t.Fatal(err)
	}
	if _, err := mark.Output(request); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("read used publication from a different subject: %v", err)
	}
	mark.SubjectID = "a"
	if _, err := mark.Output(request); err != nil {
		t.Fatal(err)
	}
	if got := state(); got.UnreadMessageCount != 0 || got.ThroughSequence != publication.Sequence {
		t.Fatalf("read state = %+v", got)
	}
	unread := &MarkMainConversationUnread{SubjectType: "waker", SubjectID: "a"}
	if err := json.Unmarshal([]byte(`{"fromMessageID":"`+publication.ID+`","observedUnreadGeneration":0}`), &unread.Body); err != nil {
		t.Fatal(err)
	}
	if _, err := unread.Output(request); err != nil {
		t.Fatal(err)
	}
	if _, err := mark.Output(request); !errors.Is(err, status_error.ReadingIntentConflict) {
		t.Fatalf("stale window consumed a new manual unread: %v", err)
	}
	if got := state(); got.UnreadMessageCount != 1 || !got.HoldUnread || got.UnreadGeneration != 1 {
		t.Fatalf("unread state = %+v", got)
	}
	t.Run("remove public messages without removing work", func(t *testing.T) {
		op := &DeleteMainConversationMessage{SubjectType: "waker", SubjectID: "b", MessageID: publication.ID}
		if _, err := op.Output(request); !errors.Is(err, status_error.ChatMessageNotFound) {
			t.Fatalf("another subject could delete the message: %v", err)
		}
		op.SubjectID = "a"
		for _, id := range []string{publication.ID, publication.ID, input.ID} {
			op.MessageID = id
			if _, err := op.Output(request); err != nil {
				t.Fatal(err)
			}
		}
		if got := state(); got.UnreadMessageCount != 0 || got.LatestReadableMessageID != "" {
			t.Fatalf("removed publication is still readable: %+v", got)
		}
		if _, err := unread.Output(request); !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("removed message still accepts an unread mark: %v", err)
		}
		if rows, _, err := service.Messages(ctx, work.ConversationID, 0, 1); err != nil || len(rows) != 0 {
			t.Fatalf("removed rows are still public: %+v, %v", rows, err)
		}
		if rows, err := store.WorkPublications(ctx, work, []string{"execution"}); err != nil || len(rows) != 1 || rows[0].ID != publication.ID {
			t.Fatalf("source work lost its publication: %+v, %v", rows, err)
		}
		if kept, replay, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "call", "public result", input.ID); err != nil || !replay || kept.ID != publication.ID {
			t.Fatalf("original publication receipt was lost: %+v, %t, %v", kept, replay, err)
		}
		if _, _, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: "a"}).resolveReply(ctx, work.ConversationID, input.ID); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
			t.Fatalf("removed message is still a public reply target: %v", err)
		}
		if _, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "late", "late result", input.ID); err != nil {
			t.Fatalf("work could not publish after removing its public input: %v", err)
		}
		if rows, more, err := service.Messages(ctx, work.ConversationID, 0, 1); err != nil || more || len(rows) != 1 || rows[0].Content != "late result" {
			t.Fatalf("public pagination counted removed rows: %+v, %t, %v", rows, more, err)
		}
	})
}

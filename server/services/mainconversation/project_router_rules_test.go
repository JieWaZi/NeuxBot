package mainconversation

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWorkerRuleReceiptReturnsOnlyToOriginalPublicInput(t *testing.T) {
	service, wakers, _, _, _ := newTestService(t)
	ctx := t.Context()
	const partner = "partner"
	if err := wakers.Create(ctx, models.Waker{ID: partner, Name: partner}); err != nil {
		t.Fatal(err)
	}
	conv, err := service.Ensure(ctx, "waker", partner)
	if err != nil {
		t.Fatal(err)
	}
	work, err := service.ReserveWork(ctx, "waker", partner, partner, DefaultWorkScope)
	if err != nil {
		t.Fatal(err)
	}
	origin := models.MainConversationMessage{
		ID: "original-public-input", ConversationID: conv.ID, Role: "user", Content: "Set a reminder",
		SourceChatID: work.WorkChatID, SourceExecutionID: "public-execution", SourceWakerID: partner,
	}
	if _, _, err := service.store.Append(ctx, origin, "waker", partner); err != nil {
		t.Fatal(err)
	}
	if found, err := service.HasAutomaticTaskReceipt(ctx, work.WorkChatID, origin.SourceExecutionID); err != nil || found {
		t.Fatalf("plain public input qualified as a receipt = %t, %v", found, err)
	}
	task := eventlog.AutomaticTaskReceipt{ReceiptID: "receipt", TaskID: "saved-task", Name: "Reminder", NextRunAt: 1795827600000}
	for range 2 {
		published, err := service.PublishAutomaticTaskReceipt(ctx, work.WorkChatID, origin.SourceExecutionID, partner, task)
		if err != nil || !published {
			t.Fatalf("public result = %t, %v", published, err)
		}
	}
	if published, err := service.PublishAutomaticTaskReceipt(ctx, "ordinary-chat", "ordinary-execution", partner, task); err != nil || published {
		t.Fatalf("ordinary Chat unexpectedly published = %t, %v", published, err)
	}
	if err := service.tx.Transaction(ctx, func(tx context.Context) error {
		return dbhelper.GetCtxDB(tx, nil).Create(&models.MainConversationInput{
			ConversationID: conv.ID, WorkChatID: work.WorkChatID, ExecutionID: "private-execution",
			PublicMessageID: "private-input", PrivateSource: true, CommitStarted: true, Status: constants.MainConversationInputAccepted,
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	if published, err := service.PublishAutomaticTaskReceipt(ctx, work.WorkChatID, "private-execution", partner, task); err != nil || published {
		t.Fatalf("private turn unexpectedly published = %t, %v", published, err)
	}
	if published, err := service.PublishAutomaticTaskReceipt(ctx, work.WorkChatID, "direct-private-execution", partner, task); err != nil || published {
		t.Fatalf("direct private turn unexpectedly published = %t, %v", published, err)
	}
	if found, err := service.HasAutomaticTaskReceipt(ctx, work.WorkChatID, origin.SourceExecutionID); err != nil || !found {
		t.Fatalf("committed public receipt = %t, %v", found, err)
	}
	rows, _, err := service.Messages(ctx, conv.ID, 0, 10)
	if err != nil || len(rows) != 2 || rows[1].ReplyToMessageID != origin.ID || rows[1].Content != "" ||
		len(rows[1].AutomaticTaskReceipts) != 1 || rows[1].AutomaticTaskReceipts[0] != task {
		t.Fatalf("public history = %+v; error %v", rows, err)
	}
}

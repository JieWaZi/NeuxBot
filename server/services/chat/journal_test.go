package chat

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestLoadExecutionChatRecoversMissingProjectionAndCatchesUpAuthority(t *testing.T) {
	ctx := context.Background()
	_, manager := newRecoveryTestService(t, t.TempDir())
	chatID := "45454545454545454545454545454545"
	if _, err := manager.logStore.Create(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker", WorkspaceDirectory: t.TempDir(), Title: "授权状态"}),
	}}); err != nil {
		t.Fatal(err)
	}
	chat, err := manager.LoadExecutionChat(ctx, chatID)
	if err != nil || chat.WakerID != "waker" || chat.ActiveExecutionID != "" {
		t.Fatalf("recover identity = %+v, %v", chat, err)
	}
	if _, err := manager.logStore.Append(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "active",
		Data: marshalData(eventlog.ExecutionCreatedData{}),
	}}); err != nil {
		t.Fatal(err)
	}
	chat, err = manager.LoadExecutionChat(ctx, chatID)
	if err != nil || chat.ActiveExecutionID != "active" || chat.LatestSequence != 2 {
		t.Fatalf("catch up execution identity = %+v, %v", chat, err)
	}
	if _, err := manager.logStore.Append(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "active",
		Data: marshalData(eventlog.ExecutionFinishedData{}),
	}}); err != nil {
		t.Fatal(err)
	}
	chat, err = manager.LoadExecutionChat(ctx, chatID)
	if err != nil || chat.ActiveExecutionID != "" || chat.LatestSequence != 3 {
		t.Fatalf("catch up completion = %+v, %v", chat, err)
	}
	indexed, err := manager.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil || indexed.ActiveExecutionID != "" || indexed.LatestSequence != chat.LatestSequence {
		t.Fatalf("recovered index = %+v, %v", indexed, err)
	}
}

func TestReadExecutionRecordsReturnsOnlyTargetAndRejectsUserMessage(t *testing.T) {
	_, manager := newRecoveryTestService(t, t.TempDir())
	chatID := "46464646464646464646464646464646"
	if _, err := manager.logStore.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker", WorkspaceDirectory: t.TempDir(), Title: "执行事实"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Content: "input"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "first", Data: marshalData(eventlog.ExecutionCreatedData{})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "first", Data: marshalData(eventlog.ExecutionFinishedData{})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "second", Data: marshalData(eventlog.ExecutionCreatedData{})},
	}); err != nil {
		t.Fatal(err)
	}
	records, err := manager.ReadExecutionRecords(chatID, "second")
	if err != nil || len(records) != 2 || records[0].Type != constants.ChatEventTypeCreated || records[1].ExecutionID != "second" {
		t.Fatalf("execution records = %+v, %v", records, err)
	}
	for _, id := range []string{"missing", "user"} {
		if _, err := manager.ReadExecutionRecords(chatID, id); !hasStatusErrorCode(err, status_error.ExecutionNotFound.Code()) {
			t.Fatalf("ReadExecutionRecords(%q) error = %v", id, err)
		}
	}
	if _, err := manager.LoadExecutionChat(context.Background(), "47474747474747474747474747474747"); !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		t.Fatalf("missing chat error = %v", err)
	}
}

package chat

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
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

func TestCommittedInteractionResolutionSurvivesRequestCancellation(t *testing.T) {
	for _, kind := range []string{"elicitation", "approval"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			_, manager := newRecoveryTestService(t, t.TempDir())
			chatID := "48484848484848484848484848484848"
			workspace := t.TempDir()
			if _, err := manager.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{ID: chatID, WakerID: "worker", Title: "人工决定", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}}}}); err != nil {
				t.Fatal(err)
			}
			var resolved eventlog.Record
			var err error
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if kind == "elicitation" {
				requested := eventlog.ElicitationRequestedData{ElicitationID: "question", Mode: "form", Message: "选择交付方式"}
				record, appendErr := manager.AppendElicitationFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeElicitationRequested, Data: marshalData(requested)})
				if appendErr != nil {
					t.Fatal(appendErr)
				}
				manager.PublishElicitationFact(ctx, record, []models.PendingElicitation{{ElicitationID: "question"}})
				resolved, err = manager.AppendElicitationFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeElicitationResolved, Data: marshalData(eventlog.ElicitationResolvedData{ElicitationID: "question", Result: constants.ElicitationResultInvalidated})})
				if err != nil {
					t.Fatal(err)
				}
				manager.PublishElicitationFact(cancelled, resolved, nil)
			} else {
				requested := eventlog.ApprovalRequestedData{ApprovalID: "approval", Title: "批准读取"}
				record, appendErr := manager.AppendApprovalFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeApprovalRequested, Data: marshalData(requested)})
				if appendErr != nil {
					t.Fatal(appendErr)
				}
				manager.PublishApprovalFact(ctx, record, []models.PendingApproval{{ApprovalID: "approval"}})
				resolved, err = manager.AppendApprovalFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeApprovalResolved, Data: marshalData(eventlog.ApprovalResolvedData{ApprovalID: "approval", Result: constants.ApprovalResultInvalidated})})
				if err != nil {
					t.Fatal(err)
				}
				manager.PublishApprovalFact(cancelled, resolved, nil)
			}
			// 直接读取SQLite，避免摘要的惰性恢复掩盖提交后投影丢失。
			indexed, err := manager.queryIndexes.chatController.Get(ctx, chatID)
			if err != nil {
				t.Fatal(err)
			}
			if indexed.LatestSequence != resolved.Sequence || indexed.WaitingElicitation || indexed.WaitingApproval || len(indexed.PendingElicitations) != 0 || len(indexed.PendingApprovals) != 0 {
				t.Fatalf("committed resolution left stale user action: %+v", indexed)
			}
		})
	}
}

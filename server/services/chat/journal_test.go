package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
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

// TestUsageFoundationForkPreservesOriginWithoutNewConsumption 验证带用量的父回合只继承身份和展示，不创建子消费事实。
func TestUsageFoundationForkPreservesOriginWithoutNewConsumption(t *testing.T) {
	manager, parent := forkFixture(t)
	root, err := manager.storage.AgentWorkspace("waker")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := manager.logStore.(*conversationJournal).Store
	session := agent.NativeSessionKey{Provider: "codex", Subject: "fixture", SessionID: "native-parent"}
	key := agent.NativeResultKey{Session: session, InvocationID: "spent-invocation"}
	inputTokens, outputTokens := int64(250), int64(30)
	if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, Turn: acpmeta.NativeTurn{
		InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted,
		ConsumedInputIDs: []string{"spent-input"},
		Usage:            &acpmeta.TurnUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, Complete: true, Source: "fixture"},
	}}); err != nil {
		t.Fatal(err)
	}
	changes := conversationfixture.Facts([]eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "spent", Data: marshalData(eventlog.ExecutionCreatedData{RuntimeID: "custom-codex", Model: "model"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "spent", InvocationID: "spent-host"},
	})
	changes = append(changes,
		agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}},
		agent.ConversationChange{Input: &agent.HostInput{Session: session, HostInvocationID: "spent-host", AdapterInputID: "spent-input", ExecutionID: "spent", AssistantMessageID: "spent"}},
		agent.ConversationChange{Consumption: &agent.InputConsumption{HostInvocationID: "spent-host", Result: key}},
		conversationfixture.NativeMessage(t, store, parent, "spent", agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "inherit this answer"}),
	)
	changes = append(changes, conversationfixture.Facts([]eventlog.AppendInput{
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "spent", InvocationID: "spent-host", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, RuntimeID: "custom-codex", WorkingDirectory: root, SessionOutputID: "native-parent", ForkMode: "turn", ForkPosition: "turn-spent"})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "spent", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	})...)
	if _, err := conversationfixture.Commit(ctx, store, parent, changes); err != nil {
		t.Fatal(err)
	}
	parentMessages, err := manager.ReadExecutionMessages(ctx, parent, "spent")
	if err != nil || len(parentMessages) != 1 || parentMessages[0].RunMetrics == nil || parentMessages[0].RunMetrics.InputTokens != 250 {
		t.Fatalf("parent usage fixture missing: %+v error=%v", parentMessages, err)
	}
	child, err := manager.ForkChat(ctx, ForkInput{ChatID: parent, ExecutionID: "spent", ClientRequestID: "usage-origin"}, NativeFork{Execute: func(_ context.Context, input NativeForkInput) (string, error) {
		if input.Invocation.ForkPosition != "turn-spent" {
			t.Fatalf("wrong fork boundary: %+v", input.Invocation)
		}
		return "native-child", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	records, err := manager.logStore.ReadAll(child)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		switch record.Type {
		case constants.ChatEventTypeExecutionCreated, constants.ChatEventTypeExecutionStarted, constants.ChatEventTypeInvocationFinished, constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			t.Fatalf("fork created execution or consumption fact: %+v", record)
		case constants.ChatEventTypeMessageInherited:
			var inherited messageview.InheritedMessage
			if err := json.Unmarshal(record.Data, &inherited); err != nil {
				t.Fatal(err)
			}
			if inherited.SourceMessageID != "spent" {
				continue
			}
			found = true
			if !inherited.Inherited || inherited.SourceChatID != parent || inherited.ID == "spent" || inherited.RunMetrics != nil || inherited.Content != "inherit this answer" {
				t.Fatalf("inherited usage or origin corrupted: %+v", inherited)
			}
		}
	}
	if !found {
		t.Fatal("source message identity missing")
	}
	page, err := manager.ReadMessageHistory(ctx, child, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range page.Resources {
		if !message.Inherited || message.RunMetrics != nil {
			t.Fatalf("public inherited history gained new consumption: %+v", message)
		}
	}
}

package chat

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkResultRecoveryPreservesViewedAndExcludesProcessOnlyRounds(t *testing.T) {
	ctx := context.Background()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID, executionID := util.New(), util.New()
	wakerID := util.New()
	layout, err := chatstorage.NewLayout(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := layout.ChatWorkspace(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureExecutionWorkspace(directory, chatID, wakerID, nil); err != nil {
		t.Fatal(err)
	}
	inputs := []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: wakerID, WorkspaceDirectory: directory, Title: "结果恢复"})}}
	inputs = append(inputs, workResultRound(executionID, constants.RuntimeEventTypeTextDelta, "最终成果")...)
	records, err := store.Create(chatID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	db, service := newRecoveryTestService(t, store.Root())
	results := controllers.NewWorkResultController(db)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	service.queryIndexes.results = results
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	v, found, err := results.Get(ctx, constants.WorkResultOwnerKindChat, chatID)
	if err != nil || !found {
		t.Fatalf("recover result: %+v %v", v, err)
	}
	terminal := records[len(records)-1]
	if v.LatestVersion != terminal.Sequence || v.Reference.MessageID != executionID {
		t.Fatalf("exact published result=%+v", v)
	}
	if _, err := results.MarkViewed(ctx, v.OwnerKind, v.OwnerID, v.LatestVersion, terminal.RecordedAt+1); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.recovery.reconcileChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	after, _, err := results.Get(ctx, v.OwnerKind, v.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ViewedVersion != v.LatestVersion {
		t.Fatalf("projection repair reset read fact=%+v", after)
	}
	processID := util.New()
	if _, err := store.Append(chatID, workResultRound(processID, constants.RuntimeEventTypeCommentaryDelta, "正在协调")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.recovery.reconcileChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	after, _, err = results.Get(ctx, v.OwnerKind, v.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LatestVersion != v.LatestVersion || after.ViewedVersion != v.LatestVersion {
		t.Fatalf("internal process created unread result: %+v", after)
	}
	chat, err := service.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastEndedExecutionID != processID || chat.LastEndedExecutionStatus != constants.ExecutionStatusCompleted {
		t.Fatalf("last ended projection=%+v", chat)
	}
	newID := util.New()
	tail, err := store.Append(chatID, workResultRound(newID, constants.RuntimeEventTypeTextDelta, "新的最终成果"))
	if err != nil {
		t.Fatal(err)
	}
	// 模拟 JSONL durable 成功但 SQLite 未推进，恢复必须一起补齐游标和交付。
	if _, err := service.recovery.reconcileChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	after, _, err = results.Get(ctx, v.OwnerKind, v.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	chat, err = service.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LatestVersion != tail[len(tail)-1].Sequence || after.ViewedVersion != v.LatestVersion || chat.LatestSequence != after.LatestVersion {
		t.Fatalf("recover newer result: receipt=%+v chat=%+v", after, chat)
	}
	liveID := util.New()
	live := workResultRound(liveID, constants.RuntimeEventTypeTextDelta, "实时交付")
	if _, err := store.Append(chatID, live[:len(live)-1]); err != nil {
		t.Fatal(err)
	}
	if err := service.catchUpLiveProjection(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	commit, err := service.CommitTerminal(ctx, TerminalFact{Execution: models.Execution{ID: liveID, ChatID: chatID, Status: constants.ExecutionStatusRunning}, Type: constants.ChatEventTypeExecutionCompleted, Status: constants.ExecutionStatusCompleted, Data: live[len(live)-1].Data})
	if err != nil {
		t.Fatal(err)
	}
	service.PublishTerminal(commit)
	after, _, err = results.Get(ctx, v.OwnerKind, v.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	chat, err = service.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Reference.MessageID != liveID || after.LatestVersion != chat.LatestSequence || chat.LastEndedExecutionStatus != constants.ExecutionStatusCompleted {
		t.Fatalf("durable terminal and result were not advanced together: receipt=%+v chat=%+v", after, chat)
	}
	if err := service.DeleteChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := results.Get(ctx, v.OwnerKind, v.OwnerID); err != nil || found {
		t.Fatalf("real deletion retained result: found=%t error=%v", found, err)
	}
}

func workResultRound(executionID string, kind constants.RuntimeEventType, text string) []eventlog.AppendInput {
	return []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{RuntimeID: "codex"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID},
		{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: executionID, Data: marshalData(eventlog.RuntimeMessageData{MessageRevision: 10, Content: []eventlog.RuntimeMessageContent{{Type: kind, Data: marshalData(map[string]string{"text": text})}}})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	}
}

func TestFailedRoundPublishesReadableErrorAsUnreadResult(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID, executionID := util.New(), util.New()
	records, err := store.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: util.New()})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{RuntimeID: "codex"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID},
		{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeFailed", ErrorMessage: "服务暂时不可用"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := resultFromRecords(records, executionID)
	if err != nil || receipt == nil {
		t.Fatalf("失败信息应作为可读结果: %+v %v", receipt, err)
	}
	if receipt.Reference.MessageID != executionID || receipt.Reference.ChatID != chatID || receipt.LatestVersion != records[len(records)-1].Sequence {
		t.Fatalf("失败结果必须精确定位原失败消息: %+v", receipt)
	}
}

func TestFileResultReferenceRequiresVisibleCardButEmptyReadyKeepsBodyVersion(t *testing.T) {
	for _, test := range []struct {
		// name 标识实际文件交付或可见采集状态。
		name string
		// state 是独立采集状态。
		state string
		// files 是实际净变更数量。
		files int
		// visible 表示该结果必须通过文件卡片确认阅读。
		visible bool
	}{{"pending", "pending", 0, false}, {"no_changes", "ready", 0, false}, {"files", "ready", 2, true}, {"partial", "partial", 0, true}, {"unavailable", "unavailable", 0, true}, {"incomplete", "incomplete", 0, true}} {
		t.Run(test.name, func(t *testing.T) {
			store, err := eventlog.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chatID, executionID := util.New(), util.New()
			inputs := []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: util.New(), WorkspaceDirectory: "/workspace"})}}
			inputs = append(inputs, workResultRound(executionID, constants.RuntimeEventTypeTextDelta, "unchanged final answer")...)
			records, err := store.Create(chatID, inputs)
			if err != nil {
				t.Fatal(err)
			}
			terminal := records[len(records)-1]
			version := 2
			if test.state == "pending" {
				version = 1
			}
			added, err := store.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeFileChanges, MessageID: executionID, ExecutionID: executionID, Data: marshalData(eventlog.FileChangesData{RecordID: "exact-file-record", Version: version, State: test.state, TotalFiles: test.files})}})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := resultFromRecords(append(records, added...), executionID)
			if err != nil || receipt == nil {
				t.Fatalf("readable result missing: %+v %v", receipt, err)
			}
			wantVersion := terminal.Sequence
			if test.visible {
				wantVersion = added[0].Sequence
			}
			if receipt.LatestVersion != wantVersion {
				t.Fatalf("file presentation advanced wrong result version: %+v", receipt)
			}
			if test.visible {
				if receipt.Reference.FileChangeRecordID != "exact-file-record" || receipt.Reference.FileChangeRecordVersion != version {
					t.Fatalf("file identity missing: %+v", receipt.Reference)
				}
			} else if receipt.Reference.FileChangeRecordID != "" || receipt.Reference.FileChangeRecordVersion != 0 {
				t.Fatalf("invisible state blocked body reading: %+v", receipt.Reference)
			}
		})
	}
}

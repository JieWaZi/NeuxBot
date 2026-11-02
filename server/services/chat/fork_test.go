package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func forkFixture(t *testing.T) (*service, string) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "fork.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	m := newTestService(t, Config{StorageDirectory: t.TempDir(), QueryIndexes: QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), RuntimeSessions: controllers.NewRuntimeSessionController(database.DB), DeletionIntents: controllers.NewChatDeletionIntentController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}})
	id := "12345678901234567890123456789012"
	root, err := m.storage.ChatWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = workspace.EnsureExecutionWorkspace(root, id, "waker", nil); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "attachments", "before.txt"), []byte("独立附件"), 0600); err != nil {
		t.Fatal(err)
	}
	facts := []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker", WorkspaceDirectory: root, Title: "原会话"})}}
	for _, mark := range []string{"before", "after"} {
		facts = append(facts,
			eventlog.AppendInput{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-" + mark, Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: mark, Attachments: []eventlog.AttachmentReference{{Name: "before.txt", Path: "attachments/before.txt", Managed: true}}})},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: mark, Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user-" + mark, RuntimeID: "custom-codex", Model: "model", Reasoning: "high", PermissionLevel: constants.PermissionLevelDefault})},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: mark},
			eventlog.AppendInput{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: mark, Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, RuntimeID: "custom-codex", WorkingDirectory: root, SessionOutputID: "native-parent", ForkMode: "turn", ForkPosition: "turn-" + mark})},
			eventlog.AppendInput{Type: constants.ChatEventTypeNativeSessionUpdated, ExecutionID: mark, Data: marshalData(eventlog.RuntimeSessionData{RuntimeID: "custom-codex", SessionID: "native-parent", LastExecutionID: mark})},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: mark, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		)
	}
	if _, err = m.logStore.Create(id, facts); err != nil {
		t.Fatal(err)
	}
	return m, id
}

// TestForkCopiesBoundaryWithoutExecutionsAndSurvivesParentDeletion 验证继承历史边界和独立生命周期。
func TestForkCopiesBoundaryWithoutExecutionsAndSurvivesParentDeletion(t *testing.T) {
	m, parent := forkFixture(t)
	calls := 0
	native := func(_ context.Context, input NativeForkInput) (string, error) {
		calls++
		if input.Invocation.ForkPosition != "turn-before" || input.Invocation.SessionOutputID != "native-parent" {
			t.Fatalf("wrong native boundary: %+v", input)
		}
		return "native-child", nil
	}
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "stable-request"}
	child, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native})
	if err != nil {
		t.Fatal(err)
	}
	same, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native})
	if err != nil || same != child || calls != 1 {
		t.Fatalf("retry: %s %v calls=%d", same, err, calls)
	}
	detail, err := m.LoadExecutionContext(context.Background(), child)
	if err != nil {
		t.Fatal(err)
	}
	inheritedPage, pageErr := m.ReadMessageHistory(context.Background(), child, 0, 20)
	if pageErr != nil {
		t.Fatal(pageErr)
	}
	if len(inheritedPage.Resources) != 2 || inheritedPage.Resources[0].Content != "before" || detail.Chat.ForkSourceChatID != parent {
		t.Fatalf("unexpected inherited state: %+v", detail)
	}
	if detail.Chat.LastReasoning != "high" {
		t.Fatal("reasoning snapshot lost")
	}
	if detail.RuntimeSessions["custom-codex"].SessionID != "native-child" {
		t.Fatal("parent session binding copied")
	}
	capabilities, err := m.ReadMessageHistory(context.Background(), child, 0, 50)
	if err != nil || capabilities.ForkLatestExecutionID == "" {
		t.Fatalf("fork head missing: %+v %v", capabilities, err)
	}
	latest := capabilities.Resources[len(capabilities.Resources)-1]
	if latest.Fork == nil || !latest.Fork.Available {
		t.Fatalf("fork of fork unavailable: %+v", latest)
	}
	if err = m.DeleteChat(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(detail.Chat.WorkspaceDirectory, "attachments/before.txt"))
	if err != nil || string(content) != "独立附件" {
		t.Fatalf("child attachment: %q %v", content, err)
	}
	page, err := m.ReadMessageHistory(context.Background(), child, 0, 1)
	if err != nil || len(page.Resources) != 1 || !page.HasMore || !page.Resources[0].Inherited {
		t.Fatalf("pagination: %+v %v", page, err)
	}
	older, err := m.ReadMessageHistory(context.Background(), child, page.NextBeforeSequence, 1)
	if err != nil || len(older.Resources) != 1 || older.Resources[0].Content != "before" {
		t.Fatalf("older page: %+v %v", older, err)
	}
	if err = m.DeleteChat(context.Background(), child); err != nil {
		t.Fatal(err)
	}
	if id, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native}); err != nil || id != child || calls != 1 {
		t.Fatalf("deleted child retry: %s %v", id, err)
	}
}

// TestForkRejectsConcurrentDeletionAndUncertainRetry 验证共享操作保护和不确定结果的重试边界。
func TestForkRejectsConcurrentDeletionAndUncertainRetry(t *testing.T) {
	m, parent := forkFixture(t)
	calls := 0
	native := func(_ context.Context, input NativeForkInput) (string, error) {
		if input.ReconcileOnly {
			return "", status_error.ChatForkIndeterminate
		}
		calls++
		if err := m.DeleteChat(context.Background(), parent); !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Fatalf("concurrent delete: %v", err)
		}
		if err := m.DeleteWakerChats(context.Background(), "waker"); !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Fatalf("concurrent waker delete: %v", err)
		}
		release, err := chatstorage.AcquireOperation(m.storage.Root(), parent)
		if release != nil {
			release()
		}
		if !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Fatalf("send admission: %v", err)
		}
		return "", context.DeadlineExceeded
	}
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "uncertain"}
	if _, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native}); !errors.Is(err, status_error.ChatForkIndeterminate) || calls != 1 {
		t.Fatalf("blind retry: %v calls=%d", err, calls)
	}
	input.ExecutionID = "after"
	if _, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native}); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("request collision: %v", err)
	}
}

// TestForkRecoveryPublishesDurableNativeResult 验证进程重启后完成已确认原生分叉的发布。
func TestForkRecoveryPublishesDurableNativeResult(t *testing.T) {
	m, parent := forkFixture(t)
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "restart"}
	operation, err := m.prepareFork(input)
	if err != nil {
		t.Fatal(err)
	}
	var payload forkPayload
	if err = json.Unmarshal(operation.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if err = m.prepareForkWorkspace(operation.ChatID, payload); err != nil {
		t.Fatal(err)
	}
	operation.Prepared = true
	operation.NativeStarted = true
	operation.SessionID = "durable-child"
	if err = chatstorage.WriteForkOperation(m.storage.ForkDirectory(parent, input.ClientRequestID), operation); err != nil {
		t.Fatal(err)
	}
	if err = m.recoverForks(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := m.LoadExecutionContext(context.Background(), operation.ChatID)
	if err != nil || state.RuntimeSessions["custom-codex"].SessionID != "durable-child" {
		t.Fatalf("recovery: %+v %v", state, err)
	}
}

// failForkProjection 模拟 JSONL 已落盘后 SQLite 创建失败。
type failForkProjection struct {
	// ChatController 复用真实临时 SQLite 的其他操作。
	controllers.ChatController
}

// Create 让查询投影失败，但不改变已持久化的原生分支身份。
func (f failForkProjection) Create(context.Context, models.Chat) error {
	return errors.New("injected projection failure")
}

// TestForkProjectionFailureRecoversWithoutSecondNativeCall 验证投影失败和重启的幂等恢复。
func TestForkProjectionFailureRecoversWithoutSecondNativeCall(t *testing.T) {
	m, parent := forkFixture(t)
	original := m.queryIndexes.chatController
	m.queryIndexes.chatController = failForkProjection{ChatController: original}
	calls := 0
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "projection-fail"}
	native := func(context.Context, NativeForkInput) (string, error) { calls++; return "durable-child", nil }
	if _, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native}); err == nil {
		t.Fatal("projection failure hidden")
	}
	m.queryIndexes.chatController = original
	if err := m.recoverForks(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native})
	if err != nil || calls != 1 {
		t.Fatalf("%s %v calls=%d", id, err, calls)
	}
	detail, err := m.LoadExecutionContext(context.Background(), id)
	if err != nil || detail.Chat.LastReasoning != "high" {
		t.Fatalf("%+v %v", detail, err)
	}
}

// TestForkDeletionSealsVisibleChild 验证发布标记尚未写入时删除子会话不会复活。
func TestForkDeletionSealsVisibleChild(t *testing.T) {
	m, parent := forkFixture(t)
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "delete-visible"}
	operation, err := m.prepareFork(input)
	if err != nil {
		t.Fatal(err)
	}
	var payload forkPayload
	if err = json.Unmarshal(operation.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if err = m.prepareForkWorkspace(operation.ChatID, payload); err != nil {
		t.Fatal(err)
	}
	operation.Prepared = true
	operation.NativeStarted = true
	operation.SessionID = "native-child"
	directory := m.storage.ForkDirectory(parent, input.ClientRequestID)
	if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
		t.Fatal(err)
	}
	if err = m.publishFork(context.Background(), directory, operation, payload); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteChat(context.Background(), operation.ChatID); err != nil {
		t.Fatal(err)
	}
	if err = m.recoverForks(context.Background()); err != nil {
		t.Fatal(err)
	}
	childRoot, err := m.storage.ChatRoot(operation.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(childRoot); !os.IsNotExist(err) {
		t.Fatalf("deleted child resurrected: %v", err)
	}
}

// TestForkDeletionCancelsUnpublishedChild 验证删除来源后未公开历史和附件被清理。
func TestForkDeletionCancelsUnpublishedChild(t *testing.T) {
	m, parent := forkFixture(t)
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "delete-pending"}
	operation, err := m.prepareFork(input)
	if err != nil {
		t.Fatal(err)
	}
	var payload forkPayload
	if err = json.Unmarshal(operation.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if err = m.prepareForkWorkspace(operation.ChatID, payload); err != nil {
		t.Fatal(err)
	}
	operation.Prepared = true
	operation.NativeStarted = true
	operation.SessionID = "native-child"
	directory := m.storage.ForkDirectory(parent, input.ClientRequestID)
	if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteChat(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	if err = m.recoverForks(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, exists, err := chatstorage.ReadForkOperation(directory)
	if err != nil || !exists || !updated.Cancelled || string(updated.Payload) != "null" {
		t.Fatalf("pending history remains: %+v %v", updated, err)
	}
	childRoot, err := m.storage.ChatRoot(operation.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(childRoot); !os.IsNotExist(err) {
		t.Fatalf("pending workspace remains: %v", err)
	}
}

// TestForkPreflightFailureCanRetry 验证无原生副作用的失败不会进入对账死路。
func TestForkPreflightFailureCanRetry(t *testing.T) {
	m, parent := forkFixture(t)
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "preflight"}
	ready := false
	calls := 0
	native := NativeFork{
		Preflight: func(context.Context, NativeForkInput) error {
			if !ready {
				return status_error.RuntimeUnavailable
			}
			return nil
		},
		Execute: func(_ context.Context, input NativeForkInput) (string, error) {
			if input.ReconcileOnly {
				t.Fatal("retry was treated as an uncertain native call")
			}
			calls++
			return "native-child", nil
		},
	}
	if _, err := m.ForkChat(context.Background(), input, native); !errors.Is(err, status_error.RuntimeUnavailable) || calls != 0 {
		t.Fatalf("preflight: %v calls=%d", err, calls)
	}
	operation, exists, err := chatstorage.ReadForkOperation(m.storage.ForkDirectory(parent, input.ClientRequestID))
	if err != nil || !exists || operation.NativeStarted {
		t.Fatalf("preflight crossed native boundary: %+v %v", operation, err)
	}
	ready = true
	if _, err = m.ForkChat(context.Background(), input, native); err != nil || calls != 1 {
		t.Fatalf("retry: %v calls=%d", err, calls)
	}
}

// TestDeleteWakerCancelsPendingFork 验证删除伙伴时待发布子会话不会在重启后成为孤儿。
func TestDeleteWakerCancelsPendingFork(t *testing.T) {
	m, parent := forkFixture(t)
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "delete-waker"}
	operation, err := m.prepareFork(input)
	if err != nil {
		t.Fatal(err)
	}
	var payload forkPayload
	if err = json.Unmarshal(operation.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if err = m.prepareForkWorkspace(operation.ChatID, payload); err != nil {
		t.Fatal(err)
	}
	operation.Prepared = true
	operation.NativeStarted = true
	operation.SessionID = "native-child"
	directory := m.storage.ForkDirectory(parent, input.ClientRequestID)
	if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteWakerChats(context.Background(), "waker"); err != nil {
		t.Fatal(err)
	}
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	childLog, err := m.forkLogExists(operation.ChatID)
	if err != nil || childLog {
		t.Fatalf("deleted waker has child chat: exists=%t err=%v", childLog, err)
	}
	updated, exists, err := chatstorage.ReadForkOperation(directory)
	if err != nil || !exists || !updated.Cancelled {
		t.Fatalf("pending fork not cancelled: %+v %v", updated, err)
	}
}

// TestDeleteWakerIncludesPublishedFork 验证新分支公开后伙伴删除会枚举并删除它。
func TestDeleteWakerIncludesPublishedFork(t *testing.T) {
	m, parent := forkFixture(t)
	child, err := m.ForkChat(context.Background(), ForkInput{
		ChatID: parent, ExecutionID: "before", ClientRequestID: "delete-published-waker",
	}, NativeFork{Execute: func(context.Context, NativeForkInput) (string, error) {
		return "native-child", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteWakerChats(context.Background(), "waker"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{parent, child} {
		exists, checkErr := m.forkLogExists(id)
		if checkErr != nil || exists {
			t.Fatalf("waker deletion left chat %s: exists=%t err=%v", id, exists, checkErr)
		}
	}
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists, err := m.forkLogExists(child)
	if err != nil || exists {
		t.Fatalf("published fork resurrected: exists=%t err=%v", exists, err)
	}
}

// TestForkAttachmentFailureRetriesPreparationBeforeNativeSideEffect 验证缺失附件不会先执行原生副作用。
func TestForkAttachmentFailureRetriesPreparationBeforeNativeSideEffect(t *testing.T) {
	m, parent := forkFixture(t)
	root, err := m.storage.ChatWorkspace(parent)
	if err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(root, "attachments", "before.txt")
	if err = os.Remove(attachment); err != nil {
		t.Fatal(err)
	}
	calls := 0
	input := ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "file-fail"}
	native := func(context.Context, NativeForkInput) (string, error) { calls++; return "native-child", nil }
	if _, err = m.ForkChat(context.Background(), input, NativeFork{Execute: native}); err == nil || calls != 0 {
		t.Fatal("native called before attachments were ready")
	}
	operation, exists, err := chatstorage.ReadForkOperation(m.storage.ForkDirectory(parent, input.ClientRequestID))
	if err != nil || !exists || operation.NativeStarted {
		t.Fatalf("intent lost: %+v %v", operation, err)
	}
	if err = os.WriteFile(attachment, []byte("restored"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := m.ForkChat(context.Background(), input, NativeFork{Execute: native})
	if err != nil || id != operation.ChatID || calls != 1 {
		t.Fatalf("retry: %s %v calls=%d", id, err, calls)
	}
}

// TestForkActiveAndUnsupportedBoundaryAreRejected 验证等待状态、团队和末尾精度来自权威事实。
func TestForkActiveAndUnsupportedBoundaryAreRejected(t *testing.T) {
	m, parent := forkFixture(t)
	records, state, err := m.loadChatState(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"active", "approval", "input", "team"} {
		changed := state
		switch kind {
		case "active":
			changed.chat.ActiveExecutionID = "running"
		case "approval":
			changed.chat.WaitingApproval = true
		case "input":
			changed.chat.WaitingElicitation = true
		case "team":
			changed.threadWorkspaceID = "team"
		}
		targets, err := forkPoints(records, changed)
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range targets {
			if point.target.Available || point.target.ReasonCode == "" {
				t.Fatal(kind, point)
			}
		}
	}
	for i := range records {
		if records[i].Type == constants.ChatEventTypeInvocationFinished {
			var data eventlog.InvocationFinishedData
			_ = json.Unmarshal(records[i].Data, &data)
			data.ForkMode = "latest"
			data.ForkPosition = ""
			records[i].Data = marshalData(data)
		}
	}
	points, err := forkPoints(records, state)
	if err != nil || len(points) != 2 || points[0].target.Available || !points[1].target.Available {
		t.Fatalf("latest boundary: %+v %v", points, err)
	}
}

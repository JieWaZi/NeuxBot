package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// failingDeleteTransactionController 模拟 staging 完成后的数据库提交失败。
type failingDeleteTransactionController struct {
	// TransactionController 补齐本测试不调用的真实事务能力。
	controllers.TransactionController
}

// Transaction 拒绝删除事务，验证文件聚合会回滚到正式位置。
func (*failingDeleteTransactionController) Transaction(
	context.Context,
	func(context.Context) error,
) error {
	return status_error.DatabaseError
}

func TestDeleteWakerChatsSkipsMissingRegisteredChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chats, _ := newDeleteTestService(t)
	// 查询不存在的对话会注册地址，但没有创建权威日志。
	missingID := "11111111111111111111111111111111"
	if _, err := chats.logStore.ReadAll(missingID); !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		t.Fatalf("query missing chat: %v", err)
	}
	if err := chats.DeleteWakerChats(ctx, "waker-target", func([]string) (func() error, error) { return nil, nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("delete waker with missing registered chat: %v", err)
	}
}

func TestDeleteMissingChatsCleansIndexesAndFinalizes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, chats, _ := newDeleteTestService(t)
	chatID := "12121212121212121212121212121212"
	controller := controllers.NewChatController(db)
	if err := controller.Create(ctx, models.Chat{ID: chatID, WakerID: "waker-target"}); err != nil {
		t.Fatal(err)
	}
	sessions := controllers.NewRuntimeSessionController(db)
	if err := sessions.Upsert(ctx, models.RuntimeSession{ChatID: chatID, RuntimeID: "codex", SessionID: "stale"}); err != nil {
		t.Fatal(err)
	}
	finalized := false
	if err := chats.DeleteChatsAtomically(ctx, []string{chatID, chatID}, func(context.Context) error {
		finalized = true
		return nil
	}); err != nil {
		t.Fatalf("delete missing chats: %v", err)
	}
	if !finalized {
		t.Fatal("parent aggregate was not finalized")
	}
	if _, err := controller.Get(ctx, chatID); !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		t.Fatalf("stale chat index remains: %v", err)
	}
	if session, err := sessions.Get(ctx, chatID, "codex"); err != nil || session.SessionID != "" {
		t.Fatalf("stale runtime session remains: %+v, %v", session, err)
	}
	if err := chats.DeleteChat(ctx, chatID); err != nil {
		t.Fatalf("repeat deletion: %v", err)
	}
}

func TestDeleteChatsPreservesReadAndDatabaseErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chats, _ := newDeleteTestService(t)
	chatID := "14141414141414141414141414141414"
	path, err := chats.logStore.LogPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	finalized := false
	finalize := func(context.Context) error {
		finalized = true
		return nil
	}
	if err := chats.DeleteChatsAtomically(ctx, []string{chatID}, finalize); !hasStatusErrorCode(err, status_error.ChatLogCorrupted.Code()) {
		t.Fatalf("corrupt log must block deletion: %v", err)
	}
	if err := chats.DeleteWakerChats(ctx, "waker-target", func([]string) (func() error, error) { return nil, nil }, func(context.Context) error { return nil }); !hasStatusErrorCode(err, status_error.ChatLogCorrupted.Code()) {
		t.Fatalf("corrupt log must block waker deletion: %v", err)
	}
	if finalized {
		t.Fatal("parent deletion finalized despite corrupt log")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	chats.queryIndexes.transactionController = &failingDeleteTransactionController{}
	if err := chats.DeleteChatsAtomically(ctx, []string{chatID}, finalize); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("database failure must block deletion: %v", err)
	}
	if finalized {
		t.Fatal("parent deletion finalized despite database failure")
	}
}

func TestDeleteChatRemovesAuthorityIndexesAndManagedDirectories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, chatService, logStore := newDeleteTestService(t)
	chatController := controllers.NewChatController(db)
	runtimeSessionController := controllers.NewRuntimeSessionController(db)

	chatID := "81818181818181818181818181818181"
	messageID := "82828282828282828282828282828282"
	workspaceDirectory, err := chatService.storage.AgentWorkspace("waker-1")
	if err != nil {
		t.Fatalf("AgentWorkspace() error = %v", err)
	}
	err = workspace.EnsureAgentWorkspace(workspaceDirectory, "waker-1")
	if err != nil {
		t.Fatalf("EnsureAgentWorkspace() error = %v", err)
	}
	contextDirectory, err := chatService.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(contextDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	externalDirectory := t.TempDir()
	externalPath := filepath.Join(externalDirectory, "brief.txt")
	if err := os.WriteFile(externalPath, []byte("brief"), 0o600); err != nil {
		t.Fatalf("write external attachment: %v", err)
	}
	prepared, err := workspace.PrepareChatAttachments(
		contextDirectory,
		chatID,
		messageID,
		[]workspace.AttachmentInput{{SourcePath: externalPath}},
	)
	if err != nil {
		t.Fatalf("PrepareChatAttachments() error = %v", err)
	}
	attachmentDirectory := filepath.Dir(prepared.RuntimePaths[0])
	attachments := []eventlog.AttachmentReference{{
		Name:      prepared.References[0].Name,
		Path:      prepared.References[0].Path,
		MediaType: prepared.References[0].MediaType,
		Managed:   prepared.References[0].Managed,
	}}
	records, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: workspaceDirectory,
				Title:              "delete me",
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: messageID,
			Data: marshalData(eventlog.MessageCreatedData{
				Role:        constants.MessageRoleUser,
				Content:     "hello",
				Attachments: attachments,
			}),
		},
	})
	if err != nil {
		t.Fatalf("Create() log error = %v", err)
	}
	if err := chatController.Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            "waker-1",
		WorkspaceDirectory: workspaceDirectory,
		Title:              "delete me",
		LatestSequence:     records[len(records)-1].Sequence,
	}); err != nil {
		t.Fatalf("Create() chat error = %v", err)
	}
	if err := runtimeSessionController.Upsert(ctx, models.RuntimeSession{
		ChatID:    chatID,
		RuntimeID: "codex",
		SessionID: "session-1",
	}); err != nil {
		t.Fatalf("Upsert() session error = %v", err)
	}

	// 混合批量删除同时覆盖正常日志和不存在的关联。
	if err := chatService.DeleteChats(ctx, []string{chatID, "13131313131313131313131313131313"}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := chatService.DeleteChat(ctx, chatID); err != nil {
		t.Fatalf("repeat DeleteChat() error = %v", err)
	}
	if _, err := logStore.ReadAll(chatID); err == nil {
		t.Fatal("deleted JSONL can still be read")
	}
	for _, directory := range []string{contextDirectory, attachmentDirectory} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("managed directory %q still exists: %v", directory, err)
		}
	}
	if _, err := chatController.Get(ctx, chatID); err == nil {
		t.Fatal("chat query index still exists")
	}
	if session, err := runtimeSessionController.Get(ctx, chatID, "codex"); err != nil || session.SessionID != "" {
		t.Fatalf("native session after delete = %+v, error = %v", session, err)
	}

}

func TestDeleteChatOwnershipFailurePreservesAuthorityAndIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, chatService, logStore := newDeleteTestService(t)
	chatController := controllers.NewChatController(db)
	chatID := "91919191919191919191919191919191"
	workspaceDirectory, err := chatService.storage.AgentWorkspace("waker-1")
	if err != nil {
		t.Fatalf("AgentWorkspace() error = %v", err)
	}
	err = workspace.EnsureAgentWorkspace(workspaceDirectory, "waker-1")
	if err != nil {
		t.Fatalf("EnsureAgentWorkspace() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(workspaceDirectory, constants.ChatContextOwnerFileName),
		[]byte(`{"kind":"chat-execution-workspace","chatId":"other"}`),
		0o600,
	); err != nil {
		t.Fatalf("write mismatched owner: %v", err)
	}
	records, err := logStore.Create(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{
			WakerID:            "waker-1",
			WorkspaceDirectory: workspaceDirectory,
			Title:              "keep me",
		}),
	}})
	if err != nil {
		t.Fatalf("Create() log error = %v", err)
	}
	if err := chatController.Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            "waker-1",
		WorkspaceDirectory: workspaceDirectory,
		Title:              "keep me",
		LatestSequence:     records[0].Sequence,
	}); err != nil {
		t.Fatalf("Create() chat error = %v", err)
	}

	err = chatService.DeleteChat(ctx, chatID)
	if !errors.Is(err, status_error.ChatDirectoryOwnershipInvalid) &&
		err != status_error.ChatDirectoryOwnershipInvalid {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := logStore.ReadAll(chatID); err != nil {
		t.Fatalf("authority was removed after ownership failure: %v", err)
	}
	if _, err := chatController.Get(ctx, chatID); err != nil {
		t.Fatalf("query index was removed after ownership failure: %v", err)
	}
}

func TestDeleteChatDatabaseFailureRestoresStagedAggregate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chatService, logStore := newDeleteTestService(t)
	chatID := "83838383838383838383838383838383"
	workspaceDirectory, err := chatService.storage.AgentWorkspace("waker-1")
	if err != nil {
		t.Fatalf("AgentWorkspace() error = %v", err)
	}
	if err := workspace.EnsureAgentWorkspace(workspaceDirectory, "waker-1"); err != nil {
		t.Fatalf("EnsureAgentWorkspace() error = %v", err)
	}
	if _, err := logStore.Create(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{
			WakerID: "waker-1", WorkspaceDirectory: workspaceDirectory, Title: "rollback",
		}),
	}}); err != nil {
		t.Fatalf("Create() log error = %v", err)
	}
	if err := chatService.queryIndexes.chatController.Create(ctx, models.Chat{
		ID: chatID, WakerID: "waker-1", WorkspaceDirectory: workspaceDirectory, Title: "rollback",
	}); err != nil {
		t.Fatalf("Create() chat index error = %v", err)
	}
	chatService.queryIndexes.transactionController = &failingDeleteTransactionController{}
	if err := chatService.DeleteChat(ctx, chatID); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("DeleteChat() error = %v, want DatabaseError", err)
	}
	if _, err := logStore.ReadAll(chatID); err != nil {
		t.Fatalf("authority was not restored: %v", err)
	}
	if _, err := os.Stat(workspaceDirectory); err != nil {
		t.Fatalf("workspace was not restored: %v", err)
	}
}

func TestDeleteChatDoesNotRequireMessageView(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, chatService, logStore := newDeleteTestService(t)
	if err := db.AutoMigrate(
		&models.Chat{},
		&models.RuntimeSession{},
	); err != nil {
		t.Fatalf("migrate delete test indexes: %v", err)
	}
	chatController := chatService.queryIndexes.chatController
	chatID := "92929292929292929292929292929292"
	messageID := "93939393939393939393939393939393"
	workspaceDirectory, err := chatService.storage.AgentWorkspace("waker-1")
	if err != nil {
		t.Fatalf("AgentWorkspace() error = %v", err)
	}
	if err := workspace.EnsureAgentWorkspace(workspaceDirectory, "waker-1"); err != nil {
		t.Fatalf("EnsureAgentWorkspace() error = %v", err)
	}
	records, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: workspaceDirectory,
				Title:              "delete unprojectable log",
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: messageID,
			Data:        marshalData(eventlog.ExecutionCreatedData{}),
		},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "malformed-message", Data: []byte(`[]`)},
		{
			Type:        constants.ChatEventTypeExecutionCompleted,
			ExecutionID: messageID,
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusCompleted,
			}),
		},
	})
	if err != nil {
		t.Fatalf("Create() log error = %v", err)
	}
	if err := chatController.Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            "waker-1",
		WorkspaceDirectory: workspaceDirectory,
		Title:              "delete unprojectable log",
		LatestSequence:     records[len(records)-1].Sequence,
	}); err != nil {
		t.Fatalf("Create() chat error = %v", err)
	}

	if err := chatService.DeleteChat(ctx, chatID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := logStore.ReadAll(chatID); err == nil {
		t.Fatal("deleted unprojectable JSONL can still be read")
	}
}

func TestDeleteProvisionalTeamRemovesPartialMemberChatsAndPlannedIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chatService, logStore := newDeleteTestService(t)
	threadID := "thread-provisional-delete"
	provisioningID := "provision-provisional-delete"
	leadChatID := "71717171717171717171717171717171"
	missingChatID := "72727272727272727272727272727272"
	threadWorkspace, _ := chatService.storage.TeamWorkspace(threadID)
	leadWorkspace, _ := chatService.storage.TeamMemberWorkspace(threadID, "lead-1", true)
	if err := workspace.EnsureThreadExecutionWorkspace(
		threadWorkspace,
		threadID,
		"lead-1",
	); err != nil {
		t.Fatalf("ensure thread workspace: %v", err)
	}
	if err := workspace.EnsureThreadMemberExecutionWorkspace(
		leadWorkspace,
		threadID,
		leadChatID,
		"lead-1",
	); err != nil {
		t.Fatalf("ensure lead workspace: %v", err)
	}
	if _, err := logStore.Create(leadChatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{
			WakerID:            "lead-1",
			WorkspaceDirectory: leadWorkspace,
			Title:              "partial team",
			ThreadWorkspaceID:  threadID,
		}),
	}}); err != nil {
		t.Fatalf("create partial team log: %v", err)
	}
	for _, chatID := range []string{leadChatID, missingChatID} {
		if err := chatService.queryIndexes.chatController.Create(ctx, models.Chat{
			ID: chatID, WakerID: "lead-1", WorkspaceDirectory: leadWorkspace, Title: "partial",
		}); err != nil {
			t.Fatalf("create planned chat index %s: %v", chatID, err)
		}
	}
	if err := chatService.DeleteProvisionalTeam(
		ctx,
		threadID,
		provisioningID,
		[]string{leadChatID, missingChatID},
	); err != nil {
		t.Fatalf("DeleteProvisionalTeam() error = %v", err)
	}
	teamRoot, _ := chatService.storage.TeamRoot(threadID)
	if _, err := os.Stat(teamRoot); !os.IsNotExist(err) {
		t.Fatalf("provisional team root still exists: %v", err)
	}
	for _, chatID := range []string{leadChatID, missingChatID} {
		if _, err := chatService.queryIndexes.chatController.Get(ctx, chatID); err == nil {
			t.Fatalf("planned chat index %s still exists", chatID)
		}
	}
}

func TestDeleteWakerChatsUsesAuthorityAndRejectsActiveExecution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chatService, logStore := newDeleteTestService(t)
	targetID := "94949494949494949494949494949494"
	otherID := "95959595959595959595959595959595"
	executionID := "96969696969696969696969696969696"
	for _, input := range []struct {
		// ChatID 是测试写入的权威对话标识。
		ChatID string
		// WakerID 是权威创建事件中的伙伴标识。
		WakerID string
	}{
		{ChatID: targetID, WakerID: "waker-target"},
		{ChatID: otherID, WakerID: "waker-other"},
	} {
		workspaceDirectory, err := chatService.storage.AgentWorkspace(input.WakerID)
		if err != nil {
			t.Fatalf("AgentWorkspace(%s) error = %v", input.ChatID, err)
		}
		if err := workspace.EnsureAgentWorkspace(workspaceDirectory, input.WakerID); err != nil {
			t.Fatalf("EnsureAgentWorkspace(%s) error = %v", input.ChatID, err)
		}
		entries := []eventlog.AppendInput{{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            input.WakerID,
				WorkspaceDirectory: workspaceDirectory,
				Title:              "authority only",
			}),
		}}
		if input.ChatID == targetID {
			entries = append(entries, eventlog.AppendInput{
				Type:        constants.ChatEventTypeExecutionCreated,
				ExecutionID: executionID,
				Data:        marshalData(eventlog.ExecutionCreatedData{}),
			})
		}
		if _, err := logStore.Create(input.ChatID, entries); err != nil {
			t.Fatalf("Create(%s) error = %v", input.ChatID, err)
		}
	}
	teamThreadID := "97979797979797979797979797979797"
	teamChatID := "98989898989898989898989898989898"
	teamSourceDirectory := t.TempDir()
	teamWorkspace, err := chatService.storage.TeamWorkspace(teamThreadID)
	if err != nil {
		t.Fatalf("TeamWorkspace() error = %v", err)
	}
	if err := workspace.EnsureThreadExecutionWorkspace(
		teamWorkspace,
		teamThreadID,
		"waker-target",
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() error = %v", err)
	}
	teamMemberWorkspace, err := chatService.storage.TeamMemberWorkspace(
		teamThreadID,
		"waker-target",
		true,
	)
	if err != nil {
		t.Fatalf("TeamMemberWorkspace() error = %v", err)
	}
	if err := workspace.EnsureThreadMemberExecutionWorkspace(
		teamMemberWorkspace,
		teamThreadID,
		teamChatID,
		"waker-target",
	); err != nil {
		t.Fatalf("EnsureThreadMemberExecutionWorkspace() error = %v", err)
	}
	if _, err := logStore.Create(teamChatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{
			WakerID:                      "waker-target",
			WorkspaceDirectory:           teamMemberWorkspace,
			AdditionalWorkingDirectories: []string{teamSourceDirectory},
			Title:                        "historical team chat",
			ThreadWorkspaceID:            teamThreadID,
		}),
	}}); err != nil {
		t.Fatalf("Create(team chat) error = %v", err)
	}

	if err := chatService.DeleteWakerChats(ctx, "waker-target", func([]string) (func() error, error) { return nil, nil }, func(context.Context) error { return nil }); !errors.Is(err, status_error.WakerHasRunningWork) {
		t.Fatalf("DeleteWakerChats() error = %v, want WakerHasRunningWork", err)
	}
	if _, err := logStore.ReadAll(targetID); err != nil {
		t.Fatalf("active authority was removed: %v", err)
	}
	if _, err := logStore.Append(targetID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeExecutionCompleted,
		ExecutionID: executionID,
		Data: marshalData(eventlog.ExecutionFinishedData{
			Status: constants.ExecutionStatusCompleted,
		}),
	}}); err != nil {
		t.Fatalf("complete authority execution: %v", err)
	}
	if err := chatService.DeleteWakerChats(ctx, "waker-target", func([]string) (func() error, error) { return nil, nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("DeleteWakerChats() error = %v", err)
	}
	if _, err := logStore.ReadAll(targetID); err == nil {
		t.Fatal("target authority still exists")
	}
	if _, err := logStore.ReadAll(otherID); err != nil {
		t.Fatalf("other waker authority was removed: %v", err)
	}
	if _, err := logStore.ReadAll(teamChatID); err != nil {
		t.Fatalf("historical team authority was removed: %v", err)
	}
}

func newDeleteTestService(
	t *testing.T,
) (*gorm.DB, *service, *conversationJournal) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "delete-test.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	logStore := newTestConversationJournal(t, t.TempDir())
	chatService := newTestService(t, Config{
		StorageDirectory: logStore.Root(),
		Conversations:    logStore.Store,
		QueryIndexes: QueryIndexDependencies{
			Chats:           controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions: controllers.NewTransactionController(
				dbhelper.NewGormTransactionManager(database.DB),
			),
		},
	})
	return database.DB, chatService, logStore
}

func TestStartRemovesDeletionTombstoneWithoutManifest(t *testing.T) {
	ctx := context.Background()
	db, chatService, _ := newDeleteTestService(t)
	controller := controllers.NewChatDeletionIntentController(db)
	intentID := "61616161616161616161616161616161"
	if err := controller.CreatePrepared(ctx, models.ChatDeletionIntent{
		ID:      intentID,
		ChatIDs: []string{"62626262626262626262626262626262"},
	}); err != nil {
		t.Fatalf("create orphaned deletion tombstone: %v", err)
	}
	if err := chatService.Start(ctx); err != nil {
		t.Fatalf("start chat service: %v", err)
	}
	if _, exists, err := controller.Get(ctx, intentID); err != nil || exists {
		t.Fatalf("orphaned deletion tombstone remains: exists=%t error=%v", exists, err)
	}
}

func TestDeleteChatsDeduplicatesThreadWorkspace(t *testing.T) {
	t.Parallel()
	t.Run("complete", func(t *testing.T) {
		testDeleteThreadWorkspace(t, false)
	})
	t.Run("missing-member-log", func(t *testing.T) {
		testDeleteThreadWorkspace(t, true)
	})
}

func testDeleteThreadWorkspace(t *testing.T, missingLog bool) {
	t.Helper()
	ctx := context.Background()
	db, chatService, _ := newDeleteTestService(t)
	chatController := controllers.NewChatController(db)

	threadID := "71717171717171717171717171717171"
	leadChatID := "72727272727272727272727272727272"
	memberChatID := "73737373737373737373737373737373"
	workingDirectory := t.TempDir()
	threadWorkspace, err := chatService.storage.TeamWorkspace(threadID)
	if err != nil {
		t.Fatalf("TeamWorkspace() error = %v", err)
	}
	if err := workspace.EnsureThreadExecutionWorkspace(
		threadWorkspace,
		threadID,
		"waker-lead",
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() error = %v", err)
	}
	for _, input := range []struct {
		// chatID 标识待删除的团队成员 Chat。
		chatID string
		// wakerID 标识拥有该成员 Chat 的伙伴。
		wakerID string
		// title 保存成员 Chat 的测试标题。
		title string
	}{
		{chatID: leadChatID, wakerID: "waker-lead", title: "lead"},
		{chatID: memberChatID, wakerID: "waker-member", title: "member"},
	} {
		memberWorkspace, err := chatService.storage.TeamMemberWorkspace(
			threadID,
			input.wakerID,
			input.wakerID == "waker-lead",
		)
		if err != nil {
			t.Fatalf("TeamMemberWorkspace() error = %v", err)
		}
		if err := workspace.EnsureThreadMemberExecutionWorkspace(
			memberWorkspace,
			threadID,
			input.chatID,
			input.wakerID,
		); err != nil {
			t.Fatalf("EnsureThreadMemberExecutionWorkspace() error = %v", err)
		}
		records, err := chatService.logStore.Create(input.chatID, []eventlog.AppendInput{{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:                      input.wakerID,
				ProjectName:                  "team",
				WorkspaceDirectory:           memberWorkspace,
				AdditionalWorkingDirectories: []string{workingDirectory},
				Title:                        input.title,
				ThreadWorkspaceID:            threadID,
			}),
		}})
		if err != nil {
			t.Fatalf("Create() log error = %v", err)
		}
		if err := chatController.Create(ctx, models.Chat{
			ID:                           input.chatID,
			WakerID:                      input.wakerID,
			ProjectName:                  "team",
			WorkspaceDirectory:           memberWorkspace,
			AdditionalWorkingDirectories: []string{workingDirectory},
			Title:                        input.title,
			LatestSequence:               records[0].Sequence,
		}); err != nil {
			t.Fatalf("Create() chat error = %v", err)
		}
	}
	if missingLog {
		path, err := chatService.logStore.LogPath(memberChatID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if !missingLog {
		if err := chatService.DeleteChat(ctx, leadChatID); !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("partial team deletion must be rejected: %v", err)
		}
	}
	if err := chatService.DeleteChats(ctx, []string{leadChatID, memberChatID}); err != nil {
		t.Fatalf("DeleteChats() error = %v", err)
	}
	if _, err := os.Stat(threadWorkspace); !os.IsNotExist(err) {
		t.Fatalf("thread workspace still exists after delete: %v", err)
	}
}

func TestDeletingOneChatPreservesSharedAgentAndOtherPrivateFiles(t *testing.T) {
	_, m, _ := newDeleteTestService(t)
	cwd, err := m.storage.AgentWorkspace("shared-waker")
	if err != nil {
		t.Fatal(err)
	}
	if err = workspace.EnsureAgentWorkspace(cwd, "shared-waker"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"chat-a", "chat-b"} {
		if _, err = m.logStore.Create(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "shared-waker", WorkspaceDirectory: cwd, Title: id})}}); err != nil {
			t.Fatal(err)
		}
		private, _ := m.storage.ChatRoot(id)
		if err = os.MkdirAll(filepath.Join(private, "attachments"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(private, "attachments", "keep.txt"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(cwd, "result.txt"), []byte("persistent"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteChat(context.Background(), "chat-a"); err != nil {
		t.Fatal(err)
	}
	first, _ := m.storage.ChatRoot("chat-a")
	if _, err = os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("deleted Chat remains: %v", err)
	}
	second, _ := m.storage.ChatRoot("chat-b")
	for _, file := range []string{filepath.Join(cwd, "result.txt"), filepath.Join(second, "attachments", "keep.txt")} {
		if _, err = os.Stat(file); err != nil {
			t.Fatalf("unrelated file removed %s: %v", file, err)
		}
	}
}

func TestDeleteProvisionalTeamWithoutRuntimeRootRemovesExactPrivateChats(t *testing.T) {
	_, m, _ := newDeleteTestService(t)
	for _, id := range []string{"planned", "unrelated"} {
		thread := "other-thread"
		if id == "planned" {
			thread = "planned-thread"
		}
		cwd, _ := m.storage.TeamMemberWorkspace(thread, "waker", true)
		if _, err := m.logStore.Create(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker", WorkspaceDirectory: cwd, ThreadWorkspaceID: thread, Title: id})}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.DeleteProvisionalTeam(context.Background(), "planned-thread", "provision", []string{"planned"}); err != nil {
		t.Fatal(err)
	}
	private, _ := m.storage.ChatRoot("planned")
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("private Chat remains after Team rollback: %v", err)
	}
	if _, err := m.logStore.ReadAll("unrelated"); err != nil {
		t.Fatal("unrelated private Chat removed", err)
	}
}

func TestWakerDeletionBlocksAdmissionUntilItsFilesAreSettled(t *testing.T) {
	_, m, _ := newDeleteTestService(t)
	settled := false
	err := m.DeleteWakerChats(context.Background(), "waker", func([]string) (func() error, error) {
		return func() error {
			settled = true
			release, err := chatstorage.AcquireAdmission(m.storage.Root(), "waker", "new-chat")
			if release != nil {
				release()
			}
			if !errors.Is(err, status_error.ChatHasActiveExecution) {
				t.Fatalf("admission overlapped rollback: %v", err)
			}
			return status_error.ManagedWorkspaceError
		}, nil
	}, func(context.Context) error { return status_error.DatabaseError })
	if !settled || !errors.Is(err, status_error.DatabaseError) || !errors.Is(err, status_error.ManagedWorkspaceError) {
		t.Fatalf("lost deletion or rollback outcome: settled=%v err=%v", settled, err)
	}
	release, err := chatstorage.AcquireAdmission(m.storage.Root(), "waker", "new-chat")
	if err != nil {
		t.Fatal("writer not released after settlement", err)
	}
	release()
}

func TestDeleteChatProjectEntriesRetainOtherChatsAndRollbackWithAuthority(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			ctx := t.Context()
			_, chats, logs := newDeleteTestService(t)
			root, err := chats.storage.AgentWorkspace("entry-waker")
			if err != nil {
				t.Fatal(err)
			}
			if err := workspace.EnsureAgentWorkspace(root, "entry-waker"); err != nil {
				t.Fatal(err)
			}
			chatIDs := []string{"81818181818181818181818181818181", "82828282828282828282828282828282"}
			projects := []string{t.TempDir(), t.TempDir()}
			for index, id := range chatIDs {
				if _, _, err := workspace.PrepareProjectEntries(root, id, []workspace.ProjectLink{{EntryName: "same", SourcePath: projects[index]}}); err != nil {
					t.Fatal(err)
				}
				if _, err := logs.Create(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "entry-waker", WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{projects[index]}, Title: "entry"})}}); err != nil {
					t.Fatal(err)
				}
				if err := chats.queryIndexes.chatController.Create(ctx, models.Chat{ID: id, WakerID: "entry-waker", WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{projects[index]}, Title: "entry"}); err != nil {
					t.Fatal(err)
				}
			}
			if rollback {
				err = chats.DeleteChatsAtomically(ctx, []string{chatIDs[0]}, func(context.Context) error {
					if _, err := os.Lstat(filepath.Join(root, "projects", chatIDs[0])); !os.IsNotExist(err) {
						t.Fatalf("namespace was not staged before DB mutation: %v", err)
					}
					return status_error.DatabaseError
				})
			} else {
				err = chats.DeleteChat(ctx, chatIDs[0])
			}
			if rollback {
				if !errors.Is(err, status_error.DatabaseError) {
					t.Fatalf("delete error=%v", err)
				}
				if _, err := logs.ReadAll(chatIDs[0]); err != nil {
					t.Fatalf("authority rollback failed: %v", err)
				}
				if _, err := os.Readlink(filepath.Join(root, "projects", chatIDs[0], "same")); err != nil {
					t.Fatalf("project entry rollback failed: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(filepath.Join(root, "projects", chatIDs[0])); !os.IsNotExist(err) {
					t.Fatalf("deleted Chat entry namespace survived: %v", err)
				}
			}
			if _, err := os.Readlink(filepath.Join(root, "projects", chatIDs[1], "same")); err != nil {
				t.Fatalf("other Chat entry changed: %v", err)
			}
			for _, project := range projects {
				if _, err := os.Stat(project); err != nil {
					t.Fatalf("real project was removed: %v", err)
				}
			}
		})
	}
}

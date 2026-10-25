package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func TestStartIsolatesCorruptChatLog(t *testing.T) {
	t.Parallel()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	logPath, err := logStore.LogPath(chatID)
	if err != nil {
		t.Fatalf("LogPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(
		logPath,
		[]byte("not-json\n"),
		0o600,
	); err != nil {
		t.Fatalf("write corrupt log: %v", err)
	}
	_, manager := newRecoveryTestService(t, logStore.Root())
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	health := manager.RecoveryHealth()
	if health.Isolated != 1 || len(health.Chats) != 1 ||
		health.Chats[0].State != constants.ChatRecoveryStateIsolated {
		t.Fatalf("RecoveryHealth() = %+v", health)
	}
}

func TestStartRebuildsMissingCurrentProjectionWithoutHistoricalRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "71717171717171717171717171717171"
	executionID := "72727272727272727272727272727272"
	records, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: recoveryTestData(t, eventlog.ChatCreatedData{
				WakerID:            "73737373737373737373737373737373",
				WorkspaceDirectory: t.TempDir(),
				Title:              "重建当前投影",
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: executionID,
			Data: recoveryTestData(t, eventlog.ExecutionCreatedData{
				RuntimeID: "codex",
				Model:     "gpt-5",
			}),
		},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID},
		{
			Type:        constants.ChatEventTypeExecutionCompleted,
			ExecutionID: executionID,
			Data: recoveryTestData(t, eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusCompleted,
			}),
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	database, manager := newRecoveryTestService(t, logStore.Root())
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	chat, err := controllers.NewChatController(database).Get(ctx, chatID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if chat.LatestSequence != records[len(records)-1].Sequence ||
		chat.ActiveExecutionID != "" || chat.LastRuntimeID != "codex" {
		t.Fatalf("rebuilt Chat = %+v", chat)
	}
	assertTableAbsent(t, database, "message_offsets")
	assertTableAbsent(t, database, "executions")
	health := manager.RecoveryHealth()
	if health.Missing != 1 || health.Rebuilt != 1 || health.Isolated != 0 {
		t.Fatalf("RecoveryHealth() = %+v", health)
	}
}

func TestStartReportsRebuiltDerivedFileIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "81818181818181818181818181818181"
	wakerID := "82828282828282828282828282828282"
	workspaceDirectory := t.TempDir()
	records, err := logStore.Create(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: recoveryTestData(t, eventlog.ChatCreatedData{
			WakerID:            wakerID,
			WorkspaceDirectory: workspaceDirectory,
			Title:              "索引恢复",
		}),
	}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	indexPath, err := logStore.IndexPath(chatID)
	if err != nil {
		t.Fatalf("IndexPath() error = %v", err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("Remove(index) error = %v", err)
	}
	database, manager := newRecoveryTestService(t, logStore.Root())
	if err := controllers.NewChatController(database).Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            wakerID,
		WorkspaceDirectory: workspaceDirectory,
		Title:              "索引恢复",
		LatestSequence:     records[0].Sequence,
		CreatedAt:          records[0].RecordedAt,
		UpdatedAt:          records[0].RecordedAt,
	}); err != nil {
		t.Fatalf("Create projection error = %v", err)
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	health := manager.RecoveryHealth()
	if health.Rebuilt != 1 || health.Repaired != 1 || health.Isolated != 0 ||
		len(health.Chats) != 1 ||
		health.Chats[0].State != constants.ChatRecoveryStateRepaired ||
		!slices.Contains(health.Chats[0].Issues, constants.ChatRecoveryIssueFileIndexRebuilt) {
		t.Fatalf("RecoveryHealth() = %+v", health)
	}
}

func TestStartReplaysOnlyLaggingCurrentTailAndCancelsInterruptedExecution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "11111111111111111111111111111111"
	executionID := "22222222222222222222222222222222"
	wakerID := "33333333333333333333333333333333"
	workingDirectory := t.TempDir()
	created, err := logStore.Create(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: recoveryTestData(t, eventlog.ChatCreatedData{
			WakerID:            wakerID,
			WorkspaceDirectory: workingDirectory,
			Title:              "恢复尾部",
		}),
	}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	database, manager := newRecoveryTestService(t, logStore.Root())
	chatController := controllers.NewChatController(database)
	if err := chatController.Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            wakerID,
		WorkspaceDirectory: workingDirectory,
		Title:              "恢复尾部",
		LatestSequence:     created[0].Sequence,
		CreatedAt:          created[0].RecordedAt,
		UpdatedAt:          created[0].RecordedAt,
	}); err != nil {
		t.Fatalf("Create projection error = %v", err)
	}
	appended, err := logStore.Append(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeTitleUpdated,
			Data: recoveryTestData(t, eventlog.ChatTitleUpdatedData{Title: "尾部新标题"}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: executionID,
			Data: recoveryTestData(t, eventlog.ExecutionCreatedData{
				RuntimeID: "claude",
				Model:     "sonnet",
			}),
		},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID},
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	recovered, err := logStore.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	terminal := recovered[len(recovered)-1]
	if terminal.Type != constants.ChatEventTypeExecutionCanceled ||
		terminal.Sequence != appended[len(appended)-1].Sequence+1 {
		t.Fatalf("terminal = %+v", terminal)
	}
	chat, err := chatController.Get(ctx, chatID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if chat.Title != "尾部新标题" || chat.ActiveExecutionID != "" ||
		chat.LatestSequence != terminal.Sequence || chat.LastRuntimeID != "claude" {
		t.Fatalf("recovered Chat = %+v", chat)
	}
	manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	afterSecondStart, err := logStore.ReadAll(chatID)
	if err != nil || len(afterSecondStart) != len(recovered) {
		t.Fatalf("second recovery records = %d, error = %v", len(afterSecondStart), err)
	}
}

func newRecoveryTestService(t *testing.T, logDirectory string) (*gorm.DB, *service) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "recovery.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("DB() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	manager := newTestService(t, Config{
		StorageDirectory: logDirectory,
		QueryIndexes: QueryIndexDependencies{
			Chats:           controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions: controllers.NewTransactionController(
				dbhelper.NewGormTransactionManager(database.DB),
			),
		},
	})
	t.Cleanup(manager.Close)
	return database.DB, manager
}

func recoveryTestData(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	data, err := eventlog.MarshalData(value)
	if err != nil {
		t.Fatalf("MarshalData() error = %v", err)
	}
	return data
}

func assertTableAbsent(t *testing.T, database *gorm.DB, table string) {
	t.Helper()
	if database.Migrator().HasTable(table) {
		t.Fatalf("table %q exists, want SQLite current projections only", table)
	}
}

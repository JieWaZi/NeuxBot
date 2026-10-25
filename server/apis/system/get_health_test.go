package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestGetHealthReportsDegradedWhenChatIsIsolated(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "health.db"),
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
	manager, err := chat.New(chat.Config{
		StorageDirectory: logStore.Root(),
		Projects:         controllers.NewProjectController(database.DB),
		QueryIndexes: chat.QueryIndexDependencies{
			Chats:           controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions: controllers.NewTransactionController(
				dbhelper.NewGormTransactionManager(database.DB),
			),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(manager.Close)
	previousManager := global.ChatService
	global.ChatService = manager
	t.Cleanup(func() { global.ChatService = previousManager })

	value, err := (&GetHealth{}).Output(nil)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	response, ok := value.(HealthResponse)
	if !ok {
		t.Fatalf("Output() type = %T, want HealthResponse", value)
	}
	if response.Status != "degraded" ||
		response.ChatIndexes.Isolated != 1 ||
		response.ChatIndexes.Corrupt != 1 {
		t.Fatalf("HealthResponse = %+v", response)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal HealthResponse: %v", err)
	}
	var responseFields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &responseFields); err != nil {
		t.Fatalf("unmarshal HealthResponse fields: %v", err)
	}
	indexesPayload, exists := responseFields["chatIndexes"]
	if !exists {
		t.Fatalf("HealthResponse JSON = %s, missing chatIndexes", payload)
	}
	if _, exists := responseFields["chatProjections"]; exists {
		t.Fatalf("HealthResponse JSON = %s, contains legacy chatProjections", payload)
	}
	var indexFields map[string]json.RawMessage
	if err := json.Unmarshal(indexesPayload, &indexFields); err != nil {
		t.Fatalf("unmarshal chatIndexes: %v", err)
	}
	var diagnostics []map[string]json.RawMessage
	if err := json.Unmarshal(indexFields["chats"], &diagnostics); err != nil {
		t.Fatalf("unmarshal chat diagnostics: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("chat diagnostics = %d, want 1", len(diagnostics))
	}
	if _, exists := diagnostics[0]["indexedSequence"]; !exists {
		t.Fatalf("RecoveryDiagnostic JSON = %s, missing indexedSequence", indexesPayload)
	}
	if _, exists := diagnostics[0]["projectedSequence"]; exists {
		t.Fatalf("RecoveryDiagnostic JSON = %s, contains legacy projectedSequence", indexesPayload)
	}
	var state string
	if err := json.Unmarshal(diagnostics[0]["state"], &state); err != nil {
		t.Fatalf("unmarshal recovery state: %v", err)
	}
	if state == "" {
		t.Fatalf("RecoveryDiagnostic JSON state must not be empty, got %q", state)
	}
	if state != "isolated" {
		t.Fatalf("RecoveryDiagnostic JSON state = %q, want isolated", state)
	}
}

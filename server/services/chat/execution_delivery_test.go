package chat

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestDeletedChatRejectsLateExecutionDelivery(t *testing.T) {
	ctx := t.Context()
	_, chats, _ := newDeleteTestService(t)
	const chatID = "725ab4696f4e49f08a94930de45a3d26"
	const executionID = "execution"
	cwd, err := chats.storage.AgentWorkspace("waker")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(cwd, "waker"); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{
		ID: chatID, WakerID: "waker", WorkspaceDirectory: cwd, AdditionalWorkingDirectories: []string{cwd},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.AppendExecutionFacts(ctx, chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	}); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(source, []byte("workflow-owned result"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened := 0
	delivery := ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{{
		ID: "report", Name: "report.txt", Open: func() (io.ReadCloser, error) {
			opened++
			return os.Open(source)
		},
	}}}
	if err := chats.PublishExecutionDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	directory, err := chats.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.DeleteChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	if err := chats.PublishExecutionDelivery(ctx, delivery); !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		t.Fatalf("late delivery did not fail at the deleted chat: %v", err)
	}
	if opened != 1 {
		t.Fatalf("late delivery reopened the workflow-owned source: opens=%d", opened)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("late delivery recreated the chat directory: %v", err)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "workflow-owned result" {
		t.Fatalf("chat deletion changed the source result: %q %v", content, err)
	}
}

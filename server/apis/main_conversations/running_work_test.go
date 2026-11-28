package main_conversations

import (
	"context"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestMainRunningWorkUsesOnlyConfirmedMainComposerExecutions(t *testing.T) {
	rows := []models.MainConversationActivity{
		{WorkChatID: "chat", ExecutionID: "running", InputMessageID: "running", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "pending", InputMessageID: "pending", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "finished", InputMessageID: "finished", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "missing", InputMessageID: "missing", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "recovering", InputMessageID: "recovering", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "removed-input", FromMain: true},
		{WorkChatID: "chat", ExecutionID: "source-execution", InputMessageID: "source-public-message"},
		{WorkChatID: "chat", ExecutionID: "no-public-input"},
		{WorkChatID: "chat", ExecutionID: "approval", InputMessageID: "approval", FromMain: true, WaitingApproval: true},
		{WorkChatID: "chat", ExecutionID: "question", InputMessageID: "question", FromMain: true, WaitingElicitation: true},
	}
	statuses := map[string]constants.ExecutionStatus{
		"running": constants.ExecutionStatusRunning, "pending": constants.ExecutionStatusPending, "finished": constants.ExecutionStatusCompleted,
		"removed-input": constants.ExecutionStatusRunning,
	}
	var reads []string
	err := markRunningMainWork(t.Context(), rows, func(_ context.Context, chatID, executionID string) (models.Execution, error) {
		if chatID != "chat" {
			t.Fatalf("wrong source: %s", chatID)
		}
		reads = append(reads, executionID)
		if executionID == "missing" {
			return models.Execution{}, status_error.ExecutionNotFound
		}
		if executionID == "recovering" {
			return models.Execution{}, status_error.ChatLogNativeOutcomePending
		}
		return models.Execution{ID: executionID, ChatID: chatID, Status: statuses[executionID]}, nil
	})
	if err != nil || !reflect.DeepEqual(reads, []string{"running", "pending", "finished", "missing", "recovering", "removed-input"}) {
		t.Fatalf("running work read identities=%v error=%v", reads, err)
	}
	for _, row := range rows {
		if row.Running != (row.ExecutionID == "running" || row.ExecutionID == "removed-input") {
			t.Errorf("wrong running classification: %+v", row)
		}
	}
}

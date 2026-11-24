package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestManagedResourceChangesPreserveDurableTimelinePositionOnRetry(t *testing.T) {
	t.Parallel()
	_, chat := newRecoveryTestService(t, t.TempDir())
	const chatID = "12121212121212121212121212121212"
	workspace := t.TempDir()
	if _, err := chat.CreateDormantChats(t.Context(), []DormantChat{{Chat: models.Chat{
		ID: chatID, WakerID: "waker", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
	}}}); err != nil {
		t.Fatal(err)
	}
	records, err := chat.AppendExecutionFacts(t.Context(), chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "request", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Revise a skill."})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "request"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 2 {
		if err := chat.RecordManagedSkillChange(ctx, chatID, "execution", "revise", "skill", "version", "Release notes"); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := chat.ReadManagedResourceChanges(t.Context(), chatID, 0)
	if err != nil || len(changes) != 1 || changes[0].Sequence != records[len(records)-1].Sequence+1 ||
		changes[0].OccurredAt == 0 || changes[0].Action != "revise" || changes[0].SkillID != "skill" ||
		changes[0].VersionID != "version" || changes[0].Name != "Release notes" {
		t.Fatalf("unexpected durable skill changes: %+v %v", changes, err)
	}
	events, err := chat.ListEvents(t.Context(), chatID, records[len(records)-1].Sequence, 1)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeManagedSkillChanged ||
		events[0].Sequence != changes[0].Sequence || events[0].Message != nil {
		t.Fatalf("unexpected skill change subscription event: %+v %v", events, err)
	}
	for range 2 {
		if err := chat.RecordManagedConnectorChange(ctx, chatID, "execution", "command", "create", "connector", "waker", "Test MCP"); err != nil {
			t.Fatal(err)
		}
	}
	changes, err = chat.ReadManagedResourceChanges(t.Context(), chatID, 0)
	if err != nil || len(changes) != 2 || changes[1].Sequence != changes[0].Sequence+1 ||
		changes[1].ConnectorID != "connector" || changes[1].OwnerWakerID != "waker" || changes[1].Name != "Test MCP" {
		t.Fatalf("unexpected durable connector changes: %+v %v", changes, err)
	}
	window, err := chat.ReadManagedResourceChanges(t.Context(), chatID, changes[1].Sequence)
	if err != nil || len(window) != 1 || window[0].ConnectorID != "connector" {
		t.Fatalf("unexpected resource changes in message window: %+v %v", window, err)
	}
	events, err = chat.ListEvents(t.Context(), chatID, changes[0].Sequence, 1)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeManagedConnectorChanged || events[0].Sequence != changes[1].Sequence {
		t.Fatalf("unexpected connector change subscription event: %+v %v", events, err)
	}
}

func TestReviewToolEvidenceUsesOnlyStructuredDiagnostics(t *testing.T) {
	msg := messageview.Message{DisplayParts: []messageview.MessageDisplayPart{
		{ID: "failure", ToolCallID: "native-1", ToolCategory: "command", ToolAction: "execute_command", ToolStatus: constants.MessageToolStatusFailed},
		{ID: "recovery", ToolCallID: "native-2", ToolCategory: "command", ToolAction: "run_tests", ToolStatus: constants.MessageToolStatusCompleted},
	}}
	parts := []agent.MessagePart{
		{ToolCallID: "native-1", Data: json.RawMessage(`{"toolName":"Bash","exitCode":127,"output":"private output","input":{"command":"private command"}}`)},
		{ToolCallID: "native-2", Data: json.RawMessage(`{"toolName":"RunTests"}`)},
	}
	steps := reviewToolSteps(msg, parts)
	if len(steps) != 2 || !steps[0].Failed || !strings.Contains(steps[0].Name, "Bash") || steps[0].Summary != "Exit code 127" || steps[1].Failed {
		t.Fatalf("unexpected safe tool evidence: %+v", steps)
	}
	body, err := json.Marshal(steps)
	if err != nil || strings.Contains(string(body), "private") {
		t.Fatalf("unsafe tool evidence: %s, %v", body, err)
	}
}

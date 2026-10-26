package execution

import (
	"context"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

func TestToolInputSnapshotsKeepLiveReplayEquivalent(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "10101010101010101010101010101010"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	var lastLive *messageview.Message
	for _, event := range []runtimes.Event{
		{Type: runtimes.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "read-1", "name": "read", "kind": "read"})},
		{Type: runtimes.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "read-1", "name": "read", "input": map[string]any{"path": "s"}})},
		{Type: runtimes.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "read-1", "name": "read", "input": map[string]any{"path": "src/main.go"}})},
		{Type: runtimes.EventTypeToolResult, Data: marshalData(map[string]any{"toolCallId": "read-1", "status": "completed"})},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
			t.Fatal(err)
		}
		lastLive = (<-live).Message
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lastLive.DisplayParts, replay[0].DisplayParts) {
		t.Fatalf("live path=%q label=%q vs replay path=%q label=%q", lastLive.DisplayParts[0].ToolSafeSummary.Path, lastLive.DisplayParts[0].ToolLabel, replay[0].DisplayParts[0].ToolSafeSummary.Path, replay[0].DisplayParts[0].ToolLabel)
	}
}
func TestToolInputActionKeepsLiveReplayEquivalent(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "12121212121212121212121212121212"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	var lastLive *messageview.Message
	for _, event := range []runtimes.Event{
		{Type: runtimes.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "shell-1"})},
		{Type: runtimes.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "shell-1", "name": "command", "input": map[string]any{"command": "g"}})},
		{Type: runtimes.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "shell-1", "name": "command", "input": map[string]any{"command": "go test ./x"}})},
		{Type: runtimes.EventTypeToolResult, Data: marshalData(map[string]any{"toolCallId": "shell-1", "status": "completed"})},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
			t.Fatal(err)
		}
		lastLive = (<-live).Message
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lastLive.DisplayParts, replay[0].DisplayParts) {
		t.Fatalf("live=%+v vs replay=%+v", lastLive.DisplayParts[0], replay[0].DisplayParts[0])
	}
}

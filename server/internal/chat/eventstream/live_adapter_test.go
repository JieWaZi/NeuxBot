package eventstream

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
)

func TestLiveAdapterAppliesRuntimeEventWithBoundMessageScope(t *testing.T) {
	t.Parallel()

	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatalf("NewLiveAdapter() error = %v", err)
	}
	payload, err := json.Marshal(map[string]string{"text": "继续"})
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}
	event, revision, err := adapter.Apply("invocation-2", runtimecontract.Event{
		Type: runtimecontract.EventTypeTextDelta,
		Data: payload,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !event.Transient || event.Type != constants.ChatEventTypeRuntimeEvent {
		t.Fatalf("live event = %#v", event)
	}
	if event.Message == nil ||
		event.Message.ID != "execution-1" ||
		event.Message.Revision != revision ||
		!strings.Contains(event.Message.Content, "继续") {
		t.Fatalf("live message snapshot = %#v, revision = %d", event.Message, revision)
	}
}

func TestNewLiveAdapterRejectsMissingTargetMessage(t *testing.T) {
	t.Parallel()

	if _, err := NewLiveAdapter(fixtureRecords(t), "missing-message"); err == nil {
		t.Fatal("NewLiveAdapter() error = nil, want missing target error")
	}
}

// TestLiveAdapterDiscardsPrivateRuntimeHistory 覆盖实时公开投影不会累积历史工具参数。
func TestLiveAdapterDiscardsPrivateRuntimeHistory(t *testing.T) {
	t.Parallel()
	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	for index := range 100 {
		payload, err := json.Marshal(map[string]any{
			"toolCallId": "write-1", "name": "Write",
			"input": map[string]any{"file_path": "/workspace/project/note.md", "content": strings.Repeat("private body", index+1)},
		})
		if err != nil {
			t.Fatal(err)
		}
		event, _, err := adapter.Apply("invocation-2", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: payload})
		if err != nil || event.Message == nil || len(event.Message.DisplayParts) == 0 {
			t.Fatalf("live update %d: event=%+v error=%v", index, event, err)
		}
	}
	for _, message := range adapter.messageView.Messages() {
		if len(message.RuntimeEvents) != 0 || len(message.FileChangeDiffs) != 0 {
			t.Fatalf("live projection retained private history: runtime events=%d diffs=%d", len(message.RuntimeEvents), len(message.FileChangeDiffs))
		}
	}
}

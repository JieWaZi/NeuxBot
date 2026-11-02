package eventstream

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
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
	event, revision, err := adapter.Apply("invocation-2", agentapi.Event{
		Type: agentapi.EventTypeTextDelta,
		Data: payload,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !event.Transient || event.Type != constants.ChatEventTypeRuntimeEvent {
		t.Fatalf("live event = %#v", event)
	}
	if event.Message != nil || event.Live == nil || event.Live.MessageID != "execution-1" || event.Live.Revision != revision || len(event.Live.Updates) != 1 || len(event.Live.Updates[0].Parts) != 1 || event.Live.Updates[0].Parts[0].Text != "继续" {
		t.Fatalf("live increment = %#v, revision = %d", event.Live, revision)
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
		event, _, err := adapter.Apply("invocation-2", agentapi.Event{Type: agentapi.EventTypeToolUse, Data: payload})
		if err != nil || event.Live == nil || len(event.Live.Updates) == 0 || len(event.Live.Updates[0].Parts) == 0 {
			t.Fatalf("live update %d: event=%+v error=%v", index, event, err)
		}
	}
	for _, message := range adapter.messageView.Messages() {
		if len(message.RuntimeEvents) != 0 || len(message.FileChangeDiffs) != 0 {
			t.Fatalf("live projection retained private history: runtime events=%d diffs=%d", len(message.RuntimeEvents), len(message.FileChangeDiffs))
		}
	}
}

func TestLiveIncrementFramesHaveIndependentGenerationAndContinuousCoverage(t *testing.T) {
	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	baseline := adapter.MessageRevision()
	for index := 0; index < 300; index++ {
		if _, err := adapter.ApplyState("live-invocation", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"后续"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	sequence := int64(0)
	generation := ""
	for adapter.PendingCount() > 0 {
		event, err := adapter.Drain()
		if err != nil {
			t.Fatal(err)
		}
		frame := event.Live
		sequence++
		if event.Message != nil || event.Sequence != 0 || frame == nil || frame.Sequence != sequence || frame.BaseRevision != baseline {
			t.Fatalf("invalid live frame: %+v", event)
		}
		if generation == "" {
			generation = frame.Generation
		} else if frame.Generation != generation {
			t.Fatal("generation changed within one adapter")
		}
		for _, update := range frame.Updates {
			if update.BaseRevision != baseline || update.Revision <= baseline {
				t.Fatalf("coverage gap: %+v after %d", update, baseline)
			}
			baseline = update.Revision
			for _, operation := range update.Parts {
				if operation.Kind == constants.MessageIncrementPartUpsert {
					t.Fatal("plain text rebuilt a completed tool")
				}
			}
		}
		if frame.Revision != baseline || len(frame.Updates) > 128 {
			t.Fatalf("invalid frame coverage: %+v", frame)
		}
	}
	rebuilt, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	if generation == "" || generation == rebuilt.generation {
		t.Fatal("rebuilt stream reused previous generation")
	}
}

func TestLiveIncrementSplitsLargeUTF8InputIntoBoundedFrames(t *testing.T) {
	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("中文", 40*1024)
	payload, _ := json.Marshal(map[string]string{"text": text})
	if _, err = adapter.ApplyState("large-invocation", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: payload}); err != nil {
		t.Fatal(err)
	}
	var rebuilt strings.Builder
	for adapter.PendingCount() > 0 {
		event, err := adapter.Drain()
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(event)
		if len(encoded) > 65*1024 {
			t.Fatalf("frame escaped payload budget: %d", len(encoded))
		}
		for _, update := range event.Live.Updates {
			for _, operation := range update.Parts {
				rebuilt.WriteString(operation.Text)
			}
		}
	}
	if rebuilt.String() != text {
		t.Fatal("UTF-8 split changed body")
	}
}

func TestLiveIncrementToolPayloadNeverExposesPrivateInputOrOutput(t *testing.T) {
	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []agentapi.EventType{agentapi.EventTypeToolUse, agentapi.EventTypeToolInputSnapshot, agentapi.EventTypeToolUpdate, agentapi.EventTypeToolResult} {
		data, _ := json.Marshal(map[string]any{"toolCallId": "private-tool", "name": "write_file", "status": "completed", "input": map[string]any{"file_path": "/workspace/project/safe.md", "content": "PRIVATE_BODY", "env": map[string]string{"TOKEN": "PRIVATE_SECRET"}}, "output": "PRIVATE_OUTPUT"})
		event, _, err := adapter.Apply("private-invocation", agentapi.Event{Type: kind, Data: data})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(event)
		for _, forbidden := range []string{"PRIVATE_BODY", "PRIVATE_SECRET", "PRIVATE_OUTPUT", "rawInput", "rawOutput"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("unsafe field in public stream: %s", forbidden)
			}
		}
	}
}

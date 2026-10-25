package eventstream

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
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
	event, revision, err := adapter.Apply("invocation-2", runtimes.Event{
		Type: runtimes.EventTypeTextDelta,
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

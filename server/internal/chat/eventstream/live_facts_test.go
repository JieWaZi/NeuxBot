package eventstream

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestLiveFactsKeepRevisionMonotonicAndRejectOtherScopes(t *testing.T) {
	adapter, err := NewLiveAdapter(fixtureRecords(t), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := json.Marshal(map[string]any{"toolCallId": "live-tool", "name": "read"})
	if _, err := adapter.ApplyState("new-invocation", agentapi.Event{Type: agentapi.EventTypeToolUse, Data: tool}); err != nil {
		t.Fatal(err)
	}
	data, _ := eventlog.MarshalData(eventlog.ApprovalResolvedData{ToolCallID: "live-tool", Result: constants.ApprovalResultSelected})
	fact := eventlog.Record{Sequence: 70, ExecutionID: "execution-1", InvocationID: "new-invocation", Type: constants.ChatEventTypeApprovalResolved, Data: data}
	if changed, err := adapter.ApplyFact(fact); err != nil || !changed {
		t.Fatalf("ApplyFact=%v,%v", changed, err)
	}
	approvedRevision := adapter.MessageRevision()
	if changed, err := adapter.ApplyFact(fact); err != nil || changed || adapter.MessageRevision() != approvedRevision {
		t.Fatalf("duplicate fact changed live revision: %v %v", changed, err)
	}
	revision, err := adapter.ApplyState("new-invocation", agentapi.Event{Type: agentapi.EventTypeToolInputSnapshot, Data: tool})
	if err != nil || revision <= approvedRevision {
		t.Fatalf("next Runtime revision=%d <= approved revision=%d, error=%v", revision, approvedRevision, err)
	}
	fact.ExecutionID = "another-execution"
	if _, err := adapter.ApplyFact(fact); err == nil {
		t.Fatal("live adapter accepted another execution's fact")
	}
	fact.ExecutionID = "execution-1"
	fact.Type = constants.ChatEventTypeAssistantMessage
	if _, err := adapter.ApplyFact(fact); err == nil {
		t.Fatal("live fact port accepted a Runtime message and could replay its content twice")
	}
}

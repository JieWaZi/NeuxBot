package runtimes

import (
	"encoding/json"
	"testing"
	"time"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestACPBridgePreservesExplicitEmptyToolCollections 验证清空集合是有效更新，不能按省略字段丢弃。
func TestACPBridgePreservesExplicitEmptyToolCollections(t *testing.T) {
	for _, field := range []string{"content", "locations"} {
		t.Run(field, func(t *testing.T) {
			var notification acp.SessionNotification
			if err := json.Unmarshal([]byte(`{"sessionId":"session","update":{"sessionUpdate":"tool_call_update","toolCallId":"tool","`+field+`":[]}}`), &notification); err != nil {
				t.Fatal(err)
			}
			bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, standardACPRuntimeBinding(), time.Second)
			events, err := bridge.mapNotification(notification)
			if err != nil || len(events) != 1 || events[0].Type != EventTypeToolUpdate {
				t.Fatalf("explicit collection clear was lost: events=%+v err=%v", events, err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload[field]) != "[]" {
				t.Fatalf("clear payload %s=%s, want []", field, payload[field])
			}
		})
	}
}

// TestACPToolCollectionClearSurvivesWireRoundTrip 验证内嵌 Agent 的类型化序列化不会把清空更新变成无操作。
func TestACPToolCollectionClearSurvivesWireRoundTrip(t *testing.T) {
	update := acp.SessionToolCallUpdate{SessionUpdate: "tool_call_update", ToolCallId: "tool", Content: []acp.ToolCallContent{}, Locations: []acp.ToolCallLocation{}}
	encoded, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["content"]) != "[]" || string(fields["locations"]) != "[]" {
		t.Fatalf("wire serialization dropped explicit clears: %s", encoded)
	}
}

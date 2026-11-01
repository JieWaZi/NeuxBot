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

func TestDurableApprovalRevisionDoesNotClaimUnpersistedTextCoverage(t *testing.T) {
	records := fixtureRecords(t)
	adapter, err := NewLiveAdapter(records, "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	before := adapter.MessageRevision()
	if _, err = adapter.ApplyState("uncommitted", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"pending"}`)}); err != nil {
		t.Fatal(err)
	}
	data, _ := eventlog.MarshalData(eventlog.ApprovalResolvedData{ToolCallID: "tool-command", Result: constants.ApprovalResultSelected})
	fact := eventlog.Record{Sequence: 100, ExecutionID: "execution-1", Type: constants.ChatEventTypeApprovalResolved, Data: data}
	// 交互不能声称未写入完整消息日志的正文已经被覆盖。
	durable, err := Replay(append(append([]eventlog.Record(nil), records...), fact))
	if err != nil {
		t.Fatal(err)
	}
	if got := durable.MessageRevisions()["execution-1"]; got != before {
		t.Fatalf("approval advanced body coverage %d -> %d", before, got)
	}
	if _, err = adapter.ApplyFact(fact); err != nil {
		t.Fatal(err)
	}
	frame, err := adapter.Drain()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Live.BaseRevision != before || frame.Live.Updates[0].Parts[0].Text != "pending" {
		t.Fatalf("lost pending prefix: %+v", frame.Live)
	}
	// 初始化在已提交审批之后时，第一条正文仍以相同的正文覆盖版本为起点。
	recovered, err := NewLiveAdapter(append(append([]eventlog.Record(nil), records...), fact), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := recovered.Apply("after-approval", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"new"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if event.Live.BaseRevision != before {
		t.Fatalf("metadata revision created false live gap: %+v", event.Live)
	}
}

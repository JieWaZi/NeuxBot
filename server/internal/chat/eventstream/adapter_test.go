package eventstream

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// TestReplayAfterRestoresPrefixWithoutKeepingSnapshots 验证游标前恢复状态，但只为游标后事实保留公开事件。
func TestReplayAfterRestoresPrefixWithoutKeepingSnapshots(t *testing.T) {
	records := fixtureRecords(t)
	full, err := Replay(records)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ReplayAfter(records, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.events) != 1 {
		t.Fatalf("retained %d events, want only one event after cursor", len(restored.events))
	}
	if got, want := restored.EventsAfter(4, 0), full.EventsAfter(4, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored tail = %+v, want %+v", got, want)
	}
}

// TestReplayAfterKeepsHighestObservedRevision 保留审批更新后 Runtime 修订号暂时落后时的去重基线。
func TestReplayAfterKeepsHighestObservedRevision(t *testing.T) {
	records := fixtureRecords(t)
	approval, err := eventlog.MarshalData(eventlog.ApprovalResolvedData{ToolCallID: "tool-command", Result: constants.ApprovalResultSelected})
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, eventlog.Record{Sequence: 6, ExecutionID: "execution-1", Type: constants.ChatEventTypeApprovalResolved, Data: approval})
	late := executionMessageRecord(t, 7, constants.ChatEventTypeAssistantMessage, "text_delta", map[string]any{"text": "继续"})
	var data eventlog.RuntimeMessageData
	if err := json.Unmarshal(late.Data, &data); err != nil {
		t.Fatal(err)
	}
	data.MessageRevision = 3
	late.Data, err = eventlog.MarshalData(data)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, late)
	adapter, err := ReplayAfter(records, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := adapter.MessageRevisions()["execution-1"]; got != 6 {
		t.Fatalf("revision baseline = %d, want highest observed revision 6", got)
	}
}

// TestReplayAfterKeepsInteractionTerminalAndRevisionSemantics 验证每个游标边界都保留审批、问答和终态信封及最终修订号。
func TestReplayAfterKeepsInteractionTerminalAndRevisionSemantics(t *testing.T) {
	records := fixtureRecords(t)
	appendRecord := func(kind constants.ChatEventType, value any) {
		data, err := eventlog.MarshalData(value)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, eventlog.Record{Sequence: int64(len(records) + 1), Type: kind, ExecutionID: "execution-1", Data: data})
	}
	appendRecord(constants.ChatEventTypeApprovalRequested, eventlog.ApprovalRequestedData{ApprovalID: "approval", ToolCallID: "tool-command"})
	appendRecord(constants.ChatEventTypeApprovalResolved, eventlog.ApprovalResolvedData{ApprovalID: "approval", ToolCallID: "tool-command", Result: constants.ApprovalResultSelected})
	appendRecord(constants.ChatEventTypeElicitationRequested, eventlog.ElicitationRequestedData{ElicitationID: "question", Mode: "form", Message: "确认参数"})
	appendRecord(constants.ChatEventTypeElicitationResolved, eventlog.ElicitationResolvedData{ElicitationID: "question", Result: constants.ElicitationResultAccepted})
	appendRecord(constants.ChatEventTypeExecutionCompleted, eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	full, err := Replay(records)
	if err != nil {
		t.Fatal(err)
	}
	for after := int64(0); after <= int64(len(records)); after++ {
		restored, err := ReplayAfter(records, after)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := restored.EventsAfter(after, 0), full.EventsAfter(after, 0); !reflect.DeepEqual(got, want) {
			t.Fatalf("cursor %d event envelopes changed", after)
		}
		if got, want := restored.MessageRevisions(), full.MessageRevisions(); !reflect.DeepEqual(got, want) {
			t.Fatalf("cursor %d revision baseline = %v, want %v", after, got, want)
		}
	}
}

// TestReleaseHistoryKeepsOnlyCurrentStreamState 验证已交付快照释放后仍能生成完整增量消息，且不会重新累积历史。
func TestReleaseHistoryKeepsOnlyCurrentStreamState(t *testing.T) {
	records := fixtureRecords(t)
	adapter, err := ReplayAfter(records, 4)
	if err != nil {
		t.Fatal(err)
	}
	delivered := adapter.EventsAfter(4, 0)
	adapter.ReleaseHistory()
	for index := 0; index < 8; index++ {
		record := executionMessageRecord(t, int64(6+index), constants.ChatEventTypeAssistantMessage, "text_delta", map[string]any{"text": "追加"})
		event, err := adapter.Apply(record)
		if err != nil || event.Message == nil || !strings.HasSuffix(event.Message.Content, strings.Repeat("追加", index+1)) {
			t.Fatalf("stream message = %+v, error %v", event.Message, err)
		}
		if len(adapter.events) != 0 {
			t.Fatalf("stream retained %d delivered events", len(adapter.events))
		}
	}
	if delivered[0].Message.Content != "最终正文" {
		t.Fatal("previously delivered snapshot changed during live streaming")
	}
}

// TestReplayAfterStillValidatesPrefixEnvelopes 验证静默恢复前缀没有跳过公开事件载荷校验。
func TestReplayAfterStillValidatesPrefixEnvelopes(t *testing.T) {
	for _, kind := range []constants.ChatEventType{constants.ChatEventTypeApprovalRequested, constants.ChatEventTypeApprovalResolved, constants.ChatEventTypeElicitationRequested, constants.ChatEventTypeElicitationResolved, constants.ChatEventTypeExecutionCompleted} {
		if _, err := ReplayAfter([]eventlog.Record{{Sequence: 1, Type: kind, Data: json.RawMessage(`[`)}}, 1); err == nil {
			t.Fatalf("accepted malformed prefix event %s", kind)
		}
	}
}

// BenchmarkReplayAfterLatest 覆盖重连时仅恢复当前消息状态的时间及分配成本。
func BenchmarkReplayAfterLatest(b *testing.B) {
	for _, count := range []int{128, 512} {
		b.Run(fmt.Sprintf("tools=%d", count), func(b *testing.B) {
			records := cursorToolRecords(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := ReplayAfter(records, records[len(records)-1].Sequence); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func cursorToolRecords(t testing.TB, count int) []eventlog.Record {
	t.Helper()
	encode := func(value any) json.RawMessage {
		data, err := eventlog.MarshalData(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	records := []eventlog.Record{{Sequence: 1, Type: constants.ChatEventTypeCreated, Data: encode(eventlog.ChatCreatedData{WorkspaceDirectory: "/workspace"})}, {
		Sequence: 2, Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "active", Data: encode(eventlog.ExecutionCreatedData{}),
	}}
	for index := 0; index < count; index++ {
		sequence := int64(index + 3)
		records = append(records, eventlog.Record{
			Sequence: sequence, Type: constants.ChatEventTypeAssistantMessage, MessageID: "active", ExecutionID: "active",
			Data: encode(eventlog.RuntimeMessageData{MessageRevision: sequence, Content: []eventlog.RuntimeMessageContent{{Type: "tool_use", Data: encode(map[string]any{
				"toolCallId": fmt.Sprintf("tool-%d", index), "name": "read_file", "path": "/workspace/main.go",
			})}}}),
		})
	}
	return records
}

func TestAdapterAppliesRecordsIncrementallyAndKeepsStableMessageRevision(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter()
	records := fixtureRecords(t)
	for index, record := range records {
		event, err := adapter.Apply(record)
		if err != nil {
			t.Fatalf("Apply(record %d) error = %v", index, err)
		}
		if event.Sequence != record.Sequence {
			t.Fatalf("event.Sequence = %d, want %d", event.Sequence, record.Sequence)
		}
		if event.Message != nil && event.Message.Revision != record.Sequence {
			t.Fatalf("event.Message.Revision = %d, want %d", event.Message.Revision, record.Sequence)
		}
	}
	if events := adapter.EventsAfter(1, 1); len(events) != 1 ||
		events[0].Message == nil || events[0].Message.ExecutionID != "execution-1" {
		t.Fatalf("execution-created event = %#v, want assistant execution snapshot", events)
	}

	events := adapter.EventsAfter(2, 2)
	if len(events) != 2 || events[0].Sequence != 3 || events[1].Sequence != 4 {
		t.Fatalf("EventsAfter(2, 2) = %#v, want sequences 3 and 4", events)
	}
	if events[0].Message == nil || len(events[0].Message.DisplayParts) != 1 {
		t.Fatalf("tool-use message snapshot = %#v", events[0].Message)
	}
	partID := events[0].Message.DisplayParts[0].ID
	if partID == "" || events[1].Message == nil || events[1].Message.DisplayParts[0].ID != partID {
		t.Fatalf("tool part id did not remain stable across snapshots: %#v %#v", events[0].Message, events[1].Message)
	}
}

func TestAdapterPublicEventsExcludeRuntimePayloadAndTechnicalExitCode(t *testing.T) {
	t.Parallel()

	adapter, err := Replay(fixtureRecords(t))
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	encoded, err := json.Marshal(adapter.EventsAfter(0, 0))
	if err != nil {
		t.Fatalf("Marshal(events) error = %v", err)
	}
	payload := string(encoded)
	var envelopes []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelopes); err != nil {
		t.Fatalf("Unmarshal(events) error = %v", err)
	}
	for index, envelope := range envelopes {
		for _, key := range []string{
			"chatId",
			"messageId",
			"executionId",
			"invocationId",
			"runtimeEventType",
		} {
			if _, exists := envelope[key]; exists {
				t.Fatalf("public event %d exposed redundant envelope field %q", index, key)
			}
		}
	}
	for _, forbidden := range []string{
		"private command output",
		"exitCode",
		"resultCount",
		"\"data\"",
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("public events leaked %q in %s", forbidden, payload)
		}
	}
	for _, required := range []string{
		"\"type\":\"assistant.message\"",
		"\"toolStatus\":\"failed\"",
		"\"text\":\"最终正文\"",
	} {
		if !strings.Contains(payload, required) {
			t.Fatalf("public events missing %q in %s", required, payload)
		}
	}
}

func TestAdapterTerminalEventIncludesCompleteMessageSnapshot(t *testing.T) {
	t.Parallel()

	records := fixtureRecords(t)
	finished, err := eventlog.MarshalData(eventlog.ExecutionFinishedData{
		Status:       constants.ExecutionStatusFailed,
		ErrorCode:    "runtime_failed",
		ErrorMessage: "执行失败",
	})
	if err != nil {
		t.Fatalf("MarshalData(finished) error = %v", err)
	}
	records = append(records, eventlog.Record{
		SchemaVersion: 1,
		ChatID:        "chat-1",
		Sequence:      6,
		RecordedAt:    600,
		Type:          constants.ChatEventTypeExecutionFailed,
		ExecutionID:   "execution-1",
		Data:          finished,
	})

	adapter, err := Replay(records)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	events := adapter.EventsAfter(5, 0)
	if len(events) != 1 {
		t.Fatalf("terminal events = %d, want 1", len(events))
	}
	message := events[0].Message
	if message == nil ||
		message.ID != "execution-1" ||
		message.ExecutionID != "execution-1" ||
		message.Status != constants.ExecutionStatusFailed ||
		message.ErrorCode != "runtime_failed" ||
		message.ErrorMessage != "执行失败" {
		t.Fatalf("terminal message snapshot = %#v", message)
	}
}

func TestAdapterPublishesOnlySafeApprovalFields(t *testing.T) {
	t.Parallel()
	line := 0
	data, err := eventlog.MarshalData(eventlog.ApprovalRequestedData{
		ApprovalID: "approval-1",
		ToolCallID: "tool-1",
		Title:      "读取文件",
		Kind:       "read",
		Command:    "curl https://chatgpt.com/",
		Locations: []eventlog.ApprovalLocationData{{
			Path: "/workspace/main.go",
			Line: &line,
		}},
		Options: []eventlog.ApprovalOptionData{{
			ID: "allow-once", Name: "Allow once", Kind: "allow_once",
		}},
	})
	if err != nil {
		t.Fatalf("MarshalData(approval) error = %v", err)
	}
	event, err := NewAdapter().Apply(eventlog.Record{
		SchemaVersion: 1,
		ChatID:        "conversation-1",
		Sequence:      1,
		RecordedAt:    100,
		Type:          constants.ChatEventTypeApprovalRequested,
		ExecutionID:   "execution-1",
		Data:          data,
	})
	if err != nil || event.ApprovalRequest == nil ||
		event.ApprovalRequest.ApprovalID != "approval-1" ||
		event.ApprovalRequest.Command != "curl https://chatgpt.com/" ||
		event.ApprovalRequest.Locations[0].Line == nil ||
		*event.ApprovalRequest.Locations[0].Line != 0 {
		t.Fatalf("approval event = %#v, %v", event, err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal(event) error = %v", err)
	}
	for _, forbidden := range []string{"rawInput", "requestId", "headers", "environment"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("approval event leaked %q in %s", forbidden, encoded)
		}
	}
}

func fixtureRecords(t *testing.T) []eventlog.Record {
	t.Helper()
	return []eventlog.Record{
		messageRecord(t, 1),
		executionRecord(t, 2),
		executionMessageRecord(t, 3, constants.ChatEventTypeAssistantMessage, "tool_use", map[string]any{
			"id":      "tool-command",
			"name":    "Bash",
			"command": "cat /workspace/project/secret.txt",
		}),
		executionMessageRecord(t, 4, constants.ChatEventTypeToolMessage, "tool_result", map[string]any{
			"tool_use_id": "tool-command",
			"exit_code":   7,
			"output":      "private command output",
		}),
		executionMessageRecord(t, 5, constants.ChatEventTypeAssistantMessage, "text_delta", map[string]any{"text": "最终正文"}),
	}
}

func messageRecord(t *testing.T, sequence int64) eventlog.Record {
	t.Helper()
	data, err := eventlog.MarshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser})
	if err != nil {
		t.Fatalf("MarshalData(message) error = %v", err)
	}
	return eventlog.Record{
		SchemaVersion: 1,
		ChatID:        "chat-1",
		Sequence:      sequence,
		RecordedAt:    sequence * 100,
		Type:          constants.ChatEventTypeUserMessageCreated,
		MessageID:     "user-1",
		Data:          data,
	}
}

func executionRecord(t *testing.T, sequence int64) eventlog.Record {
	t.Helper()
	data, err := eventlog.MarshalData(eventlog.ExecutionCreatedData{
		UserMessageID: "user-1",
	})
	if err != nil {
		t.Fatalf("MarshalData(execution) error = %v", err)
	}
	return eventlog.Record{
		SchemaVersion: 1,
		ChatID:        "chat-1",
		Sequence:      sequence,
		RecordedAt:    sequence * 100,
		Type:          constants.ChatEventTypeExecutionCreated,
		MessageID:     "execution-1",
		ExecutionID:   "execution-1",
		Data:          data,
	}
}

func executionMessageRecord(
	t *testing.T,
	sequence int64,
	recordType constants.ChatEventType,
	contentType constants.RuntimeEventType,
	payload map[string]any,
) eventlog.Record {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}
	data, err := eventlog.MarshalData(eventlog.RuntimeMessageData{
		Content: []eventlog.RuntimeMessageContent{{
			Type: contentType,
			Data: rawPayload,
		}},
		MessageRevision: sequence,
	})
	if err != nil {
		t.Fatalf("MarshalData(runtime) error = %v", err)
	}
	return eventlog.Record{
		SchemaVersion: 1,
		ChatID:        "chat-1",
		Sequence:      sequence,
		RecordedAt:    sequence * 100,
		Type:          recordType,
		MessageID:     "execution-1",
		ExecutionID:   "execution-1",
		InvocationID:  "invocation-1",
		Data:          data,
	}
}

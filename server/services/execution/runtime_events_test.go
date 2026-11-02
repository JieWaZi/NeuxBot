package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"

	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestRuntimeEventSinkPersistsOnlyCompleteAssistantMessage(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	for _, text := range []string{"one", "two"} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{
			Type: agentapi.EventTypeTextDelta,
			Data: marshalData(struct {
				// Text 是本次测试交付的实时文本增量。
				Text string `json:"text"`
			}{Text: text}),
		}); err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
	}
	beforeFlush, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() before flush error = %v", err)
	}
	if len(beforeFlush) != 3 {
		t.Fatalf("records before flush = %d, want 3", len(beforeFlush))
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	afterFlush, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() after flush error = %v", err)
	}
	if len(afterFlush) != 4 || afterFlush[3].Type != constants.ChatEventTypeAssistantMessage {
		t.Fatalf("records after flush = %+v", afterFlush)
	}
	var persisted eventlog.RuntimeMessageData
	if err := json.Unmarshal(afterFlush[3].Data, &persisted); err != nil {
		t.Fatalf("decode persisted execution message: %v", err)
	}
	if len(persisted.Content) != 1 ||
		persisted.Content[0].Type != agentapi.EventTypeTextDelta ||
		string(persisted.Content[0].Data) != `{"text":"onetwo"}` ||
		persisted.MessageRevision != 5 {
		t.Fatalf("persisted execution message = %#v", persisted)
	}
}

func TestRuntimeEventSinkPublishesCompleteLiveSnapshotBeforePersistence(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "abababababababababababababababab"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	for _, text := range []string{"你好", "！"} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{
			Type: agentapi.EventTypeTextDelta,
			Data: marshalData(struct {
				// Text 是验证实时消息视图立即可见的文本增量。
				Text string `json:"text"`
			}{Text: text}),
		}); err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
	}

	var lastLive *messageview.Message
	for lastLive == nil || lastLive.Content != "你好！" {
		select {
		case event := <-live:
			if !event.Transient || event.Sequence != 0 || event.Message == nil {
				t.Fatalf("live event = %#v", event)
			}
			lastLive = event.Message
		case <-time.After(time.Second):
			t.Fatal("latest live snapshot was not published within the display window")
		}
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records before complete message = %d, want 3", len(records))
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	records, err = store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() after flush error = %v", err)
	}
	persisted, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if lastLive == nil || len(persisted) != 1 ||
		persisted[0].Content != lastLive.Content ||
		persisted[0].Revision != lastLive.Revision ||
		!reflect.DeepEqual(persisted[0].DisplayParts, lastLive.DisplayParts) {
		t.Fatalf("live snapshot %#v != persisted snapshot %#v", lastLive, persisted)
	}
}

func TestRuntimeEventSinkDoesNotPromoteToolDiffsToFileOutcomes(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	for _, event := range []agentapi.Event{
		{
			Type: agentapi.EventTypeToolUse,
			Data: marshalData(map[string]any{
				"name": "file_modify", "status": "in_progress", "toolCallId": "edit-1",
				"content": []any{map[string]any{
					"type": "diff", "path": "/workspace/project/main.go",
					"oldText": "old\n", "newText": "new\n",
				}},
			}),
		},
		{
			Type: agentapi.EventTypeToolResult,
			Data: marshalData(map[string]any{
				"name": "file_modify", "status": "completed", "toolCallId": "edit-1",
				"content": []any{map[string]any{
					"type": "diff", "path": "/workspace/project/main.go",
					"oldText": "old\n", "newText": "new\nmore\n",
				}},
			}),
		},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
			t.Fatalf("Accept(%s) error = %v", event.Type, err)
		}
	}

	var lastLive *messageview.Message
	liveCount := 0
	for liveCount < 2 {
		select {
		case event := <-live:
			if event.Message == nil {
				t.Fatalf("live event = %#v", event)
			}
			lastLive = event.Message
			liveCount++
		case <-time.After(time.Second):
			t.Fatal("live file change snapshot was not published")
		}
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	persisted, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if lastLive == nil || len(persisted) != 1 || len(persisted[0].FileChanges) != 0 || persisted[0].FileChangeRecord != nil || len(lastLive.FileChanges) != 0 {
		t.Fatalf("tool output was promoted to independent file outcome: live=%+v durable=%+v", lastLive, persisted)
	}
	if !reflect.DeepEqual(persisted[0].DisplayParts, lastLive.DisplayParts) {
		t.Fatal("safe tool projection differs after durable replay")
	}
}

func TestExecutionTerminalRevisionStaysAheadOfTransientOnlyEvents(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "acacacacacacacacacacacacacacacac"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	for index := 0; index < 20; index++ {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{
			Type: agentapi.EventTypeUsage,
			Data: marshalData(map[string]any{"turn": map[string]any{"output_tokens": index + 1}}),
		}); err != nil {
			t.Fatalf("Accept(usage %d) error = %v", index, err)
		}
	}
	if _, err := store.Append(chatID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeExecutionCompleted,
		ExecutionID: "execution-1",
		Data: marshalData(eventlog.ExecutionFinishedData{
			Status:          constants.ExecutionStatusCompleted,
			Usage:           sink.Usage(),
			MessageRevision: sink.MessageRevision(),
		}),
	}}); err != nil {
		t.Fatalf("Append(execution completed) error = %v", err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Status != constants.ExecutionStatusCompleted ||
		messages[0].Revision <= sink.MessageRevision() {
		t.Fatalf("terminal message = %+v, transient revision = %d", messages, sink.MessageRevision())
	}
}

func TestRuntimeEventSinkPersistsToolMessagesAndKeepsOnlyLatestUsage(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "dededededededededededededededede"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	for _, event := range []agentapi.Event{
		{Type: agentapi.EventTypeStatus, Data: json.RawMessage(`{"state":"thinking"}`)},
		{Type: agentapi.EventTypeRaw, Data: json.RawMessage(`{"type":"system","subtype":"thinking_tokens"}`)},
		{Type: agentapi.EventTypeToolUse, Data: marshalData(map[string]any{"id": "tool-1", "name": "Bash"})},
		{Type: agentapi.EventTypeToolResult, Data: marshalData(map[string]any{"tool_use_id": "tool-1", "is_error": false})},
		{Type: agentapi.EventTypeUsage, Data: json.RawMessage(`{"turn":{"output_tokens":10}}`)},
		{Type: agentapi.EventTypeUsage, Data: json.RawMessage(`{"turn":{"output_tokens":12}}`)},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
			t.Fatalf("Accept(%s) error = %v", event.Type, err)
		}
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 5 ||
		records[3].Type != constants.ChatEventTypeAssistantMessage ||
		records[4].Type != constants.ChatEventTypeToolMessage {
		t.Fatalf("persisted tool records = %+v", records)
	}
	if usage := string(sink.Usage()); usage != `{"turn":{"output_tokens":12}}` {
		t.Fatalf("Usage() = %s, want latest usage", usage)
	}
	if _, err := store.Append(chatID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeExecutionCompleted,
		ExecutionID: "execution-1",
		Data: marshalData(eventlog.ExecutionFinishedData{
			Status: constants.ExecutionStatusCompleted,
			Usage:  sink.Usage(),
		}),
	}}); err != nil {
		t.Fatalf("Append(execution completed) error = %v", err)
	}
	records, err = store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() after terminal error = %v", err)
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].RunMetrics == nil ||
		messages[0].RunMetrics.OutputTokens != 12 {
		t.Fatalf("replayed usage = %+v", messages)
	}
}

func TestRuntimeEventSinkDoesNotPersistEveryPartialToolInput(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "cdcdababcdcdababcdcdababcdcdabab"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	send := func(kind agentapi.EventType, data map[string]any) *messageview.Message {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
		if kind == agentapi.EventTypeToolInputSnapshot {
			return nil
		}
		revision := sink.MessageRevision()
		for {
			event := <-live
			if event.Message != nil && event.Message.Revision == revision {
				return event.Message
			}
		}
	}
	send(agentapi.EventTypeToolUse, map[string]any{"toolCallId": "write-1", "name": "write_file", "kind": "edit", "input": map[string]any{"path": "notes.md"}})
	for i := range 1000 {
		send(agentapi.EventTypeToolInputSnapshot, map[string]any{"toolCallId": "write-1", "input": map[string]any{"path": "notes.md", "content": fmt.Sprintf("body-%d", i)}})
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) > 8 {
		t.Fatalf("records while tool input is streaming = %d, want bounded complete parameter segments", len(records))
	}
	lastLive := send(agentapi.EventTypeToolResult, map[string]any{"toolCallId": "write-1", "status": "completed"})
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err = store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) > 10 {
		t.Fatalf("records after complete tool = %d, want bounded parameter segments plus result", len(records))
	}
	var stored eventlog.RuntimeMessageData
	if err := json.Unmarshal(records[len(records)-2].Data, &stored); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stored.Content[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["input"].(map[string]any)["content"] != "body-999" {
		t.Fatalf("persisted final input = %#v", payload)
	}
	messages, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !reflect.DeepEqual(messages[0].DisplayParts, lastLive.DisplayParts) || messages[0].Revision != lastLive.Revision {
		t.Fatalf("live/replay mismatch: live=%+v persisted=%+v", lastLive, messages)
	}
}

func TestRuntimeEventSinkPreservesToolOutputUpdates(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "cdcdababcdcdababcdcdababcdcdabac"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	for _, payload := range []map[string]any{
		{"toolCallId": "shell", "name": "bash", "input": map[string]any{"command": "example"}},
		{"toolCallId": "shell", "rawOutput": "first output"},
		{"toolCallId": "shell", "rawOutput": "second output"},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: agentapi.EventTypeToolUse, Data: marshalData(payload)}); err != nil {
			t.Fatal(err)
		}
		<-live
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 6 {
		t.Fatalf("records = %d, want both execution output updates preserved", len(records))
	}
}

func TestRuntimeEventSinkBatchesToolUpdatesWithoutLosingContent(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "cdcdababcdcdababcdcdababcdcdabad"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	send := func(kind agentapi.EventType, data map[string]any) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
		if kind == agentapi.EventTypeToolUpdate {
			return
		}
		revision := sink.MessageRevision()
		for event := range live {
			if event.Message != nil && event.Message.Revision == revision {
				break
			}
		}
	}
	send(agentapi.EventTypeToolUse, map[string]any{"toolCallId": "tool", "sessionUpdate": "tool_call", "status": "in_progress", "input": map[string]any{}})
	for i := range 300 {
		send(agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "tool", "sessionUpdate": "tool_call_update", "rawInput": map[string]any{"value": i}, "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": fmt.Sprintf("chunk-%d", i)}}}})
	}
	send(agentapi.EventTypeToolResult, map[string]any{"toolCallId": "tool", "status": "completed"})
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) > 10 {
		t.Fatalf("%d records for 300 updates, want bounded batches instead of per-update writes", len(records))
	}
	index := 0
	for _, record := range records[3:] {
		var message eventlog.RuntimeMessageData
		if err := json.Unmarshal(record.Data, &message); err != nil {
			t.Fatal(err)
		}
		for _, content := range message.Content {
			var payload map[string]any
			if err := json.Unmarshal(content.Data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["sessionUpdate"] != "tool_call_update" {
				continue
			}
			blocks := payload["content"].([]any)
			text := blocks[0].(map[string]any)["content"].(map[string]any)["text"]
			if text != fmt.Sprintf("chunk-%d", index) {
				t.Fatalf("content[%d]=%v", index, text)
			}
			index++
		}
	}
	if index != 300 {
		t.Fatalf("preserved %d chunks, want all 300", index)
	}
}

func TestRuntimeEventSinkStoresGrowingToolSnapshotsWithoutQuadraticJSONL(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "ababcdcdababcdcdababcdcdababcdcd"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	updates := make([]json.RawMessage, 0, 128)
	for index := range 128 {
		payload := marshalData(map[string]any{
			"toolCallId": "tool", "content": []any{map[string]any{
				"type": "content", "content": map[string]any{"type": "text", "text": strings.Repeat("abcdef0123456789", (index+1)*64)},
			}},
		})
		updates = append(updates, payload)
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: agentapi.EventTypeToolUpdate, Data: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	var logBytes int64
	for _, record := range records {
		logBytes += record.Length
	}
	if logBytes > 4*int64(len(updates[len(updates)-1])) {
		t.Fatalf("JSONL size = %d, final snapshot = %d; cumulative snapshots were stored repeatedly", logBytes, len(updates[len(updates)-1]))
	}
	t.Logf("JSONL size = %d bytes; final snapshot = %d bytes", logBytes, len(updates[len(updates)-1]))
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].RuntimeEvents) != len(updates) {
		t.Fatalf("replayed %d runtime events, want %d", len(messages[0].RuntimeEvents), len(updates))
	}
	for index, event := range messages[0].RuntimeEvents {
		if !reflect.DeepEqual(event.Data, updates[index]) {
			t.Fatalf("replayed update %d differs from original", index)
		}
	}
	public, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	window, err := store.ReadMessageWindow(chatID, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	windowPublic, err := messageview.ReplayPublicMessages(window.Records)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || len(windowPublic) != 1 || !reflect.DeepEqual(public[0].DisplayParts, windowPublic[0].DisplayParts) {
		t.Fatalf("public projection differs across full and paged replay")
	}
	projection, err := store.ReadProjectionAfter(chatID, records[len(records)-2].Sequence, 0)
	if err != nil {
		t.Fatal(err)
	}
	projectionPublic, err := messageview.ReplayPublicMessages(projection.Records)
	if err != nil {
		t.Fatal(err)
	}
	if len(projectionPublic) != 1 || !reflect.DeepEqual(public[0].DisplayParts, projectionPublic[0].DisplayParts) {
		t.Fatalf("public projection differs after cursor replay")
	}
}

func TestRuntimeEventSinkCompactedToolUpdatesKeepPrivateHistoryWithoutFileOutcome(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "abcddcbaabcddcbaabcddcbaabcddcba"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	send := func(kind agentapi.EventType, payload map[string]any) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: kind, Data: marshalData(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	send(agentapi.EventTypeToolUse, map[string]any{"toolCallId": "edit", "name": "file_modify", "status": "in_progress"})
	for _, newText := range []string{strings.Repeat("first line\n", 40), strings.Repeat("first line\n", 40) + "last line\n"} {
		send(agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "edit", "content": []any{map[string]any{
			"type": "diff", "path": "/workspace/project/main.go", "oldText": "old\n", "newText": newText,
		}}})
	}
	send(agentapi.EventTypeToolResult, map[string]any{"toolCallId": "edit", "status": "completed"})
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].FileChangeDiffs) != 0 || messages[0].FileChangeRecord != nil {
		t.Fatalf("legacy tool data created independent file result: %+v", messages)
	}
	found := false
	for _, event := range messages[0].RuntimeEvents {
		if event.Type == agentapi.EventTypeToolUpdate && strings.Contains(string(event.Data), "last line") {
			found = true
		}
	}
	if !found {
		t.Fatal("bounded batching lost the original private Runtime tool data")
	}
}

func chatCreatedInput() eventlog.AppendInput {
	return eventlog.AppendInput{
		Type: constants.ChatEventTypeCreated,
		Data: marshalData(eventlog.ChatCreatedData{
			WakerID:            "waker-1",
			WorkspaceDirectory: "/workspace/project",
			Title:              "测试对话",
		}),
	}
}

func activeChatInputs() []eventlog.AppendInput {
	return []eventlog.AppendInput{
		chatCreatedInput(),
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: "execution-1",
			Data:        marshalData(eventlog.ExecutionCreatedData{}),
		},
		{
			Type:         constants.ChatEventTypeExecutionStarted,
			ExecutionID:  "execution-1",
			InvocationID: "invocation-1",
		},
	}
}

func TestStartedTurnUsesExecutionAsAssistantIdentityWithoutPlaceholderFact(t *testing.T) {
	t.Parallel()

	chatID := "12121212121212121212121212121212"
	executionID := "34343434343434343434343434343434"
	inputs := startedMessageEvents(
		messageChat{
			isNew: true,
			chat: models.Chat{
				ID:                 chatID,
				WakerID:            "56565656565656565656565656565656",
				WorkspaceDirectory: t.TempDir(),
				Title:              "测试对话",
			},
		},
		pendingMessage{
			message: userMessage{Text: "开始"},
			runtime: runtimeSelection{RuntimeID: "codex"},
		},
		models.Execution{
			ID:            executionID,
			ChatID:        chatID,
			UserMessageID: "78787878787878787878787878787878",
			RuntimeID:     "codex",
		},
		"90909090909090909090909090909090",
		nil,
		nil,
		nil,
		nil,
	)
	inputs = append(inputs, eventlog.AppendInput{
		Type:         constants.ChatEventTypeExecutionStarted,
		ExecutionID:  executionID,
		InvocationID: "90909090909090909090909090909090",
	})
	if len(inputs) != 4 {
		t.Fatalf("accepted first turn facts = %d, want 4", len(inputs))
	}
	for _, input := range inputs {
		if string(input.Type) == "message."+"assistant_created" {
			t.Fatal("accepted turn must not persist an empty assistant placeholder")
		}
	}

	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	records, err := store.Create(chatID, inputs)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	messageIDs := make([]string, 0, 2)
	for _, record := range records {
		if record.MessageID != "" {
			messageIDs = append(messageIDs, record.MessageID)
		}
	}
	if len(messageIDs) != 2 || messageIDs[1] != executionID {
		t.Fatalf("message identities = %q, want user ID then execution ID %q", messageIDs, executionID)
	}
}

// runtimeEventTestJournal 为 Runtime 聚合测试提供最小权威事实端口。
type runtimeEventTestJournal struct {
	// Journal 保留本测试不使用的执行查询边界。
	chatservice.Journal
	// store 保存测试中的权威 JSONL 记录。
	store *eventlog.Store
	// live 接收测试消费者折叠后的消息；真实线上信封仍只有 Live。
	// message 保存测试消费者从增量构建的当前视图。
	message *messageview.Message
	// live 接收不入 JSONL 的 transient 消息增量。
	live chan eventstream.PublicEvent
	// fullReads 记录误读完整历史的次数。
	fullReads int
	// executionRecords 记录初始化读取的当前执行事实量。
	executionRecords int
}

// ForkChat 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) ForkChat(context.Context, chatservice.ForkInput, chatservice.NativeFork) (string, error) {
	panic("unexpected ForkChat call")
}

// LoadExecutionContext 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	panic("unexpected LoadExecutionContext call")
}

// CommitStartedTurn 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) CommitStartedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitStartedTurn call")
}

// CommitAcceptedTurn 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) CommitAcceptedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitAcceptedTurn call")
}

// MarkExecutionStarted 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) MarkExecutionStarted(context.Context, models.Execution, string) (models.Execution, error) {
	panic("unexpected MarkExecutionStarted call")
}

// ReadRecords 返回建立实时投影所需的当前事实。
func (j *runtimeEventTestJournal) ReadRecords(chatID string) ([]eventlog.Record, error) {
	j.fullReads++
	return j.store.ReadAll(chatID)
}

// ReadExecutionRecords 通过真实 JSONL 索引读取当前执行事实。
func (j *runtimeEventTestJournal) ReadExecutionRecords(chatID, executionID string) ([]eventlog.Record, error) {
	records, err := j.store.ReadMessage(chatID, executionID)
	j.executionRecords = len(records)
	return records, err
}

// AppendExecutionFacts 同步追加完整 Runtime 消息事实。
func (j *runtimeEventTestJournal) AppendExecutionFacts(_ context.Context, chatID string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	return j.store.Append(chatID, facts)
}

// AppendApprovalFact 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) AppendApprovalFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendApprovalFact call")
}

// PublishApprovalFact 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) PublishApprovalFact(context.Context, eventlog.Record, []models.PendingApproval) {
	panic("unexpected PublishApprovalFact call")
}

// PublishElicitationFact 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) PublishElicitationFact(context.Context, eventlog.Record, []models.PendingElicitation) {
	panic("unexpected PublishElicitationFact call")
}

// AppendElicitationFact 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) AppendElicitationFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendElicitationFact call")
}

// CommitTerminal 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) CommitTerminal(context.Context, chatservice.TerminalFact) (chatservice.TerminalCommit, error) {
	panic("unexpected CommitTerminal call")
}

// PublishTerminal 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) PublishTerminal(chatservice.TerminalCommit) {
	panic("unexpected PublishTerminal call")
}

// PublishLive 交付 transient 消息快照供测试断言。
func (j *runtimeEventTestJournal) PublishLive(_ string, event eventstream.PublicEvent) {
	event = foldTestLive(&j.message, event)
	j.live <- event
}

// foldTestLive 模拟客户端逐操作折叠，仅供实时与耐久重放的等价性断言。
func foldTestLive(current **messageview.Message, event eventstream.PublicEvent) eventstream.PublicEvent {
	// 本测试替身模拟呈现消费者，以继续比较原始实时折叠与完整日志重放。
	if event.Message != nil || event.Live == nil {
		panic("live wire unexpectedly contains a snapshot")
	}
	if *current == nil {
		*current = &messageview.Message{ID: event.Live.MessageID, Revision: event.Live.BaseRevision}
	}
	message := **current
	message.DisplayParts = append([]messageview.MessageDisplayPart(nil), message.DisplayParts...)
	for _, update := range event.Live.Updates {
		if update.BaseRevision != message.Revision {
			panic("noncontiguous test live message")
		}
		for _, change := range update.Parts {
			index := -1
			for i, part := range message.DisplayParts {
				if part.ID == change.PartID {
					index = i
					break
				}
			}
			switch change.Kind {
			case constants.MessageIncrementTextAppend:
				if change.PartType == constants.MessageDisplayPartText || change.PartType == constants.MessageDisplayPartProcessCommentary {
					message.Content += change.Text
				}
				if index < 0 {
					message.DisplayParts = append(message.DisplayParts, messageview.MessageDisplayPart{ID: change.PartID, Type: change.PartType})
					index = len(message.DisplayParts) - 1
				}
				message.DisplayParts[index].Text += change.Text
				message.DisplayParts[index].Type = change.PartType
			case constants.MessageIncrementPartType:
				if index < 0 {
					panic("unknown text phase target")
				}
				message.DisplayParts[index].Type = change.PartType
			case constants.MessageIncrementPartUpsert:
				if index < 0 {
					message.DisplayParts = append(message.DisplayParts, *change.Part)
				} else {
					message.DisplayParts[index] = *change.Part
				}
			}
		}
		if update.RunMetrics != nil {
			metrics := *update.RunMetrics
			message.RunMetrics = &metrics
		}
		message.Revision, message.UpdatedAt = update.Revision, update.UpdatedAt
	}

	*current = &message
	event.Message = &message // 仅测试断言便利字段，不经过服务端公开通道。
	return event
}

// newRuntimeEventSinkForTest 组合 Runtime 事件管线的最小 Journal 端口。
func newRuntimeEventSinkForTest(
	store *eventlog.Store,
	chatID string,
) (*runtimeEventSink, <-chan eventstream.PublicEvent) {
	live := make(chan eventstream.PublicEvent, 256)
	journal := &runtimeEventTestJournal{store: store, live: live}
	return &runtimeEventSink{
		journal:     journal,
		chatID:      chatID,
		executionID: "execution-1",
	}, live
}

func TestCommandFactsKeepLiveAndDurableToolCaptionsEqual(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "99999999999999999999999999999999"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	sink.messageFacts = &executionMessageFacts{}
	send := func(kind agentapi.EventType, data map[string]any) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
	}
	send(agentapi.EventTypeToolUse, map[string]any{"id": "tool", "name": "bash", "input": map[string]any{"command": "allycli workflow source save --input source.json"}})
	op, _ := capability.Find("workflow.source.save")
	if err := sink.messageFacts.record(context.Background(), sink.journal, chatID, "execution-1", op, capability.Result{CommandID: "receipt"}); err != nil {
		t.Fatal(err)
	}
	send(agentapi.EventTypeToolResult, map[string]any{"tool_use_id": "tool", "output": `{"protocolVersion":1,"ok":true,"commandId":"receipt","operation":"workflow.source.save"}`, "is_error": false})
	send(agentapi.EventTypeTextDelta, map[string]any{"text": "done"})
	var lastLive *messageview.Message
	for index := range 3 {
		event := <-live
		lastLive = event.Message
		if index == 0 {
			continue
		}
		for _, part := range event.Message.DisplayParts {
			if part.ToolCallID == "tool" && part.ToolLabel != "保存工作流" {
				t.Fatalf("live business caption lost: %+v", part)
			}
		}
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !reflect.DeepEqual(messages[0].DisplayParts, lastLive.DisplayParts) {
		t.Fatalf("live/durable diverged: live=%+v durable=%+v", lastLive, messages)
	}
}

func TestRuntimeEventSinkInitializesOnlyCurrentExecution(t *testing.T) {
	t.Parallel()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "43434343434343434343434343434343"
	inputs := []eventlog.AppendInput{chatCreatedInput()}
	for i := range 128 {
		id := fmt.Sprintf("old-execution-%d", i)
		inputs = append(inputs,
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: marshalData(eventlog.ExecutionCreatedData{})},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Data: marshalData(eventlog.ExecutionFinishedData{})},
		)
	}
	inputs = append(inputs, activeChatInputs()[1:]...)
	if _, err := store.Create(chatID, inputs); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: marshalData(map[string]any{"text": "current answer"})}); err != nil {
		t.Fatal(err)
	}
	event := <-live
	if event.Message == nil || event.Message.ID != "execution-1" || event.Message.Content != "current answer" {
		t.Fatalf("current live message = %+v", event.Message)
	}
	journal := sink.journal.(*runtimeEventTestJournal)
	if journal.fullReads != 0 || journal.executionRecords != 3 {
		t.Fatalf("live initialization read full history %d times and %d execution records; want 0 and 3", journal.fullReads, journal.executionRecords)
	}
}

// LoadExecutionChat 不参与 Runtime 事件聚合测试。
func (j *runtimeEventTestJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	panic("unexpected LoadExecutionChat call")
}

// TestRuntimePendingTextAllocatesLinearly 防止完整消息聚合对每个短片段重复解码和编码已经累积的全文。
func TestRuntimePendingTextAllocatesLinearly(t *testing.T) {
	measure := func(count int) uint64 {
		store, err := eventlog.NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		chatID := "67676767676767676767676767676767"
		if _, err = store.Create(chatID, activeChatInputs()); err != nil {
			t.Fatal(err)
		}
		sink, _ := newRuntimeEventSinkForTest(store, chatID)
		event := agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: []byte(`{"text":"abcdefgh"}`)}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for index := 0; index < count; index++ {
			if err = sink.appendAssistantContentLocked("invocation-1", event, int64(index+4)); err != nil {
				t.Fatal(err)
			}
		}
		if err = sink.Flush(); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		records, err := store.ReadAll(chatID)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := messageview.ReplayPublicMessages(records)
		if err != nil || len(messages) != 1 || messages[0].Content != strings.Repeat("abcdefgh", count) {
			t.Fatalf("aggregated text differs: messages=%d err=%v", len(messages), err)
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	small, large := measure(1000), measure(2000)
	if float64(large) > float64(small)*2.8 {
		t.Fatalf("doubling text chunks allocated %d -> %d bytes (%.2fx), want <= 2.8x", small, large, float64(large)/float64(small))
	}
}

// BenchmarkRuntimePendingTextChunks 覆盖完整消息聚合的短片段规模；临时日志准备与结果读取不计入测量。
func BenchmarkRuntimePendingTextChunks(b *testing.B) {
	event := agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: []byte(`{"text":"abcdefgh"}`)}
	for _, count := range []int{1000, 10000, 20000} {
		b.Run(fmt.Sprintf("chunks=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				store, err := eventlog.NewStore(b.TempDir())
				if err != nil {
					b.Fatal(err)
				}
				chatID := "68686868686868686868686868686868"
				if _, err = store.Create(chatID, activeChatInputs()); err != nil {
					b.Fatal(err)
				}
				sink, _ := newRuntimeEventSinkForTest(store, chatID)
				b.StartTimer()
				for index := 0; index < count; index++ {
					if err = sink.appendAssistantContentLocked("invocation-1", event, int64(index+4)); err != nil {
						b.Fatal(err)
					}
				}
				if err = sink.Flush(); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				records, err := store.ReadAll(chatID)
				if err != nil {
					b.Fatal(err)
				}
				messages, err := messageview.ReplayPublicMessages(records)
				if err != nil || len(messages) != 1 || messages[0].Content != strings.Repeat("abcdefgh", count) {
					b.Fatalf("aggregated text differs: messages=%d err=%v", len(messages), err)
				}
				b.StartTimer()
			}
		})
	}
}

// TestRuntimeTextSegmentsKeepOrderAcrossToolAndCancel 验证已解码文本聚合仍保留语义段、工具边界和取消前已经收到的全部内容。
func TestRuntimeTextSegmentsKeepOrderAcrossToolAndCancel(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "69696969696969696969696969696969"
	if _, err = store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, event := range []agentapi.Event{
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"前"}`)},
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"文"}`)},
		{Type: agentapi.EventTypeCommentaryDelta, Data: json.RawMessage(`{"text":"过"}`)},
		{Type: agentapi.EventTypeCommentaryDelta, Data: json.RawMessage(`{"text":"程"}`)},
		{Type: agentapi.EventTypeThinkingStart, Data: json.RawMessage(`{}`)},
		{Type: agentapi.EventTypeThinkingDelta, Data: json.RawMessage(`{"text":"推"}`)},
		{Type: agentapi.EventTypeThinkingDelta, Data: json.RawMessage(`{"text":"理"}`)},
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"结"}`)},
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"果"}`)},
		{Type: agentapi.EventTypeToolUse, Data: json.RawMessage(`{"toolCallId":"read-1","name":"read"}`)},
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"尾"}`)},
		{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"部"}`)},
	} {
		if err = sink.acceptInvocation(ctx, "invocation-1", event); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	if err = sink.acceptInvocation(ctx, "invocation-1", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"迟到"}`)}); err != status_error.OperationCanceled {
		t.Fatalf("late canceled chunk accepted: %v", err)
	}
	if err = sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	var contents []eventlog.RuntimeMessageContent
	for _, record := range records {
		if record.Type != constants.ChatEventTypeAssistantMessage {
			continue
		}
		var data eventlog.RuntimeMessageData
		if err = json.Unmarshal(record.Data, &data); err != nil {
			t.Fatal(err)
		}
		contents = append(contents, data.Content...)
	}
	wantTypes := []agentapi.EventType{agentapi.EventTypeTextDelta, agentapi.EventTypeCommentaryDelta, agentapi.EventTypeThinkingStart, agentapi.EventTypeThinkingDelta, agentapi.EventTypeTextDelta, agentapi.EventTypeToolUse, agentapi.EventTypeTextDelta}
	wantText := []string{"前文", "过程", "", "推理", "结果", "", "尾部"}
	if len(contents) != len(wantTypes) {
		t.Fatalf("content boundary count=%d, want %d", len(contents), len(wantTypes))
	}
	for index, content := range contents {
		var payload map[string]any
		if err = json.Unmarshal(content.Data, &payload); err != nil {
			t.Fatal(err)
		}
		text, _ := payload["text"].(string)
		if content.Type != wantTypes[index] || text != wantText[index] {
			t.Fatalf("segment %d=%s/%q, want %s/%q", index, content.Type, text, wantTypes[index], wantText[index])
		}
	}
	messages, err := messageview.ReplayPublicMessages(records)
	if err != nil || len(messages) != 1 || messages[0].Content != "前文过程结果尾部" {
		t.Fatalf("canceled text differs: messages=%+v err=%v", messages, err)
	}
}

// TestRuntimeEventSinkCompactsMixedCumulativeInput 保留真实混合更新的正文、状态与元数据，跨条数和字节边界仅提交每批最新参数。
func TestRuntimeEventSinkCompactsMixedCumulativeInput(t *testing.T) {
	for _, test := range []struct {
		// name 标识触发提交的批次边界。
		name string
		// count 是连续混合更新数量。
		count int
		// inputStep 是每次累计参数正文增长的字节数。
		inputStep int
		// progressPadding 是每次有序正文的额外字节数。
		progressPadding int
	}{
		{name: "count_boundary", count: 300, inputStep: 128},
		{name: "byte_boundary", count: 100, inputStep: 1024, progressPadding: 4096},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := eventlog.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chatID := "dededededededededededededededede"
			if _, err := store.Create(chatID, activeChatInputs()); err != nil {
				t.Fatal(err)
			}
			sink, live := newRuntimeEventSinkForTest(store, chatID)
			originalBytes := 0
			send := func(kind agentapi.EventType, payload map[string]any) {
				t.Helper()
				data := marshalData(payload)
				originalBytes += len(data)
				if err := sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: kind, Data: data}); err != nil {
					t.Fatal(err)
				}
			}
			send(agentapi.EventTypeToolUse, map[string]any{"toolCallId": "mixed", "name": "write_file", "rawInput": map[string]any{"path": "note.md"}})
			finalInput := ""
			padding := strings.Repeat("p", test.progressPadding)
			for i := range test.count {
				finalInput = strings.Repeat("x", (i+1)*test.inputStep)
				send(agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "mixed", "sessionUpdate": "tool_call_update", "status": "in_progress", "rawInput": map[string]any{"path": "note.md", "content": finalInput}, "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": fmt.Sprintf("chunk-%d", i) + padding}}}, "_meta": map[string]any{"progress": i}})
				if i%7 == 0 {
					send(agentapi.EventTypeToolInputSnapshot, map[string]any{"toolCallId": "mixed", "rawInput": map[string]any{"path": "note.md", "content": finalInput}})
				}
			}
			send(agentapi.EventTypeToolResult, map[string]any{"toolCallId": "mixed", "status": "completed"})
			if err := sink.Flush(); err != nil {
				t.Fatal(err)
			}
			records, err := store.ReadAll(chatID)
			if err != nil {
				t.Fatal(err)
			}
			totalBytes, progress, inputs := 0, 0, 0
			latestInput := ""
			for _, record := range records[3:] {
				totalBytes += len(record.Data)
				var message eventlog.RuntimeMessageData
				if err := json.Unmarshal(record.Data, &message); err != nil {
					t.Fatal(err)
				}
				for _, content := range message.Content {
					var payload map[string]any
					if err := json.Unmarshal(content.Data, &payload); err != nil {
						t.Fatal(err)
					}
					if _, present := payload["rawInput"]; present && content.Type != agentapi.EventTypeToolUse {
						inputs++
						latestInput, _ = payload["rawInput"].(map[string]any)["content"].(string)
					}
					if content.Type != agentapi.EventTypeToolUpdate {
						continue
					}
					blocks := payload["content"].([]any)
					text := blocks[0].(map[string]any)["content"].(map[string]any)["text"]
					if text != fmt.Sprintf("chunk-%d", progress)+padding || payload["_meta"].(map[string]any)["progress"] != float64(progress) || payload["status"] != "in_progress" {
						t.Fatalf("ordered progress %d changed", progress)
					}
					progress++
				}
			}
			if progress != test.count || inputs == 0 || latestInput != finalInput {
				t.Fatalf("progress=%d inputs=%d latest input length=%d", progress, inputs, len(latestInput))
			}
			if totalBytes >= originalBytes/5 {
				t.Fatalf("persisted %d bytes from %d original bytes, want at least 80%% reduction", totalBytes, originalBytes)
			}
			t.Logf("mixed update bytes: original=%d persisted=%d reduction=%.1f%%", originalBytes, totalBytes, 100*(1-float64(totalBytes)/float64(originalBytes)))
			assertRuntimeSinkLiveMatchesReplay(t, live, records)
		})
	}
}

// TestRuntimeEventSinkKeepsSnapshotMetadataInDeliveryOrder 防止后到快照的状态、名称和清空操作被早到更新覆盖。
func TestRuntimeEventSinkKeepsSnapshotMetadataInDeliveryOrder(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "ababdededededededededededededede"
	if _, err := store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, live := newRuntimeEventSinkForTest(store, chatID)
	for _, event := range []agentapi.Event{
		{Type: agentapi.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read_file", "rawInput": map[string]any{"path": "old.md"}})},
		{Type: agentapi.EventTypeToolUpdate, Data: marshalData(map[string]any{"toolCallId": "tool", "rawInput": map[string]any{"path": "old.md"}, "title": "old", "name": "read_file", "status": "in_progress", "locations": []any{map[string]any{"path": "old.md"}}, "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "progress"}}}})},
		{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", "rawInput": nil, "title": "new", "name": "write_file", "status": "completed", "locations": []any{}})},
	} {
		if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	titles := []string{}
	for _, record := range records[3:] {
		var message eventlog.RuntimeMessageData
		if err := json.Unmarshal(record.Data, &message); err != nil {
			t.Fatal(err)
		}
		for _, content := range message.Content {
			var payload map[string]any
			if err := json.Unmarshal(content.Data, &payload); err != nil {
				t.Fatal(err)
			}
			if title, ok := payload["title"].(string); ok {
				titles = append(titles, title)
			}
		}
	}
	if !reflect.DeepEqual(titles, []string{"old", "new"}) {
		t.Fatalf("metadata delivery order = %v, want [old new]", titles)
	}
	var lastLive *messageview.Message
	for len(live) > 0 {
		if event := <-live; event.Message != nil {
			lastLive = event.Message
		}
	}
	replay, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if lastLive == nil || len(replay) != 1 || !reflect.DeepEqual(lastLive.DisplayParts, replay[0].DisplayParts) || lastLive.Revision != replay[0].Revision {
		t.Fatalf("metadata live/replay differ: live=%+v replay=%+v", lastLive, replay)
	}
}

// TestRuntimeEventSinkCompactionPreservesScopeAndClears 参数清空不能丢失，也不能跨工具或 Invocation 合并。
func TestRuntimeEventSinkCompactionPreservesScopeAndClears(t *testing.T) {
	for _, inputKey := range []string{"rawInput", "input"} {
		t.Run(inputKey, func(t *testing.T) {
			store, err := eventlog.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chatID := "abcdedededededededededededededed"
			if _, err := store.Create(chatID, activeChatInputs()); err != nil {
				t.Fatal(err)
			}
			sink, live := newRuntimeEventSinkForTest(store, chatID)
			send := func(invocationID string, kind agentapi.EventType, payload map[string]any) {
				t.Helper()
				if err := sink.acceptInvocation(context.Background(), invocationID, agentapi.Event{Type: kind, Data: marshalData(payload)}); err != nil {
					t.Fatal(err)
				}
			}
			send("invocation-1", agentapi.EventTypeToolUse, map[string]any{"toolCallId": "a", "name": "file_modify", "kind": "edit", inputKey: map[string]any{"path": "a.md"}})
			send("invocation-1", agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "a", inputKey: map[string]any{"path": "a.md", "content": "private body"}, "status": "in_progress", "content": []any{map[string]any{"type": "diff", "path": "a.md", "oldText": "old", "newText": "new"}}, "_meta": map[string]any{"progress": 1}})
			send("invocation-1", agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "a", inputKey: nil, "content": []any{}, "locations": []any{}, "_meta": map[string]any{"progress": 2}})
			send("invocation-1", agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "b", "name": "read_file", inputKey: map[string]any{"path": "b.md"}, "status": "in_progress", "content": []any{}, "_meta": map[string]any{"progress": 3}})
			send("invocation-2", agentapi.EventTypeToolUpdate, map[string]any{"toolCallId": "b", "name": "read_file", inputKey: map[string]any{"path": "c.md"}, "status": "in_progress", "content": []any{}, "_meta": map[string]any{"progress": 4}})
			send("invocation-2", agentapi.EventTypeToolResult, map[string]any{"toolCallId": "b", "status": "completed"})
			if err := sink.Flush(); err != nil {
				t.Fatal(err)
			}
			records, err := store.ReadAll(chatID)
			if err != nil {
				t.Fatal(err)
			}
			scopes := []string{}
			paths := []any{}
			progress := []float64{}
			for _, record := range records[3:] {
				var message eventlog.RuntimeMessageData
				if err := json.Unmarshal(record.Data, &message); err != nil {
					t.Fatal(err)
				}
				for _, content := range message.Content {
					var payload map[string]any
					if err := json.Unmarshal(content.Data, &payload); err != nil {
						t.Fatal(err)
					}
					if value, present := payload[inputKey]; present && content.Type != agentapi.EventTypeToolUse {
						scopes = append(scopes, record.InvocationID+":"+payload["toolCallId"].(string))
						if input, ok := value.(map[string]any); ok {
							paths = append(paths, input["path"])
						} else {
							paths = append(paths, value)
						}
					}
					if content.Type == agentapi.EventTypeToolUpdate {
						progress = append(progress, payload["_meta"].(map[string]any)["progress"].(float64))
						if len(progress) == 2 && (len(payload["content"].([]any)) != 0 || len(payload["locations"].([]any)) != 0) {
							t.Fatal("explicit empty content or locations lost")
						}
					}
				}
			}
			if !reflect.DeepEqual(scopes, []string{"invocation-1:a", "invocation-1:b", "invocation-2:b"}) || !reflect.DeepEqual(paths, []any{nil, "b.md", "c.md"}) || !reflect.DeepEqual(progress, []float64{1, 2, 3, 4}) {
				t.Fatalf("scope/clear/order changed: scopes=%v paths=%v progress=%v", scopes, paths, progress)
			}
			message := assertRuntimeSinkLiveMatchesReplay(t, live, records)
			if len(message.DisplayParts) != 3 || len(message.FileChanges) != 0 || message.DisplayParts[0].ToolSafeSummary != nil {
				t.Fatalf("cleared tool facts retained: %+v", message)
			}
		})
	}
}

// assertRuntimeSinkLiveMatchesReplay 校验实时最终快照与耐久日志重放具有相同公开展示和修订号。
func assertRuntimeSinkLiveMatchesReplay(t *testing.T, live <-chan eventstream.PublicEvent, records []eventlog.Record) *messageview.Message {
	t.Helper()
	var lastLive *messageview.Message
	for len(live) > 0 {
		if event := <-live; event.Message != nil {
			lastLive = event.Message
		}
	}
	replay, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if lastLive == nil || len(replay) != 1 || !reflect.DeepEqual(lastLive.DisplayParts, replay[0].DisplayParts) || !reflect.DeepEqual(lastLive.FileChanges, replay[0].FileChanges) || lastLive.Revision != replay[0].Revision {
		t.Fatalf("live/replay differ: live=%+v replay=%+v", lastLive, replay)
	}
	return &replay[0]
}

// TestRuntimeEventSinkKeepsInputProjectionContext 参数必须在原事件身份上下文中投影，不能跨越名称变化前移或后移。
func TestRuntimeEventSinkKeepsInputProjectionContext(t *testing.T) {
	for _, inputKey := range []string{"rawInput", "input"} {
		otherInputKey := "rawInput"
		if inputKey == "rawInput" {
			otherInputKey = "input"
		}
		for _, test := range []struct {
			// name 标识身份元数据与参数的原始先后关系。
			name string
			// events 是真实交付顺序下的规范工具更新。
			events []agentapi.Event
			// wantFallback 表示自定义工具是否应保留输入中的通用摘要。
			wantFallback bool
		}{
			{name: "same_mixed_event", events: []agentapi.Event{{Type: agentapi.EventTypeToolUpdate, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "customTool", inputKey: map[string]any{"ticket": "ABC"}, "status": "in_progress", "content": []any{}, "_meta": map[string]any{"progress": 1}})}}, wantFallback: true},
			{name: "same_snapshot_event", events: []agentapi.Event{{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "customTool", inputKey: map[string]any{"ticket": "ABC"}})}}, wantFallback: true},
			{name: "metadata_before_snapshot", events: []agentapi.Event{
				{Type: agentapi.EventTypeToolUpdate, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "customTool"})},
				{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", inputKey: map[string]any{"ticket": "ABC"}})},
			}, wantFallback: true},
			{name: "snapshot_before_metadata", events: []agentapi.Event{
				{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", inputKey: map[string]any{"ticket": "ABC"}})},
				{Type: agentapi.EventTypeToolUpdate, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "customTool"})},
			}},
			{name: "different_input_sources_across_metadata", events: []agentapi.Event{
				{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", inputKey: map[string]any{"ticket": "ABC"}})},
				{Type: agentapi.EventTypeToolUpdate, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "customTool"})},
				{Type: agentapi.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", otherInputKey: map[string]any{"ticket": "DEF"}})},
			}},
		} {
			t.Run(inputKey+"/"+test.name, func(t *testing.T) {
				store, err := eventlog.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				chatID := "efefdededededededededededededede"
				if _, err := store.Create(chatID, activeChatInputs()); err != nil {
					t.Fatal(err)
				}
				sink, live := newRuntimeEventSinkForTest(store, chatID)
				events := append([]agentapi.Event{{Type: agentapi.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}}, test.events...)
				events = append(events, agentapi.Event{Type: agentapi.EventTypeToolResult, Data: marshalData(map[string]any{"toolCallId": "tool", "status": "completed"})})
				for _, event := range events {
					if err := sink.acceptInvocation(context.Background(), "invocation-1", event); err != nil {
						t.Fatal(err)
					}
				}
				if err := sink.Flush(); err != nil {
					t.Fatal(err)
				}
				records, err := store.ReadAll(chatID)
				if err != nil {
					t.Fatal(err)
				}
				message := assertRuntimeSinkLiveMatchesReplay(t, live, records)
				if len(message.DisplayParts) != 1 || strings.Contains(message.DisplayParts[0].ToolLabel, "ABC") != test.wantFallback {
					t.Fatalf("input applied in wrong identity context: %+v", message.DisplayParts)
				}
			})
		}
	}
}

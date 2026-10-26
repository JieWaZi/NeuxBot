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
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
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
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{
			Type: runtimes.EventTypeTextDelta,
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
		persisted.Content[0].Type != runtimes.EventTypeTextDelta ||
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
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{
			Type: runtimes.EventTypeTextDelta,
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

func TestRuntimeEventSinkKeepsFileChangesEqualBetweenLiveAndReplay(t *testing.T) {
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
	for _, event := range []runtimes.Event{
		{
			Type: runtimes.EventTypeToolUse,
			Data: marshalData(map[string]any{
				"name": "file_modify", "status": "in_progress", "toolCallId": "edit-1",
				"content": []any{map[string]any{
					"type": "diff", "path": "/workspace/project/main.go",
					"oldText": "old\n", "newText": "new\n",
				}},
			}),
		},
		{
			Type: runtimes.EventTypeToolResult,
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
	if lastLive == nil || len(persisted) != 1 ||
		!reflect.DeepEqual(persisted[0].FileChanges, lastLive.FileChanges) ||
		len(persisted[0].FileChanges) != 1 ||
		persisted[0].FileChanges[0].AddedLines != 2 ||
		persisted[0].FileChanges[0].DeletedLines != 1 {
		t.Fatalf("live file changes %#v != persisted %#v", lastLive, persisted)
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
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{
			Type: runtimes.EventTypeUsage,
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
	for _, event := range []runtimes.Event{
		{Type: runtimes.EventTypeStatus, Data: json.RawMessage(`{"state":"thinking"}`)},
		{Type: runtimes.EventTypeRaw, Data: json.RawMessage(`{"type":"system","subtype":"thinking_tokens"}`)},
		{Type: runtimes.EventTypeToolUse, Data: marshalData(map[string]any{"id": "tool-1", "name": "Bash"})},
		{Type: runtimes.EventTypeToolResult, Data: marshalData(map[string]any{"tool_use_id": "tool-1", "is_error": false})},
		{Type: runtimes.EventTypeUsage, Data: json.RawMessage(`{"turn":{"output_tokens":10}}`)},
		{Type: runtimes.EventTypeUsage, Data: json.RawMessage(`{"turn":{"output_tokens":12}}`)},
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
	send := func(kind runtimes.EventType, data map[string]any) *messageview.Message {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
		if kind == runtimes.EventTypeToolInputSnapshot {
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
	send(runtimes.EventTypeToolUse, map[string]any{"toolCallId": "write-1", "name": "write_file", "kind": "edit", "input": map[string]any{"path": "notes.md"}})
	for i := range 1000 {
		send(runtimes.EventTypeToolInputSnapshot, map[string]any{"toolCallId": "write-1", "input": map[string]any{"path": "notes.md", "content": fmt.Sprintf("body-%d", i)}})
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("records while tool input is streaming = %d, want 3 initial facts and one tool start", len(records))
	}
	lastLive := send(runtimes.EventTypeToolResult, map[string]any{"toolCallId": "write-1", "status": "completed"})
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err = store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 6 {
		t.Fatalf("records after complete tool = %d, want start, latest input, result", len(records))
	}
	var stored eventlog.RuntimeMessageData
	if err := json.Unmarshal(records[4].Data, &stored); err != nil {
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
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{Type: runtimes.EventTypeToolUse, Data: marshalData(payload)}); err != nil {
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
	send := func(kind runtimes.EventType, data map[string]any) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
		if kind == runtimes.EventTypeToolUpdate {
			return
		}
		revision := sink.MessageRevision()
		for event := range live {
			if event.Message != nil && event.Message.Revision == revision {
				break
			}
		}
	}
	send(runtimes.EventTypeToolUse, map[string]any{"toolCallId": "tool", "sessionUpdate": "tool_call", "status": "in_progress", "input": map[string]any{}})
	for i := range 300 {
		send(runtimes.EventTypeToolUpdate, map[string]any{"toolCallId": "tool", "sessionUpdate": "tool_call_update", "rawInput": map[string]any{"value": i}, "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": fmt.Sprintf("chunk-%d", i)}}}})
	}
	send(runtimes.EventTypeToolResult, map[string]any{"toolCallId": "tool", "status": "completed"})
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
	// store 保存测试中的权威 JSONL 记录。
	store *eventlog.Store
	// live 接收不入 JSONL 的 transient 消息快照。
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
	j.live <- event
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
	send := func(kind runtimes.EventType, data map[string]any) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{Type: kind, Data: marshalData(data)}); err != nil {
			t.Fatal(err)
		}
	}
	send(runtimes.EventTypeToolUse, map[string]any{"id": "tool", "name": "bash", "input": map[string]any{"command": "allycli workflow source save --input source.json"}})
	op, _ := contract.Find("workflow.source.save")
	if err := sink.messageFacts.record(context.Background(), sink.journal, chatID, "execution-1", op, agentcapability.Result{CommandID: "receipt"}); err != nil {
		t.Fatal(err)
	}
	send(runtimes.EventTypeToolResult, map[string]any{"tool_use_id": "tool", "output": `{"protocolVersion":1,"ok":true,"commandId":"receipt","operation":"workflow.source.save"}`, "is_error": false})
	send(runtimes.EventTypeTextDelta, map[string]any{"text": "done"})
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
	if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: marshalData(map[string]any{"text": "current answer"})}); err != nil {
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
		event := runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"abcdefgh"}`)}
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
	event := runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"abcdefgh"}`)}
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
	for _, event := range []runtimes.Event{
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"前"}`)},
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"文"}`)},
		{Type: runtimes.EventTypeCommentaryDelta, Data: json.RawMessage(`{"text":"过"}`)},
		{Type: runtimes.EventTypeCommentaryDelta, Data: json.RawMessage(`{"text":"程"}`)},
		{Type: runtimes.EventTypeThinkingStart, Data: json.RawMessage(`{}`)},
		{Type: runtimes.EventTypeThinkingDelta, Data: json.RawMessage(`{"text":"推"}`)},
		{Type: runtimes.EventTypeThinkingDelta, Data: json.RawMessage(`{"text":"理"}`)},
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"结"}`)},
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"果"}`)},
		{Type: runtimes.EventTypeToolUse, Data: json.RawMessage(`{"toolCallId":"read-1","name":"read"}`)},
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"尾"}`)},
		{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"部"}`)},
	} {
		if err = sink.acceptInvocation(ctx, "invocation-1", event); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	if err = sink.acceptInvocation(ctx, "invocation-1", runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: json.RawMessage(`{"text":"迟到"}`)}); err != status_error.OperationCanceled {
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
	wantTypes := []runtimes.EventType{runtimes.EventTypeTextDelta, runtimes.EventTypeCommentaryDelta, runtimes.EventTypeThinkingStart, runtimes.EventTypeThinkingDelta, runtimes.EventTypeTextDelta, runtimes.EventTypeToolUse, runtimes.EventTypeTextDelta}
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

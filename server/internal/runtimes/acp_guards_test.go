package runtimes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// blockingUntilCanceledSink 等待事件交付 Context 超时。
type blockingUntilCanceledSink struct{}

// Accept 模拟不会主动返回的产品事件持久化。
func (*blockingUntilCanceledSink) Accept(ctx context.Context, _ Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestACPBridgeRejectsUpdatesWhenBoundedQueueIsFull(t *testing.T) {
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		time.Second,
	)
	for range acpUpdateQueueCapacity {
		if err := bridge.enqueue(acpDelivery{}); err != nil {
			t.Fatalf("fill ACP queue: %v", err)
		}
	}
	if err := bridge.enqueue(acpDelivery{}); !errors.Is(err, status_error.RuntimeEventQueueOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestACPBridgeWaitsForConsumerDuringShortBurst(t *testing.T) {
	sink := &recordingSink{}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	event, err := newEvent(EventTypeTextDelta, map[string]any{"text": "chunk"})
	if err != nil {
		t.Fatal(err)
	}
	for range acpUpdateQueueCapacity {
		if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
			t.Fatal(err)
		}
	}
	accepted := make(chan error, 1)
	go func() { accepted <- bridge.enqueue(acpDelivery{events: []Event{event}}) }()
	select {
	case err := <-accepted:
		t.Fatalf("burst rejected before consumer could resume: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	go bridge.run(context.Background())
	bridge.activateDelivery()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != acpUpdateQueueCapacity+1 {
		t.Fatalf("delivered %d chunks, want complete burst", len(sink.events))
	}
}

func TestACPBridgeCoalescesQueuedToolInputSnapshots(t *testing.T) {
	sink := &recordingSink{}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	client := newACPClient()
	if err := client.register(bridge); err != nil {
		t.Fatal(err)
	}
	updates := []acp.SessionUpdate{acp.StartToolCall("write", "Write file")}
	for i := range 1000 {
		updates = append(updates, acp.UpdateToolCall("write", acp.WithUpdateRawInput(map[string]any{"content": fmt.Sprintf("body-%d", i)})))
	}
	updates = append(updates,
		acp.UpdateToolCall("write", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)),
		acp.UpdateAgentMessageText("finished"),
	)
	for _, update := range updates {
		if err := client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "session", Update: update}); err != nil {
			t.Fatalf("queued tool snapshot rejected: %v", err)
		}
	}
	client.unregister(bridge)
	go bridge.run(context.Background())
	bridge.activateDelivery()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 4 {
		t.Fatalf("delivered %d events, want start, latest input, result, text", len(sink.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(sink.events[1].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["input"].(map[string]any)["content"] != "body-999" {
		t.Fatalf("last input = %#v", payload)
	}
	if sink.events[1].Type != EventTypeToolInputSnapshot || sink.events[2].Type != EventTypeToolResult || sink.events[3].Type != EventTypeTextDelta {
		t.Fatalf("event order = %#v", sink.events)
	}
}

// TestACPBridgePreservesOmittedStatusContentUpdates 覆盖运行开始后省略状态的正文更新不能按参数快照合并或删减。
func TestACPBridgePreservesOmittedStatusContentUpdates(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, workBuddyACPRuntimeBinding(), time.Second)
	start := acp.StartToolCall("write", "Write", acp.WithStartStatus(acp.ToolCallStatusInProgress))
	if events, err := bridge.mapNotification(acp.SessionNotification{SessionId: "session", Update: start}); err != nil || len(events) != 1 || events[0].Type != EventTypeToolUse {
		t.Fatalf("start events=%+v error=%v", events, err)
	}
	for _, text := range []string{"first output", "second output"} {
		update := acp.UpdateToolCall("write", acp.WithUpdateRawInput(map[string]any{"path": "note.md"}), acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock(text))}))
		if err := bridge.enqueue(acpDelivery{notification: &acp.SessionNotification{SessionId: "session", Update: update}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(bridge.updates) != 2 {
		t.Fatalf("queued %d updates, want both content chunks", len(bridge.updates))
	}
	for _, want := range []string{"first output", "second output"} {
		delivery := <-bridge.updates
		events, err := bridge.mapNotification(*delivery.notification)
		if err != nil || len(events) != 1 || events[0].Type != EventTypeToolUpdate {
			t.Fatalf("events=%+v error=%v", events, err)
		}
		var payload map[string]any
		if err := json.Unmarshal(events[0].Data, &payload); err != nil {
			t.Fatal(err)
		}
		content, ok := payload["content"].([]any)
		if !ok || len(content) != 1 || content[0].(map[string]any)["content"].(map[string]any)["text"] != want || payload["toolCallId"] != "write" || payload["rawInput"] == nil {
			t.Fatalf("content update lost fields or order: %+v", payload)
		}
	}
}

func TestACPBridgeCoalescingPreservesFieldsAndOrder(t *testing.T) {
	sink := &recordingSink{}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	client := newACPClient()
	if err := client.register(bridge); err != nil {
		t.Fatal(err)
	}
	first := acp.UpdateToolCall("one", acp.WithUpdateTitle("Write file"), acp.WithUpdateKind(acp.ToolKindEdit), acp.WithUpdateRawInput(map[string]any{"path": "before.md"}))
	updates := []acp.SessionUpdate{
		first,
		acp.UpdateToolCall("one", acp.WithUpdateRawInput(map[string]any{"path": "after.md"})),
		acp.UpdateAgentMessageText("between"),
		acp.UpdateToolCall("one", acp.WithUpdateRawOutput("output")),
		acp.UpdateToolCall("two", acp.WithUpdateRawInput(map[string]any{"path": "two.md"})),
		acp.UpdateToolCall("one", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)),
	}
	for _, update := range updates {
		if err := client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "session", Update: update}); err != nil {
			t.Fatal(err)
		}
	}
	client.unregister(bridge)
	go bridge.run(context.Background())
	bridge.activateDelivery()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 5 {
		t.Fatalf("events = %#v", sink.events)
	}
	var payload map[string]any
	if err := json.Unmarshal(sink.events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["title"] != "Write file" || payload["kind"] != "edit" || payload["input"].(map[string]any)["path"] != "after.md" {
		t.Fatalf("merged fields = %#v", payload)
	}
	if first.ToolCallUpdate.RawInput.(map[string]any)["path"] != "before.md" {
		t.Fatal("coalescing mutated the original notification")
	}
	if sink.events[1].Type != EventTypeTextDelta || sink.events[4].Type != EventTypeToolResult {
		t.Fatalf("event order = %#v", sink.events)
	}
	for i, id := range []string{"one", "two", "one"} {
		if err := json.Unmarshal(sink.events[i+2].Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["toolCallId"] != id {
			t.Fatalf("tool order[%d] = %v, want %s", i, payload["toolCallId"], id)
		}
	}
}

func TestACPBridgePreservesToolContentChunks(t *testing.T) {
	sink := &recordingSink{}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, workBuddyACPRuntimeBinding(), time.Second)
	for _, text := range []string{"first", "second"} {
		update := acp.UpdateToolCall("tool", acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock(text))}))
		if err := bridge.enqueue(acpDelivery{notification: &acp.SessionNotification{SessionId: "session", Update: update}}); err != nil {
			t.Fatal(err)
		}
	}
	go bridge.run(context.Background())
	bridge.activateDelivery()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("tool content chunks = %d, want both chunks preserved", len(sink.events))
	}
}

// TestACPBridgeSeparatesToolInputSnapshots 验证参数生成、执行进度与终态保持不同事件语义。
func TestACPBridgeSeparatesToolInputSnapshots(t *testing.T) {
	for _, binding := range []acpRuntimeBinding{piACPRuntimeBinding(), workBuddyACPRuntimeBinding()} {
		t.Run(binding.baseRuntimeID, func(t *testing.T) {
			bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, binding, time.Second)
			updates := []acp.SessionUpdate{acp.StartToolCall("write-1", "Write", acp.WithStartStatus(acp.ToolCallStatusPending))}
			for _, body := range []string{"a", "ab", "abc"} {
				opts := []acp.ToolCallUpdateOpt{acp.WithUpdateRawInput(map[string]any{"path": "note.md", "content": body})}
				if binding.baseRuntimeID == RuntimeIDPi {
					opts = append(opts, acp.WithUpdateStatus(acp.ToolCallStatusPending))
				} else {
					opts = append(opts, acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock("parameter fragment"))}))
				}
				updates = append(updates, acp.UpdateToolCall("write-1", opts...))
			}
			updates = append(updates,
				acp.UpdateToolCall("write-1", acp.WithUpdateStatus(acp.ToolCallStatusInProgress), acp.WithUpdateRawInput(map[string]any{"path": "note.md", "content": "abc"})),
				acp.UpdateToolCall("write-1", acp.WithUpdateStatus(acp.ToolCallStatusInProgress), acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock("working output"))})),
				acp.UpdateToolCall("write-1", acp.WithUpdateStatus(acp.ToolCallStatusCompleted), acp.WithUpdateRawOutput("done")),
			)
			inputType := EventTypeToolInputSnapshot
			if binding.baseRuntimeID == RuntimeIDWorkBuddy {
				inputType = EventTypeToolUpdate
			}
			wantTypes := []EventType{EventTypeToolUse, inputType, inputType, inputType, EventTypeToolUpdate, EventTypeToolUpdate, EventTypeToolResult}
			for index, update := range updates {
				events, err := bridge.mapNotification(acp.SessionNotification{SessionId: "session", Update: update})
				if err != nil || len(events) != 1 || events[0].Type != wantTypes[index] {
					t.Fatalf("update[%d] events=%+v error=%v, want %s", index, events, err, wantTypes[index])
				}
				var payload map[string]any
				if err := json.Unmarshal(events[0].Data, &payload); err != nil {
					t.Fatal(err)
				}
				if payload["toolCallId"] != "write-1" {
					t.Fatalf("unstable tool identity: %+v", payload)
				}
				if index >= 1 && index <= 3 {
					if (payload["content"] != nil) != (binding.baseRuntimeID == RuntimeIDWorkBuddy) || payload["input"].(map[string]any)["content"] != []string{"a", "ab", "abc"}[index-1] {
						t.Fatalf("tool update lost cumulative input or content: %+v", payload)
					}
				}
				if index == 5 && payload["content"] == nil {
					t.Fatal("execution output was discarded")
				}
			}
		})
	}
}

// TestACPBridgeKeepsAmbiguousToolContent 验证正文、执行输出和差异不能当作参数快照。
func TestACPBridgeKeepsAmbiguousToolContent(t *testing.T) {
	textContent := []acp.ToolCallContent{acp.ToolContent(acp.TextBlock("tool body"))}
	for _, binding := range []acpRuntimeBinding{standardACPRuntimeBinding(), workBuddyACPRuntimeBinding()} {
		bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, binding, time.Second)
		updates := []acp.SessionUpdate{
			acp.UpdateToolCall("tool", acp.WithUpdateContent(textContent)),
			acp.UpdateToolCall("tool", acp.WithUpdateRawInput(map[string]any{"path": "a"}), acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolDiffContent("a", "new", "old")})),
			acp.UpdateToolCall("tool", acp.WithUpdateRawInput(map[string]any{"path": "a"}), acp.WithUpdateRawOutput("output"), acp.WithUpdateContent(textContent)),
		}
		updates = append(updates, acp.UpdateToolCall("tool", acp.WithUpdateRawInput(map[string]any{"path": "a"}), acp.WithUpdateContent(textContent)))
		for index, update := range updates {
			events, err := bridge.mapNotification(acp.SessionNotification{SessionId: "session", Update: update})
			if err != nil || len(events) != 1 || events[0].Type != EventTypeToolUpdate {
				t.Fatalf("binding=%s update=%d events=%+v error=%v", binding.baseRuntimeID, index, events, err)
			}
			var payload map[string]any
			if err := json.Unmarshal(events[0].Data, &payload); err != nil || payload["content"] == nil {
				t.Fatalf("tool body lost: payload=%+v error=%v", payload, err)
			}
		}
	}
}

// TestPublicEventSinkPassesToolInputSnapshot 验证 canonical 参数状态与工具进度不会被公开事件过滤器丢弃。
func TestPublicEventSinkPassesToolInputSnapshot(t *testing.T) {
	sink := &recordingSink{}
	for _, kind := range []EventType{EventTypeToolInputSnapshot, EventTypeToolUpdate} {
		if err := (publicEventSink{sink: sink}).Accept(context.Background(), Event{Type: kind}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.events) != 2 {
		t.Fatal("canonical tool input or progress was filtered")
	}
}

// TestConnectionTestSinkObservesToolInputSnapshot 验证缺少开始事件时参数与进度仍算连接测试中的工具调用。
func TestConnectionTestSinkObservesToolInputSnapshot(t *testing.T) {
	for _, kind := range []EventType{EventTypeToolInputSnapshot, EventTypeToolUpdate} {
		sink := &connectionTestSink{}
		if err := sink.Accept(context.Background(), Event{Type: kind}); err != nil {
			t.Fatal(err)
		}
		if _, usedTool := sink.result(); !usedTool {
			t.Fatal("connection test failed to observe tool activity")
		}
	}
}

// TestACPBridgeRejectsInternalToolsDuringTextGeneration 覆盖内部 canonical 工具事件不能绕过文本调用隔离。
func TestACPBridgeRejectsInternalToolsDuringTextGeneration(t *testing.T) {
	for _, kind := range []EventType{EventTypeToolUse, EventTypeToolInputSnapshot, EventTypeToolUpdate, EventTypeToolResult} {
		t.Run(string(kind), func(t *testing.T) {
			bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, standardACPRuntimeBinding(), time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			collector := &textCollector{cancel: cancel}
			bridge.textCollector = collector
			bridge.rejectTextInteraction = collector.reject
			if err := bridge.enqueue(acpDelivery{events: []Event{{Type: kind}}}); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
				t.Fatalf("enqueue tool = %v, want text isolation failure", err)
			}
			if ctx.Err() == nil || !collector.rejected || len(bridge.updates) != 0 || !errors.Is(bridge.currentFailure(), status_error.RuntimePolicyUnsupported) {
				t.Fatal("text tool rejection did not cancel generation before queue delivery")
			}
		})
	}
}

// TestACPBridgeInputSnapshotPreventsUnsafeReplay 覆盖缺少开始事件时也保守记录工具尝试，不推断文件已经写入。
func TestACPBridgeInputSnapshotPreventsUnsafeReplay(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, standardACPRuntimeBinding(), time.Second)
	events, err := bridge.mapNotification(acp.SessionNotification{SessionId: "session", Update: acp.UpdateToolCall("write", acp.WithUpdateKind(acp.ToolKindEdit), acp.WithUpdateRawInput(map[string]any{"path": "note.md"}))})
	if err != nil || len(events) != 1 || events[0].Type != EventTypeToolInputSnapshot {
		t.Fatalf("map input snapshot: events=%+v error=%v", events, err)
	}
	if !bridge.sideEffects.ToolUse || bridge.sideEffects.FileWrite {
		t.Fatalf("input-generation side effects = %+v, want tool attempt without inferred file write", bridge.sideEffects)
	}
}

// gatedACPBurstSink 控制交付进度以反复形成真实满队列背压。
type gatedACPBurstSink struct {
	// entered 记录已经开始交付的事件。
	entered chan Event
	// release 允许当前交付继续，关闭后允许队列排空。
	release chan struct{}
}

func (s *gatedACPBurstSink) Accept(ctx context.Context, event Event) error {
	s.entered <- event
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestACPBridgeSurvivesRepeatedFullQueueBackpressure 覆盖每次等待空位都释放消费者快照锁。
func TestACPBridgeSurvivesRepeatedFullQueueBackpressure(t *testing.T) {
	sink := &gatedACPBurstSink{entered: make(chan Event, acpUpdateQueueCapacity+8), release: make(chan struct{})}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, standardACPRuntimeBinding(), time.Second)
	event, err := newEvent(EventTypeTextDelta, map[string]any{"text": "chunk"})
	if err != nil {
		t.Fatal(err)
	}
	for range acpUpdateQueueCapacity {
		if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
			t.Fatal(err)
		}
	}
	go bridge.run(context.Background())
	bridge.activateDelivery()
	<-sink.entered
	if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
		t.Fatal(err)
	}
	for round := range 4 {
		accepted := make(chan error, 1)
		go func() { accepted <- bridge.enqueue(acpDelivery{events: []Event{event}}) }()
		select {
		case err := <-accepted:
			t.Fatalf("round %d full queue did not wait: %v", round, err)
		case <-time.After(10 * time.Millisecond):
		}
		sink.release <- struct{}{}
		if err := <-accepted; err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		<-sink.entered
	}
	close(sink.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(sink.entered) + 5; got != acpUpdateQueueCapacity+5 {
		t.Fatalf("delivered %d events, want %d", got, acpUpdateQueueCapacity+5)
	}
}

// failingACPDeliverySink 模拟已经启动后无法再保存运行事件的宿主。
type failingACPDeliverySink struct{}

func (*failingACPDeliverySink) Accept(_ context.Context, event Event) error {
	if event.Type == EventTypeTextDelta {
		return status_error.ChatLogError
	}
	return nil
}

func TestACPBackendCancelsPromptWhenEventDeliveryFails(t *testing.T) {
	backend := newACPBackend()
	agent := testCodexACPAgentWithCatalog()
	agent.blockPrompt = true
	agent.promptStarted = make(chan struct{})
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	request := testCodexACPRunRequest(t, "delivery-failed")
	request.Sink = &failingACPDeliverySink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan Result, 1)
	go func() { resultCh <- backend.Run(ctx, request) }()
	select {
	case result := <-resultCh:
		if result.Failure == nil || result.Failure.Code != FailureCodeEventDelivery {
			t.Fatalf("result = %#v", result)
		}
	case <-time.After(time.Second):
		cancel()
		<-resultCh
		t.Fatal("prompt kept running after its event consumer failed")
	}
}

func TestACPStartupFailurePreservesDeliveryCause(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	bridge.setFailure(status_error.RuntimeEventQueueOverflow)
	result := acpFailedBridgeResult(acpRunRequest{}, bridge, "prompt_write", context.Canceled)
	if result.Failure == nil || result.Failure.Code != FailureCodeEventDelivery || result.Failure.Message != status_error.RuntimeEventQueueOverflow.Error() {
		t.Fatalf("result = %#v, want original queue overflow instead of cancellation", result.Failure)
	}
}

func TestACPStartupFailurePreservesInactivityStage(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	bridge.setFailure(status_error.RuntimeInactivityTimeout)
	result := acpFailedBridgeResult(acpRunRequest{}, bridge, "prompt_write", context.Canceled)
	if result.Failure == nil || result.Failure.Code != FailureCodeInactivityTimeout || result.Failure.Stage != "inactivity_watchdog" {
		t.Fatalf("result = %#v, want inactivity watchdog cause", result.Failure)
	}
}

func TestACPBridgeBoundsEventDelivery(t *testing.T) {
	bridge := newACPBridge(
		"session",
		"invocation",
		&blockingUntilCanceledSink{},
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		20*time.Millisecond,
	)
	go bridge.run(context.Background())
	bridge.activateDelivery()
	event, err := newEvent(EventTypeTextDelta, map[string]any{"text": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
		t.Fatal(err)
	}
	closeContext, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := bridge.closeDelivery(closeContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delivery error = %v", err)
	}
}

func TestACPFailureCodeUsesStandardErrorsAndProviderEvidence(t *testing.T) {
	tests := []struct {
		// name 是失败映射场景。
		name string
		// err 是 ACP 或 Provider 返回的错误。
		err error
		// expected 是 NeuxBot 保持的稳定失败码。
		expected FailureCode
	}{
		{name: "cancelled", err: acp.NewRequestCancelled(nil), expected: FailureCodeContextCanceled},
		{name: "host queue overflow", err: status_error.RuntimeEventQueueOverflow, expected: FailureCodeEventDelivery},
		{name: "auth", err: acp.NewAuthRequired(nil), expected: FailureCodeAuthRequired},
		{name: "provider auth", err: acp.NewInternalError(map[string]any{"errorKind": "authentication_failed"}), expected: FailureCodeAuthRequired},
		{name: "provider billing", err: acp.NewInternalError(map[string]any{"errorKind": "billing_error"}), expected: FailureCodeQuotaExhausted},
		{name: "provider plan upgrade", err: acp.NewInternalError(map[string]any{"errorKind": "plan_upgrade_required"}), expected: FailureCodePlanUpgradeRequired},
		{name: "provider overloaded", err: acp.NewInternalError(map[string]any{"errorKind": "overloaded"}), expected: FailureCodeUpstreamUnavailable},
		{name: "provider unknown billing", err: &acp.RequestError{Code: -32603, Message: "API Error: 402 Insufficient Balance", Data: map[string]any{"errorKind": "unknown"}}, expected: FailureCodeQuotaExhausted},
		{name: "invalid params", err: acp.NewInvalidParams(nil), expected: FailureCodeInvalidRequest},
		{name: "session not found via invalid params", err: acp.NewInvalidParams(map[string]any{"message": "Session not found"}), expected: FailureCodeSessionNotFound},
		{name: "method", err: acp.NewMethodNotFound("session/resume"), expected: FailureCodeProtocolIncompatible},
		{name: "oversized frame", err: errors.New("claude frame too large"), expected: FailureCodeFrameTooLarge},
		{name: "process exit", err: errors.New("codex app-server exited with code 7"), expected: FailureCodeProcessExited},
		{name: "rate limit", err: errors.New("provider rate limit"), expected: FailureCodeRateLimited},
		{name: "upstream", err: errors.New("service unavailable"), expected: FailureCodeUpstreamUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := acpFailureCode(test.err); actual != test.expected {
				t.Fatalf("acpFailureCode() = %q, want %q", actual, test.expected)
			}
		})
	}
}

// burstPauseSink 只暂停第一条正文，模拟可恢复的短暂宿主背压。
type burstPauseSink struct {
	// count 记录已经完整交付的正文片段数。
	count int
	// delay 是首次正文处理的暂停窗口。
	delay time.Duration
}

func (s *burstPauseSink) Accept(ctx context.Context, event Event) error {
	if event.Type == EventTypeTextDelta {
		s.count++
		if s.count == 1 {
			select {
			case <-time.After(s.delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}
func TestACPBackendBackpressuresBurstBeyondTransportQueue(t *testing.T) {
	backend := newACPBackend()
	agent := testCodexACPAgentWithCatalog()
	for range 2200 {
		agent.scriptedUpdates = append(agent.scriptedUpdates, acp.UpdateAgentMessageText("chunk"))
	}
	agent.scriptedResponse = &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	req := testCodexACPRunRequest(t, "review-queues")
	req.Prepared.defaults.EventDelivery = time.Second
	sink := &burstPauseSink{delay: 200 * time.Millisecond}
	req.Sink = sink
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	begin := time.Now()
	result := backend.Run(ctx, req)
	t.Logf("elapsed=%v delivered=%d failure=%+v stop=%q", time.Since(begin), sink.count, result.Failure, result.StopReason)
	if result.Failure != nil {
		t.Fatalf("short burst failed before one-second delivery timeout: %+v; delivered=%d/2200", result.Failure, sink.count)
	}
	if sink.count != 2200 {
		t.Fatalf("delivered=%d/2200", sink.count)
	}
}

func TestACPResultPreservesPromptFailureBeforeCleanup(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	prompt := acpPromptResult{err: acp.NewAuthRequired(nil)}
	result := acpResult(acpRunRequest{}, "session", prompt, bridge, nil, errors.New("close failed after main failure"))
	if result.Failure == nil || result.Failure.Code != FailureCodeAuthRequired {
		t.Fatalf("primary auth failure masked by cleanup: %+v", result.Failure)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "session_close_failed" {
		t.Fatalf("cleanup diagnostic missing: %+v", result.Diagnostics)
	}
}

func TestACPBridgeBoundsQueuedPayloadBytes(t *testing.T) {
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), 20*time.Millisecond)
	event := Event{Type: EventTypeTextDelta, Data: make([]byte, 12*1024*1024)}
	if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
		t.Fatal(err)
	}
	if err := bridge.enqueue(acpDelivery{events: []Event{event}}); !errors.Is(err, status_error.RuntimeEventQueueOverflow) {
		t.Fatalf("byte budget overflow=%v", err)
	}
}

// sharedPressureAgent 在共用 Codex Agent 上为慢会话生成积压，同时保留一个正常会话。
type sharedPressureAgent struct {
	// fakeCodexACPAgent 提供真实 SDK 协议所需的标准会话与配置行为。
	*fakeCodexACPAgent
	// slowStarted 通知慢会话 Prompt 已经接纳。
	slowStarted chan struct{}
	// healthyStarted 通知正常会话 Prompt 已经接纳。
	healthyStarted chan struct{}
	// startBurst 让两次调用都进入 Prompt 后再开始持续输出。
	startBurst chan struct{}
}

func (a *sharedPressureAgent) Prompt(ctx context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
	if req.SessionId == "slow" {
		close(a.slowStarted)
		select {
		case <-a.startBurst:
		case <-ctx.Done():
			return acp.PromptResponse{}, ctx.Err()
		}
		for range 2200 {
			if err := a.connection.SessionUpdate(ctx, acp.SessionNotification{SessionId: req.SessionId, Update: acp.UpdateAgentMessageText("burst")}); err != nil {
				return acp.PromptResponse{}, err
			}
		}
	} else {
		close(a.healthyStarted)
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
			return acp.PromptResponse{}, ctx.Err()
		}
		if err := a.connection.SessionUpdate(ctx, acp.SessionNotification{SessionId: req.SessionId, Update: acp.UpdateAgentMessageText("healthy")}); err != nil {
			return acp.PromptResponse{}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// blockedRuntimeTextSink 允许 invocation_started 通过，只让正文交付耗尽期限。
type blockedRuntimeTextSink struct {
	// entered 在正文消费者已经启动后关闭，供取消回归同步。
	entered chan struct{}
}

func (s *blockedRuntimeTextSink) Accept(ctx context.Context, event Event) error {
	if event.Type != EventTypeTextDelta {
		return nil
	}
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestACPSharedAgentKeepsHealthySessionAfterSlowSessionStops(t *testing.T) {
	for _, scenario := range []string{"delivery timeout", "user cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			backend := newACPBackend()
			base := testCodexACPAgentWithCatalog()
			base.sessionIDs = []acp.SessionId{"slow", "healthy"}
			agent := &sharedPressureAgent{fakeCodexACPAgent: base, slowStarted: make(chan struct{}), healthyStarted: make(chan struct{}), startBurst: make(chan struct{})}
			factoryCalls := 0
			replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { factoryCalls++; return agent, nil })
			t.Cleanup(func() { _ = backend.Close(context.Background()) })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			slowCtx, cancelSlow := context.WithCancel(ctx)
			defer cancelSlow()
			slow := testCodexACPRunRequest(t, "shared-pressure")
			slow.Prepared.defaults.EventDelivery = 40 * time.Millisecond
			blocked := &blockedRuntimeTextSink{entered: make(chan struct{})}
			slow.Sink = blocked
			healthy := testCodexACPRunRequest(t, "shared-pressure")
			healthy.InvocationID = "healthy"
			healthy.Prepared.defaults.EventDelivery = time.Second
			healthy.Sink = &recordingSink{}
			slowCh := make(chan Result, 1)
			healthyCh := make(chan Result, 1)
			go func() { slowCh <- backend.Run(slowCtx, slow) }()
			<-agent.slowStarted
			go func() { healthyCh <- backend.Run(ctx, healthy) }()
			<-agent.healthyStarted
			close(agent.startBurst)
			if scenario == "user cancellation" {
				<-blocked.entered
				cancelSlow()
			}
			slowResult := <-slowCh
			healthyResult := <-healthyCh
			if slowResult.Failure == nil {
				t.Fatal("slow session unexpectedly succeeded")
			}
			if healthyResult.Failure != nil {
				t.Fatalf("healthy session failed with slow session: %+v", healthyResult.Failure)
			}
			if factoryCalls != 1 {
				t.Fatalf("factory calls=%d, want shared Codex process", factoryCalls)
			}
		})
	}
}

func TestACPClaudeOwnsEachSessionAndReusesItAcrossInvocations(t *testing.T) {
	backend := newACPBackend()
	agents := []*fakeCodexACPAgent{}
	replaceACPAgentFactory(backend, RuntimeIDClaude, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		agent := testCodexACPAgentWithCatalog()
		agent.sessionID = acp.SessionId(fmt.Sprintf("claude-%d", len(agents)+1))
		agent.simplePrompt = true
		agents = append(agents, agent)
		return agent, nil
	})
	defer backend.Close(context.Background())
	firstReq := matrixACPRunRequest(t, RuntimeIDClaude, SessionDirective{Mode: SessionModeNew})
	firstReq.InvocationID = "first-chat"
	first := backend.Run(context.Background(), firstReq)
	secondReq := matrixACPRunRequest(t, RuntimeIDClaude, SessionDirective{Mode: SessionModeNew})
	secondReq.InvocationID = "second-chat"
	second := backend.Run(context.Background(), secondReq)
	if first.Failure != nil || second.Failure != nil {
		t.Fatalf("first=%+v second=%+v", first.Failure, second.Failure)
	}
	if len(agents) != 2 || first.Session.OutputID == second.Session.OutputID {
		t.Fatalf("independent chats share connection: factories=%d first=%q second=%q", len(agents), first.Session.OutputID, second.Session.OutputID)
	}
	firstReq.InvocationID = "first-chat-next"
	firstReq.Request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	resumed := backend.Run(context.Background(), firstReq)
	if resumed.Failure != nil || len(agents) != 2 {
		t.Fatalf("warm resume created new process: result=%+v factories=%d", resumed.Failure, len(agents))
	}
	if agents[0].promptCalls != 2 || agents[0].resumeCalls != 0 {
		t.Fatalf("warm session prompts=%d resumes=%d", agents[0].promptCalls, agents[0].resumeCalls)
	}
}

func TestACPBridgeDrainTimeoutCancelsCurrentDelivery(t *testing.T) {
	sink := &blockedRuntimeTextSink{entered: make(chan struct{})}
	bridge := newACPBridge("session", "invocation", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	go bridge.run(context.Background())
	bridge.activateDelivery()
	if err := bridge.enqueue(acpDelivery{events: []Event{{Type: EventTypeTextDelta}}}); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err == nil {
		t.Fatal("drain unexpectedly succeeded")
	}
	select {
	case <-bridge.done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drain timeout left cooperative consumer running")
	}
}

func TestACPUnregisterDoesNotBlockOtherSessionRoutes(t *testing.T) {
	client := newACPClient()
	slow := newACPBridge("slow", "slow-call", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	healthy := newACPBridge("healthy", "healthy-call", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	if err := client.register(slow); err != nil {
		t.Fatal(err)
	}
	if err := client.register(healthy); err != nil {
		t.Fatal(err)
	}
	slow.enqueueMutex.Lock()
	locked := true
	defer func() {
		if locked {
			slow.enqueueMutex.Unlock()
		}
	}()
	slow.routeMutex.Lock()
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "slow", Update: acp.UpdateAgentMessageText("waiting")})
	}()
	// 精确停在引用接纳屏障，确认 SessionUpdate 已取得共享路由读锁。
	deadline := time.Now().Add(time.Second)
	for client.mutex.TryLock() {
		client.mutex.Unlock()
		if time.Now().After(deadline) {
			slow.routeMutex.Unlock()
			t.Fatal("notification did not enter route")
		}
		time.Sleep(time.Millisecond)
	}
	slow.routeMutex.Unlock()
	unregistered := make(chan struct{})
	go func() { client.unregister(slow); close(unregistered) }()
	deadline = time.Now().Add(100 * time.Millisecond)
	for {
		slow.routeMutex.Lock()
		closed := slow.routeClosed
		slow.routeMutex.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unregister kept global route lock behind blocked enqueue")
		}
		time.Sleep(time.Millisecond)
	}
	other := make(chan error, 1)
	go func() {
		other <- client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "healthy", Update: acp.UpdateAgentMessageText("healthy")})
	}()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("one closing session blocked a healthy route")
	}
	// 引用仍在入队阶段时注销不能返回，防止随后关闭 channel 与发送并发。
	select {
	case <-unregistered:
		t.Fatal("unregister returned before acquired notification reference finished")
	default:
	}
	slow.enqueueMutex.Unlock()
	locked = false
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquired notification did not finish")
	}
	select {
	case <-unregistered:
	case <-time.After(time.Second):
		t.Fatal("unregister did not finish after reference release")
	}
	close(slow.updates)
}

func TestACPBridgePreservesMetadataProgressWithRawInput(t *testing.T) {
	for _, key := range []string{"terminal_output", "terminal_output_delta", "mcp_output_delta", "progress"} {
		t.Run(key, func(t *testing.T) {
			sink := &recordingSink{}
			bridge := newACPBridge("s", "i", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
			for _, chunk := range []string{"first", "second"} {
				update := acp.UpdateToolCall("tool", acp.WithUpdateRawInput(map[string]any{"command": "echo"}))
				update.ToolCallUpdate.Meta = map[string]any{key: map[string]any{"data": chunk}}
				if err := bridge.enqueue(acpDelivery{notification: &acp.SessionNotification{SessionId: "s", Update: update}}); err != nil {
					t.Fatal(err)
				}
			}
			if len(bridge.updates) != 2 {
				t.Fatalf("%s output collapsed to %d input snapshots", key, len(bridge.updates))
			}
			go bridge.run(context.Background())
			bridge.activateDelivery()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := bridge.closeDelivery(ctx); err != nil {
				t.Fatal(err)
			}
			if len(sink.events) != 2 {
				t.Fatalf("%s output delivery count=%d", key, len(sink.events))
			}
			for i, want := range []string{"first", "second"} {
				if sink.events[i].Type != EventTypeToolUpdate {
					t.Fatalf("%s mapped as %s", key, sink.events[i].Type)
				}
				var payload map[string]any
				if err := json.Unmarshal(sink.events[i].Data, &payload); err != nil {
					t.Fatal(err)
				}
				meta := payload["_meta"].(map[string]any)
				if meta[key].(map[string]any)["data"] != want {
					t.Fatalf("%s output order/content lost: %+v", key, payload)
				}
			}

		})
	}
}

func TestACPResultPreservesCauseOrderAcrossDrain(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel_first_%v", cancelFirst), func(t *testing.T) {
			bridge := newACPBridge("s", "i", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
			if cancelFirst {
				bridge.recordPrimaryCause(status_error.OperationCanceled, "prompt")
				bridge.setFailure(context.DeadlineExceeded)
			} else {
				bridge.setFailure(status_error.ChatLogError)
				bridge.recordPrimaryCause(status_error.OperationCanceled, "prompt")
			}
			result := acpResult(acpRunRequest{}, "s", acpPromptResult{err: status_error.OperationCanceled}, bridge, bridge.currentFailure(), nil)
			want := FailureCodeEventDelivery
			if cancelFirst {
				want = FailureCodeContextCanceled
			}
			if result.Failure == nil || result.Failure.Code != want {
				t.Fatalf("first known cause was overwritten: %+v", result.Failure)
			}
			if cancelFirst && (len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "event_delivery_after_termination") {
				t.Fatalf("late delivery failure lost diagnostic: %+v", result.Diagnostics)
			}
		})
	}
}

func TestACPBridgeCancellationStillDrainsAcceptedText(t *testing.T) {
	sink := &recordingSink{}
	bridge := newACPBridge("s", "i", sink, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	for _, text := range []string{"first", "second"} {
		event, err := newEvent(EventTypeTextDelta, map[string]any{"text": text})
		if err != nil {
			t.Fatal(err)
		}
		if err = bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
			t.Fatal(err)
		}
	}
	bridge.recordPrimaryCause(status_error.OperationCanceled, "prompt")
	go bridge.run(context.Background())
	bridge.activateDelivery()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.closeDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("cancel cause discarded accepted text: %d events", len(sink.events))
	}
	result := acpResult(acpRunRequest{}, "s", acpPromptResult{err: status_error.OperationCanceled}, bridge, nil, nil)
	if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
		t.Fatalf("cancel primary cause=%+v", result.Failure)
	}
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
)

// runEventBudgetTurn 在真实 Session 中交付可控原生事实，外部 CLI 是唯一替身。
func runEventBudgetTurn(t *testing.T, limits native.Limits, produce func(context.Context, native.EventSink) error, observer turnObserver) turnResult {
	t.Helper()
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
			if err := sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted}); err != nil {
				return native.TurnResult{Status: native.StatusFailed}
			}
			if err := produce(ctx, sink); err != nil {
				return native.TurnResult{Status: native.StatusFailed}
			}
			if err := sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "final-answer", Phase: native.PhaseFinal, Delta: "done", Complete: true}}); err != nil {
				return native.TurnResult{Status: native.StatusFailed}
			}
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	runtime := started(t, driver, func(c *testConfig) { c.Grants.AllowAllTools = true })
	input := spec()
	input.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowAll}
	input.Limits = limits
	return runTurn(runtime, context.Background(), input, turnOptions{Observer: observer})
}

// TestStreamingPayloadDoesNotConsumeDiscreteEventCount 防止合法小包正文、思考和两种原生参数形式再按 token 次数中止。
func TestStreamingPayloadDoesNotConsumeDiscreteEventCount(t *testing.T) {
	const fragments = 17000
	for _, kind := range []string{"thinking", "assistant", "assistant-with-empty-text", "input-snapshot", "input-with-content", "input-with-empty-text", "tool-content"} {
		t.Run(kind, func(t *testing.T) {
			delivered := 0
			result := runEventBudgetTurn(t, native.Limits{}, func(ctx context.Context, sink native.EventSink) error {
				for i := 1; i <= fragments; i++ {
					var event native.Event
					switch kind {
					case "thinking":
						event = native.Event{Kind: native.EventThinking, Text: "x"}
					case "assistant", "assistant-with-empty-text":
						event = native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "commentary", Phase: native.PhaseCommentary, Delta: "x"}}
						if kind == "assistant-with-empty-text" {
							event.Message.Content = []native.ContentBlock{{Type: "text"}}
						}
					default:
						tool := &native.ToolEvent{ID: "write", Name: "write", Update: i > 1, Status: "in_progress", Input: []byte(`{"content":"` + strings.Repeat("x", i) + `"}`)}
						if kind == "input-with-content" || kind == "input-with-empty-text" || kind == "tool-content" {
							tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
							tool.Text = "x"
							if kind == "input-with-empty-text" {
								tool.Content = append(tool.Content, native.ContentBlock{Type: "text"})
							}
						}
						if kind == "tool-content" {
							tool.Input = nil
						}
						event = native.Event{Kind: native.EventTool, Tool: tool}
					}
					if err := sink.Accept(ctx, event); err != nil {
						return err
					}
				}
				return nil
			}, observerFunc(func(_ context.Context, e observedEvent) error {
				if e.NativeEvent.Message != nil && e.NativeEvent.Message.MessageID == "final-answer" {
					return nil
				}
				if e.NativeEvent.Kind == native.EventPromptAccepted {
					return nil
				}
				delivered++
				if tool := e.NativeEvent.Tool; tool != nil && tool.Input != nil {
					var input struct {
						Content string `json:"content"` // Content 是按顺序累积的参数正文。
					}
					if err := json.Unmarshal(tool.Input, &input); err != nil {
						return err
					}
					if len(input.Content) != delivered {
						t.Fatalf("snapshot reordered at %d: %d", delivered, len(input.Content))
					}
					tool.Input[0] = '!'
				}
				return nil
			}))
			if result.Status != native.StatusCompleted || result.Output.Text != "done" || delivered != fragments {
				t.Fatalf("stream stopped: delivered=%d result=%+v", delivered, result)
			}
		})
	}
}

// TestCodeBuddyArgumentFragmentDistributionPreservesAllUpdates 用去内容化的真实碎片长度重放多工具参数，验证每个增量和最终快照完整有序。
func TestCodeBuddyArgumentFragmentDistributionPreservesAllUpdates(t *testing.T) {
	data, err := os.ReadFile("testdata/codebuddy_argument_fragments.json")
	if err != nil {
		t.Fatal(err)
	}
	var runs [][][2]int
	if err := json.Unmarshal(data, &runs); err != nil {
		t.Fatal(err)
	}
	observedUpdates, observedBytes, completed := 0, 0, 0
	expectedUpdates, expectedBytes := 0, 0
	for _, tool := range runs {
		for _, run := range tool {
			expectedUpdates += run[1]
			expectedBytes += run[0] * run[1]
		}
	}
	result := runEventBudgetTurn(t, native.Limits{}, func(ctx context.Context, sink native.EventSink) error {
		for i, toolRuns := range runs {
			id := fmt.Sprintf("tool-%d", i)
			body := ""
			if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: id, Name: "write", Status: "in_progress", Input: []byte(`{}`)}}); err != nil {
				return err
			}
			for _, run := range toolRuns {
				for j := 0; j < run[1]; j++ {
					fragment := strings.Repeat("x", run[0])
					body += fragment
					input := []byte(`{"content":"` + body + `"}`)
					if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: id, Update: true, Input: input, Text: fragment, Content: []native.ContentBlock{{Type: "text", Text: fragment}}}}); err != nil {
						return err
					}
				}
			}
			if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: id, Update: true, Status: "completed", Output: []byte(`{"saved":true}`)}}); err != nil {
				return err
			}
		}
		return nil
	}, observerFunc(func(_ context.Context, e observedEvent) error {
		tool := e.NativeEvent.Tool
		if tool == nil {
			return nil
		}
		if tool.Status == "completed" {
			completed++
			return nil
		}
		if len(tool.Content) == 0 {
			return nil
		}
		observedUpdates++
		observedBytes += len(tool.Content[0].Text)
		if tool.ID != fmt.Sprintf("tool-%d", completed) {
			t.Fatalf("tool lifecycle reordered: completed=%d tool=%s", completed, tool.ID)
		}
		var input struct {
			Content string `json:"content"` // Content 是按顺序累积的参数正文。
		}
		if err := json.Unmarshal(tool.Input, &input); err != nil {
			return err
		}
		if !strings.HasSuffix(input.Content, tool.Content[0].Text) {
			t.Fatal("snapshot lost its fragment")
		}
		return nil
	}))
	if result.Status != native.StatusCompleted || observedUpdates != expectedUpdates || observedBytes != expectedBytes || completed != len(runs) {
		t.Fatalf("replay changed: updates=%d/%d bytes=%d/%d completed=%d/%d fault=%v", observedUpdates, expectedUpdates, observedBytes, expectedBytes, completed, len(runs), result.Fault)
	}
	if observedUpdates <= 16384 {
		t.Fatal("fixture no longer exercises a stream above the old raw event limit")
	}
	t.Logf("replayed %d tools, %d original fragment lengths, %d parameter bytes", completed, observedUpdates, observedBytes)
}

// TestDiscreteEventFloodStillStops 保留独立身份、真实状态变化以及没有新增载荷的重复通知保护。
func TestDiscreteEventFloodStillStops(t *testing.T) {
	for _, kind := range []string{"tools", "status", "locations", "location-clear", "title", "kind", "messages", "phase", "structured-content", "empty-thinking", "empty-message", "duplicate-input", "duplicate-status", "empty-tool-content", "usage"} {
		t.Run(kind, func(t *testing.T) {
			delivered := 0
			result := runEventBudgetTurn(t, native.Limits{MaxEvents: 16}, func(ctx context.Context, sink native.EventSink) error {
				for i := 0; i < 100; i++ {
					tool := &native.ToolEvent{ID: "tool", Name: "write", Update: i > 0, Input: []byte(`{"content":"x"}`)}
					event := native.Event{Kind: native.EventTool, Tool: tool}
					switch kind {
					case "tools":
						tool.ID = fmt.Sprint(i)
						tool.Update = false
					case "status":
						tool.Status = []string{"pending", "in_progress"}[i%2]
						tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
					case "locations":
						tool.Locations = []native.Location{{Path: fmt.Sprint(i)}}
						tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
					case "location-clear":
						tool.Locations = []native.Location{}
						if i%2 == 0 {
							tool.Locations = []native.Location{{Path: "file"}}
						}
						tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
					case "kind":
						tool.Kind = []string{"read", "edit"}[i%2]
						tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
					case "phase":
						event = native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{Phase: []native.MessagePhase{native.PhaseCommentary, native.PhaseFinal}[i%2], Delta: "x"}}
					case "structured-content":
						tool.Content = []native.ContentBlock{{Type: "diff", Diff: &native.Diff{Path: "file", NewText: "x"}}}
					case "title":
						tool.Title = fmt.Sprint(i)
						tool.Content = []native.ContentBlock{{Type: "text", Text: "x"}}
					case "messages":
						event = native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: fmt.Sprint(i), Phase: native.PhaseCommentary, Delta: "x"}}
					case "empty-thinking":
						event = native.Event{Kind: native.EventThinking}
					case "empty-message":
						event = native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "empty"}}
					case "duplicate-status":
						tool.Input = nil
						tool.Status = "in_progress"
					case "empty-tool-content":
						tool.Input = nil
						tool.Content = []native.ContentBlock{{Type: "text"}}
					case "usage":
						event = native.Event{Kind: native.EventUsage, Usage: &native.Usage{Known: true, InputTokens: int64(i)}}
					}
					if err := sink.Accept(ctx, event); err != nil {
						return err
					}
				}
				return nil
			}, observerFunc(func(context.Context, observedEvent) error { delivered++; return nil }))
			requireCode(t, result.Fault, native.FaultLimit)
			if delivered != 16 {
				t.Fatalf("flood escaped count: delivered=%d fault=%v", delivered, result.Fault)
			}
			product := chatResult(SessionPlan{}, SessionDirective{}, result, &chatObserver{})
			if product.Failure == nil || string(product.Failure.Code) != "EVENT_BUDGET_EXCEEDED" {
				t.Fatalf("count misclassified: %+v", product.Failure)
			}
		})
	}
}

// TestStreamingBytesAndRawNotificationCostStayBounded 新分层不能抵扣真实正文或每个原始通知的固定处理开销。
func TestStreamingBytesAndRawNotificationCostStayBounded(t *testing.T) {
	for _, kind := range []string{"single", "cumulative", "raw-overhead"} {
		t.Run(kind, func(t *testing.T) {
			limits := native.Limits{MaxOutputBytes: 64, MaxEventBytes: 512, MaxTotalEventBytes: 1024, MaxEvents: 1000}
			result := runEventBudgetTurn(t, limits, func(ctx context.Context, sink native.EventSink) error {
				text := strings.Repeat("x", 100)
				if kind == "single" {
					text = strings.Repeat("x", 600)
				} else if kind == "raw-overhead" {
					text = "x"
				}
				for i := 0; i < 100; i++ {
					if err := sink.Accept(ctx, native.Event{Kind: native.EventThinking, Text: text}); err != nil {
						return err
					}
				}
				return nil
			}, nil)
			requireCode(t, result.Fault, native.FaultLimit)
			product := chatResult(SessionPlan{}, SessionDirective{}, result, &chatObserver{})
			want := "EVENT_BUDGET_EXCEEDED"
			if kind == "single" {
				want = "FRAME_TOO_LARGE"
			}
			if product.Failure == nil || string(product.Failure.Code) != want {
				t.Fatalf("wrong byte limit classification: %+v", product.Failure)
			}
		})
	}
}

// TestCompletionNotificationsRemainDiscrete 防止终态通知夹带正文后伪装成无限小包流。
func TestCompletionNotificationsRemainDiscrete(t *testing.T) {
	for _, kind := range []string{"tool", "message-without-id"} {
		t.Run(kind, func(t *testing.T) {
			result := runEventBudgetTurn(t, native.Limits{MaxEvents: 16}, func(ctx context.Context, sink native.EventSink) error {
				for i := 0; i < 100; i++ {
					event := native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Name: "write", Update: i > 0, Status: "completed", Content: []native.ContentBlock{{Type: "text", Text: "x"}}}}
					if kind == "message-without-id" {
						event = native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{Phase: native.PhaseFinal, Complete: true, Delta: "x"}}
					}
					if err := sink.Accept(ctx, event); err != nil {
						return err
					}
				}
				return nil
			}, nil)
			requireCode(t, result.Fault, native.FaultLimit)
			if result.Fault.Limit != native.LimitEventCount {
				t.Fatalf("completion flood lost its count limit: %v", result.Fault)
			}
		})
	}
}

// TestInterleavedToolStreamsKeepPrivateState 验证并行工具、重复元数据及观察者修改副本不改变次序、参数和真正终态。
func TestInterleavedToolStreamsKeepPrivateState(t *testing.T) {
	const updates = 10000
	latest := map[string]int{}
	completions, sequence := 0, uint64(0)
	result := runEventBudgetTurn(t, native.Limits{MaxEvents: 6}, func(ctx context.Context, sink native.EventSink) error {
		for _, id := range []string{"a", "b"} {
			line := 1
			if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: id, Name: "write", Title: "Write", Kind: "edit", Status: "in_progress", Locations: []native.Location{{Path: id, Line: &line}}}}); err != nil {
				return err
			}
		}
		for i := 1; i <= updates; i++ {
			for _, id := range []string{"a", "b"} {
				line := 1
				tool := &native.ToolEvent{ID: id, Update: true, Title: "Write", Kind: "edit", Status: "in_progress", Locations: []native.Location{{Path: id, Line: &line}}, Input: []byte(`{"content":"` + strings.Repeat("x", i) + `"}`), Content: []native.ContentBlock{{Type: "text", Text: "x"}}}
				if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: tool}); err != nil {
					return err
				}
			}
		}
		for _, id := range []string{"a", "b"} {
			if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: id, Update: true, Status: "completed", Output: []byte(`{"saved":true}`)}}); err != nil {
				return err
			}
		}
		return nil
	}, observerFunc(func(_ context.Context, e observedEvent) error {
		sequence++
		if e.Sequence != sequence {
			t.Fatalf("event sequence changed: %d/%d", e.Sequence, sequence)
		}
		tool := e.NativeEvent.Tool
		if tool == nil {
			return nil
		}
		if tool.Status == "completed" {
			completions++
			return nil
		}
		if tool.Input != nil {
			var input struct {
				Content string `json:"content"` // Content 是按顺序累积的参数正文。
			}
			if err := json.Unmarshal(tool.Input, &input); err != nil {
				return err
			}
			latest[tool.ID]++
			if len(input.Content) != latest[tool.ID] {
				t.Fatal("interleaved input changed")
			}
			tool.Input[0] = '!'
		}
		tool.Title = "observer-title"
		tool.Status = "completed"
		if len(tool.Locations) > 0 {
			tool.Locations[0].Path = "observer-path"
			*tool.Locations[0].Line = 99
		}
		return nil
	}))
	if result.Status != native.StatusCompleted || result.Output.Text != "done" || latest["a"] != updates || latest["b"] != updates || completions != 2 {
		t.Fatalf("stream boundaries changed: latest=%v completions=%d result=%+v", latest, completions, result)
	}
}

// TestInteractiveInputStillSharesDiscreteBudget 请求和回答沿用同一离散预算，长思考不会抢占人工输入额度。
func TestInteractiveInputStillSharesDiscreteBudget(t *testing.T) {
	answered, handled, thinking := 0, 0, 0
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
			_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			for i := 0; i < 17000; i++ {
				if err := sink.Accept(ctx, native.Event{Kind: native.EventThinking, Text: "x"}); err != nil {
					return native.TurnResult{Status: native.StatusFailed}
				}
			}
			for i := 0; i < 10; i++ {
				response, err := sink.Interact(ctx, native.InteractionRequest{ID: fmt.Sprint(i), Kind: native.InteractionInput, Prompt: "choose"})
				if err != nil {
					return native.TurnResult{Status: native.StatusFailed}
				}
				if response.Action == native.InteractionAccept {
					answered++
				}
			}
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	runtime := started(t, driver, nil)
	input := spec()
	input.Policies.Interaction = InteractionInteractive
	input.Limits.MaxEvents = 6
	result := runTurn(runtime, context.Background(), input, turnOptions{Observer: observerFunc(func(_ context.Context, e observedEvent) error {
		if e.NativeEvent.Kind == native.EventThinking {
			thinking++
		}
		return nil
	}), InteractionHandler: interactionFunc(func(_ context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
		handled++
		return native.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: native.InteractionAccept, Content: map[string]any{"answer": "yes"}}, nil
	})})
	requireCode(t, result.Fault, native.FaultLimit)
	if result.Fault.Limit != native.LimitEventCount || result.Fault.Stage != "interaction" || thinking != 17000 || answered != 2 || handled != 3 {
		t.Fatalf("interaction budget changed: thinking=%d answered=%d handled=%d result=%+v", thinking, answered, handled, result)
	}
}

// TestBudgetFailureClassificationUsesTypedReason 所有 Runtime 的分类只使用结构化原因，不猜测错误文本。
func TestBudgetFailureClassificationUsesTypedReason(t *testing.T) {
	for _, runtimeID := range []string{"codex", "claude", "kimi", "cursor", "pi", "opencode", "gemini", "grok", "workbuddy", "qoder", "qoder-cn", "custom"} {
		for _, sample := range []struct {
			kind       native.LimitKind // kind 是真实预算维度。
			misleading string           // misleading 是刻意与维度相反的技术说明。
			want       string           // want 是稳定产品失败码。
		}{{native.LimitEventCount, "frame too large", "EVENT_BUDGET_EXCEEDED"}, {native.LimitTotalEventBytes, "opaque upstream text", "EVENT_BUDGET_EXCEEDED"}, {native.LimitSingleEventBytes, "event count", "FRAME_TOO_LARGE"}} {
			f := &native.Fault{Code: native.FaultLimit, Limit: sample.kind, Stage: "event", Message: sample.misleading}
			if got := ClassifyFailure(runtimeID, f); string(got) != sample.want {
				t.Fatalf("%s/%s classified %s", runtimeID, sample.kind, got)
			}
		}
	}
}

// TestCodexTerminalMetadataUsesStreamingBudget 通过真实 ACP/native/Session 链验证17KB终端小包保留正文，空元数据仍按洪泛中止。
func TestCodexTerminalMetadataUsesStreamingBudget(t *testing.T) {
	const fragments = 17000
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint("empty=", empty), func(t *testing.T) {
			agent := newNativeIntegrationAgent()
			agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.StartToolCall("command", "Run", sdk.WithStartStatus(sdk.ToolCallStatusInProgress))}); err != nil {
					return sdk.PromptResponse{}, err
				}
				data := "x"
				if empty {
					data = ""
				}
				for i := 0; i < fragments; i++ {
					update := sdk.SessionUpdate{ToolCallUpdate: &sdk.SessionToolCallUpdate{ToolCallId: "command", Meta: map[string]any{"terminal_output_delta": map[string]any{"terminal_id": "command", "data": data}}}}
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				updates := []sdk.SessionUpdate{
					{ToolCallUpdate: &sdk.SessionToolCallUpdate{ToolCallId: "command", Status: sdk.Ptr(sdk.ToolCallStatusCompleted), RawOutput: map[string]any{"formatted_output": strings.Repeat("x", fragments)}}},
					{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: sdk.Ptr("answer"), Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "source": "native_phase", "complete": true}}, Content: sdk.TextBlock("done")}},
				}
				for _, update := range updates {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return agent, nil }, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			manager.Start(ctx)
			delivered, terminal := 0, 0
			sequence := uint64(0)
			result := runNativeIntegration(t, manager, ctx, nativeIntegrationPlan(t.TempDir()), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "read terminal output"}}, SessionResources{}, resourcePreparation{}, observerFunc(func(_ context.Context, e observedEvent) error {
				sequence++
				if sequence != e.Sequence {
					t.Fatal("native event sequence changed")
				}
				if tool := e.NativeEvent.Tool; tool != nil {
					if tool.Status == "completed" {
						terminal++
						if len(tool.Content) != 0 {
							t.Fatal("complete output replayed as delta")
						}
					}
					if len(tool.Content) > 0 {
						if len(tool.Content) != 1 || tool.Content[0].Text != "x" {
							t.Fatal("terminal bytes changed")
						}
						delivered++
					}
				}
				return nil
			}), true)
			if empty {
				requireCode(t, result.Fault, native.FaultLimit)
				if result.Fault.Limit != native.LimitEventCount || delivered != 0 || terminal != 0 {
					t.Fatalf("empty stream escaped budget: delivered=%d terminal=%d result=%+v", delivered, terminal, result)
				}
			} else if result.Status != native.StatusCompleted || result.Output.Text != "done" || delivered != fragments || terminal != 1 {
				t.Fatalf("terminal stream failed: delivered=%d terminal=%d result=%+v", delivered, terminal, result)
			}
		})
	}
}

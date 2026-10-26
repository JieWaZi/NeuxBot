package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// commonBindingAgent 只提供绑定所需的标准 ACP 能力前提，不模拟任何厂商的原生协议。
type commonBindingAgent struct {
	// fakeCodexACPAgent 复用标准 SDK 会话、通知和取消行为，实际 factory 不启动 CLI。
	*fakeCodexACPAgent
}

// NewSession 提供通用契约测试所需模式，实际选择仍经过每个真实 Binding 的规则。
func (a *commonBindingAgent) NewSession(ctx context.Context, req acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	response, err := a.fakeCodexACPAgent.NewSession(ctx, req)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "yolo"}, {Id: "bypassPermissions"}, {Id: "agent"}, {Id: "agent-full-access"}, {Id: "read-only"}, {Id: "plan"}}}
	return response, err
}

// allRegisteredBindingIDs 使公共契约覆盖集合随注册表变化，避免只覆盖既有两个验收矩阵成员。
func allRegisteredBindingIDs(t *testing.T) []string {
	t.Helper()
	bindings := defaultACPRuntimeBindings()
	registry, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != len(registry) {
		t.Fatalf("binding/registry coverage differs: %d/%d", len(bindings), len(registry))
	}
	ids := make([]string, 0, len(bindings))
	for id := range registry {
		if _, ok := bindings[id]; !ok {
			t.Fatalf("registered CLI has no common contract coverage: %s", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func commonBindingRequest(t *testing.T, id string, sink EventSink) acpRunRequest {
	t.Helper()
	request := testCodexACPRunRequest(t, "common-events-"+id)
	request.Prepared.definition.id = id
	request.Prepared.definition.baseRuntimeID = id
	request.Request.Target.RuntimeID = id
	request.Sink = sink
	return request
}
func closeCommonBindingBackend(t *testing.T, backend *acpBackend, agent *fakeCodexACPAgent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := backend.Close(ctx); err != nil {
		t.Fatal(err)
	}
	agent.mutex.Lock()
	closed := agent.closed
	agent.mutex.Unlock()
	if !closed {
		t.Fatal("binding did not release its fake Agent")
	}
}

// TestACPRegisteredBindingsPreserveCommonToolEventContract 仅验证全部注册 Binding 的公共交付契约，不能替代原生 CLI 验收。
func TestACPRegisteredBindingsPreserveCommonToolEventContract(t *testing.T) {
	for _, id := range allRegisteredBindingIDs(t) {
		t.Run(id, func(t *testing.T) {
			backend := newACPBackend()
			base := &fakeCodexACPAgent{sessionID: acp.SessionId(id + "-common"), selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId)}
			base.scriptedUpdates = []acp.SessionUpdate{
				acp.StartToolCall("same-tool", "Write", acp.WithStartKind(acp.ToolKindEdit), acp.WithStartStatus(acp.ToolCallStatusPending)),
				acp.UpdateToolCall("same-tool", acp.WithUpdateRawInput(map[string]any{"path": "note.txt", "content": "first"})),
				acp.UpdateToolCall("same-tool", acp.WithUpdateRawInput(map[string]any{"path": "note.txt", "content": "second"})),
				acp.UpdateToolCall("same-tool", acp.WithUpdateStatus(acp.ToolCallStatusInProgress), acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock("progress-one"))})),
				acp.UpdateToolCall("same-tool", acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock("progress-two"))})),
				acp.UpdateToolCall("same-tool", acp.WithUpdateStatus(acp.ToolCallStatusCompleted), acp.WithUpdateKind(acp.ToolKindEdit), acp.WithUpdateRawOutput("complete")),
				acp.UpdateAgentMessageText("done"),
			}
			base.scriptedResponse = &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}
			replaceACPAgentFactory(backend, id, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
				return &commonBindingAgent{fakeCodexACPAgent: base}, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })
			sink := &recordingSink{}
			result := backend.Run(context.Background(), commonBindingRequest(t, id, sink))
			if result.Failure != nil || result.StopReason != "end_turn" {
				t.Fatalf("common contract failed: %+v", result)
			}
			starts, results, snapshots := 0, 0, 0
			lastInput := ""
			progress := []string{}
			terminalSeen := false
			finalTextSeen := false
			for _, event := range sink.events {
				switch event.Type {
				case EventTypeToolUse, EventTypeToolInputSnapshot, EventTypeToolUpdate, EventTypeToolResult:
					var payload map[string]any
					if err := json.Unmarshal(event.Data, &payload); err != nil {
						t.Fatal(err)
					}
					if payload["toolCallId"] != "same-tool" {
						t.Fatalf("unstable tool identity: %+v", payload)
					}
					switch event.Type {
					case EventTypeToolUse:
						starts++
					case EventTypeToolInputSnapshot:
						snapshots++
						lastInput = payload["input"].(map[string]any)["content"].(string)
					case EventTypeToolUpdate:
						if terminalSeen {
							t.Fatal("progress arrived after terminal")
						}
						content := payload["content"].([]any)
						block := content[0].(map[string]any)["content"].(map[string]any)
						progress = append(progress, block["text"].(string))
					case EventTypeToolResult:
						results++
						terminalSeen = true
						if payload["rawOutput"] != "complete" {
							t.Fatalf("terminal output lost: %+v", payload)
						}
					}
				case EventTypeTextDelta:
					if !terminalSeen {
						t.Fatal("final text crossed tool terminal boundary")
					}
					var payload struct {
						// Text 是工具终态之后应交付的最终正文。
						Text string `json:"text"`
					}
					if err := json.Unmarshal(event.Data, &payload); err != nil {
						t.Fatal(err)
					}
					if payload.Text != "done" {
						t.Fatalf("final text changed: %q", payload.Text)
					}
					finalTextSeen = true
				}
			}
			if starts != 1 || results != 1 || snapshots < 1 || lastInput != "second" || fmt.Sprint(progress) != "[progress-one progress-two]" || !finalTextSeen {
				t.Fatalf("starts=%d results=%d snapshots=%d input=%q progress=%v final_text=%v", starts, results, snapshots, lastInput, progress, finalTextSeen)
			}
			closeCommonBindingBackend(t, backend, base)
		})
	}
}

// TestACPRegisteredBindingsCancelAndCloseCommonInvocation 验证同一标准 ACP fake 在全部 Binding 下均可取消并回收。
func TestACPRegisteredBindingsCancelAndCloseCommonInvocation(t *testing.T) {
	for _, id := range allRegisteredBindingIDs(t) {
		t.Run(id, func(t *testing.T) {
			backend := newACPBackend()
			base := &fakeCodexACPAgent{sessionID: acp.SessionId(id + "-cancel"), selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), blockPrompt: true, promptStarted: make(chan struct{})}
			replaceACPAgentFactory(backend, id, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
				return &commonBindingAgent{fakeCodexACPAgent: base}, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })
			sink := newSignalingRecordingSink()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan Result, 1)
			request := commonBindingRequest(t, id, sink)
			go func() { done <- backend.Run(ctx, request) }()
			sink.waitForEvent(t, EventTypeTextDelta)
			cancel()
			select {
			case result := <-done:
				if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
					t.Fatalf("cancel cause lost: %+v", result.Failure)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled common invocation did not finish")
			}
			closeCommonBindingBackend(t, backend, base)
		})
	}
}

package runtimehost

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// chatDriverFixture 记录真实模块交付与会话租约生命周期。
type chatDriverFixture struct {
	// opened 保存公开驱动准备输入。
	opened []c.OpenRequest
	// turns 保存仅含本轮输入的Prompt。
	turns []c.TurnRequest
	// closed 保存原生释放次数。
	closed atomic.Int32
	// result 可提供真实驱动的最终用量或无Fault取消终态。
	result *c.DriverResult
	// failClose 模拟原生尚未退出。
	failClose atomic.Bool
}

func (d *chatDriverFixture) ID() string { return "codex" }
func (d *chatDriverFixture) Capabilities() c.DriverCapabilities {
	return c.DriverCapabilities{Version: "chat-fixture", HotResume: true, ColdResume: true}
}
func (d *chatDriverFixture) Open(_ context.Context, r c.OpenRequest) (c.DriverSession, error) {
	d.opened = append(d.opened, r)
	return d, nil
}
func (d *chatDriverFixture) Close(context.Context) error {
	if d.failClose.Load() {
		return status_error.RuntimeShutdownFailed
	}
	d.closed.Add(1)
	return nil
}
func (d *chatDriverFixture) Prompt(ctx context.Context, r c.TurnRequest, sink c.DriverSink) c.DriverResult {
	d.turns = append(d.turns, r)
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &c.AssistantMessage{MessageID: "answer", Phase: c.PhaseFinal, Delta: "done", Complete: true}})
	if d.result != nil {
		return *d.result
	}
	return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn, NativeSession: c.NativeSessionInfo{Handle: "native-session", Resumable: true}}
}

// chatSinkFixture 捕获既有聊天事件协议。
type chatSinkFixture struct {
	// events 保存模块经Host转换的顺序事件。
	events []runtimecontract.Event
}

func (s *chatSinkFixture) Accept(_ context.Context, e runtimecontract.Event) error {
	s.events = append(s.events, e)
	return nil
}
func chatFixture(t *testing.T, d *chatDriverFixture) (*ChatHost, chatTestRequest) {
	t.Helper()
	request := chatTestRequest{Target: runtimeconfig.RuntimeTarget{RuntimeID: "codex", Isolated: true}, Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{ID: "codex", BaseRuntimeID: "codex"}}, Workspace: runtimecontract.Workspace{Path: t.TempDir(), Managed: true}, Session: runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}, Message: runtimecontract.Message{Text: "first"}, Policy: runtimecontract.RunPolicy{Model: "model", Execution: runtimecontract.ExecutionPolicy{PermissionLevel: runtimeconfig.PermissionLevelAuto}}}
	host := NewChat(ChatConfig{Launch: func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
		return runtimeconfig.LaunchFacts{BaseRuntimeID: "codex", Executable: "fixture"}, nil
	}, Driver: func(Environment, acpdriver.Factory) (c.Driver, error) { return d, nil }})
	t.Cleanup(func() { _ = host.Close(context.Background()) })
	return host, request
}
func TestChatHostRetainsHotResourcesAndSendsOnlyAddedInput(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	var releases atomic.Int32
	request.SessionResources = &chatTestResourcePlan{Key: "resources-v1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
	}}
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || result.Session.OutputID != "native-session" {
		t.Fatalf("first result=%+v", result)
	}
	if releases.Load() != 0 {
		t.Fatal("hot native session lost its resource lease")
	}
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: result.Session.OutputID}
	request.Message.Text = "second"
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "first"}, {Role: "assistant", Content: "done"}}
	result = runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || len(driver.opened) != 1 || len(driver.turns) != 2 || driver.turns[1].Input.Text != "second" || releases.Load() != 0 {
		t.Fatalf("resume result=%+v opens=%d turns=%+v releases=%d", result, len(driver.opened), driver.turns, releases.Load())
	}
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 {
		t.Fatalf("released=%d", releases.Load())
	}
}

// TestChatPreparationDefersBusinessAcquisition 核验会话接纳不会反向取得业务资源或启动原生进程。
func TestChatPreparationDefersBusinessAcquisition(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	prepared, err := host.PrepareSession(context.Background(), request.plan(), request.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	if !prepared.NeedsResources() || prepared.Resources().Target.RuntimeID != "" || len(driver.opened) != 0 || len(driver.turns) != 0 {
		t.Fatalf("会话接纳提前取得资源或启动进程: resources=%+v opens=%d turns=%d", prepared.Resources(), len(driver.opened), len(driver.turns))
	}
	released := 0
	if err := prepared.Bind(prepared.Context(), runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { released++; return nil }}); err != nil {
		t.Fatal(err)
	}
	if prepared.NeedsResources() || len(driver.opened) != 0 || len(driver.turns) != 0 || released != 0 {
		t.Fatalf("资源绑定提前打开进程或丢失租约: opens=%d turns=%d releases=%d", len(driver.opened), len(driver.turns), released)
	}
	if err := prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("未提交资源未释放: releases=%d", released)
	}
}

// TestChatNilBindContextStillTransfersResources 核验参数无效时资源仍进入同一清理所有者。
func TestChatNilBindContextStillTransfersResources(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	prepared, err := host.PrepareSession(context.Background(), request.plan(), request.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	released := 0
	err = prepared.Bind(nil, runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { released++; return nil }})
	if !errors.Is(err, status_error.BadRequest) || released != 0 {
		t.Fatalf("invalid binding lost ownership: err=%v releases=%d", err, released)
	}
	if err := prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("partial binding releases=%d", released)
	}
}

// TestChatInvalidBindDoesNotAcquireHiddenLaunchResources 保证无效绑定没有宿主自行创建的无主启动租约。
func TestChatInvalidBindDoesNotAcquireHiddenLaunchResources(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate", true: "closed"}[closed], func(t *testing.T) {
			driver := &chatDriverFixture{}
			host, request := chatFixture(t, driver)
			launches := 0
			original := host.config.Launch
			host.config.Launch = func(target runtimeconfig.RuntimeTarget, detection runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
				launches++
				return original(target, detection)
			}
			prepared, err := host.PrepareSession(context.Background(), request.plan(), request.Session)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close(context.Background())
			if closed {
				if err := prepared.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else if err := prepared.Bind(prepared.Context(), runtimecontract.SessionResources{Target: request.Target}); err != nil {
				t.Fatal(err)
			}
			before := launches
			released := false
			err = prepared.Bind(context.Background(), runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { released = true; return nil }})
			if err == nil || launches != before || released {
				t.Fatalf("invalid bind acquired or consumed new resources: error=%v launches=%d before=%d released=%v", err, launches, before, released)
			}
		})
	}
}

// TestChatHostPluginMutationClosesOnlyIdleConsumers 防止插件管理释放仍被活动原生消费者读取的资源。
func TestChatHostPluginMutationClosesOnlyIdleConsumers(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	var releases atomic.Int32
	request.SessionResources = &chatTestResourcePlan{Key: "plugin-v1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		return &runtimecontract.SessionResources{Target: request.Target, PluginResources: []plugins.Snapshot{{ID: "plugin-1"}}, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	prepared, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.CloseIdlePlugin(context.Background(), "plugin-1"); !errors.Is(err, status_error.PluginBusy) || releases.Load() != 0 {
		t.Fatalf("active plugin consumer released: %v releases=%d", err, releases.Load())
	}
	if err := prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.CloseIdlePlugin(context.Background(), "another-plugin"); err != nil || releases.Load() != 0 {
		t.Fatalf("unrelated plugin consumer released: %v releases=%d", err, releases.Load())
	}
	if err := host.CloseIdlePlugin(context.Background(), "plugin-1"); err != nil || releases.Load() != 1 {
		t.Fatalf("idle plugin consumer retained: %v releases=%d", err, releases.Load())
	}
}
func TestChatHostNativeCloseFailureKeepsAccountUntilRetried(t *testing.T) {
	driver := &chatDriverFixture{}
	driver.failClose.Store(true)
	host, request := chatFixture(t, driver)
	request.Target.CloseAfterRun = true
	var releases atomic.Int32
	request.SessionResources = &chatTestResourcePlan{Key: "resources-v1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
	}}
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || releases.Load() != 0 {
		t.Fatalf("result=%+v released=%d", result, releases.Load())
	}
	if err := host.CloseWorkspace(context.Background(), request.Workspace.Path); err == nil {
		t.Fatal("accepted prepare over unclosed native credentials")
	}
	driver.failClose.Store(false)
	if err := host.CloseWorkspace(context.Background(), request.Workspace.Path); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 {
		t.Fatalf("released=%d", releases.Load())
	}
}

func TestChatObserverPreservesInputSnapshotDiffAndResult(t *testing.T) {
	sink := &chatSinkFixture{}
	bridge := &chatObserver{sink: sink}
	invocation := c.InvocationRef{ID: "invoke"}
	events := []c.DriverEvent{
		{Kind: c.EventPromptAccepted},
		{Kind: c.EventThinking, Text: "thinking"},
		{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Name: "edit", Kind: "edit", Status: "in_progress", Input: json.RawMessage(`{"path":"main.go"}`)}},
		{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Update: true, Input: json.RawMessage(`{"path":"other.go"}`)}},
		{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Update: true, Content: []c.ContentBlock{{Type: "diff", Diff: &c.Diff{Path: "other.go", OldText: "a", NewText: "b"}}}}},
		{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Update: true, Status: "completed", Output: json.RawMessage(`{"ok":true}`)}},
	}
	for _, event := range events {
		if err := bridge.Accept(context.Background(), c.Event{Invocation: invocation, DriverEvent: event}); err != nil {
			t.Fatal(err)
		}
	}
	want := []runtimecontract.EventType{runtimecontract.EventTypeInvocationStarted, runtimecontract.EventTypeThinkingStart, runtimecontract.EventTypeThinkingDelta, runtimecontract.EventTypeToolUse, runtimecontract.EventTypeToolInputSnapshot, runtimecontract.EventTypeToolUpdate, runtimecontract.EventTypeToolResult}
	if len(sink.events) != len(want) {
		t.Fatalf("events=%+v", sink.events)
	}
	for i, event := range sink.events {
		if event.Type != want[i] {
			t.Fatalf("event %d=%s want=%s", i, event.Type, want[i])
		}
	}
	var snapshot map[string]any
	_ = json.Unmarshal(sink.events[4].Data, &snapshot)
	if snapshot["toolCallId"] != "tool" || snapshot["rawInput"].(map[string]any)["path"] != "other.go" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if !bridge.effects.FileWrite || !bridge.effects.ToolUse || !bridge.effects.ReasoningOutput {
		t.Fatalf("effects=%+v", bridge.effects)
	}
}

// chatApprovalFixture 回传原始审批身份并记录安全展示事实。
type chatApprovalFixture struct {
	request runtimecontract.ApprovalRequest // request 是收到的产品审批事实。
}

func (f *chatApprovalFixture) Request(_ context.Context, r runtimecontract.ApprovalRequest) (runtimecontract.ApprovalDecision, error) {
	f.request = r
	return runtimecontract.ApprovalDecision{OptionID: "original-allow"}, nil
}

// chatInputFixture 保留公开Schema及用户原始表单内容。
type chatInputFixture struct {
	request runtimecontract.ElicitationRequest // request 是收到的产品输入事实。
}

func (f *chatInputFixture) Request(_ context.Context, r runtimecontract.ElicitationRequest) (runtimecontract.ElicitationDecision, error) {
	f.request = r
	return runtimecontract.ElicitationDecision{Accepted: true, Content: map[string]any{"answer": "value"}}, nil
}
func TestChatInteractionPreservesOriginalChoiceAndSchema(t *testing.T) {
	approval, input := &chatApprovalFixture{}, &chatInputFixture{}
	bridge := chatInteraction{approval: approval, input: input}
	ref := c.InvocationRef{ID: "invoke", SessionID: "session", Revision: 1}
	response, err := bridge.Handle(context.Background(), c.InteractionRequest{Invocation: ref, ID: "approval", Kind: c.InteractionApproval, Approval: &c.ApprovalRequest{ToolCallID: "tool", Title: "execute", Options: []c.ApprovalOption{{ID: "original-allow", Name: "Once", Kind: "allow_once"}, {ID: "deny", Kind: "reject_once"}}}})
	if err != nil || response.OptionID != "original-allow" || response.Invocation != ref || approval.request.InvocationID != "invoke" {
		t.Fatalf("response=%+v request=%+v err=%v", response, approval.request, err)
	}
	response, err = bridge.Handle(context.Background(), c.InteractionRequest{Invocation: ref, ID: "form", Kind: c.InteractionInput, Prompt: "provide", Input: &c.InputRequest{Mode: "form", RequestedSchema: map[string]any{"type": "object"}}})
	if err != nil || response.Action != c.InteractionAccept || response.Content["answer"] != "value" || input.request.RequestedSchema["type"] != "object" {
		t.Fatalf("response=%+v request=%+v err=%v", response, input.request, err)
	}
	bridge.readOnly = true
	response, err = bridge.Handle(context.Background(), c.InteractionRequest{Invocation: ref, ID: "readonly", Kind: c.InteractionApproval, Approval: &c.ApprovalRequest{Options: []c.ApprovalOption{{ID: "original-allow", Kind: "allow_once"}}}})
	if err != nil || response.Action != c.InteractionCancel {
		t.Fatalf("readonly response=%+v err=%v", response, err)
	}
}
func TestChatHostColdResumeDoesNotReplayHistory(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: "native-session"}
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "old prompt"}}
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || len(driver.opened) != 1 || driver.opened[0].Session.NativeHandle != "native-session" || len(driver.opened[0].Seed) != 0 {
		t.Fatalf("result=%+v opened=%+v", result, driver.opened)
	}
}
func TestChatHostChangedModelStartsNewSessionWithBoundedHistory(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Model = "new-model"
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "old prompt"}, {Role: "assistant", Content: "old answer"}}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Mode != c.SessionNew || len(driver.opened[1].Seed) != 2 {
		t.Fatalf("result=%+v opened=%+v", second, driver.opened)
	}
}

func TestChatHostClosedSessionUsesActualPreviousConfiguration(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			driver := &chatDriverFixture{}
			host, request := chatFixture(t, driver)
			request.Target.CloseAfterRun = true
			first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
			request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
			request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "prior input"}}
			if changed {
				request.Policy.Model = "another-model"
			}
			result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
			if result.Failure != nil || len(driver.opened) != 2 {
				t.Fatalf("result=%+v opens=%d", result, len(driver.opened))
			}
			opened := driver.opened[1]
			if changed && (opened.Session.Mode != c.SessionNew || len(opened.Seed) != 1) {
				t.Fatalf("changed configuration resumed old context: %+v", opened)
			}
			if !changed && (opened.Session.Mode != c.SessionResume || len(opened.Seed) != 0) {
				t.Fatalf("unchanged configuration replayed history: %+v", opened)
			}
		})
	}
}
func TestChatResourcesFailedReleaseCanRetry(t *testing.T) {
	calls := 0
	resources := &nativeResources{snapshot: runtimecontract.SessionResources{Close: func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("close pending")
		}
		return nil
	}}}
	if resources.Close(context.Background()) == nil {
		t.Fatal("missing release failure")
	}
	if err := resources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChatSummaryColdRestoresWholeNativeContextWithoutSeed(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Execution.WorkspaceReadOnly = true
	request.Message.Text = "summarize"
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "must not replay"}}
	result := runSummaryFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.NativeHandle != first.Session.OutputID || len(driver.opened[1].Seed) != 0 {
		t.Fatalf("summary result=%+v opened=%+v", result, driver.opened)
	}
	if !driver.opened[1].Workspace.ReadOnly {
		t.Fatal("summary retained a native owner or lost readonly policy")
	}
	request.Policy.Execution.WorkspaceReadOnly = false
	request.Message.Text = "continue"
	result = runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || driver.opened[2].Session.Mode != c.SessionResume || len(driver.opened[2].Seed) != 0 {
		t.Fatalf("summary overwrote ordinary configuration: %+v", result)
	}
}
func TestChatAllElevenProfilesPreservePermissionAndInputFacts(t *testing.T) {
	entries := []struct{ base, normal, automatic, full string }{
		{runtimeconfig.RuntimeIDCodex, "read-only", "agent", "agent-full-access"}, {runtimeconfig.RuntimeIDClaude, "default", "auto", "bypassPermissions"}, {runtimeconfig.RuntimeIDKimi, "", "", ""}, {runtimeconfig.RuntimeIDCursor, "agent", "agent", "agent"}, {runtimeconfig.RuntimeIDPi, "", "", ""}, {runtimeconfig.RuntimeIDOpenCode, "", "", ""}, {runtimeconfig.RuntimeIDGemini, "default", "default", "yolo"}, {runtimeconfig.RuntimeIDGrok, "", "", ""}, {runtimeconfig.RuntimeIDWorkBuddy, "default", "auto", "bypassPermissions"}, {runtimeconfig.RuntimeIDQoder, "", "", ""}, {runtimeconfig.RuntimeIDQoderCN, "", "", ""},
	}
	for _, entry := range entries {
		t.Run(entry.base, func(t *testing.T) {
			for index, permission := range []runtimeconfig.PermissionLevel{runtimeconfig.PermissionLevelDefault, runtimeconfig.PermissionLevelAuto, runtimeconfig.PermissionLevelFullAccess} {
				request := chatTestRequest{Target: runtimeconfig.RuntimeTarget{RuntimeID: entry.base}, Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{ID: entry.base}}, Workspace: runtimecontract.Workspace{Path: t.TempDir()}, Policy: runtimecontract.RunPolicy{Execution: runtimecontract.ExecutionPolicy{PermissionLevel: permission}}, Message: runtimecontract.Message{Text: "current", Attachments: []runtimecontract.Attachment{{Name: "file", Path: "attachments/file.txt"}}, CodeReferences: []runtimecontract.CodeReference{{Path: "main.go", StartLine: 1, EndLine: 2, Content: "fixed"}}}, SelectedSkills: []string{"chosen"}, SelectedConnectors: []string{"connector"}, Skills: []runtimecontract.Skill{{Name: "chosen", Path: t.TempDir()}}, Connectors: []runtimecontract.Connector{{ID: "connector", Name: "remote", Transport: runtimeconfig.ConnectorTransportStreamableHTTP, URL: "https://example.invalid/mcp", Headers: map[string]string{"Authorization": "private-secret"}}}}
				spec, err := technicalSessionPlan(request.plan())
				bindings := nativeResourceBindings(runtimecontract.SessionResources{Skills: request.Skills, Connectors: request.Connectors})
				input := technicalTurnInput(request.Workspace.Path, runtimecontract.SessionResources{Connectors: request.Connectors}, request.turn())
				if err != nil || spec.Profile.NativeMode != []string{entry.normal, entry.automatic, entry.full}[index] || len(input.Attachments) != 1 || input.Attachments[0].Path != filepath.Join(request.Workspace.Path, "attachments/file.txt") || len(input.CodeReferences) != 1 || input.CodeReferences[0].Content != "fixed" || len(input.SelectedSkills) != 1 || len(input.SelectedMCP) != 1 || len(bindings) != 2 || bindings[1].MCP.Headers["Authorization"] != "private-secret" {
					t.Fatalf("spec=%+v bindings=%+v err=%v", spec, bindings, err)
				}
				encoded, _ := json.Marshal(spec)
				if strings.Contains(string(encoded), "private-secret") {
					t.Fatal("secret entered durable spec")
				}
				if input.Text != "current" {
					t.Fatal("Host changed current user text with native CLI syntax")
				}
			}
		})
	}
}
func TestChatSecretRotationDoesNotInvalidateRetainedResourceSnapshot(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Connectors = []runtimecontract.Connector{{ID: "mcp", Name: "server", Transport: runtimeconfig.ConnectorTransportStreamableHTTP, URL: "https://example.invalid", Headers: map[string]string{"Authorization": "first-secret"}}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Connectors[0].Headers["Authorization"] = "rotated-secret"
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 1 {
		t.Fatalf("secret refresh replaced native owner: result=%+v opens=%d", second, len(driver.opened))
	}
	request.Target.NativePluginScope = "changed-plugin-snapshot"
	third := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if third.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Mode != c.SessionNew {
		t.Fatalf("plugin change reused native owner: result=%+v opens=%+v", third, driver.opened)
	}
}

func TestChatHostDeliversFinalPromptResponseUsage(t *testing.T) {
	driver := &chatDriverFixture{result: &c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn, Usage: c.Usage{Known: true, InputTokens: 11, OutputTokens: 13, CachedReadTokens: 17, CachedWriteTokens: 19, ThoughtTokens: 23, ContextKnown: true, ContextTokens: 29, ContextWindowTokens: 31}}}
	host, request := chatFixture(t, driver)
	sink := &chatSinkFixture{}
	result := runChatFixture(context.Background(), host, request, sink)
	if result.Failure != nil {
		t.Fatalf("result=%+v", result)
	}
	var usage *runtimecontract.UsageData
	for _, event := range sink.events {
		if event.Type == runtimecontract.EventTypeUsage {
			usage = &runtimecontract.UsageData{}
			if err := json.Unmarshal(event.Data, usage); err != nil {
				t.Fatal(err)
			}
		}
	}
	if usage == nil || usage.Turn == nil || usage.Context == nil {
		t.Fatalf("missing final prompt response usage: events=%+v", sink.events)
	}
	if *usage.Turn != (runtimecontract.TurnUsageData{InputTokens: 11, OutputTokens: 13, CacheReadTokens: 17, CacheCreationTokens: 19, ReasoningOutputTokens: 23}) || *usage.Context != (runtimecontract.ContextUsageData{UsedTokens: 29, WindowTokens: 31}) {
		t.Fatalf("usage=%+v context=%+v", usage.Turn, usage.Context)
	}
}
func TestChatResultMapsNativeCancelledStatusWithoutFault(t *testing.T) {
	result := chatResult(runtimecontract.SessionPlan{}, runtimecontract.SessionDirective{}, c.Result{Status: c.StatusCancelled, StopReason: c.StopCancelled, Effects: c.Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != runtimediagnostic.FailureCodeContextCanceled || result.TerminationReason != constants.RuntimeTerminationReasonCancelled {
		t.Fatalf("cancelled mapped as success: %+v", result)
	}
}
func TestChatResultCancellationDoesNotReplacePrimaryFault(t *testing.T) {
	result := chatResult(runtimecontract.SessionPlan{}, runtimecontract.SessionDirective{}, c.Result{Status: c.StatusCancelled, StopReason: c.StopCancelled, Fault: &c.Fault{Code: c.FaultUnsupported, Stage: "prompt", Message: "unsupported native capability"}, Effects: c.Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != runtimediagnostic.FailureCodeUnsupportedCapability {
		t.Fatalf("lost primary fault: %+v", result)
	}
}

func TestChatPrepareRetainsActualResourcesWithoutReacquiringOnHotTurn(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	acquisitions, releases := 0, 0
	request.SessionResources = &chatTestResourcePlan{Key: "account:revision-1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		acquisitions++
		target := request.Target
		target.AccountID = "managed-account"
		target.Environment = map[string]string{"PRIVATE_KEY": "actual-secret"}
		return &runtimecontract.SessionResources{Target: target, Skills: []runtimecontract.Skill{{Name: "actual-skill", Path: t.TempDir()}}, PluginResources: []plugins.Snapshot{{ID: "plugin-actual", Name: "actual-snapshot"}}, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	prepared, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if acquisitions != 1 || prepared.Resources().Target.AccountID != "managed-account" || len(prepared.Resources().Skills) != 1 || prepared.Resources().PluginResources[0].Name != "actual-snapshot" || len(driver.turns) != 0 {
		t.Fatalf("prepared=%+v acquisitions=%d turns=%d", prepared, acquisitions, len(driver.turns))
	}
	first := prepared.Run(context.Background(), request.turn(), &chatSinkFixture{})
	if first.Failure != nil {
		t.Fatalf("first=%+v", first)
	}
	if err := prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Message.Text = "second"
	prepared, err = prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if acquisitions != 1 || prepared.Resources().Target.Environment["PRIVATE_KEY"] != "actual-secret" || prepared.Resources().PluginResources[0].Name != "actual-snapshot" {
		t.Fatalf("hot preparation=%+v acquisitions=%d", prepared, acquisitions)
	}
	second := prepared.Run(context.Background(), request.turn(), &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 1 || releases != 0 {
		t.Fatalf("second=%+v opens=%d releases=%d", second, len(driver.opened), releases)
	}
	_ = prepared.Close(context.Background())
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases != 1 {
		t.Fatalf("releases=%d", releases)
	}
}

func TestChatPrepareUnusedColdResourcesCloseButUnusedHotPreparationKeepsOwner(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	acquisitions, releases := 0, 0
	request.SessionResources = &chatTestResourcePlan{Key: "resources", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		acquisitions++
		return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	unused, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = unused.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases != 1 || len(driver.turns) != 0 {
		t.Fatalf("unused releases=%d turns=%d", releases, len(driver.turns))
	}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	unused, err = prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = unused.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || acquisitions != 2 || releases != 1 || len(driver.opened) != 1 {
		t.Fatalf("second=%+v acquisitions=%d releases=%d opens=%d", second, acquisitions, releases, len(driver.opened))
	}
}

func TestChatResourceRevisionChangeReplacesNativeOwner(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	acquisitions, releases := 0, 0
	request.SessionResources = &chatTestResourcePlan{Key: "account:1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		acquisitions++
		return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.SessionResources.Key = "account:2"
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "history"}}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || acquisitions != 2 || releases != 1 || len(driver.opened) != 2 || driver.opened[1].Session.Mode != c.SessionNew || len(driver.opened[1].Seed) != 1 {
		t.Fatalf("second=%+v acquisitions=%d releases=%d opened=%+v", second, acquisitions, releases, driver.opened)
	}
}

func TestChatAccountMutationClosesOnlyIdleNativeOwner(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	releases := 0
	request.Target.AccountID = "account-a"
	request.SessionResources = &chatTestResourcePlan{Key: "account-a:1", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if first.Failure != nil {
		t.Fatalf("first=%+v", first)
	}
	if err := host.CloseIdleAccount(context.Background(), "account-b"); err != nil {
		t.Fatal(err)
	}
	if releases != 0 {
		t.Fatal("unrelated account reclaimed active credentials")
	}
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	prepared, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.CloseIdleAccount(context.Background(), "account-a"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("prepared account mutation err=%v", err)
	}
	if releases != 0 {
		t.Fatal("prepared run lost credentials")
	}
	if err = prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = host.CloseIdleAccount(context.Background(), "account-a"); err != nil {
		t.Fatal(err)
	}
	if releases != 1 {
		t.Fatalf("releases=%d owner=%+v", releases, host.runtimes[request.Workspace.Path])
	}
}

func TestChatHotSessionUsesCurrentTurnResourceSelections(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Skills = []runtimecontract.Skill{{Name: "skill", Path: t.TempDir()}}
	request.SelectedSkills = []string{"skill"}
	request.Connectors = []runtimecontract.Connector{{ID: "mcp", Name: "connector", Transport: runtimeconfig.ConnectorTransportStreamableHTTP, URL: "https://example.invalid/mcp"}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Skills = append([]runtimecontract.Skill(nil), request.Skills...)
	request.Connectors = append([]runtimecontract.Connector(nil), request.Connectors...)
	request.SelectedSkills = nil
	request.SelectedConnectors = []string{request.Connectors[0].ID}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 1 || len(driver.turns) != 2 {
		t.Fatalf("second=%+v opens=%d turns=%d", second, len(driver.opened), len(driver.turns))
	}
	input := driver.turns[1].Input
	if len(input.SelectedSkills) != 0 || len(input.SelectedMCP) != 1 || input.SelectedMCP[0] != "connector" || input.Text != "first" {
		t.Fatalf("hot turn reused old selections: %+v", input)
	}
}

func TestChatUnsubmittedResourceCloseFailureRemainsOwnedForRetry(t *testing.T) {
	for _, prepareFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "abandoned", true: "prepare-failed"}[prepareFails], func(t *testing.T) {
			driver := &chatDriverFixture{}
			host, request := chatFixture(t, driver)
			canRelease := false
			attempts := 0
			request.SessionResources = &chatTestResourcePlan{Key: "account", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
				return &runtimecontract.SessionResources{Target: request.Target, Close: func(context.Context) error {
					attempts++
					if !canRelease {
						return errors.New("resource close pending")
					}
					return nil
				}}, nil
			}}
			if prepareFails {
				host.config.PrepareWorkspace = func(runtimecontract.SessionPlan) error { return errors.New("workspace preparation failed") }
			}
			prepared, err := prepareChatFixture(context.Background(), host, request)
			if prepareFails {
				if err == nil {
					t.Fatal("missing preparation error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err = prepared.Close(context.Background()); err == nil {
					t.Fatal("missing pending cleanup error")
				}
			}
			if attempts != 1 {
				t.Fatalf("initial cleanup attempts=%d", attempts)
			}
			canRelease = true
			if err = host.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if attempts != 2 {
				t.Fatalf("host discarded unsubmitted resource ownership: attempts=%d", attempts)
			}
		})
	}
}

func TestChatColdResourcesCanRequireNewSessionInsteadOfRestoringOldNativeScope(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: "old-native-session"}
	request.History = []runtimecontract.HistoryMessage{{Role: "user", Content: "accepted history"}}
	request.SessionResources = &chatTestResourcePlan{Key: "native-resources", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		return &runtimecontract.SessionResources{Target: request.Target, Session: &runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}}, nil
	}}
	prepared, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prepared.Close(context.Background()) }()
	result := prepared.Run(context.Background(), request.turn(), &chatSinkFixture{})
	if result.Failure != nil || prepared.Session().Mode != runtimecontract.SessionModeNew || len(driver.opened) != 1 || driver.opened[0].Session.Mode != c.SessionNew || len(driver.opened[0].Seed) != 1 {
		t.Fatalf("result=%+v directive=%+v opened=%+v", result, prepared.Session(), driver.opened)
	}
}

func TestChatPreparationProtectsPluginBeforeAcquisitionCompletes(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	entered, release := make(chan struct{}), make(chan struct{})
	request.SessionResources = &chatTestResourcePlan{Key: "plugin", PluginIDs: []string{"plugin-1"}, Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		close(entered)
		<-release
		return &runtimecontract.SessionResources{Target: request.Target, PluginResources: []plugins.Snapshot{{ID: "plugin-1"}}}, nil
	}}
	// result 交付准备句柄，避免测试协程与清理并发修改会话。
	type result struct {
		prepared runtimecontract.PreparedChatRun // prepared 是尚未提交输入的准备。
		err      error                           // err 是准备返回的准确错误。
	}
	done := make(chan result, 1)
	go func() {
		prepared, err := prepareChatFixture(context.Background(), host, request)
		done <- result{prepared, err}
	}()
	<-entered
	err := host.CloseIdlePlugin(context.Background(), "plugin-1")
	close(release)
	prepared := <-done
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	defer func() { _ = prepared.prepared.Close(context.Background()) }()
	if !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("preparing plugin was reclaimed: %v", err)
	}
}

func TestChatManagedSummarySnapshotCannotRetainReadOnlyConsumer(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Target.AccountID = "managed-account"
	target := request.Target
	releases := 0
	request.SessionResources = &chatTestResourcePlan{Key: "managed-account", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		// 受管资源快照可覆盖请求启动事实；摘要临时性必须由Core策略保护。
		return &runtimecontract.SessionResources{Target: target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if first.Failure != nil {
		t.Fatalf("first=%+v", first)
	}
	request.Session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Execution.WorkspaceReadOnly = true
	summary := runSummaryFixture(context.Background(), host, request, &chatSinkFixture{})
	if summary.Failure != nil || len(driver.opened) != 2 || releases != 2 {
		t.Fatalf("managed summary retained consumer: result=%+v opens=%d releases=%d", summary, len(driver.opened), releases)
	}
	request.Policy.Execution.WorkspaceReadOnly = false
	continued := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if continued.Failure != nil || len(driver.opened) != 3 || driver.opened[2].Workspace.ReadOnly {
		t.Fatalf("ordinary continuation retained summary policy: result=%+v opened=%+v", continued, driver.opened)
	}
}

func TestChatFailedColdAcquisitionRetainsPartialLeaseForRetry(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	releases := 0
	request.SessionResources = &chatTestResourcePlan{Key: "partial-account", Acquire: func(context.Context) (*runtimecontract.SessionResources, error) {
		target := request.Target
		target.AccountID = "partial-account"
		return &runtimecontract.SessionResources{Target: target, Close: func(context.Context) error {
			releases++
			if releases == 1 {
				return status_error.DatabaseError
			}
			return nil
		}}, status_error.PluginNotReady
	}}
	if _, err := prepareChatFixture(context.Background(), host, request); !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("preparation failure=%v", err)
	}
	if releases != 1 || len(driver.opened) != 0 {
		t.Fatalf("partial acquisition lost cleanup or opened consumer: releases=%d opens=%d", releases, len(driver.opened))
	}
	if err := host.CloseIdleAccount(context.Background(), "partial-account"); err != nil {
		t.Fatal(err)
	}
	if releases != 2 {
		t.Fatalf("partial account owner was not retained for retry: releases=%d", releases)
	}
}

// chatTestRequest 仅为原有行为案例聚合输入，生产契约保持会话、资源和本轮输入分离。
type chatTestRequest struct {
	Target             runtimeconfig.RuntimeTarget      // Target 是被测启动选择。
	Detection          runtimeconfig.DetectionResult    // Detection 是被测探测快照。
	Workspace          runtimecontract.Workspace        // Workspace 是被测目录范围。
	Instructions       []runtimecontract.Instruction    // Instructions 是被测静态指令。
	Skills             []runtimecontract.Skill          // Skills 是被测静态技能。
	Connectors         []runtimecontract.Connector      // Connectors 是被测静态连接器。
	Policy             runtimecontract.RunPolicy        // Policy 是被测权限与模型。
	Session            runtimecontract.SessionDirective // Session 是案例的准备动作。
	Message            runtimecontract.Message          // Message 是案例当前消息。
	History            []runtimecontract.HistoryMessage // History 是仅新会话使用的历史。
	SelectedSkills     []string                         // SelectedSkills 是本轮选择的技能名称。
	SelectedConnectors []string                         // SelectedConnectors 是本轮选择的连接器身份。
	SessionResources   *chatTestResourcePlan            // SessionResources 是测试调用方显式获取资源的动作。
}

// chatTestResourcePlan 只模拟调用方取得领域资源，不传入 Host 或 Core。
type chatTestResourcePlan struct {
	Key       string                                                           // Key 是静态资源代际。
	PluginIDs []string                                                         // PluginIDs 是接纳前保护的插件身份。
	Acquire   func(context.Context) (*runtimecontract.SessionResources, error) // Acquire 是测试边界提供的资源物化动作。
}

func (r chatTestRequest) plan() runtimecontract.SessionPlan {
	plan := runtimecontract.SessionPlan{Target: runtimecontract.DeclareTarget(r.Target), Detection: r.Detection, Workspace: r.Workspace, Instructions: r.Instructions, Skills: r.Skills, Connectors: runtimecontract.DeclareConnectors(r.Connectors), Policy: r.Policy}
	if r.SessionResources != nil {
		plan.ResourceKey = r.SessionResources.Key
		plan.PluginIDs = r.SessionResources.PluginIDs
	}
	return plan
}
func (r chatTestRequest) turn() runtimecontract.TurnInput {
	return runtimecontract.TurnInput{Message: r.Message, History: r.History, SelectedSkills: r.SelectedSkills, SelectedConnectors: r.SelectedConnectors}
}
func prepareChatFixture(ctx context.Context, host *ChatHost, request chatTestRequest) (runtimecontract.PreparedChatRun, error) {
	prepared, err := host.PrepareSession(ctx, request.plan(), request.Session)
	if err != nil {
		return nil, err
	}
	if prepared.NeedsResources() {
		snapshot := &runtimecontract.SessionResources{Target: request.Target, Skills: request.Skills, Connectors: request.Connectors}
		if request.SessionResources != nil {
			snapshot, err = request.SessionResources.Acquire(prepared.Context())
		}
		if snapshot != nil {
			bindErr := prepared.Bind(prepared.Context(), *snapshot)
			if err == nil {
				err = bindErr
			}
		}
		if err != nil {
			_ = prepared.Close(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	return prepared, nil
}
func runChatFixture(ctx context.Context, host *ChatHost, request chatTestRequest, sink runtimecontract.EventSink) runtimecontract.Result {
	prepared, err := prepareChatFixture(ctx, host, request)
	if err != nil {
		return chatFailed(request.plan(), request.Session, err)
	}
	defer prepared.Close(context.WithoutCancel(ctx))
	return prepared.Run(ctx, request.turn(), sink)
}
func runSummaryFixture(ctx context.Context, host *ChatHost, request chatTestRequest, sink runtimecontract.EventSink) runtimecontract.Result {
	plan := request.plan()
	plan.ReadOnlyHandoff = true
	prepared, err := host.PrepareSession(ctx, plan, request.Session)
	if err != nil {
		return chatFailed(plan, request.Session, err)
	}
	defer prepared.Close(context.WithoutCancel(ctx))
	if prepared.NeedsResources() {
		resources := &runtimecontract.SessionResources{Target: request.Target, Skills: request.Skills, Connectors: request.Connectors}
		if request.SessionResources != nil {
			resources, err = request.SessionResources.Acquire(prepared.Context())
		}
		if resources != nil {
			bindErr := prepared.Bind(prepared.Context(), *resources)
			if err == nil {
				err = bindErr
			}
		}
		if err != nil {
			return chatFailed(plan, request.Session, err)
		}
	}
	return prepared.Run(ctx, request.turn(), sink)
}

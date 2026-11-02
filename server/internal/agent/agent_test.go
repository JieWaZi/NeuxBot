package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	acp "github.com/JieWaZi/acp-go/sdk"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"github.com/sirupsen/logrus"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Migrated from host/chat_test.go; existing behavior assertions are preserved.
// chatDriverFixture 记录真实模块交付与会话租约生命周期。
type chatDriverFixture struct {
	// opened 保存公开驱动准备输入。
	opened []native.OpenRequest
	// turns 保存仅含本轮输入的Prompt。
	turns []native.TurnRequest
	// closed 保存原生释放次数。
	closed atomic.Int32
	// result 可提供真实驱动的最终用量或无Fault取消终态。
	result *native.TurnResult
	// failClose 模拟原生尚未退出。
	failClose atomic.Bool
}

func (d *chatDriverFixture) ID() string { return "codex" }

func (d *chatDriverFixture) Open(_ context.Context, r native.OpenRequest) (NativeConnection, error) {
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
func (d *chatDriverFixture) Prompt(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
	d.turns = append(d.turns, r)
	_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
	_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "answer", Phase: native.PhaseFinal, Delta: "done", Complete: true}})
	if d.result != nil {
		return *d.result
	}
	return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn, NativeSession: native.NativeSessionInfo{Handle: "native-session", Resumable: true}}
}

// chatSinkFixture 捕获既有聊天事件协议。
type chatSinkFixture struct {
	// events 保存模块经Host转换的顺序事件。
	events []Event
}

func (s *chatSinkFixture) Accept(_ context.Context, e Event) error {
	s.events = append(s.events, e)
	return nil
}
func chatFixture(t *testing.T, d *chatDriverFixture) (*Manager, chatTestRequest) {
	t.Helper()
	request := chatTestRequest{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true}, Detection: DetectionResult{Runtime: RuntimeSummary{ID: "codex", BaseRuntimeID: "codex"}}, Workspace: Workspace{Path: t.TempDir(), Managed: true}, Session: SessionDirective{Mode: SessionModeNew}, Message: Message{Text: "first"}, Policy: RunPolicy{Model: "model", Execution: ExecutionPolicy{PermissionLevel: PermissionLevelAuto}}}
	host := NewManager(ManagerConfig{Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
		return LaunchFacts{BaseRuntimeID: "codex", Executable: "fixture"}, nil
	}, Open: func(ctx context.Context, _ native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		return d.Open(ctx, request)
	}})
	t.Cleanup(func() { _ = host.Close(context.Background()) })
	return host, request
}
func TestChatHostRetainsHotResourcesAndSendsOnlyAddedInput(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	var releases atomic.Int32
	request.SessionResources = &chatTestResourcePlan{Key: "resources-v1", Acquire: func(context.Context) (*SessionResources, error) {
		return &SessionResources{Target: request.Target, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
	}}
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || result.Session.OutputID != "native-session" {
		t.Fatalf("first result=%+v", result)
	}
	if releases.Load() != 0 {
		t.Fatal("hot native session lost its resource lease")
	}
	request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
	request.Message.Text = "second"
	request.History = []HistoryMessage{{Role: "user", Content: "first"}, {Role: "assistant", Content: "done"}}
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
	if err := prepared.Bind(prepared.Context(), SessionResources{Target: request.Target, Close: func(context.Context) error { released++; return nil }}); err != nil {
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
	err = prepared.Bind(nil, SessionResources{Target: request.Target, Close: func(context.Context) error { released++; return nil }})
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
			host.config.Launch = func(target RuntimeTarget, detection DetectionResult) (LaunchFacts, error) {
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
			} else if err := prepared.Bind(prepared.Context(), SessionResources{Target: request.Target}); err != nil {
				t.Fatal(err)
			}
			before := launches
			released := false
			err = prepared.Bind(context.Background(), SessionResources{Target: request.Target, Close: func(context.Context) error { released = true; return nil }})
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
	request.SessionResources = &chatTestResourcePlan{Key: "plugin-v1", Acquire: func(context.Context) (*SessionResources, error) {
		return &SessionResources{Target: request.Target, PluginResources: []plugins.Snapshot{{ID: "plugin-1"}}, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
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
	request.SessionResources = &chatTestResourcePlan{Key: "resources-v1", Acquire: func(context.Context) (*SessionResources, error) {
		return &SessionResources{Target: request.Target, Close: func(context.Context) error { releases.Add(1); return nil }}, nil
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
	invocation := native.InvocationRef{ID: "invoke"}
	events := []native.Event{
		{Kind: native.EventPromptAccepted},
		{Kind: native.EventThinking, Text: "thinking"},
		{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Name: "edit", Kind: "edit", Status: "in_progress", Input: json.RawMessage(`{"path":"main.go"}`)}},
		{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Update: true, Input: json.RawMessage(`{"path":"other.go"}`)}},
		{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Update: true, Content: []native.ContentBlock{{Type: "diff", Diff: &native.Diff{Path: "other.go", OldText: "a", NewText: "b"}}}}},
		{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Update: true, Status: "completed", Output: json.RawMessage(`{"ok":true}`)}},
	}
	for _, event := range events {
		if err := bridge.Accept(context.Background(), observedEvent{Invocation: invocation, NativeEvent: event}); err != nil {
			t.Fatal(err)
		}
	}
	want := []EventType{EventTypeInvocationStarted, EventTypeThinkingStart, EventTypeThinkingDelta, EventTypeToolUse, EventTypeToolInputSnapshot, EventTypeToolUpdate, EventTypeToolResult}
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
	request ApprovalRequest // request 是收到的产品审批事实。
}

func (f *chatApprovalFixture) Request(_ context.Context, r ApprovalRequest) (ApprovalDecision, error) {
	f.request = r
	return ApprovalDecision{OptionID: "original-allow"}, nil
}

// chatInputFixture 保留公开Schema及用户原始表单内容。
type chatInputFixture struct {
	request ElicitationRequest // request 是收到的产品输入事实。
}

func (f *chatInputFixture) Request(_ context.Context, r ElicitationRequest) (ElicitationDecision, error) {
	f.request = r
	return ElicitationDecision{Accepted: true, Content: map[string]any{"answer": "value"}}, nil
}
func TestChatInteractionPreservesOriginalChoiceAndSchema(t *testing.T) {
	approval, input := &chatApprovalFixture{}, &chatInputFixture{}
	bridge := chatInteraction{approval: approval, input: input}
	ref := native.InvocationRef{ID: "invoke", SessionID: "session", Revision: 1}
	response, err := bridge.Handle(context.Background(), native.InteractionRequest{Invocation: ref, ID: "approval", Kind: native.InteractionApproval, Approval: &native.ApprovalRequest{ToolCallID: "tool", Title: "execute", Options: []native.ApprovalOption{{ID: "original-allow", Name: "Once", Kind: "allow_once"}, {ID: "deny", Kind: "reject_once"}}}})
	if err != nil || response.OptionID != "original-allow" || response.Invocation != ref || approval.request.InvocationID != "invoke" {
		t.Fatalf("response=%+v request=%+v err=%v", response, approval.request, err)
	}
	response, err = bridge.Handle(context.Background(), native.InteractionRequest{Invocation: ref, ID: "form", Kind: native.InteractionInput, Prompt: "provide", Input: &native.InputRequest{Mode: "form", RequestedSchema: map[string]any{"type": "object"}}})
	if err != nil || response.Action != native.InteractionAccept || response.Content["answer"] != "value" || input.request.RequestedSchema["type"] != "object" {
		t.Fatalf("response=%+v request=%+v err=%v", response, input.request, err)
	}
	bridge.readOnly = true
	response, err = bridge.Handle(context.Background(), native.InteractionRequest{Invocation: ref, ID: "readonly", Kind: native.InteractionApproval, Approval: &native.ApprovalRequest{Options: []native.ApprovalOption{{ID: "original-allow", Kind: "allow_once"}}}})
	if err != nil || response.Action != native.InteractionCancel {
		t.Fatalf("readonly response=%+v err=%v", response, err)
	}
}
func TestChatHostColdResumeDoesNotReplayHistory(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Session = SessionDirective{Mode: SessionModeResume, ID: "native-session"}
	request.History = []HistoryMessage{{Role: "user", Content: "old prompt"}}
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure != nil || len(driver.opened) != 1 || driver.opened[0].Session.NativeHandle != "native-session" || len(driver.opened[0].Seed) != 0 {
		t.Fatalf("result=%+v opened=%+v", result, driver.opened)
	}
}
func TestChatHostChangedModelStartsNewSessionWithBoundedHistory(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Model = "new-model"
	request.History = []HistoryMessage{{Role: "user", Content: "old prompt"}, {Role: "assistant", Content: "old answer"}}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Mode != native.SessionNew || len(driver.opened[1].Seed) != 2 {
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
			request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
			request.History = []HistoryMessage{{Role: "user", Content: "prior input"}}
			if changed {
				request.Policy.Model = "another-model"
			}
			result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
			if result.Failure != nil || len(driver.opened) != 2 {
				t.Fatalf("result=%+v opens=%d", result, len(driver.opened))
			}
			opened := driver.opened[1]
			if changed && (opened.Session.Mode != native.SessionNew || len(opened.Seed) != 1) {
				t.Fatalf("changed configuration resumed old context: %+v", opened)
			}
			if !changed && (opened.Session.Mode != native.SessionResume || len(opened.Seed) != 0) {
				t.Fatalf("unchanged configuration replayed history: %+v", opened)
			}
		})
	}
}
func TestChatResourcesFailedReleaseCanRetry(t *testing.T) {
	calls := 0
	resources := &sessionResources{snapshot: SessionResources{Close: func(context.Context) error {
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
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Execution.WorkspaceReadOnly = true
	request.Message.Text = "summarize"
	request.History = []HistoryMessage{{Role: "user", Content: "must not replay"}}
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
	if result.Failure != nil || driver.opened[2].Session.Mode != native.SessionResume || len(driver.opened[2].Seed) != 0 {
		t.Fatalf("summary overwrote ordinary configuration: %+v", result)
	}
}
func TestChatAllElevenProfilesPreservePermissionAndInputFacts(t *testing.T) {
	entries := []struct {
		base      string // base 是当前原生智能体。
		normal    string // normal 是默认权限的原生模式。
		automatic string // automatic 是自动审批的原生模式。
		full      string // full 是完全授权的原生模式。
	}{
		{RuntimeIDCodex, "read-only", "agent", "agent-full-access"}, {RuntimeIDClaude, "default", "auto", "bypassPermissions"}, {RuntimeIDKimi, "", "", ""}, {RuntimeIDCursor, "agent", "agent", "agent"}, {RuntimeIDPi, "", "", ""}, {RuntimeIDOpenCode, "", "", ""}, {RuntimeIDGemini, "default", "default", "yolo"}, {RuntimeIDGrok, "", "", ""}, {RuntimeIDWorkBuddy, "default", "auto", "bypassPermissions"}, {RuntimeIDQoder, "", "", ""}, {RuntimeIDQoderCN, "", "", ""},
	}
	for _, entry := range entries {
		t.Run(entry.base, func(t *testing.T) {
			for index, permission := range []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess} {
				request := chatTestRequest{Target: RuntimeTarget{RuntimeID: entry.base}, Detection: DetectionResult{Runtime: RuntimeSummary{ID: entry.base}}, Workspace: Workspace{Path: t.TempDir()}, Policy: RunPolicy{Execution: ExecutionPolicy{PermissionLevel: permission}}, Message: Message{Text: "current", Attachments: []Attachment{{Name: "file", Path: "attachments/file.txt"}}, CodeReferences: []CodeReference{{Path: "main.go", StartLine: 1, EndLine: 2, Content: "fixed"}}}, SelectedSkills: []string{"chosen"}, SelectedConnectors: []string{"connector"}, Skills: []Skill{{Name: "chosen", Path: t.TempDir()}}, Connectors: []Connector{{ID: "connector", Name: "remote", Transport: ConnectorTransportStreamableHTTP, URL: "https://example.invalid/mcp", Headers: map[string]string{"Authorization": "private-secret"}}}}
				spec, err := nativeSessionRequest(request.plan())
				bindings := nativeResourceBindings(SessionResources{Skills: request.Skills, Connectors: request.Connectors})
				input := technicalTurnInput(request.Workspace.Path, SessionResources{Connectors: request.Connectors}, request.turn())
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
	request.Connectors = []Connector{{ID: "mcp", Name: "server", Transport: ConnectorTransportStreamableHTTP, URL: "https://example.invalid", Headers: map[string]string{"Authorization": "first-secret"}}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Connectors[0].Headers["Authorization"] = "rotated-secret"
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 1 {
		t.Fatalf("secret refresh replaced native owner: result=%+v opens=%d", second, len(driver.opened))
	}
	request.Target.NativePluginScope = "changed-plugin-snapshot"
	third := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if third.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Mode != native.SessionNew {
		t.Fatalf("plugin change reused native owner: result=%+v opens=%+v", third, driver.opened)
	}
}

func TestChatHostDeliversFinalPromptResponseUsage(t *testing.T) {
	driver := &chatDriverFixture{result: &native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn, Usage: native.Usage{Known: true, InputTokens: 11, OutputTokens: 13, CachedReadTokens: 17, CachedWriteTokens: 19, ThoughtTokens: 23, ContextKnown: true, ContextTokens: 29, ContextWindowTokens: 31}}}
	host, request := chatFixture(t, driver)
	sink := &chatSinkFixture{}
	result := runChatFixture(context.Background(), host, request, sink)
	if result.Failure != nil {
		t.Fatalf("result=%+v", result)
	}
	var usage *UsageData
	for _, event := range sink.events {
		if event.Type == EventTypeUsage {
			usage = &UsageData{}
			if err := json.Unmarshal(event.Data, usage); err != nil {
				t.Fatal(err)
			}
		}
	}
	if usage == nil || usage.Turn == nil || usage.Context == nil {
		t.Fatalf("missing final prompt response usage: events=%+v", sink.events)
	}
	if *usage.Turn != (TurnUsageData{InputTokens: 11, OutputTokens: 13, CacheReadTokens: 17, CacheCreationTokens: 19, ReasoningOutputTokens: 23}) || *usage.Context != (ContextUsageData{UsedTokens: 29, WindowTokens: 31}) {
		t.Fatalf("usage=%+v context=%+v", usage.Turn, usage.Context)
	}
}
func TestChatResultMapsNativeCancelledStatusWithoutFault(t *testing.T) {
	result := chatResult(SessionPlan{}, SessionDirective{}, turnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled, Effects: Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled || result.TerminationReason != constants.RuntimeTerminationReasonCancelled {
		t.Fatalf("cancelled mapped as success: %+v", result)
	}
}
func TestChatResultCancellationDoesNotReplacePrimaryFault(t *testing.T) {
	result := chatResult(SessionPlan{}, SessionDirective{}, turnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled, Fault: &native.Fault{Code: native.FaultUnsupported, Stage: "prompt", Message: "unsupported native capability"}, Effects: Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != FailureCodeUnsupportedCapability {
		t.Fatalf("lost primary fault: %+v", result)
	}
}

func TestChatPrepareRetainsActualResourcesWithoutReacquiringOnHotTurn(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	acquisitions, releases := 0, 0
	request.SessionResources = &chatTestResourcePlan{Key: "account:revision-1", Acquire: func(context.Context) (*SessionResources, error) {
		acquisitions++
		target := request.Target
		target.AccountID = "managed-account"
		target.Environment = map[string]string{"PRIVATE_KEY": "actual-secret"}
		return &SessionResources{Target: target, Skills: []Skill{{Name: "actual-skill", Path: t.TempDir()}}, PluginResources: []plugins.Snapshot{{ID: "plugin-actual", Name: "actual-snapshot"}}, Close: func(context.Context) error { releases++; return nil }}, nil
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
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
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
	request.SessionResources = &chatTestResourcePlan{Key: "resources", Acquire: func(context.Context) (*SessionResources, error) {
		acquisitions++
		return &SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
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
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
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
	request.SessionResources = &chatTestResourcePlan{Key: "account:1", Acquire: func(context.Context) (*SessionResources, error) {
		acquisitions++
		return &SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.SessionResources.Key = "account:2"
	request.History = []HistoryMessage{{Role: "user", Content: "history"}}
	second := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if second.Failure != nil || acquisitions != 2 || releases != 1 || len(driver.opened) != 2 || driver.opened[1].Session.Mode != native.SessionNew || len(driver.opened[1].Seed) != 1 {
		t.Fatalf("second=%+v acquisitions=%d releases=%d opened=%+v", second, acquisitions, releases, driver.opened)
	}
}

func TestChatAccountMutationClosesOnlyIdleNativeOwner(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	releases := 0
	request.Target.AccountID = "account-a"
	request.SessionResources = &chatTestResourcePlan{Key: "account-a:1", Acquire: func(context.Context) (*SessionResources, error) {
		return &SessionResources{Target: request.Target, Close: func(context.Context) error { releases++; return nil }}, nil
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
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
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
		t.Fatalf("releases=%d owner=%+v", releases, host.workspaces[request.Workspace.Path])
	}
}

func TestChatHotSessionUsesCurrentTurnResourceSelections(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	request.Skills = []Skill{{Name: "skill", Path: t.TempDir()}}
	request.SelectedSkills = []string{"skill"}
	request.Connectors = []Connector{{ID: "mcp", Name: "connector", Transport: ConnectorTransportStreamableHTTP, URL: "https://example.invalid/mcp"}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Skills = append([]Skill(nil), request.Skills...)
	request.Connectors = append([]Connector(nil), request.Connectors...)
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
			request.SessionResources = &chatTestResourcePlan{Key: "account", Acquire: func(context.Context) (*SessionResources, error) {
				return &SessionResources{Target: request.Target, Close: func(context.Context) error {
					attempts++
					if !canRelease {
						return errors.New("resource close pending")
					}
					return nil
				}}, nil
			}}
			if prepareFails {
				host.config.PrepareWorkspace = func(SessionPlan) error { return errors.New("workspace preparation failed") }
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
	request.Session = SessionDirective{Mode: SessionModeResume, ID: "old-native-session"}
	request.History = []HistoryMessage{{Role: "user", Content: "accepted history"}}
	request.SessionResources = &chatTestResourcePlan{Key: "native-resources", Acquire: func(context.Context) (*SessionResources, error) {
		return &SessionResources{Target: request.Target, Session: &SessionDirective{Mode: SessionModeNew}}, nil
	}}
	prepared, err := prepareChatFixture(context.Background(), host, request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prepared.Close(context.Background()) }()
	result := prepared.Run(context.Background(), request.turn(), &chatSinkFixture{})
	if result.Failure != nil || prepared.Session().Mode != SessionModeNew || len(driver.opened) != 1 || driver.opened[0].Session.Mode != native.SessionNew || len(driver.opened[0].Seed) != 1 {
		t.Fatalf("result=%+v directive=%+v opened=%+v", result, prepared.Session(), driver.opened)
	}
}

func TestChatPreparationProtectsPluginBeforeAcquisitionCompletes(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	entered, release := make(chan struct{}), make(chan struct{})
	request.SessionResources = &chatTestResourcePlan{Key: "plugin", PluginIDs: []string{"plugin-1"}, Acquire: func(context.Context) (*SessionResources, error) {
		close(entered)
		<-release
		return &SessionResources{Target: request.Target, PluginResources: []plugins.Snapshot{{ID: "plugin-1"}}}, nil
	}}
	// result 交付准备句柄，避免测试协程与清理并发修改会话。
	type result struct {
		prepared PreparedChatRun // prepared 是尚未提交输入的准备。
		err      error           // err 是准备返回的准确错误。
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
	request.SessionResources = &chatTestResourcePlan{Key: "managed-account", Acquire: func(context.Context) (*SessionResources, error) {
		// 受管资源快照可覆盖请求启动事实；摘要临时性必须由Core策略保护。
		return &SessionResources{Target: target, Close: func(context.Context) error { releases++; return nil }}, nil
	}}
	first := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if first.Failure != nil {
		t.Fatalf("first=%+v", first)
	}
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
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
	request.SessionResources = &chatTestResourcePlan{Key: "partial-account", Acquire: func(context.Context) (*SessionResources, error) {
		target := request.Target
		target.AccountID = "partial-account"
		return &SessionResources{Target: target, Close: func(context.Context) error {
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
	Target             RuntimeTarget         // Target 是被测启动选择。
	Detection          DetectionResult       // Detection 是被测探测快照。
	Workspace          Workspace             // Workspace 是被测目录范围。
	Instructions       []Instruction         // Instructions 是被测静态指令。
	Skills             []Skill               // Skills 是被测静态技能。
	Connectors         []Connector           // Connectors 是被测静态连接器。
	Policy             RunPolicy             // Policy 是被测权限与模型。
	Session            SessionDirective      // Session 是案例的准备动作。
	Message            Message               // Message 是案例当前消息。
	History            []HistoryMessage      // History 是仅新会话使用的历史。
	SelectedSkills     []string              // SelectedSkills 是本轮选择的技能名称。
	SelectedConnectors []string              // SelectedConnectors 是本轮选择的连接器身份。
	SessionResources   *chatTestResourcePlan // SessionResources 是测试调用方显式获取资源的动作。
}

// chatTestResourcePlan 只模拟调用方取得领域资源，不传入 Host 或 Core。
type chatTestResourcePlan struct {
	Key       string                                           // Key 是静态资源代际。
	PluginIDs []string                                         // PluginIDs 是接纳前保护的插件身份。
	Acquire   func(context.Context) (*SessionResources, error) // Acquire 是测试边界提供的资源物化动作。
}

func (r chatTestRequest) plan() SessionPlan {
	plan := SessionPlan{Target: DeclareTarget(r.Target), Detection: r.Detection, Workspace: r.Workspace, Instructions: r.Instructions, Skills: r.Skills, Connectors: DeclareConnectors(r.Connectors), Policy: r.Policy}
	if r.SessionResources != nil {
		plan.ResourceKey = r.SessionResources.Key
		plan.PluginIDs = r.SessionResources.PluginIDs
	}
	return plan
}
func (r chatTestRequest) turn() TurnInput {
	return TurnInput{Message: r.Message, History: r.History, SelectedSkills: r.SelectedSkills, SelectedConnectors: r.SelectedConnectors}
}
func prepareChatFixture(ctx context.Context, host *Manager, request chatTestRequest) (PreparedChatRun, error) {
	prepared, err := host.PrepareSession(ctx, request.plan(), request.Session)
	if err != nil {
		return nil, err
	}
	if prepared.NeedsResources() {
		snapshot := &SessionResources{Target: request.Target, Skills: request.Skills, Connectors: request.Connectors}
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
func runChatFixture(ctx context.Context, host *Manager, request chatTestRequest, sink EventSink) Result {
	prepared, err := prepareChatFixture(ctx, host, request)
	if err != nil {
		return chatFailed(request.plan(), request.Session, err)
	}
	defer prepared.Close(context.WithoutCancel(ctx))
	return prepared.Run(ctx, request.turn(), sink)
}
func runSummaryFixture(ctx context.Context, host *Manager, request chatTestRequest, sink EventSink) Result {
	plan := request.plan()
	plan.ReadOnlyHandoff = true
	prepared, err := host.PrepareSession(ctx, plan, request.Session)
	if err != nil {
		return chatFailed(plan, request.Session, err)
	}
	defer prepared.Close(context.WithoutCancel(ctx))
	if prepared.NeedsResources() {
		resources := &SessionResources{Target: request.Target, Skills: request.Skills, Connectors: request.Connectors}
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

// Migrated from host/chat_output_test.go; existing behavior assertions are preserved.
func TestChatToolContentKeepsPublicPayloadAndEmptyReplacement(t *testing.T) {
	sink := &chatSinkFixture{}
	observer := &chatObserver{sink: sink}
	blocks := []native.ContentBlock{
		{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png", URI: "file:///image.png"},
		{Type: "audio", Data: "YXVkaW8=", MIMEType: "audio/wav"},
		{Type: "resource", Data: "YmxvYg==", MIMEType: "application/octet-stream", URI: "resource://blob"},
		{Type: "resource", Text: "resource text", MIMEType: "text/plain", URI: "resource://text"},
		{Type: "resource_link", Name: "report", MIMEType: "text/plain", URI: "resource://report"},
	}
	if err := observer.Accept(context.Background(), observedEvent{NativeEvent: native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Content: blocks}}}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(sink.events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	content := payload["content"].([]any)
	decoded := make([]map[string]any, len(content))
	for i, value := range content {
		item := value.(map[string]any)
		if item["type"] != "content" {
			t.Fatalf("nonstandard content wrapper: %+v", item)
		}
		decoded[i] = item["content"].(map[string]any)
	}
	if decoded[0]["data"] != blocks[0].Data || decoded[0]["mimeType"] != "image/png" || decoded[1]["data"] != blocks[1].Data ||
		decoded[2]["resource"].(map[string]any)["blob"] != blocks[2].Data || decoded[3]["resource"].(map[string]any)["text"] != "resource text" ||
		decoded[4]["name"] != "report" || decoded[4]["mimeType"] != "text/plain" {
		t.Fatalf("lost public content: %+v", decoded)
	}
	for _, replacement := range [][]native.ContentBlock{nil, {}} {
		if err := observer.Accept(context.Background(), observedEvent{NativeEvent: native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Update: true, Content: replacement}}}); err != nil {
			t.Fatal(err)
		}
		payload = nil
		_ = json.Unmarshal(sink.events[len(sink.events)-1].Data, &payload)
		_, exists := payload["content"]
		if exists != (replacement != nil) {
			t.Fatalf("lost omission/replacement distinction: %+v", payload)
		}
	}
}

func TestChatCursorUpgradeClassification(t *testing.T) {
	err := acp.NewInternalError(map[string]any{"cursorErrorCode": "upgrade"})
	result := chatResult(SessionPlan{Detection: DetectionResult{Runtime: RuntimeSummary{BaseRuntimeID: RuntimeIDCursor}}}, SessionDirective{}, turnResult{Status: native.StatusFailed, Fault: &native.Fault{Code: native.FaultDriver, Cause: err}}, &chatObserver{})
	if result.Failure == nil || result.Failure.Code != FailureCodePlanUpgradeRequired {
		t.Fatalf("lost native upgrade classification: %+v", result)
	}
}

// usageFailureSink 只拒绝最终用量，验证交付失败保留已经执行的事实。
type usageFailureSink struct{}

func (usageFailureSink) Accept(_ context.Context, event Event) error {
	if event.Type == EventTypeUsage {
		return status_error.ChatLogError
	}
	return nil
}

func TestChatFinalUsageDeliveryFailureDoesNotReplayPrompt(t *testing.T) {
	driver := &chatDriverFixture{result: &native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn, Usage: native.Usage{Known: true, InputTokens: 1}}}
	host, request := chatFixture(t, driver)
	result := runChatFixture(context.Background(), host, request, usageFailureSink{})
	if result.Failure == nil || result.Failure.Code != FailureCodeEventDelivery || !result.ProtocolHealthy || len(driver.turns) != 1 {
		t.Fatalf("delivery failure lost execution or replayed: %+v turns=%d", result, len(driver.turns))
	}
}

func TestChatNativeCancellationReachesProductResult(t *testing.T) {
	driver := &chatDriverFixture{result: &native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}}
	host, request := chatFixture(t, driver)
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled || result.ExitCode == 0 {
		t.Fatalf("native cancelled became successful: %+v", result)
	}
}

// Migrated from host/text_test.go; existing behavior assertions are preserved.
// driverFixture 提供可控制最终消息和资源关闭的技术边界。
type driverFixture struct {
	// id 是被测用户 Profile 身份。
	id string
	// opened 保存冻结的技术配置与环境。
	opened native.OpenRequest
	// promptCalls 统计真正提交的执行次数。
	promptCalls int
	// closeCalls 统计清理重试。
	closeCalls int
	// allowClose 控制原生进程是否成功退出。
	allowClose bool
	// failure 是执行完成前返回的故障。
	failure *native.Fault
	// entered 通知已经真正提交 Prompt。
	entered chan struct{}
	// input 保存真正交付的本次任务文本。
	input string
}

func (d *driverFixture) ID() string { return d.id }

func (d *driverFixture) Open(_ context.Context, request native.OpenRequest) (NativeConnection, error) {
	d.opened = request
	return d, nil
}
func (d *driverFixture) Close(context.Context) error {
	d.closeCalls++
	if !d.allowClose {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}
func (d *driverFixture) Prompt(ctx context.Context, request native.TurnRequest, sink native.EventSink) native.TurnResult {
	d.promptCalls++
	d.input = request.Input.Text
	_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
	if d.entered != nil {
		close(d.entered)
		<-ctx.Done()
		return native.TurnResult{Status: native.StatusCancelled, Fault: &native.Fault{Code: native.FaultCancelled, Stage: "prompt", Cause: ctx.Err()}}
	}
	_ = sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "native-tool", Name: "shell", Status: "completed"}})
	_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "commentary", Phase: native.PhaseCommentary, Delta: "process", Complete: true}})
	if d.failure != nil {
		return native.TurnResult{Status: native.StatusFailed, Fault: d.failure}
	}
	_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "final", Phase: native.PhaseFinal, Delta: "trusted final", Complete: true}})
	return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopMaxTokens, Usage: native.Usage{Known: true, InputTokens: 3, CachedReadTokens: 5, OutputTokens: 7}, Configuration: native.Configuration{ActualReasoning: "low", ReasoningFallback: "thinking_off_unsupported"}}
}

func hostFixture(t *testing.T, base string, d *driverFixture) (*Manager, *int) {
	t.Helper()
	released := new(int)
	host := NewManager(ManagerConfig{Resolve: func(context.Context, TextRequest) (Environment, error) {
		return Environment{BaseRuntimeID: base, Target: RuntimeTarget{RuntimeID: d.id, Binary: "/selected/codex", PrefixArgs: []string{"--profile", "selected"}, ConfigHome: "/selected/home"}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, env Environment, _ string) (RuntimeTarget, func(context.Context) error, error) {
		target := env.Target
		target.Isolated = true
		target.Environment = map[string]string{"CODEX_HOME": "/managed/home", "OPENAI_API_KEY": "selected-account"}
		return target, func(context.Context) error { *released++; return nil }, nil
	}, Launch: func(target RuntimeTarget, _ DetectionResult) (LaunchFacts, error) {
		return LaunchFacts{BaseRuntimeID: base, Executable: target.Binary, PrefixArgs: target.PrefixArgs, Environment: environmentList(target.Environment)}, nil
	}, Open: func(ctx context.Context, _ native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		return d.Open(ctx, request)
	}})
	return host, released
}

func TestCodexProfileFinalSurvivesCleanupAndNeverReplays(t *testing.T) {
	driver := &driverFixture{id: "custom-codex"}
	host, released := hostFixture(t, RuntimeIDCodex, driver)
	result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, AccountID: "account", Model: "chosen-model", Reasoning: "high", Messages: []TextMessage{{Role: "system", Content: "task rules"}, {Role: "user", Content: "exact input"}}})
	if err != nil || result.Text != "trusted final" || result.StopReason != "max_tokens" || result.Usage == nil || result.Usage.CacheReadTokens != 5 || driver.promptCalls != 1 || *released != 0 {
		t.Fatalf("result=%+v err=%v prompt=%d release=%d", result, err, driver.promptCalls, *released)
	}
	if driver.opened.Profile.Model != "chosen-model" || driver.opened.Policies.Tools.Mode != native.ToolsAllowAll || len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Contributions.Instructions[0].Text != "task rules" || len(driver.opened.Contributions.MCP) != 0 || driver.opened.Credential.Environment["OPENAI_API_KEY"] != "selected-account" {
		t.Fatalf("open=%+v", driver.opened)
	}
	if _, err := os.Stat(driver.opened.Workspace.Root); err != nil {
		t.Fatalf("workspace released before native close: %v", err)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("cleanup replayed prompt or released twice: prompt=%d release=%d", driver.promptCalls, *released)
	}
	if _, err := os.Stat(driver.opened.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("workspace retained: %v", err)
	}
}

func TestExecutedFailureCarriesNoRetryAndNeverFallsBack(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, failure: &native.Fault{Code: native.FaultDriver, Stage: "prompt", Message: "connection timeout", Cause: context.DeadlineExceeded}}
	host, _ := hostFixture(t, RuntimeIDCodex, driver)
	defer host.Close(context.Background())
	result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "input"}}})
	var policy interface{ Retryable() bool }
	if !errors.Is(err, status_error.OperationTimeout) || !errors.As(err, &policy) || policy.Retryable() || result.Text != "" || driver.promptCalls != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAllCLIProfilesUseOneHostAndReleaseWithoutReplay(t *testing.T) {
	for _, base := range []string{"codex", "claude", "kimi", "cursor", "pi", "opencode", "gemini", "grok", "workbuddy", "qoder", "qoder-cn"} {
		t.Run(base, func(t *testing.T) {
			driver := &driverFixture{id: "profile-" + base, allowClose: true}
			host, released := hostFixture(t, base, driver)
			defer host.Close(context.Background())
			result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "system", Content: "task rules"}, {Role: "user", Content: "input"}}})
			if err != nil || result.Text != "trusted final" || driver.promptCalls != 1 || *released != 1 {
				t.Fatalf("result=%+v err=%v calls=%d released=%d", result, err, driver.promptCalls, *released)
			}
			if base != "codex" && !strings.Contains(driver.opened.Profile.DriverID, base) {
				t.Fatalf("profile lost: %+v", driver.opened)
			}
			if !strings.Contains(driver.input, "input") || (!strings.Contains(driver.input, "task rules") && (len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Contributions.Instructions[0].Text != "task rules")) {
				t.Fatalf("task messages lost: %q", driver.input)
			}
		})
	}
}

// TestOpenCodeManagedModelKeepsSystemAndAccountNamespaces 防止系统模型误加受管前缀或账号模型漏选服务商。
func TestOpenCodeManagedModelKeepsSystemAndAccountNamespaces(t *testing.T) {
	for _, tt := range []struct {
		account string // account 是本次已冻结的账号身份。
		want    string // want 是必须送给原生 OpenCode 的完整模型身份。
	}{{"", "model-a"}, {"selected-account", "ally/model-a"}} {
		driver := &driverFixture{id: "opencode-profile", allowClose: true}
		host, _ := hostFixture(t, RuntimeIDOpenCode, driver)
		_, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, AccountID: tt.account, Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
		if err != nil {
			t.Fatal(err)
		}
		if driver.opened.Profile.Model != tt.want {
			t.Fatalf("account=%q native model=%q want=%q", tt.account, driver.opened.Profile.Model, tt.want)
		}
		if err := host.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskMessagesPreserveOriginalSystemAndSoleUser(t *testing.T) {
	compiled, err := native.CompileTaskMessages("codex", []native.Message{{Role: "system", Content: " original system\n"}, {Role: "user", Content: " exact user\n"}})
	prompt, systems, limitations := compiled.Text, compiled.Instructions, compiled.Limitations
	if err != nil || prompt != " exact user\n" || !reflect.DeepEqual(systems, []string{" original system\n"}) || len(limitations) != 0 {
		t.Fatalf("prompt=%q systems=%q limitations=%q err=%v", prompt, systems, limitations, err)
	}
	compiled, err = native.CompileTaskMessages("codex", []native.Message{{Role: "user", Content: "first"}, {Role: "assistant", Content: "continuation"}, {Role: "user", Content: "second"}})
	prompt, limitations = compiled.Text, compiled.Limitations
	if err != nil || strings.Index(prompt, "first") > strings.Index(prompt, "continuation") || strings.Index(prompt, "continuation") > strings.Index(prompt, "second") || len(limitations) == 0 {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
}

func TestHostCloseCancelsActiveInvocation(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, entered: make(chan struct{})}
	host, released := hostFixture(t, RuntimeIDCodex, driver)
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "input"}}})
		finished <- err
	}()
	<-driver.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancel=%v", err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("prompt=%d release=%d", driver.promptCalls, *released)
	}
}

// TestHostCleanupRetryKeepsActiveInvocation 验证真实清理周期不会提前关闭仍在执行的独立任务。
func TestHostCleanupRetryKeepsActiveInvocation(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, entered: make(chan struct{})}
	host, released := hostFixture(t, RuntimeIDCodex, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer host.Close(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(ctx, TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "long task"}}})
		finished <- err
	}()
	select {
	case <-driver.entered:
	case <-ctx.Done():
		t.Fatal("prompt did not start")
	}
	host.Start(ctx)
	select {
	case err := <-finished:
		t.Fatalf("cleanup worker stopped an active invocation: %v", err)
	case <-time.After(11 * time.Second):
	}
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("explicit close did not cancel the active invocation: %v", err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("prompt=%d release=%d", driver.promptCalls, *released)
	}
}

// TestHostCloseRetainsLatePartialAcquisition 验证关闭仍取消资源取得，并为迟到部分结果保留同一清理所有者。
func TestHostCloseRetainsLatePartialAcquisition(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true}
	host, _ := hostFixture(t, RuntimeIDCodex, driver)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	returnAcquisition, allowRelease := make(chan struct{}), make(chan struct{})
	defer func() {
		for _, signal := range []chan struct{}{returnAcquisition, allowRelease} {
			select {
			case <-signal:
			default:
				close(signal)
			}
		}
		_ = host.Close(context.Background())
	}()
	workspace := ""
	released := 0
	host.config.Prepare = func(ctx context.Context, _ TextRequest, environment Environment, directory string) (RuntimeTarget, func(context.Context) error, error) {
		workspace = directory
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-returnAcquisition
		return environment.Target, func(context.Context) error {
			select {
			case <-allowRelease:
				released++
				return nil
			default:
				return status_error.RuntimeShutdownFailed
			}
		}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(ctx, TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "input"}}})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("resource acquisition did not start")
	}
	closeContext, closeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer closeCancel()
	if err := host.Close(closeContext); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("close discarded an acquisition still returning resources: %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("host close did not cancel resource acquisition")
	}
	close(returnAcquisition)
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("acquisition cancellation=%v", err)
	}
	if released != 0 || driver.promptCalls != 0 {
		t.Fatalf("late acquisition prompted or lost failed cleanup: prompt=%d release=%d", driver.promptCalls, released)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("workspace released before the partial resource: %v", err)
	}
	close(allowRelease)
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("partial resource released %d times", released)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace retained after resource cleanup: %v", err)
	}
}

func environmentList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

// Migrated from host/task_test.go; existing behavior assertions are preserved.
// taskResourceFixture 核验任务资源与短期授权拥有不同释放边界。
type taskResourceFixture struct {
	root     string      // root 是引擎提供的受管工作目录。
	revoked  atomic.Bool // revoked 表示调用授权是否已撤销。
	closed   int         // closed 统计成功释放资源的次数。
	acquired int         // acquired 统计资源物化次数。
}

func (r *taskResourceFixture) Acquire(_ context.Context, request ResourceRequest) (ResourceLease, error) {
	r.root = request.Workspace.Root
	r.acquired++
	if err := os.Mkdir(filepath.Join(r.root, "ally"), 0700); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *taskResourceFixture) Resources() []native.ResourceBinding {
	return []native.ResourceBinding{{ID: "ally", Path: filepath.Join(r.root, "ally")}}
}
func (r *taskResourceFixture) Close(context.Context) error {
	r.closed++
	return os.RemoveAll(filepath.Join(r.root, "ally"))
}
func (r *taskResourceFixture) Revoke() { r.revoked.Store(true) }
func (r *taskResourceFixture) LaunchEnvironment(base map[string]string) map[string]string {
	base["ALLY_CONTEXT_PATH"] = "task-connection"
	base["PATH"] = "task-bin:" + base["PATH"]
	return base
}

// taskDriverFixture 在原生关闭时验证领域授权已撤销。
type taskDriverFixture struct {
	*driverFixture                      // driverFixture 复用共享宿主的原生边界替身。
	resources      *taskResourceFixture // resources 是本次调用拥有的任务资源。
	t              *testing.T           // t 接收关闭顺序断言。
	openError      error                // openError 模拟创建原生会话后返回的启动故障。
}

func (d *taskDriverFixture) Open(_ context.Context, request native.OpenRequest) (NativeConnection, error) {
	d.opened = request
	return d, d.openError
}
func (d *taskDriverFixture) Close(ctx context.Context) error {
	if !d.resources.revoked.Load() {
		d.t.Error("native close began before invocation grant revocation")
	}
	return d.driverFixture.Close(ctx)
}

func TestTaskResourcesAndAccountRemainOwnedUntilNativeClose(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex"}, resources: resources, t: t}
	host, released := hostFixture(t, RuntimeIDCodex, driver.driverFixture)
	host.config.Open = func(ctx context.Context, _ native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		return driver.Open(ctx, request)
	}
	result, err := host.ExecuteTask(context.Background(), TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, AccountID: "account", Messages: []TextMessage{{Role: "user", Content: "maintain"}}}, Contributions: native.Contributions{Instructions: []native.Instruction{{Name: "wiki", Text: "task rules"}}, Skills: []native.Skill{{Name: "ally", ResourceID: "ally"}}}, Resources: resources})
	if err != nil || result.Text != "trusted final" || resources.acquired != 1 || resources.closed != 0 || *released != 0 || !resources.revoked.Load() {
		t.Fatalf("result=%+v err=%v acquired=%d resources=%d account=%d revoked=%v", result, err, resources.acquired, resources.closed, *released, resources.revoked.Load())
	}
	if len(driver.opened.Resources) != 1 || len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Policies.Tools.Mode != native.ToolsAllowAll || driver.opened.Session.Mode != native.SessionNew || driver.opened.Credential.Environment["ALLY_CONTEXT_PATH"] != "task-connection" || driver.opened.Credential.Environment["OPENAI_API_KEY"] != "selected-account" {
		t.Fatalf("open=%+v", driver.opened)
	}
	if _, err := os.Stat(resources.root); err != nil {
		t.Fatalf("live resources removed before native exit: %v", err)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resources.closed != 1 || *released != 1 || driver.promptCalls != 1 {
		t.Fatalf("close replayed effects: resource=%d account=%d prompt=%d", resources.closed, *released, driver.promptCalls)
	}
	if _, err := os.Stat(resources.root); !os.IsNotExist(err) {
		t.Fatalf("workspace retained after close: %v", err)
	}
}

func TestTaskCancellationRevokesBeforeFailedNativeClose(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex", entered: make(chan struct{})}, resources: resources, t: t}
	host, released := hostFixture(t, RuntimeIDCodex, driver.driverFixture)
	host.config.Open = func(ctx context.Context, _ native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		return driver.Open(ctx, request)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTask(ctx, TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "maintain"}}}, Resources: resources})
		done <- err
	}()
	<-driver.entered
	cancel()
	if err := <-done; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancel=%v", err)
	}
	if !resources.revoked.Load() || resources.closed != 0 || *released != 0 {
		t.Fatalf("cancel ownership revoked=%v resource=%d account=%d", resources.revoked.Load(), resources.closed, *released)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.promptCalls != 1 || resources.acquired != 1 || resources.closed != 1 || *released != 1 {
		t.Fatal("cleanup repeated task or released leases twice")
	}
}

// TestCredentialCleanupDoesNotRepeatSuccessfulAccountRelease 核验目录回收失败不重复释放已成功归还的账号租约。
func TestCredentialCleanupDoesNotRepeatSuccessfulAccountRelease(t *testing.T) {
	released := 0
	workspace := string([]byte{0})
	resources := &sessionResources{snapshot: SessionResources{Close: func(context.Context) error { released++; return nil }}, releaseWorkspace: func(context.Context) error { return os.RemoveAll(workspace) }}
	if err := resources.Close(context.Background()); err == nil {
		t.Fatal("invalid directory unexpectedly removed")
	}
	workspace = t.TempDir()
	if err := resources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("successful account release repeated: %d", released)
	}
}

func TestTaskOpenFailureRevokesAndRetainsReturnedSession(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex"}, resources: resources, t: t, openError: status_error.RuntimeProtocolIncompatible}
	host, released := hostFixture(t, RuntimeIDCodex, driver.driverFixture)
	host.config.Open = func(ctx context.Context, _ native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		return driver.Open(ctx, request)
	}
	_, err := host.ExecuteTask(context.Background(), TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, Messages: []TextMessage{{Role: "user", Content: "maintain"}}}, Resources: resources})
	if !errors.Is(err, status_error.RuntimeProtocolIncompatible) || !resources.revoked.Load() || resources.closed != 0 || *released != 0 || driver.promptCalls != 0 {
		t.Fatalf("open failure lost ownership: err=%v revoked=%v resource=%d account=%d prompt=%d", err, resources.revoked.Load(), resources.closed, *released, driver.promptCalls)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resources.closed != 1 || *released != 1 || driver.promptCalls != 0 {
		t.Fatal("open failure cleanup replayed invocation")
	}
}

// nativeTaskResources 在资源物化后才能提供本次调用的真实连接路径与 CLI 路径。
type nativeTaskResources struct {
	// taskResourceFixture 复用任务授权与文件租约生命周期。
	*taskResourceFixture
}

func (r *nativeTaskResources) LaunchEnvironment(base map[string]string) map[string]string {
	base[constants.AgentCLIContextEnvironment] = filepath.Join(r.root, "ally", "connection.json")
	base[constants.AgentCLIBinaryEnvironment] = filepath.Join(r.root, "ally", "allycli")
	base["PATH"] = filepath.Join(r.root, "ally") + string(os.PathListSeparator) + base["PATH"]
	return base
}

// TestTaskAuthorizedEnvironmentReachesNativeProcess 覆盖真实工厂边界，防止启动器丢失资源物化后加入的环境。
func TestTaskAuthorizedEnvironmentReachesNativeProcess(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "environment")
	cli := filepath.Join(directory, "native-fixture")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n%%s\\n%%s\\n%%s\\n%%s\\n' \"$*\" \"${%s-absent}\" \"${%s-absent}\" \"${PATH-absent}\" \"${OPENAI_API_KEY-absent}\" > %q\n", constants.AgentCLIContextEnvironment, constants.AgentCLIBinaryEnvironment, capture)
	script += `while IFS= read -r request; do
 request_id=$(printf '%s' "$request" | /usr/bin/sed -n 's/.*"id":\([^,}]*\).*/\1/p')
 if [ -n "$request_id" ]; then
  printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"controlled initialization rejection"}}\n' "$request_id"
 fi
done
`
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	base := []string{"PATH=account-bin", "OPENAI_API_KEY=selected-account"}
	host := NewManager(ManagerConfig{
		Resolve: func(context.Context, TextRequest) (Environment, error) {
			return Environment{BaseRuntimeID: RuntimeIDQoder}, nil
		},
		Prepare: func(context.Context, TextRequest, Environment, string) (RuntimeTarget, func(context.Context) error, error) {
			return RuntimeTarget{Isolated: true}, nil, nil
		},
		Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
			return LaunchFacts{BaseRuntimeID: RuntimeIDQoder, Executable: cli, PrefixArgs: []string{"--fixture"}, Environment: base}, nil
		},
	})
	defer host.Close(context.Background())
	resources := &nativeTaskResources{taskResourceFixture: &taskResourceFixture{}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := host.ExecuteTask(ctx, TaskRequest{TextRequest: TextRequest{RuntimeID: RuntimeIDQoder, Messages: []TextMessage{{Role: "user", Content: "inspect task environment"}}}, Contributions: native.Contributions{Skills: []native.Skill{{Name: "ally", ResourceID: "ally"}}}, Resources: resources})
	if err == nil {
		t.Fatal("只捕获启动配置的受控进程不应产生模型结果")
	}
	data, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := []string{"--fixture --acp", filepath.Join(resources.root, "ally", "connection.json"), filepath.Join(resources.root, "ally", "allycli"), filepath.Join(resources.root, "ally") + string(os.PathListSeparator) + "account-bin", "selected-account"}
	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("原生消费者未收到实际任务环境: got=%q want=%q", got, want)
	}
	if !reflect.DeepEqual(base, []string{"PATH=account-bin", "OPENAI_API_KEY=selected-account"}) {
		t.Fatal("任务环境反写了目录提供的基础启动快照")
	}
	if !resources.revoked.Load() || resources.closed != 1 {
		t.Fatalf("受控进程结束后未回收任务资源: revoked=%v closed=%d", resources.revoked.Load(), resources.closed)
	}
}

// Migrated from host/native_resources_test.go; existing behavior assertions are preserved.
// hostTestLogx 在任何后台输出开始前取得实例，避免并发绕过项目的入口锁。
var hostTestLogx = ginxlogx.Instance()

// hostLogCapture 接收后台原生诊断及当前断言的并发输出。
type hostLogCapture struct {
	// mutex 保护日志缓冲区。
	mutex sync.Mutex
	// buffer 保存统一输出后的诊断文本。
	buffer bytes.Buffer
}

func (capture *hostLogCapture) Write(data []byte) (int, error) {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return capture.buffer.Write(data)
}

func (capture *hostLogCapture) text() string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return capture.buffer.String()
}

// TestHostNativeLoggerAlwaysReachesLogx 防止宿主受到任意全局 slog 默认输出替换的影响。
func TestHostNativeLoggerAlwaysReachesLogx(t *testing.T) {
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close(context.Background()) })
	output, level := hostTestLogx.Out, hostTestLogx.GetLevel()
	capture := &hostLogCapture{}
	hostTestLogx.SetOutput(capture)
	hostTestLogx.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() { hostTestLogx.SetOutput(output); hostTestLogx.SetLevel(level) })
	previousDefault := slog.Default()
	unrelated := &hostLogCapture{}
	slog.SetDefault(slog.New(slog.NewTextHandler(unrelated, nil)))
	t.Cleanup(func() { slog.SetDefault(previousDefault) })
	config := hostNativeConfig(RuntimeTarget{}, LaunchFacts{}, "", "", PermissionLevelDefault)
	config.Logger.Warn("host-native-logx-sentinel")
	if strings.Count(capture.text(), "host-native-logx-sentinel") != 1 || strings.Contains(unrelated.text(), "host-native-logx-sentinel") {
		t.Fatalf("native diagnostics escaped logx: logx=%q unrelated=%q", capture.text(), unrelated.text())
	}
}

// Migrated from host/contract/session_test.go; existing behavior assertions are preserved.
// TestSessionDeclarationsExcludeRuntimeResources 防止静态声明重新接纳秘密和释放端口。
func TestSessionDeclarationsExcludeRuntimeResources(t *testing.T) {
	plan := reflect.TypeOf(SessionPlan{})
	target, _ := plan.FieldByName("Target")
	for _, name := range []string{"Environment", "RefreshCredentials", "Isolated", "CloseAfterRun"} {
		if _, exists := target.Type.FieldByName(name); exists {
			t.Errorf("static session target contains runtime resource %s", name)
		}
	}
	connectors, _ := plan.FieldByName("Connectors")
	for _, name := range []string{"Environment", "Headers"} {
		if _, exists := connectors.Type.Elem().FieldByName(name); exists {
			t.Errorf("static connector contains secret resource %s", name)
		}
	}
}

// Migrated from host/workspace/runtime_files_test.go; existing behavior assertions are preserved.
func TestPrepareRuntimeFilesLayersVirtualTeamInstructionsIntoRuntimeFile(t *testing.T) {
	workspacePath := t.TempDir()
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatalf("NewWorkspacePreparer() error = %v", err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())
	var codex RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == RuntimeIDCodex {
			codex = runtime
			break
		}
	}
	teamContent := "协作优先级\n\n# Team: 测试团队"
	capabilityContent := constants.ChannelTasksMCPCapabilityInstructions
	err = sdk.prepareRuntimeFiles(
		DetectionResult{Runtime: codex},
		Workspace{Path: workspacePath, Managed: true},
		[]Instruction{
			{Name: "IDENTITY.md", Content: "伙伴身份"},
			{Name: "PERSONA.md", Content: "工作风格"},
			{Name: "PLAYBOOK.md", Content: "工作方式"},
			{Name: constants.TeamInstructionFileName, Content: teamContent},
			{Name: constants.ChannelTasksMCPTitle, Content: capabilityContent},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	agentsPath := filepath.Join(workspacePath, codex.InstructionFileName)
	agentsText, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	agents := string(agentsText)
	for _, expected := range []string{
		"temporary or unrelated to any project must be created under `attachments/generated/`",
		"伙伴身份",
		"工作风格",
		"工作方式",
		"## TEAM.md",
		"协作优先级",
		"# Team: 测试团队",
		"## NeuxBot Team Tasks",
		capabilityContent,
	} {
		if !strings.Contains(agents, expected) {
			t.Fatalf("AGENTS.md = %q, want %q", agents, expected)
		}
	}
	for _, forbidden := range []string{
		"## IDENTITY.md",
		"## PERSONA.md",
		"## PLAYBOOK.md",
	} {
		if strings.Contains(agents, forbidden) {
			t.Fatalf("AGENTS.md = %q, must not contain %q", agents, forbidden)
		}
	}
	teamPath := filepath.Join(workspacePath, constants.TeamInstructionFileName)
	if _, err := os.Stat(teamPath); !os.IsNotExist(err) {
		t.Fatalf("virtual TEAM.md must not be materialized: %v", err)
	}
}

func TestPrepareRuntimeFilesRemovesStaleTeamMD(t *testing.T) {
	workspacePath := t.TempDir()
	teamPath := filepath.Join(workspacePath, constants.TeamInstructionFileName)
	if err := os.WriteFile(teamPath, []byte("stale team"), 0o600); err != nil {
		t.Fatalf("seed TEAM.md: %v", err)
	}
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatalf("NewWorkspacePreparer() error = %v", err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())
	var codex RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == RuntimeIDCodex {
			codex = runtime
			break
		}
	}
	err = sdk.prepareRuntimeFiles(
		DetectionResult{Runtime: codex},
		Workspace{Path: workspacePath, Managed: true},
		[]Instruction{{Name: "IDENTITY.md", Content: "solo chat"}},
		nil,
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	if _, err := os.Stat(teamPath); !os.IsNotExist(err) {
		t.Fatalf("stale TEAM.md should be removed: %v", err)
	}
}

// TestPrepareRuntimeInstructionsStayStableAcrossTurns 验证相同结构化输入不会生成每轮变化的模型指令。
func TestPrepareRuntimeInstructionsStayStableAcrossTurns(t *testing.T) {
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatalf("NewWorkspacePreparer() error = %v", err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())
	for _, runtime := range registry.Runtimes() {
		runtime := runtime
		t.Run(runtime.ID, func(t *testing.T) {
			workspacePath := t.TempDir()
			workspace := Workspace{Path: workspacePath, Managed: true}
			instructions := []Instruction{{Name: "IDENTITY.md", Content: "稳定伙伴档案"}}
			var first []byte
			for turn := range 2 {
				if prepareErr := sdk.prepareRuntimeFiles(
					DetectionResult{Runtime: runtime},
					workspace,
					instructions,
					nil,
				); prepareErr != nil {
					t.Fatalf("prepare turn %d: %v", turn+1, prepareErr)
				}
				content, readErr := os.ReadFile(filepath.Join(workspacePath, runtime.InstructionFileName))
				if readErr != nil {
					t.Fatalf("read turn %d instructions: %v", turn+1, readErr)
				}
				if turn == 0 {
					first = append([]byte(nil), content...)
					continue
				}
				if string(content) != string(first) {
					t.Fatalf("instructions changed across turns:\nfirst=%q\nsecond=%q", first, content)
				}
			}
		})
	}
}

func TestPrepareRuntimeFilesLinksCurrentSkills(t *testing.T) {
	workspacePath := t.TempDir()
	skillPath := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatalf("NewWorkspacePreparer() error = %v", err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())
	var codex RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == RuntimeIDCodex {
			codex = runtime
		}
	}
	err = sdk.prepareRuntimeFiles(
		DetectionResult{
			Runtime:      codex,
			Capabilities: CapabilitySet{CapabilitySkillDirectory: true},
		},
		Workspace{Path: workspacePath, Managed: true},
		nil,
		[]Skill{{Name: "review", Path: skillPath}},
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	target := filepath.Join(runtimeSkillRoot(DetectionResult{Runtime: codex}, workspacePath), "review")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat linked skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("skill target mode = %v, want symbolic link", info.Mode())
	}
	linked, err := filepath.EvalSymlinks(target)
	resolvedSource, sourceErr := filepath.EvalSymlinks(skillPath)
	if err != nil || sourceErr != nil || linked != resolvedSource {
		t.Fatalf("linked skill = %q, %v", linked, err)
	}
	content, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil || string(content) != "# Review" {
		t.Fatalf("materialized skill = %q, %v", content, err)
	}

}

func TestPrepareRuntimeFilesReplacesPreviousSkillVersionOnNextCall(t *testing.T) {
	workspacePath, versions := t.TempDir(), t.TempDir()
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())
	var runtime RuntimeSummary
	for _, candidate := range registry.Runtimes() {
		if candidate.ID == RuntimeIDCodex {
			runtime = candidate
			break
		}
	}
	for _, version := range []string{"1.0.0", "1.0.1"} {
		source := filepath.Join(versions, version)
		if err := os.MkdirAll(source, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(version), 0o600); err != nil {
			t.Fatal(err)
		}
		err = sdk.prepareRuntimeFiles(
			DetectionResult{Runtime: runtime, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}},
			Workspace{Path: workspacePath, Managed: true}, nil,
			[]Skill{{Name: "review", Path: source}},
		)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(runtimeSkillRoot(DetectionResult{Runtime: runtime}, workspacePath), "review", "SKILL.md"))
		if err != nil || string(content) != version {
			t.Fatalf("Runtime kept stale skill: got %q want %q error=%v", content, version, err)
		}
	}
}

// TestPrepareRuntimeFilesWritesProjectRoutingForCodexAndClaude 验证所有 Runtime 使用同一份项目文件路由规则。
func TestPrepareRuntimeFilesWritesProjectRoutingForCodexAndClaude(t *testing.T) {
	workspacePath := t.TempDir()
	for _, relativePath := range []string{"projects/app", "projects/docs"} {
		if err := os.MkdirAll(filepath.Join(workspacePath, relativePath), 0o700); err != nil {
			t.Fatalf("create project directory %q: %v", relativePath, err)
		}
	}
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatalf("NewWorkspacePreparer() error = %v", err)
	}
	sdk := NewWorkspacePreparer(registry.Runtimes())

	for _, runtimeID := range []string{RuntimeIDCodex, RuntimeIDClaude} {
		t.Run(runtimeID, func(t *testing.T) {
			var summary RuntimeSummary
			for _, candidate := range registry.Runtimes() {
				if candidate.ID == runtimeID {
					summary = candidate
					break
				}
			}
			err := sdk.prepareRuntimeFiles(
				DetectionResult{Runtime: summary},
				Workspace{
					Path:    workspacePath,
					Managed: true,
					ProjectDirectories: []WorkspaceProjectDirectory{
						{Path: "projects/app", Default: true},
						{Path: "projects/docs"},
					},
				},
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
			}
			content, err := os.ReadFile(filepath.Join(workspacePath, summary.InstructionFileName))
			if err != nil {
				t.Fatalf("read Runtime instructions: %v", err)
			}
			text := string(content)
			for _, expected := range []string{
				"`projects/app` (default project directory)",
				"`projects/docs`",
				"New or modified files that clearly belong to a project must be written under that project's directory",
				"temporary or unrelated to any project must be created under `attachments/generated/`",
				"Other paths under `attachments/` contain user-provided files; do not overwrite them",
				"Do not create user deliverables in the NeuxBot Chat workspace root",
			} {
				if !strings.Contains(text, expected) {
					t.Fatalf("Runtime instructions = %q, want %q", text, expected)
				}
			}
			for _, forbidden := range []string{"projects/share", "select the share directory"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("Runtime instructions = %q, must not contain %q", text, forbidden)
				}
			}
		})
	}
}

func TestLinkOrCopyRuntimeSkillFallsBackForExistingTarget(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatalf("write source skill: %v", err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("create conflicting target: %v", err)
	}
	if err := linkOrCopyRuntimeSkill(source, target); err != nil {
		t.Fatalf("linkOrCopyRuntimeSkill() error = %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat copied skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("fallback target mode = %v, want copied directory", info.Mode())
	}
	content, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read copied skill: %v", err)
	}
	if string(content) != "# Review" {
		t.Fatalf("copied skill = %q", content)
	}
}

func TestRuntimeSkillPreservesExecutableAndKnowledgeReference(t *testing.T) {
	source := t.TempDir()
	resources := map[string]string{
		"SKILL.md":                 "# Skill\nRead references/wiki-guide.md and run scripts/allycli.",
		"references/wiki-guide.md": "Knowledge reference fixture",
		"scripts/allycli":          "CLI fixture",
	}
	for name, content := range resources {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, copyFallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("copy=%t", copyFallback), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "ally")
			if copyFallback {
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := linkOrCopyRuntimeSkill(source, target); err != nil {
				t.Fatal(err)
			}
			for name, want := range resources {
				path := filepath.Join(target, filepath.FromSlash(name))
				content, err := os.ReadFile(path)
				if err != nil || string(content) != want {
					t.Fatalf("skill resource unavailable: %s %v", name, err)
				}
			}
			info, err := os.Stat(filepath.Join(target, "scripts", "allycli"))
			if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
				t.Fatalf("skill CLI lost execution permission: %v", err)
			}
		})
	}
}

// Migrated from host/workspace/plugin_environment_test.go; existing behavior assertions are preserved.
// TestPluginEnvironmentKeepsNativeIdentitySeparateFromModelAccount 防止模型账号覆盖原生插件唯一配置源。
func TestPluginEnvironmentKeepsNativeIdentitySeparateFromModelAccount(t *testing.T) {
	for _, cli := range []string{constants.RuntimeIDCodex, constants.RuntimeIDClaude} {
		t.Run(cli, func(t *testing.T) {
			shared, account, workspace := t.TempDir(), t.TempDir(), t.TempDir()
			variable := "CODEX_HOME"
			if cli == constants.RuntimeIDClaude {
				variable = "CLAUDE_CONFIG_DIR"
			}
			for name, body := range map[string]string{"config.toml": "model = \"selected\"\nmodel_provider = \"custom\"\n[model_providers.custom]\nbase_url = \"https://provider.invalid\"\nwire_api = \"responses\"\n", "settings.json": `{"env":{"ANTHROPIC_AUTH_TOKEN":"selected-token"}}`, "auth.json": `{"OPENAI_API_KEY":"selected-key"}`} {
				if err := os.WriteFile(filepath.Join(account, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original := `{"token":"native-identity"}`
			if err := os.WriteFile(filepath.Join(shared, "auth.json"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			// 当前测试程序仅供解析可执行路径；准备阶段不得执行它。
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			target := RuntimeTarget{RuntimeID: cli, AccountID: "managed", Environment: map[string]string{variable: account, "PATH": "/fixture/model-path", "MODEL_SECRET": "model-secret"}}
			result, err := PreparePluginEnvironment(PluginEnvironmentRequest{Target: target, Source: RuntimeTarget{RuntimeID: cli, ConfigHome: shared}, Detection: DetectionResult{Runtime: RuntimeSummary{ID: cli, BaseRuntimeID: cli}, ResolvedBinary: binary}, Workspace: workspace, DataDirectory: t.TempDir(), Environment: map[string]string{"PATH": "/fixture/plugin-bin", "PLUGIN_SECRET": "plugin-secret"}, AllowExternal: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Target.ConfigHome != shared || result.Target.Environment[variable] != shared || result.Target.NativePluginScope == "" || result.CommitScope == nil {
				t.Fatalf("native source was replaced by model account: %+v", result.Target)
			}
			path := result.Target.Environment["PATH"]
			if !strings.HasSuffix(path, "/fixture/plugin-bin"+string(os.PathListSeparator)+"/fixture/model-path") {
				t.Fatalf("plugin and model command paths lost ordering: %q", path)
			}
			if result.Target.Environment["PLUGIN_SECRET"] != "plugin-secret" || result.Target.Environment["MODEL_SECRET"] != "model-secret" {
				t.Fatal("private execution environment was lost")
			}
			if cli == constants.RuntimeIDCodex && (result.Target.Environment[constants.AIAccountCodexAPIKeyEnvironment] != "selected-key" || result.Target.CodexPluginConfiguration["model"] != `"selected"`) {
				t.Fatal("selected Codex model account was lost")
			}
			if cli == constants.RuntimeIDClaude && result.Target.ClaudeSettingsPath != filepath.Join(account, "settings.json") {
				t.Fatal("selected Claude model settings were lost")
			}
			if target.Environment[variable] != account || target.Environment["PATH"] != "/fixture/model-path" {
				t.Fatal("assembly rewrote the caller's model environment")
			}
			if err := result.CommitScope(); err != nil {
				t.Fatal(err)
			}
			if body, _ := os.ReadFile(filepath.Join(shared, "auth.json")); string(body) != original {
				t.Fatal("model credentials overwrote native identity")
			}
		})
	}
}

// TestPluginEnvironmentLeavesOtherRuntimeHomesUnchanged 防止无原生插件能力的智能体被套用厂商目录约定。
func TestPluginEnvironmentLeavesOtherRuntimeHomesUnchanged(t *testing.T) {
	result, err := PreparePluginEnvironment(PluginEnvironmentRequest{Target: RuntimeTarget{RuntimeID: "pi", ConfigHome: "/fixture/pi", Environment: map[string]string{"PI_CODING_AGENT_DIR": "/fixture/account", "PATH": "/fixture/base"}}, Detection: DetectionResult{Runtime: RuntimeSummary{ID: "pi", BaseRuntimeID: "pi"}}, Environment: map[string]string{"PATH": "/fixture/plugin"}})
	if err != nil || result.Target.ConfigHome != "/fixture/pi" || result.Target.Environment["PI_CODING_AGENT_DIR"] != "/fixture/account" || result.Target.Environment["PATH"] != "/fixture/plugin"+string(os.PathListSeparator)+"/fixture/base" || result.CommitScope != nil {
		t.Fatalf("unrelated native environment changed: %+v %v", result, err)
	}
}

// Migrated from host/workspace/plugin_fallback_real_test.go; existing behavior assertions are preserved.
// TestGeminiPluginFallbackDiscovery 用真实CLI只读发现生产Skill投射，不发起模型调用。
func TestGeminiPluginFallbackDiscovery(t *testing.T) {
	if os.Getenv("NEUXALLY_GEMINI_PLUGIN_ACCEPTANCE") == "" {
		t.Skip("explicit isolated Gemini skill discovery only")
	}
	binary, err := exec.LookPath("gemini")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for name, content := range map[string]string{".codex-plugin/plugin.json": `{"name":"fallback-acceptance","version":"1"}`, "skills/native-probe/SKILL.md": "---\nname: native-probe\ndescription: Verify Skill fallback discovery.\n---\nRead references/rules.md.", "skills/native-probe/references/rules.md": "Complete relative resource preserved."} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := plugins.Stage(filepath.Join(root, "staging"), source)
	if err != nil {
		t.Fatal(err)
	}
	pkg, pkgRoot, err := plugins.SelectTarget(stage.Root, stage.Package, RuntimeIDGemini)
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	var runtime RuntimeSummary
	for _, summary := range sdk.Runtimes() {
		if summary.ID == RuntimeIDGemini {
			runtime = summary
		}
	}
	skills := []Skill{}
	for _, skill := range pkg.Skills {
		skills = append(skills, Skill{Name: skill.Name, Path: filepath.Join(pkgRoot, skill.Path)})
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := NewWorkspacePreparer(sdk.Runtimes()).prepareRuntimeFiles(DetectionResult{Runtime: runtime, State: DetectionStateReady, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}, Workspace{Path: workspace, Managed: true}, nil, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".gemini/skills/native-probe/references/rules.md")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "skills", "list")
	command.WaitDelay = time.Second
	command.Dir = workspace
	environment, err := accountconfig.Prepare(workspace, accountconfig.Spec{RuntimeID: constants.RuntimeIDGemini, ID: "fallback-fixture", Revision: 1, AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: "http://127.0.0.1:1/v1beta", APIKey: "local-placeholder-not-a-secret", Model: "gemini-3.8-flash", SelectedModels: []string{"gemini-3.8-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	command.Env = append(accountconfig.Environment(os.Environ()), "GEMINI_CLI_NO_RELAUNCH=1")
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "native-probe") {
		t.Fatalf("real fallback discovery: %s %v", output, err)
	}
	version, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Gemini %s real CLI discovered production Skill projection: %s", strings.TrimSpace(string(version)), strings.TrimSpace(string(output)))
}

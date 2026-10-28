package runtimehost

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
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
	events []runtimes.Event
}

func (s *chatSinkFixture) Accept(_ context.Context, e runtimes.Event) error {
	s.events = append(s.events, e)
	return nil
}
func chatFixture(t *testing.T, d *chatDriverFixture) (*ChatHost, runtimes.RunRequest) {
	t.Helper()
	request := runtimes.RunRequest{Target: runtimes.RuntimeTarget{RuntimeID: "codex"}, Detection: runtimes.DetectionResult{Runtime: runtimes.RuntimeSummary{ID: "codex", BaseRuntimeID: "codex"}}, Workspace: runtimes.Workspace{Path: t.TempDir(), Managed: true}, Session: runtimes.SessionDirective{Mode: runtimes.SessionModeNew}, Message: runtimes.Message{Text: "first"}, Policy: runtimes.RunPolicy{Model: "model", Execution: runtimes.ExecutionPolicy{PermissionLevel: runtimes.PermissionLevelAuto}}}
	host := NewChat(ChatConfig{Launch: func(runtimes.RuntimeTarget, runtimes.DetectionResult) (runtimes.LaunchFacts, error) {
		return runtimes.LaunchFacts{BaseRuntimeID: "codex", Executable: "fixture"}, nil
	}, Driver: func(Environment, acpdriver.Factory) (c.Driver, error) { return d, nil }})
	t.Cleanup(func() { _ = host.Close(context.Background()) })
	return host, request
}
func TestChatHostRetainsHotResourcesAndSendsOnlyAddedInput(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	var releases atomic.Int32
	result := host.RunWithLease(context.Background(), request, &chatSinkFixture{}, func(context.Context) error { releases.Add(1); return nil })
	if result.Failure != nil || result.Session.OutputID != "native-session" {
		t.Fatalf("first result=%+v", result)
	}
	if releases.Load() != 0 {
		t.Fatal("hot native session lost its resource lease")
	}
	request.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: result.Session.OutputID}
	request.Message.Text = "second"
	request.History = []runtimes.HistoryMessage{{Role: "user", Content: "first"}, {Role: "assistant", Content: "done"}}
	result = host.RunWithLease(context.Background(), request, &chatSinkFixture{}, func(context.Context) error { releases.Add(1); return nil })
	if result.Failure != nil || len(driver.opened) != 1 || len(driver.turns) != 2 || driver.turns[1].Input.Text != "second" || releases.Load() != 1 {
		t.Fatalf("resume result=%+v opens=%d turns=%+v releases=%d", result, len(driver.opened), driver.turns, releases.Load())
	}
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 2 {
		t.Fatalf("released=%d", releases.Load())
	}
}
func TestChatHostNativeCloseFailureKeepsAccountUntilRetried(t *testing.T) {
	driver := &chatDriverFixture{}
	driver.failClose.Store(true)
	host, request := chatFixture(t, driver)
	request.Target.CloseAfterRun = true
	var releases atomic.Int32
	result := host.RunWithLease(context.Background(), request, &chatSinkFixture{}, func(context.Context) error { releases.Add(1); return nil })
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
	want := []runtimes.EventType{runtimes.EventTypeInvocationStarted, runtimes.EventTypeThinkingStart, runtimes.EventTypeThinkingDelta, runtimes.EventTypeToolUse, runtimes.EventTypeToolInputSnapshot, runtimes.EventTypeToolUpdate, runtimes.EventTypeToolResult}
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
	request runtimes.ApprovalRequest // request 是收到的产品审批事实。
}

func (f *chatApprovalFixture) Request(_ context.Context, r runtimes.ApprovalRequest) (runtimes.ApprovalDecision, error) {
	f.request = r
	return runtimes.ApprovalDecision{OptionID: "original-allow"}, nil
}

// chatInputFixture 保留公开Schema及用户原始表单内容。
type chatInputFixture struct {
	request runtimes.ElicitationRequest // request 是收到的产品输入事实。
}

func (f *chatInputFixture) Request(_ context.Context, r runtimes.ElicitationRequest) (runtimes.ElicitationDecision, error) {
	f.request = r
	return runtimes.ElicitationDecision{Accepted: true, Content: map[string]any{"answer": "value"}}, nil
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
	request.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: "native-session"}
	request.History = []runtimes.HistoryMessage{{Role: "user", Content: "old prompt"}}
	result := host.Run(context.Background(), request, &chatSinkFixture{})
	if result.Failure != nil || len(driver.opened) != 1 || driver.opened[0].Session.Checkpoint == nil || driver.opened[0].Session.Checkpoint.NativeHandle != "native-session" || len(driver.opened[0].Session.Seed) != 0 {
		t.Fatalf("result=%+v opened=%+v", result, driver.opened)
	}
}
func TestChatHostChangedModelStartsNewSessionWithBoundedHistory(t *testing.T) {
	driver := &chatDriverFixture{}
	host, request := chatFixture(t, driver)
	first := host.Run(context.Background(), request, &chatSinkFixture{})
	request.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Model = "new-model"
	request.History = []runtimes.HistoryMessage{{Role: "user", Content: "old prompt"}, {Role: "assistant", Content: "old answer"}}
	second := host.Run(context.Background(), request, &chatSinkFixture{})
	if second.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Mode != c.SessionNew || len(driver.opened[1].Session.Seed) != 2 {
		t.Fatalf("result=%+v opened=%+v", second, driver.opened)
	}
}

func TestChatHostClosedSessionUsesActualPreviousConfiguration(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			driver := &chatDriverFixture{}
			host, request := chatFixture(t, driver)
			request.Target.CloseAfterRun = true
			first := host.Run(context.Background(), request, &chatSinkFixture{})
			request.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: first.Session.OutputID}
			request.History = []runtimes.HistoryMessage{{Role: "user", Content: "prior input"}}
			if changed {
				request.Policy.Model = "another-model"
			}
			result := host.Run(context.Background(), request, &chatSinkFixture{})
			if result.Failure != nil || len(driver.opened) != 2 {
				t.Fatalf("result=%+v opens=%d", result, len(driver.opened))
			}
			opened := driver.opened[1].Session
			if changed && (opened.Mode != c.SessionNew || len(opened.Seed) != 1) {
				t.Fatalf("changed configuration resumed old context: %+v", opened)
			}
			if !changed && (opened.Mode != c.SessionResume || len(opened.Seed) != 0) {
				t.Fatalf("unchanged configuration replayed history: %+v", opened)
			}
		})
	}
}
func TestChatResourcesFailedReleaseCanRetry(t *testing.T) {
	calls := 0
	resources := &chatResources{release: func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("close pending")
		}
		return nil
	}}
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
	first := host.Run(context.Background(), request, &chatSinkFixture{})
	request.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: first.Session.OutputID}
	request.Policy.Execution.WorkspaceReadOnly = true
	request.Message.Text = "summarize"
	request.History = []runtimes.HistoryMessage{{Role: "user", Content: "must not replay"}}
	result := host.RunSummaryWithLease(context.Background(), request, &chatSinkFixture{}, nil)
	if result.Failure != nil || len(driver.opened) != 2 || driver.opened[1].Session.Checkpoint == nil || driver.opened[1].Session.Checkpoint.NativeHandle != first.Session.OutputID || len(driver.opened[1].Session.Seed) != 0 {
		t.Fatalf("summary result=%+v opened=%+v", result, driver.opened)
	}
	if !driver.opened[1].Workspace.ReadOnly || host.sessions[request.Workspace.Path].runtime != nil {
		t.Fatal("summary retained a native owner or lost readonly policy")
	}
	request.Policy.Execution.WorkspaceReadOnly = false
	request.Message.Text = "continue"
	result = host.Run(context.Background(), request, &chatSinkFixture{})
	if result.Failure != nil || driver.opened[2].Session.Mode != c.SessionResume || len(driver.opened[2].Session.Seed) != 0 {
		t.Fatalf("summary overwrote ordinary configuration: %+v", result)
	}
}
func TestChatAllElevenProfilesPreservePermissionAndInputFacts(t *testing.T) {
	entries := []struct{ base, normal, automatic, full string }{
		{runtimes.RuntimeIDCodex, "read-only", "agent", "agent-full-access"}, {runtimes.RuntimeIDClaude, "default", "auto", "bypassPermissions"}, {runtimes.RuntimeIDKimi, "", "", ""}, {runtimes.RuntimeIDCursor, "agent", "agent", "agent"}, {runtimes.RuntimeIDPi, "", "", ""}, {runtimes.RuntimeIDOpenCode, "", "", ""}, {runtimes.RuntimeIDGemini, "default", "default", "yolo"}, {runtimes.RuntimeIDGrok, "", "", ""}, {runtimes.RuntimeIDWorkBuddy, "default", "auto", "bypassPermissions"}, {runtimes.RuntimeIDQoder, "", "", ""}, {runtimes.RuntimeIDQoderCN, "", "", ""},
	}
	for _, entry := range entries {
		t.Run(entry.base, func(t *testing.T) {
			for index, permission := range []runtimes.PermissionLevel{runtimes.PermissionLevelDefault, runtimes.PermissionLevelAuto, runtimes.PermissionLevelFullAccess} {
				request := runtimes.RunRequest{Target: runtimes.RuntimeTarget{RuntimeID: entry.base}, Detection: runtimes.DetectionResult{Runtime: runtimes.RuntimeSummary{ID: entry.base}}, Workspace: runtimes.Workspace{Path: t.TempDir()}, Policy: runtimes.RunPolicy{Execution: runtimes.ExecutionPolicy{PermissionLevel: permission}}, Message: runtimes.Message{Text: "current", Attachments: []runtimes.Attachment{{Name: "file", Path: "attachments/file.txt"}}, CodeReferences: []runtimes.CodeReference{{Path: "main.go", StartLine: 1, EndLine: 2, Content: "fixed"}}}, Skills: []runtimes.Skill{{Name: "chosen", Path: t.TempDir(), Selected: true}}, Connectors: []runtimes.Connector{{ID: "connector", Name: "remote", Transport: runtimes.ConnectorTransportStreamableHTTP, URL: "https://example.invalid/mcp", Headers: map[string]string{"Authorization": "private-secret"}, Selected: true}}}
				spec, bindings, err := chatSpec(request)
				if err != nil || spec.Profile.NativeMode != []string{entry.normal, entry.automatic, entry.full}[index] || len(spec.Input.Attachments) != 1 || spec.Input.Attachments[0].Path != filepath.Join(request.Workspace.Path, "attachments/file.txt") || len(spec.Input.CodeReferences) != 1 || spec.Input.CodeReferences[0].Content != "fixed" || len(spec.Input.SelectedSkills) != 1 || len(spec.Input.SelectedMCP) != 1 || len(bindings) != 2 || bindings[1].MCP.Headers["Authorization"] != "private-secret" {
					t.Fatalf("spec=%+v bindings=%+v err=%v", spec, bindings, err)
				}
				encoded, _ := json.Marshal(spec)
				if strings.Contains(string(encoded), "private-secret") {
					t.Fatal("secret entered durable spec")
				}
				if entry.base == runtimes.RuntimeIDCodex && !strings.HasPrefix(spec.Input.Text, "$chosen") {
					t.Fatal("lost Codex skill selection")
				}
				if (entry.base == runtimes.RuntimeIDKimi || entry.base == runtimes.RuntimeIDPi) && !strings.HasPrefix(spec.Input.Text, "/skill:chosen") {
					t.Fatal("lost native skill selection")
				}
			}
		})
	}
}
func TestChatSecretRotationDoesNotInvalidateRetainedResourceSnapshot(t *testing.T) {
	request := runtimes.RunRequest{Target: runtimes.RuntimeTarget{RuntimeID: "codex"}, Workspace: runtimes.Workspace{Path: t.TempDir()}, Connectors: []runtimes.Connector{{ID: "mcp", Name: "server", Transport: runtimes.ConnectorTransportStreamableHTTP, URL: "https://example.invalid", Headers: map[string]string{"Authorization": "first-secret"}}}}
	first, _, err := chatSpec(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Connectors[0].Headers["Authorization"] = "rotated-secret"
	second, _, err := chatSpec(request)
	if err != nil {
		t.Fatal(err)
	}
	if chatSignature(first, request.Target) != chatSignature(second, request.Target) {
		t.Fatal("secret refresh changed static identity")
	}
	request.Target.NativePluginScope = "changed-plugin-snapshot"
	if chatSignature(first, runtimes.RuntimeTarget{}) == chatSignature(first, request.Target) {
		t.Fatal("plugin snapshot change did not invalidate native owner")
	}
}

func TestChatHostDeliversFinalPromptResponseUsage(t *testing.T) {
	driver := &chatDriverFixture{result: &c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn, Usage: c.Usage{Known: true, InputTokens: 11, OutputTokens: 13, CachedReadTokens: 17, CachedWriteTokens: 19, ThoughtTokens: 23, ContextKnown: true, ContextTokens: 29, ContextWindowTokens: 31}}}
	host, request := chatFixture(t, driver)
	sink := &chatSinkFixture{}
	result := host.Run(context.Background(), request, sink)
	if result.Failure != nil {
		t.Fatalf("result=%+v", result)
	}
	var usage *runtimes.UsageData
	for _, event := range sink.events {
		if event.Type == runtimes.EventTypeUsage {
			usage = &runtimes.UsageData{}
			if err := json.Unmarshal(event.Data, usage); err != nil {
				t.Fatal(err)
			}
		}
	}
	if usage == nil || usage.Turn == nil || usage.Context == nil {
		t.Fatalf("missing final prompt response usage: events=%+v", sink.events)
	}
	if *usage.Turn != (runtimes.TurnUsageData{InputTokens: 11, OutputTokens: 13, CacheReadTokens: 17, CacheCreationTokens: 19, ReasoningOutputTokens: 23}) || *usage.Context != (runtimes.ContextUsageData{UsedTokens: 29, WindowTokens: 31}) {
		t.Fatalf("usage=%+v context=%+v", usage.Turn, usage.Context)
	}
}
func TestChatResultMapsNativeCancelledStatusWithoutFault(t *testing.T) {
	result := chatResult(runtimes.RunRequest{}, c.Result{Status: c.StatusCancelled, StopReason: c.StopCancelled, Effects: c.Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != runtimes.FailureCodeContextCanceled || result.TerminationReason != constants.RuntimeTerminationReasonCancelled {
		t.Fatalf("cancelled mapped as success: %+v", result)
	}
}
func TestChatResultCancellationDoesNotReplacePrimaryFault(t *testing.T) {
	result := chatResult(runtimes.RunRequest{}, c.Result{Status: c.StatusCancelled, StopReason: c.StopCancelled, Fault: &c.Fault{Code: c.FaultUnsupported, Stage: "prompt", Message: "unsupported native capability"}, Effects: c.Effects{PromptAccepted: true}}, &chatObserver{})
	if result.ExitCode == 0 || result.Failure == nil || result.Failure.Code != runtimes.FailureCodeUnsupportedCapability {
		t.Fatalf("lost primary fault: %+v", result)
	}
}

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestFinalEvidenceAndLastMessage(t *testing.T) {
	cases := []struct {
		name     string                    // name 是行为用例名称。
		messages []native.AssistantMessage // messages 是依次到达的消息。
		stop     native.StopReason         // stop 是真实结束原因。
		status   OutputStatus              // status 是预期输出状态。
		text     string                    // text 是独立推导的最终正文。
	}{
		{"unknown", []native.AssistantMessage{{MessageID: "1", Delta: "guess"}}, native.StopEndTurn, OutputUnavailable, ""},
		{"last final", []native.AssistantMessage{{MessageID: "1", Phase: native.PhaseFinal, Delta: "first", Complete: true}, {MessageID: "2", Phase: native.PhaseCommentary, Delta: "tool progress"}, {MessageID: "3", Phase: native.PhaseFinal, Delta: "answer", Complete: true}}, native.StopEndTurn, OutputAvailable, "answer"},
		{"final phase", []native.AssistantMessage{{MessageID: "1", Phase: native.PhaseFinal, Delta: "answer"}}, native.StopEndTurn, OutputAvailable, "answer"},
		{"refused", nil, native.StopRefused, OutputRefused, ""},
		{"truncated", []native.AssistantMessage{{MessageID: "1", Phase: native.PhaseFinal, Delta: "part"}}, native.StopMaxTokens, OutputTruncated, "part"},
		{"cancelled", []native.AssistantMessage{{MessageID: "1", Phase: native.PhaseFinal, Delta: "part"}}, native.StopCancelled, OutputUnavailable, ""},
		{"empty", []native.AssistantMessage{{MessageID: "1", Phase: native.PhaseFinal, Complete: true}}, native.StopEndTurn, OutputEmpty, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := newCollector(OutputStrict, native.Limits{MaxOutputBytes: 64, MaxEvents: 32})
			for _, m := range tt.messages {
				if err := co.Accept(m); err != nil {
					t.Fatal(err)
				}
			}
			got := co.Finish(tt.stop)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
func TestLimitAndInterleaving(t *testing.T) {
	co := newCollector(OutputStrict, native.Limits{MaxOutputBytes: 4, MaxEvents: 32})
	if err := co.Accept(native.AssistantMessage{MessageID: "1", Phase: native.PhaseFinal, Delta: "12345"}); err == nil {
		t.Fatal("expected output limit")
	}
	if got := co.Finish(native.StopEndTurn); got.Status != OutputLimitExceeded || got.Text != "" {
		t.Fatalf("got %#v", got)
	}
	co = newCollector(OutputStrict, native.Limits{MaxOutputBytes: 64, MaxEvents: 32})
	_ = co.Accept(native.AssistantMessage{MessageID: "1", Phase: native.PhaseFinal, Delta: "old"})
	_ = co.Accept(native.AssistantMessage{MessageID: "2", Phase: native.PhaseFinal, Delta: "new"})
	if err := co.Accept(native.AssistantMessage{MessageID: "1", Phase: native.PhaseFinal, Delta: "tail"}); err == nil {
		t.Fatal("interleaving must fail conservatively")
	}
	if got := co.Finish(native.StopEndTurn); got.Status != OutputUnavailable {
		t.Fatalf("got %#v", got)
	}
}
func BenchmarkCollector(b *testing.B) {
	for range b.N {
		co := newCollector(OutputStrict, native.Limits{MaxOutputBytes: 1 << 20, MaxEvents: 4096})
		for range 100 {
			_ = co.Accept(native.AssistantMessage{MessageID: "a", Phase: native.PhaseFinal, Delta: "some answer token "})
		}
		_ = co.Finish(native.StopEndTurn)
	}
}

func TestMissingMessageIdentityIsUnavailableWithoutExecutionFailure(t *testing.T) {
	co := newCollector(OutputStrict, native.Limits{MaxOutputBytes: 64, MaxEvents: 32})
	if err := co.Accept(native.AssistantMessage{Phase: native.PhaseUnknown, Delta: "no identity"}); err != nil {
		t.Fatalf("missing proof must not fail execution: %v", err)
	}
	if got := co.Finish(native.StopEndTurn); got.Status != OutputUnavailable {
		t.Fatalf("%#v", got)
	}
}

func TestLateCommentaryClassificationRevokesOnlyItsOwnCandidates(t *testing.T) {
	cases := []struct {
		name               string       // name 是需要保留或撤销候选的行为。
		priorFinal         bool         // priorFinal 表示存在无关的已验证答案。
		verifiedCompletion bool         // verifiedCompletion 表示当前消息曾获等价完成证明。
		status             OutputStatus // status 是预期输出状态。
		text               string       // text 是独立推导的最终正文。
	}{{"unknown becomes commentary", false, false, OutputUnavailable, ""}, {"previous final survives", true, false, OutputAvailable, "real answer"}, {"completion candidate becomes commentary", false, true, OutputUnavailable, ""}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := newCollector(OutputBestEffort, native.Limits{MaxOutputBytes: 128, MaxEvents: 16})
			if tt.priorFinal {
				if err := co.Accept(native.AssistantMessage{MessageID: "answer", Phase: native.PhaseFinal, Delta: "real answer", Complete: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := co.Accept(native.AssistantMessage{MessageID: "process", Delta: "process only", Complete: tt.verifiedCompletion, CompletionVerified: tt.verifiedCompletion}); err != nil {
				t.Fatal(err)
			}
			if err := co.Accept(native.AssistantMessage{MessageID: "process", Phase: native.PhaseCommentary, Complete: true}); err != nil {
				t.Fatal(err)
			}
			got := co.Finish(native.StopEndTurn)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("commentary candidate leaked or unrelated final cleared: %#v", got)
			}
			if tt.priorFinal && got.Reliability != ReliabilityVerifiedPhase {
				t.Fatalf("unrelated final proof changed: %#v", got)
			}
		})
	}
}

// TestCompletionReclassifiesCurrentMessage 捕获完成边界重分类丢失正文、泄漏候选或覆盖旧最终答案的错误。
func TestCompletionReclassifiesCurrentMessage(t *testing.T) {
	cases := []struct {
		name       string              // name 是完成重分类场景。
		priorFinal bool                // priorFinal 表示另一个消息已有最终答案。
		before     native.MessagePhase // before 是流式正文最初的阶段。
		after      native.MessagePhase // after 是完成边界提供的权威阶段。
		tail       string              // tail 是完成通知独有的追加正文。
		status     OutputStatus        // status 是完成后的预期输出状态。
		text       string              // text 是独立推导的最终正文。
	}{
		{name: "commentary becomes final", before: native.PhaseCommentary, after: native.PhaseFinal, status: OutputAvailable, text: "body"},
		{name: "completion appends final tail", before: native.PhaseCommentary, after: native.PhaseFinal, tail: " tail", status: OutputAvailable, text: "body tail"},
		{name: "final becomes commentary", before: native.PhaseFinal, after: native.PhaseCommentary, status: OutputUnavailable},
		{name: "previous final survives retraction", priorFinal: true, before: native.PhaseFinal, after: native.PhaseCommentary, status: OutputAvailable, text: "prior answer"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := newCollector(OutputBestEffort, native.Limits{MaxOutputBytes: 32, MaxEvents: 16})
			if tt.priorFinal {
				if err := co.Accept(native.AssistantMessage{MessageID: "prior", Phase: native.PhaseFinal, Delta: "prior answer", Complete: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := co.Accept(native.AssistantMessage{MessageID: "current", Phase: tt.before, Delta: "body"}); err != nil {
				t.Fatal(err)
			}
			// 查询当前结果不能把仍可被完成阶段撤销的候选固化。
			_ = co.Finish(native.StopEndTurn)
			if err := co.Accept(native.AssistantMessage{MessageID: "current", Phase: tt.after, Delta: tt.tail, Complete: true}); err != nil {
				t.Fatalf("authoritative completion was rejected: %v", err)
			}
			got := co.Finish(native.StopEndTurn)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("reclassification lost or leaked a candidate: %+v", got)
			}
			if tt.status == OutputAvailable && got.Reliability != ReliabilityVerifiedPhase {
				t.Fatalf("final phase proof lost: %+v", got)
			}
		})
	}
}

// TestPhaseConflictWithoutCompletionRemainsInvalid 防止允许非完成通知任意改写已知消息阶段。
func TestPhaseConflictWithoutCompletionRemainsInvalid(t *testing.T) {
	for _, phase := range []native.MessagePhase{native.PhaseCommentary, native.PhaseFinal} {
		t.Run(string(phase), func(t *testing.T) {
			co := newCollector(OutputBestEffort, native.Limits{MaxOutputBytes: 32, MaxEvents: 16})
			if err := co.Accept(native.AssistantMessage{MessageID: "a", Phase: phase, Delta: "body"}); err != nil {
				t.Fatal(err)
			}
			next := native.PhaseFinal
			if phase == native.PhaseFinal {
				next = native.PhaseCommentary
			}
			var fault *native.Fault
			if err := co.Accept(native.AssistantMessage{MessageID: "a", Phase: next}); !errors.As(err, &fault) || fault.Code != native.FaultProtocol {
				t.Fatalf("non-completion phase conflict accepted: %v", err)
			}
			if got := co.Finish(native.StopEndTurn); got.Status != OutputUnavailable {
				t.Fatalf("conflicting message became output: %+v", got)
			}
		})
	}
}

// TestCommentaryBudgetIsDeferredUntilFinal 验证过程正文有界保留，溢出只在该消息被证实为最终答案时影响输出。
func TestCommentaryBudgetIsDeferredUntilFinal(t *testing.T) {
	for _, promote := range []bool{false, true} {
		t.Run(map[bool]string{false: "discard oversized process", true: "oversized process becomes final"}[promote], func(t *testing.T) {
			co := newCollector(OutputBestEffort, native.Limits{MaxOutputBytes: 4, MaxEvents: 16})
			if err := co.Accept(native.AssistantMessage{MessageID: "prior", Phase: native.PhaseFinal, Delta: "real", Complete: true}); err != nil {
				t.Fatal(err)
			}
			for range 1024 {
				if err := co.Accept(native.AssistantMessage{MessageID: "process", Phase: native.PhaseCommentary, Delta: "work"}); err != nil {
					t.Fatalf("process text consumed final output budget: %v", err)
				}
				if co.text.Len() > 4 {
					t.Fatalf("process retention exceeded output memory bound: %d", co.text.Len())
				}
			}
			if got := co.Finish(native.StopEndTurn); got.Status != OutputAvailable || got.Text != "real" {
				t.Fatalf("oversized process damaged unrelated final: %+v", got)
			}
			if promote {
				var fault *native.Fault
				if err := co.Accept(native.AssistantMessage{MessageID: "process", Phase: native.PhaseFinal, Complete: true}); !errors.As(err, &fault) || fault.Code != native.FaultLimit {
					t.Fatalf("discarded body silently became final: %v", err)
				}
				if got := co.Finish(native.StopEndTurn); got.Status != OutputLimitExceeded || got.Text != "" {
					t.Fatalf("discarded final body reported as valid output: %+v", got)
				}
				return
			}
			if err := co.Accept(native.AssistantMessage{MessageID: "next", Phase: native.PhaseFinal, Delta: "done", Complete: true}); err != nil {
				t.Fatalf("process overflow poisoned next message: %v", err)
			}
			if got := co.Finish(native.StopEndTurn); got.Status != OutputAvailable || got.Text != "done" {
				t.Fatalf("next final lost: %+v", got)
			}
		})
	}
}

func planSpec() executionSpec {
	return executionSpec{OpenRequest: native.OpenRequest{Profile: native.Profile{DriverID: "fake", Subject: "a"}, Workspace: native.WorkspaceScope{Root: "/allowed/project"}, Contributions: native.Contributions{Instructions: []native.Instruction{{Name: "profile", Text: "static"}}}}, Input: native.Input{Text: "first"}, Policies: executionPolicies{Tools: native.ToolPolicy{Mode: native.ToolsAllowList, Allow: []string{"read", "write"}}, NativeConfig: native.NativeConfigExplicitOnly, Output: OutputStrict}}
}
func TestFreezeInputAndPermissionIntersection(t *testing.T) {
	in := planSpec()
	caps := nativeCapabilities{ExplicitConfig: true, ToolAllowList: true, HotResume: true}
	grants := Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	a, err := compileExecution(in, caps, grants, native.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	in.Input.Text = "second"
	in.Contributions.Instructions[0].Text = "changed"
	in.Policies.Tools.Allow[0] = "delete"
	f := a.Spec()
	if f.Input.Text != "first" || f.Contributions.Instructions[0].Text != "static" || len(f.Policies.Tools.Allow) != 1 || f.Policies.Tools.Allow[0] != "read" {
		t.Fatalf("not frozen: %#v", f)
	}
	f.Contributions.Instructions[0].Text = "mutated"
	if a.Spec().Contributions.Instructions[0].Text != "static" {
		t.Fatal("getter exposed storage")
	}
}
func TestRejectResumeSeedAndUnknownEnforcement(t *testing.T) {
	in := planSpec()
	in.Session = native.SessionDirective{Mode: native.SessionResume}
	in.Seed = []native.SeedMessage{{Role: "user", Text: "old"}}
	if _, err := compileExecution(in, nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}, Grants{Directories: []string{"/allowed"}}, native.Limits{}); err == nil {
		t.Fatal("resume seed accepted")
	}
	in = planSpec()
	if _, err := compileExecution(in, nativeCapabilities{}, Grants{}, native.Limits{}); err == nil {
		t.Fatal("unknown isolation accepted")
	}
	in = planSpec()
	in.Workspace.Root = "/outside"
	if _, err := compileExecution(in, nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}, Grants{Directories: []string{"/allowed"}}, native.Limits{}); err == nil {
		t.Fatal("workspace expanded")
	}
}

// TestColdResumeUsesNativeIdentityAndKeepsPermissionValidation 防止原生恢复绕过能力及目录上界校验。
func TestColdResumeUsesNativeIdentityAndKeepsPermissionValidation(t *testing.T) {
	input := planSpec()
	input.Session = native.SessionDirective{Mode: native.SessionResume, NativeHandle: "persisted-native"}
	capabilities := nativeCapabilities{ExplicitConfig: true, ToolAllowList: true, ColdResume: true}
	grants := Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	compiled, err := compileExecution(input, capabilities, grants, native.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Spec().Session.NativeHandle != "persisted-native" {
		t.Fatal("native identity lost")
	}
	capabilities.ColdResume = false
	if _, err = compileExecution(input, capabilities, grants, native.Limits{}); err == nil {
		t.Fatal("unverified cold resume accepted")
	}
	capabilities.ColdResume = true
	input.Workspace.Root = "/outside"
	if _, err = compileExecution(input, capabilities, grants, native.Limits{}); err == nil {
		t.Fatal("cold resume expanded granted workspace")
	}
}

func TestRejectSymlinkEscapeAndImpossibleBudgets(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(allowed, "escape")); err != nil {
		t.Fatal(err)
	}
	in := planSpec()
	in.Workspace.Root = filepath.Join(allowed, "escape")
	grants := Grants{Directories: []string{allowed}, Tools: []string{"read"}}
	caps := nativeCapabilities{ExplicitConfig: true, ToolAllowList: true, DenyAllTools: true}
	if _, err := compileExecution(in, caps, grants, native.Limits{}); err == nil {
		t.Fatal("symlink expanded workspace outside authorization")
	}
	for _, limits := range []native.Limits{{MaxOutputBytes: 20, MaxTotalEventBytes: 10, MaxEventBytes: 10}, {MaxOutputBytes: 10, MaxEventBytes: 20, MaxTotalEventBytes: 10}} {
		in = planSpec()
		in.Limits = limits
		if _, err := compileExecution(in, caps, Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, native.Limits{}); err == nil {
			t.Fatal("impossible event/output budget accepted")
		}
	}
}
func TestFreezePreservesExactTextBytes(t *testing.T) {
	in := planSpec()
	in.Input.Text = "byte\xff"
	p, err := compileExecution(in, nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}, Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, native.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Spec().Input.Text != "byte\xff" {
		t.Fatal("freezing transformed input bytes")
	}
}

func TestSeedBudgetAcceptsBoundaryAndCountsUTF8Bytes(t *testing.T) {
	in := planSpec()
	in.Seed = make([]native.SeedMessage, 40)
	for i := range in.Seed {
		in.Seed[i] = native.SeedMessage{Role: "user"}
	}
	in.Seed[0].Text = strings.Repeat("x", 64<<10)
	caps := nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}
	grants := Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	if _, err := compileExecution(in, caps, grants, native.Limits{}); err != nil {
		t.Fatal(err)
	}
	in.Seed[1].Text = "中"
	if _, err := compileExecution(in, caps, grants, native.Limits{}); err == nil {
		t.Fatal("UTF-8 bytes expanded beyond seed limit")
	}
}

// TestThinkingPreferenceIsFrozen 防止可变偏好指针改写已接纳计划。
func TestThinkingPreferenceIsFrozen(t *testing.T) {
	off := false
	input := planSpec()
	input.Profile.Thinking = &off
	input.Profile.NativeMode = "agent-full-access"
	caps := nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}
	grants := Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	compiled, err := compileExecution(input, caps, grants, native.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	off = true
	if *compiled.Spec().Profile.Thinking {
		t.Fatal("accepted preference changed through caller pointer")
	}
	snapshot := compiled.Spec()
	*snapshot.Profile.Thinking = true
	if *compiled.Spec().Profile.Thinking {
		t.Fatal("getter exposed preference pointer")
	}
}

// TestInactivityBudgetNormalization 防止独立无活动预算丢失、负数被接受或显式设置被默认值覆盖。
func TestInactivityBudgetNormalization(t *testing.T) {
	inherited, err := normalizeLimits(native.Limits{}, defaultLimits())
	if err != nil || inherited.InactivityTimeout <= 0 {
		t.Fatalf("missing inactivity default: %+v %v", inherited, err)
	}
	explicit, err := normalizeLimits(native.Limits{InactivityTimeout: time.Second}, defaultLimits())
	if err != nil || explicit.InactivityTimeout != time.Second {
		t.Fatalf("overrode explicit inactivity budget: %+v %v", explicit, err)
	}
	if _, err := normalizeLimits(native.Limits{InactivityTimeout: -time.Second}, defaultLimits()); err == nil {
		t.Fatal("negative inactivity timeout accepted")
	}
}

// TestSeedCodeSnapshotSharesHistoricalTextBudget 防止历史代码快照绕过新会话的有界播种。
func TestSeedCodeSnapshotSharesHistoricalTextBudget(t *testing.T) {
	input := planSpec()
	input.Seed = []native.SeedMessage{{Role: "user", Text: "中", CodeReferences: []native.CodeReference{{Path: "main.go", Content: strings.Repeat("x", 64<<10)}}}}
	_, err := compileExecution(input, nativeCapabilities{ExplicitConfig: true, ToolAllowList: true}, Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, native.Limits{})
	if err == nil {
		t.Fatal("historical code snapshot exceeded seed text budget")
	}
}

func TestManagerSessionHasOnePreparedTurn(t *testing.T) {
	manager := NewManager(ManagerConfig{Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) { return LaunchFacts{}, nil }})
	plan := SessionPlan{Target: SessionTarget{RuntimeID: RuntimeIDCodex}, Workspace: Workspace{Path: t.TempDir()}, Policy: RunPolicy{Execution: ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}}}
	first, err := manager.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeNew})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	if _, err = manager.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeNew}); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("same session admitted a second turn: %v", err)
	}
	plan.Workspace.Path = t.TempDir()
	second, err := manager.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeNew})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
}

// regressionConnection 是一次原生消费者的外部边界替身。
type regressionConnection struct {
	// prompt 决定当前回合的真实事实与结束时点。
	prompt func(context.Context, native.TurnRequest, native.EventSink) native.TurnResult
	// close 决定原生消费者是否已经退出。
	close func(context.Context) error
}

func (c *regressionConnection) Prompt(ctx context.Context, r native.TurnRequest, s native.EventSink) native.TurnResult {
	return c.prompt(ctx, r, s)
}
func (c *regressionConnection) Close(ctx context.Context) error {
	if c.close != nil {
		return c.close(ctx)
	}
	return nil
}
func regressionManager(t *testing.T, connection NativeConnection) (*Manager, SessionPlan) {
	t.Helper()
	m := NewManager(ManagerConfig{Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
		return LaunchFacts{BaseRuntimeID: RuntimeIDCodex, Executable: "fixture"}, nil
	}, Open: func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		return connection, nil
	}})
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	return m, SessionPlan{Target: SessionTarget{RuntimeID: RuntimeIDCodex}, Workspace: Workspace{Path: t.TempDir()}, Policy: RunPolicy{Execution: ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}}}
}

func TestManagerLatePartialBindSurvivesCloseDeadline(t *testing.T) {
	m, plan := regressionManager(t, nil)
	p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err = m.Close(ctx); err == nil {
		t.Fatal("close discarded outstanding cold acquisition")
	}
	if p.Context().Err() == nil {
		t.Fatal("close did not cancel resource acquisition")
	}
	var attempts atomic.Int32
	allow := false
	resources := SessionResources{Target: regressionTarget(plan), Close: func(ctx context.Context) error {
		if ctx.Err() != nil {
			t.Error("cleanup inherited cancelled preparation")
		}
		attempts.Add(1)
		if !allow {
			return status_error.PluginBusy
		}
		return nil
	}}
	if err = p.Bind(p.Context(), resources); !errors.Is(err, context.Canceled) {
		t.Fatalf("late bind=%v", err)
	}
	if err = p.Close(context.Background()); err == nil {
		t.Fatal("partial resource release fault was hidden")
	}
	if len(m.owners) != 1 || attempts.Load() != 1 {
		t.Fatalf("lost cleanup owner: owners=%d attempts=%d", len(m.owners), attempts.Load())
	}
	allow = true
	if err = m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || len(m.owners) != 0 {
		t.Fatalf("cleanup retried wrong owner: attempts=%d owners=%d", attempts.Load(), len(m.owners))
	}
}

func TestManagerBoundTurnUsesRunContext(t *testing.T) {
	entered := make(chan struct{})
	connection := &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
		_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
		close(entered)
		<-ctx.Done()
		return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
	}}
	m, plan := regressionManager(t, connection)
	declaration, cancelDeclaration := context.WithCancel(context.Background())
	p, err := m.PrepareSession(declaration, plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan)}); err != nil {
		t.Fatal(err)
	}
	cancelDeclaration()
	if p.Context().Err() != nil {
		t.Fatal("short declaration context cancelled bound turn")
	}
	run, cancelRun := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { done <- p.Run(run, TurnInput{Message: Message{Text: "accepted"}}, &chatSinkFixture{}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("bound turn never started")
	}
	cancelRun()
	select {
	case result := <-done:
		if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
			t.Fatalf("run cancellation lost: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("run context did not cancel consumer")
	}
}

func TestManagerTemporaryCapacityIncludesFailedCleanupAndExcludesChats(t *testing.T) {
	var allow atomic.Bool
	var prompts, successes atomic.Int32
	m := NewManager(ManagerConfig{Resolve: func(context.Context, TextRequest) (Environment, error) {
		return Environment{BaseRuntimeID: RuntimeIDCodex, Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, Isolated: true}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, e Environment, _ string) (RuntimeTarget, func(context.Context) error, error) {
		return e.Target, func(context.Context) error { return nil }, nil
	}, Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
		return LaunchFacts{BaseRuntimeID: RuntimeIDCodex, Executable: "fixture"}, nil
	}, Open: func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		return &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
			prompts.Add(1)
			return answer(ctx, r, sink)
		}, close: func(context.Context) error {
			if !allow.Load() {
				return status_error.RuntimeShutdownFailed
			}
			successes.Add(1)
			return nil
		}}, nil
	}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	defer func() { allow.Store(true); _ = m.Close(context.Background()) }()
	input := TextRequest{RuntimeID: RuntimeIDCodex, Messages: []TextMessage{{Role: "user", Content: "once"}}}
	for range 64 {
		if result, err := m.GenerateText(context.Background(), input); err != nil || result.Text != "once" {
			t.Fatalf("capacity admission failed: %+v %v", result, err)
		}
	}
	if _, err := m.GenerateText(context.Background(), input); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("failed owners escaped capacity: %v", err)
	}
	plan := SessionPlan{Target: SessionTarget{RuntimeID: RuntimeIDCodex}, Workspace: Workspace{Path: t.TempDir()}, Policy: RunPolicy{Execution: ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}}}
	chat, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatalf("chat counted against temporary capacity: %v", err)
	}
	m.retryTemporary()
	if !chat.NeedsResources() {
		t.Fatal("temporary retry closed outstanding chat")
	}
	_ = chat.Close(context.Background())
	allow.Store(true)
	m.retryTemporary()
	if len(m.temporary) != 0 || successes.Load() != 64 || prompts.Load() != 64 {
		t.Fatalf("retry replayed task or retained released owner: temporary=%d success=%d prompt=%d", len(m.temporary), successes.Load(), prompts.Load())
	}
	if _, err = m.GenerateText(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 65 {
		t.Fatal("capacity recovery replayed an earlier prompt")
	}
}

func TestManagerCleanupRetryDoesNotRepeatSuccessfulNativeClose(t *testing.T) {
	var nativeCloses, resourceCloses atomic.Int32
	connection := &regressionConnection{prompt: answer, close: func(context.Context) error { nativeCloses.Add(1); return nil }}
	m, plan := regressionManager(t, connection)
	p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan), Close: func(context.Context) error {
		if resourceCloses.Add(1) == 1 {
			return status_error.PluginBusy
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if result := p.Run(context.Background(), TurnInput{Message: Message{Text: "once"}}, &chatSinkFixture{}); result.Failure != nil {
		t.Fatal(result.Failure)
	}
	if err = m.CloseWorkspace(context.Background(), plan.Workspace.Path); err == nil {
		t.Fatal("resource cleanup failure hidden")
	}
	if err = m.CloseWorkspace(context.Background(), plan.Workspace.Path); err != nil {
		t.Fatal(err)
	}
	if nativeCloses.Load() != 1 || resourceCloses.Load() != 2 {
		t.Fatalf("successful step repeated: native=%d resource=%d", nativeCloses.Load(), resourceCloses.Load())
	}
}

func regressionTarget(plan SessionPlan) RuntimeTarget {
	target := plan.Target.LaunchTarget()
	target.Isolated = true
	return target
}

func TestStagedStaticConfigurationChangesOpenFreshContext(t *testing.T) {
	changes := map[string]func(*SessionPlan){
		"model":         func(p *SessionPlan) { p.Policy.Model = "another" },
		"thinking":      func(p *SessionPlan) { off := false; p.Policy.Thinking = &off },
		"permissions":   func(p *SessionPlan) { p.Policy.Execution.PermissionLevel = PermissionLevelDefault },
		"resource key":  func(p *SessionPlan) { p.ResourceKey = "another" },
		"instructions":  func(p *SessionPlan) { p.Instructions = []Instruction{{Name: "profile", Content: "another"}} },
		"native config": func(p *SessionPlan) { p.Target.CodexPluginConfiguration = map[string]string{"model": "another"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			driver := &chatDriverFixture{}
			m, request := chatFixture(t, driver)
			first := runChatFixture(context.Background(), m, request, &chatSinkFixture{})
			if first.Failure != nil {
				t.Fatal(first.Failure)
			}
			plan := request.plan()
			change(&plan)
			p, err := m.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close(context.Background())
			if !p.NeedsResources() || p.Session().Mode != SessionModeNew {
				t.Fatal("changed configuration restored previous context")
			}
			target := request.Target
			target.CodexPluginConfiguration = plan.Target.CodexPluginConfiguration
			if err = p.Bind(p.Context(), SessionResources{Target: target}); err != nil {
				t.Fatal(err)
			}
			result := p.Run(context.Background(), TurnInput{Message: Message{Text: "second"}}, &chatSinkFixture{})
			if result.Failure != nil || len(driver.opened) != 2 || driver.turns[1].Input.Text != "second" {
				t.Fatalf("replacement did not preserve current turn: %+v opens=%d", result, len(driver.opened))
			}
		})
	}
}

func TestStagedClosedPreparationNoLongerNeedsResources(t *testing.T) {
	m, plan := regressionManager(t, nil)
	p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.NeedsResources() {
		t.Fatal("closed preparation requested another resource owner")
	}
	var releases atomic.Int32
	if err = p.Bind(context.Background(), SessionResources{Target: regressionTarget(plan), Close: func(context.Context) error { releases.Add(1); return nil }}); err == nil || releases.Load() != 0 {
		t.Fatal("consumed preparation accepted new resources")
	}
}

func TestManagerDetectionRefreshDoesNotInvalidateStaticSession(t *testing.T) {
	driver := &chatDriverFixture{}
	m, request := chatFixture(t, driver)
	first := runChatFixture(context.Background(), m, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Detection.Version = "refreshed"
	request.Detection.Diagnostics = []Diagnostic{{Code: "refreshed"}}
	p, err := m.PrepareSession(context.Background(), request.plan(), request.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if p.NeedsResources() {
		t.Fatal("nonconfiguration probe facts replaced a retained native consumer")
	}
}

func TestManagerBindingCancellationBeforeHandoff(t *testing.T) {
	m, plan := regressionManager(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	m.config.PrepareWorkspace = func(SessionPlan) error { close(entered); <-release; return nil }
	declaration, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := m.PrepareSession(declaration, plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	var closes atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan), Close: func(context.Context) error { closes.Add(1); return nil }})
	}()
	<-entered
	cancel()
	wasCancelled := false
	select {
	case <-p.Context().Done():
		wasCancelled = true
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	err = <-done
	if !wasCancelled || err == nil {
		t.Errorf("cancelled binding crossed handoff: preparationCancelled=%v bind=%v", wasCancelled, err)
	}
	if closes.Load() != 0 {
		t.Fatal("Bind released partially owned resources")
	}
	if err = p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("cancelled partial owner closes=%d", closes.Load())
	}
}

func TestManagerFailedReplacementCannotReuseUnclosedConsumer(t *testing.T) {
	driver := &chatDriverFixture{}
	m, request := chatFixture(t, driver)
	first := runChatFixture(context.Background(), m, request, &chatSinkFixture{})
	request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	driver.failClose.Store(true)
	changed := request.plan()
	changed.Policy.Model = "replacement"
	if _, err := m.PrepareSession(context.Background(), changed, request.Session); err == nil {
		t.Fatal("replacement bypassed failed native close")
	}
	if _, err := m.PrepareSession(context.Background(), request.plan(), request.Session); err == nil {
		t.Fatal("original declaration reused consumer after failed close")
	}
	if len(driver.opened) != 1 || len(driver.turns) != 1 {
		t.Fatal("failed cleanup opened or replayed a consumer")
	}
	driver.failClose.Store(false)
	p, err := m.PrepareSession(context.Background(), request.plan(), request.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if !p.NeedsResources() {
		t.Fatal("closed consumer was treated as hot")
	}
}

// regressionLease 保留取得失败时仍需交接的物化文件。
type regressionLease struct {
	// close 记录实际资源释放顺序。
	close func(context.Context) error
}

func (l *regressionLease) Resources() []native.ResourceBinding { return nil }
func (l *regressionLease) Close(ctx context.Context) error     { return l.close(ctx) }

// regressionTaskResources 只替换独立任务的领域物化端口。
type regressionTaskResources struct {
	// acquire 可同时返回部分文件租约和取得错误。
	acquire func(context.Context, ResourceRequest) (ResourceLease, error)
}

func (r *regressionTaskResources) Acquire(ctx context.Context, input ResourceRequest) (ResourceLease, error) {
	return r.acquire(ctx, input)
}
func (r *regressionTaskResources) LaunchEnvironment(env map[string]string) map[string]string {
	return env
}
func (r *regressionTaskResources) Revoke() {}
func TestPreparationFailureReleasesReverseEvenReturnedLease(t *testing.T) {
	var order []string
	var opened atomic.Int32
	m := NewManager(ManagerConfig{Resolve: func(context.Context, TextRequest) (Environment, error) {
		return Environment{BaseRuntimeID: RuntimeIDCodex, Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, Isolated: true}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, e Environment, _ string) (RuntimeTarget, func(context.Context) error, error) {
		return e.Target, func(context.Context) error { order = append(order, "account"); return nil }, nil
	}, Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
		t.Fatal("partial acquisition proceeded to launch")
		return LaunchFacts{}, nil
	}, Open: func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		opened.Add(1)
		return nil, nil
	}})
	defer m.Close(context.Background())
	files := &regressionTaskResources{acquire: func(context.Context, ResourceRequest) (ResourceLease, error) {
		return &regressionLease{close: func(context.Context) error { order = append(order, "resource"); return nil }}, status_error.PluginNotReady
	}}
	_, err := m.ExecuteTask(context.Background(), TaskRequest{TextRequest: TextRequest{RuntimeID: RuntimeIDCodex, Messages: []TextMessage{{Role: "user", Content: "once"}}}, Resources: files})
	if !errors.Is(err, status_error.PluginNotReady) || opened.Load() != 0 || strings.Join(order, ",") != "resource,account" {
		t.Fatalf("partial acquisition ownership lost: err=%v opens=%d order=%v", err, opened.Load(), order)
	}
}

func TestManagerBindPreservesNativeCredentialConfigurationError(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "auth.json")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "symlink":
				err = os.Symlink(filepath.Join(home, "missing"), path)
			case "oversized":
				err = os.WriteFile(path, make([]byte, (1<<20)+1), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			manager, request := chatFixture(t, &chatDriverFixture{})
			request.Target.Isolated = false
			manager.config.Launch = func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
				return LaunchFacts{BaseRuntimeID: RuntimeIDCodex, Executable: "never-started-fixture", Environment: []string{"CODEX_HOME=" + home}}, nil
			}
			prepared, err := manager.PrepareSession(context.Background(), request.plan(), request.Session)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close(context.Background())
			if err = prepared.Bind(prepared.Context(), SessionResources{Target: request.Target}); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
				t.Fatalf("native credential configuration mapped to %v, want RuntimeConfigurationInvalid", err)
			}
		})
	}
}

func TestManagerBindMapsNativePreparationFaultOnce(t *testing.T) {
	for name, test := range map[string]struct {
		code native.FaultCode // code 是原生准备边界的准确技术分类。
		want error            // want 是既有产品稳定错误。
	}{"authentication": {native.FaultAuthenticationRequired, status_error.RuntimeAuthRequired}, "configuration": {native.FaultConfigurationInvalid, status_error.RuntimeConfigurationInvalid}, "invalid": {native.FaultInvalidSpec, status_error.BadRequest}, "preparation": {native.FaultPreparation, status_error.RuntimeUnavailable}} {
		t.Run(name, func(t *testing.T) {
			m, plan := regressionManager(t, nil)
			m.config.Launch = func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
				return LaunchFacts{}, &native.Fault{Code: test.code, Stage: "launch", Message: "launch failed", Cause: errors.New("private vendor error")}
			}
			p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close(context.Background())
			if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan)}); !errors.Is(err, test.want) {
				t.Fatalf("native fault escaped product mapping: %v want %v", err, test.want)
			}
		})
	}
}

func TestSessionResourcesCannotExpandDeclaredPermissions(t *testing.T) {
	shape := reflect.TypeOf(SessionResources{})
	for _, field := range []string{"ActualPlan", "Workspace", "Policy", "Grants"} {
		if _, exists := shape.FieldByName(field); exists {
			t.Fatalf("actual resources can rewrite declared permission through %s", field)
		}
	}
}

// productSinkFunc 注入当前产品事件交付时点。
type productSinkFunc func(context.Context, Event) error

func (f productSinkFunc) Accept(ctx context.Context, e Event) error { return f(ctx, e) }
func TestManagerFinalUsageStillOwnsCurrentTurn(t *testing.T) {
	driver := &chatDriverFixture{result: &native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn, NativeSession: native.NativeSessionInfo{Handle: "native-session"}, Usage: native.Usage{Known: true}}}
	m, request := chatFixture(t, driver)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan Result, 1)
	go func() {
		done <- runChatFixture(context.Background(), m, request, productSinkFunc(func(_ context.Context, e Event) error {
			if e.Type == EventTypeUsage {
				close(entered)
				<-release
			}
			return nil
		}))
	}()
	<-entered
	_, err := m.PrepareSession(context.Background(), request.plan(), SessionDirective{Mode: SessionModeResume, ID: "native-session"})
	close(release)
	result := <-done
	if result.Session.Mode != SessionModeNew {
		t.Fatalf("result read a later session directive: %+v", result.Session)
	}
	if !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("final event delivery lost Turn ownership: %v", err)
	}
}

func TestPrepareReadOnlyHandoffRejectsWritableActualPlan(t *testing.T) {
	m, plan := regressionManager(t, nil)
	plan.ReadOnlyHandoff = true
	directive := SessionDirective{Mode: SessionModeResume, ID: "native"}
	if _, err := m.PrepareSession(context.Background(), plan, directive); err == nil {
		t.Fatal("writable handoff declaration accepted")
	}
	plan.Policy.Execution.WorkspaceReadOnly = true
	p, err := m.PrepareSession(context.Background(), plan, directive)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	var closes atomic.Int32
	if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan), Session: &SessionDirective{Mode: SessionModeNew}, Close: func(context.Context) error { closes.Add(1); return nil }}); err == nil {
		t.Fatal("handoff resources replaced native readonly restore")
	}
	if closes.Load() != 0 {
		t.Fatal("failed handoff binding discarded its resource owner")
	}
	if err = p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatal("invalid handoff resources lost release responsibility")
	}
}

func TestSameRuntimeBusyIndependentRuntimesParallel(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m, plan := regressionManager(t, nil)
	m.config.Open = func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		return &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
			entered <- struct{}{}
			<-release
			return answer(ctx, r, sink)
		}}, nil
	}
	prepare := func(plan SessionPlan) PreparedChatRun {
		p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan)}); err != nil {
			_ = p.Close(context.Background())
			t.Fatal(err)
		}
		return p
	}
	first := prepare(plan)
	defer first.Close(context.Background())
	done := make(chan Result, 2)
	go func() {
		done <- first.Run(context.Background(), TurnInput{Message: Message{Text: "first"}}, &chatSinkFixture{})
	}()
	<-entered
	if _, err := m.PrepareSession(context.Background(), plan, SessionDirective{}); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("same workspace ran concurrently: %v", err)
	}
	plan.Workspace.Path = t.TempDir()
	second := prepare(plan)
	defer second.Close(context.Background())
	go func() {
		done <- second.Run(context.Background(), TurnInput{Message: Message{Text: "second"}}, &chatSinkFixture{})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("independent workspace serialized behind native I/O")
	}
	close(release)
	for range 2 {
		if result := <-done; result.Failure != nil {
			t.Fatal(result.Failure)
		}
	}
}

func TestManagerStartRecoversFinishedTemporaryCleanupWithoutReplay(t *testing.T) {
	var allow atomic.Bool
	var prompts, successes atomic.Int32
	m := NewManager(ManagerConfig{Resolve: func(context.Context, TextRequest) (Environment, error) {
		return Environment{BaseRuntimeID: RuntimeIDCodex, Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, Isolated: true}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, e Environment, _ string) (RuntimeTarget, func(context.Context) error, error) {
		return e.Target, nil, nil
	}, Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
		return LaunchFacts{BaseRuntimeID: RuntimeIDCodex, Executable: "fixture"}, nil
	}, Open: func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		return &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
			prompts.Add(1)
			return answer(ctx, r, sink)
		}, close: func(context.Context) error {
			if !allow.Load() {
				return status_error.RuntimeShutdownFailed
			}
			successes.Add(1)
			return nil
		}}, nil
	}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	defer func() { allow.Store(true); _ = m.Close(context.Background()) }()
	if result, err := m.GenerateText(context.Background(), TextRequest{RuntimeID: RuntimeIDCodex, Messages: []TextMessage{{Role: "user", Content: "once"}}}); err != nil || result.Text != "once" {
		t.Fatalf("temporary result=%+v error=%v", result, err)
	}
	m.mutex.Lock()
	pending := len(m.temporary)
	m.mutex.Unlock()
	if pending != 1 {
		t.Fatal("failed cleanup lost its owner before worker start")
	}
	allow.Store(true)
	m.Start(context.Background())
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("explicit Start did not retry completed temporary cleanup")
		case <-poll.C:
			m.mutex.Lock()
			pending = len(m.temporary)
			m.mutex.Unlock()
			if pending == 0 {
				if prompts.Load() != 1 || successes.Load() != 1 {
					t.Fatalf("cleanup replayed prompt or close: prompts=%d success=%d", prompts.Load(), successes.Load())
				}
				return
			}
		}
	}
}

func TestManagerReplacementProtectsOldAndPendingResourceIdentities(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	connection := &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
		result := answer(ctx, r, sink)
		result.NativeSession.Handle = "native"
		return result
	}, close: func(context.Context) error { close(entered); <-release; return nil }}
	m, plan := regressionManager(t, connection)
	plan.Target.AccountID = "old-account"
	plan.PluginIDs = []string{"old-plugin"}
	first, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	if err = first.Bind(first.Context(), SessionResources{Target: regressionTarget(plan), PluginResources: []plugins.Snapshot{{ID: "old-plugin"}}}); err != nil {
		t.Fatal(err)
	}
	result := first.Run(context.Background(), TurnInput{Message: Message{Text: "first"}}, &chatSinkFixture{})
	if result.Failure != nil {
		t.Fatal(result.Failure)
	}
	plan.Target.AccountID = "pending-account"
	plan.PluginIDs = []string{"pending-plugin"}
	prepared := make(chan PreparedChatRun, 1)
	failed := make(chan error, 1)
	go func() {
		p, err := m.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeResume, ID: "native"})
		if err != nil {
			failed <- err
			return
		}
		prepared <- p
	}()
	<-entered
	for _, id := range []string{"old-account", "pending-account"} {
		if err = m.CloseIdleAccount(context.Background(), id); !errors.Is(err, status_error.AIAccountBusy) {
			t.Errorf("account %s was not protected during replacement: %v", id, err)
		}
	}
	for _, id := range []string{"old-plugin", "pending-plugin"} {
		if err = m.CloseIdlePlugin(context.Background(), id); !errors.Is(err, status_error.PluginBusy) {
			t.Errorf("plugin %s was not protected during replacement: %v", id, err)
		}
	}
	close(release)
	select {
	case p := <-prepared:
		defer p.Close(context.Background())
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("replacement did not complete after old consumer exit")
	}
}

func TestManagerColdResumeOpenFailureRetainsRetiringPartialConnection(t *testing.T) {
	var allowClose atomic.Bool
	defer allowClose.Store(true)
	var opens, prompts, closeAttempts, domainReleases atomic.Int32
	connection := &regressionConnection{prompt: func(context.Context, native.TurnRequest, native.EventSink) native.TurnResult {
		prompts.Add(1)
		t.Error("partial failed connection received Prompt")
		return native.TurnResult{}
	}, close: func(context.Context) error {
		closeAttempts.Add(1)
		if !allowClose.Load() {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}}
	m, plan := regressionManager(t, connection)
	m.config.Open = func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		opens.Add(1)
		return connection, status_error.RuntimeProtocolIncompatible
	}
	directive := SessionDirective{Mode: SessionModeResume, ID: "persisted-native"}
	prepared, err := m.PrepareSession(context.Background(), plan, directive)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	if err = prepared.Bind(prepared.Context(), SessionResources{Target: regressionTarget(plan), Close: func(context.Context) error { domainReleases.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	result := prepared.Run(context.Background(), TurnInput{Message: Message{Text: "next"}}, &chatSinkFixture{})
	if result.Failure == nil || closeAttempts.Load() != 1 || domainReleases.Load() != 0 {
		t.Fatalf("partial Open failure lost ownership: failure=%+v closes=%d releases=%d", result.Failure, closeAttempts.Load(), domainReleases.Load())
	}
	next, err := m.PrepareSession(context.Background(), plan, directive)
	if next != nil {
		defer next.Close(context.Background())
	}
	if err == nil {
		t.Error("failed partial native consumer was accepted as a hot session")
	}
	if closeAttempts.Load() != 2 || domainReleases.Load() != 0 || opens.Load() != 1 || prompts.Load() != 0 {
		t.Fatalf("next preparation bypassed same-owner cleanup: closes=%d releases=%d opens=%d prompts=%d", closeAttempts.Load(), domainReleases.Load(), opens.Load(), prompts.Load())
	}
}

func TestManagerIdleCleanupCannotClearPermanentCloseIntent(t *testing.T) {
	idleEntered, idleRelease := make(chan struct{}), make(chan struct{})
	explicitEntered, explicitRelease := make(chan struct{}), make(chan struct{})
	defer func() {
		for _, signal := range []chan struct{}{idleRelease, explicitRelease} {
			select {
			case <-signal:
			default:
				close(signal)
			}
		}
	}()
	var attempts atomic.Int32
	connection := &regressionConnection{prompt: answer, close: func(context.Context) error {
		switch attempts.Add(1) {
		case 1:
			close(idleEntered)
			<-idleRelease
			return status_error.RuntimeShutdownFailed
		case 2:
			close(explicitEntered)
			<-explicitRelease
			return nil
		default:
			return nil
		}
	}}
	m, plan := regressionManager(t, connection)
	plan.Target.AccountID = "account"
	p, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err = p.Bind(p.Context(), SessionResources{Target: regressionTarget(plan)}); err != nil {
		t.Fatal(err)
	}
	if result := p.Run(context.Background(), TurnInput{Message: Message{Text: "first"}}, &chatSinkFixture{}); result.Failure != nil {
		t.Fatal(result.Failure)
	}
	idleDone := make(chan error, 1)
	go func() { idleDone <- m.CloseIdleAccount(context.Background(), "account") }()
	<-idleEntered
	closeDone := make(chan error, 1)
	go func() { closeDone <- m.CloseWorkspace(context.Background(), plan.Workspace.Path) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		m.mutex.Lock()
		owner := m.workspaces[plan.Workspace.Path]
		m.mutex.Unlock()
		owner.mu.Lock()
		closing := owner.closing && owner.closeAttempt != nil
		owner.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("explicit workspace close did not claim permanent intent")
		case <-time.After(time.Millisecond):
		}
	}
	close(idleRelease)
	if err = <-idleDone; err == nil {
		t.Error("idle cleanup failure was hidden")
	}
	<-explicitEntered
	prepared := make(chan PreparedChatRun, 1)
	prepareErrors := make(chan error, 1)
	go func() {
		next, prepareErr := m.PrepareSession(context.Background(), plan, SessionDirective{Mode: SessionModeResume, ID: "native"})
		prepared <- next
		prepareErrors <- prepareErr
	}()
	returned := false
	select {
	case next := <-prepared:
		returned = true
		if next != nil {
			defer next.Close(context.Background())
			t.Error("new preparation entered permanently closing session")
		}
		if prepareErr := <-prepareErrors; prepareErr == nil {
			t.Error("permanent close admitted a new turn")
		}
	case <-time.After(20 * time.Millisecond):
		t.Error("new preparation entered cleanup instead of rejecting permanent close")
	}
	close(explicitRelease)
	if err = <-closeDone; err != nil {
		t.Fatal(err)
	}
	if !returned {
		next := <-prepared
		if next != nil {
			defer next.Close(context.Background())
		}
		<-prepareErrors
	}
}

func TestManagerHotValidationFailureRetiresUnclosedConnection(t *testing.T) {
	var allowClose atomic.Bool
	defer allowClose.Store(true)
	var opens, prompts, closeAttempts, domainReleases atomic.Int32
	connection := &regressionConnection{prompt: func(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
		prompts.Add(1)
		result := answer(ctx, r, sink)
		result.NativeSession.Handle = "native"
		return result
	}, close: func(context.Context) error {
		closeAttempts.Add(1)
		if !allowClose.Load() {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}}
	m, plan := regressionManager(t, connection)
	m.config.Open = func(context.Context, native.ConnectionConfig, native.OpenRequest) (NativeConnection, error) {
		opens.Add(1)
		return connection, nil
	}
	first, err := m.PrepareSession(context.Background(), plan, SessionDirective{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	if err = first.Bind(first.Context(), SessionResources{Target: regressionTarget(plan), Close: func(context.Context) error { domainReleases.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	if result := first.Run(context.Background(), TurnInput{Message: Message{Text: "first"}}, &chatSinkFixture{}); result.Failure != nil {
		t.Fatal(result.Failure)
	}
	directive := SessionDirective{Mode: SessionModeResume, ID: "native"}
	hot, err := m.PrepareSession(context.Background(), plan, directive)
	if err != nil {
		t.Fatal(err)
	}
	defer hot.Close(context.Background())
	if hot.NeedsResources() {
		t.Fatal("unchanged consumer did not use the owned hot connection")
	}
	result := hot.Run(context.Background(), TurnInput{Message: Message{Text: "invalid", Attachments: []Attachment{{Name: "outside", Path: "attachments/outside", RuntimePath: t.TempDir()}}}}, &chatSinkFixture{})
	if result.Failure == nil || closeAttempts.Load() != 1 || prompts.Load() != 1 || domainReleases.Load() != 0 {
		t.Fatalf("invalid hot input escaped cleanup ownership: failure=%+v closes=%d prompts=%d releases=%d", result.Failure, closeAttempts.Load(), prompts.Load(), domainReleases.Load())
	}
	next, err := m.PrepareSession(context.Background(), plan, directive)
	if next != nil {
		defer next.Close(context.Background())
	}
	if err == nil || closeAttempts.Load() != 2 || opens.Load() != 1 || prompts.Load() != 1 || domainReleases.Load() != 0 {
		t.Fatalf("hot validation failure reused unclosed consumer: err=%v closes=%d opens=%d prompts=%d releases=%d", err, closeAttempts.Load(), opens.Load(), prompts.Load(), domainReleases.Load())
	}
}

// fakeDriver 仅是测试中一次原生打开函数的配置，不保留生产驱动框架。
type fakeDriver struct {
	caps  nativeCapabilities                                                  // caps 使权限与流规则测试可以明确验证未知能力。
	open  func(context.Context, native.OpenRequest) (NativeConnection, error) // open 是测试的窄原生边界。
	opens atomic.Int32                                                        // opens 核验未重放原生准备。
}
type fakeSession struct {
	prompt func(context.Context, native.TurnRequest, native.EventSink) native.TurnResult // prompt 是本轮协议事实。
	close  func(context.Context) error                                                   // close 是原生退出回执。
}

func (s *fakeSession) Prompt(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
	return s.prompt(ctx, r, sink)
}
func (s *fakeSession) Close(ctx context.Context) error {
	if s.close != nil {
		return s.close(ctx)
	}
	return nil
}
func caps() nativeCapabilities {
	return nativeCapabilities{Version: "test", ExplicitConfig: true, DenyAllTools: true, ToolAllowList: true, HotResume: true}
}
func spec() executionSpec {
	return executionSpec{OpenRequest: native.OpenRequest{Profile: native.Profile{DriverID: "fake"}}, Input: native.Input{Text: "first"}}
}
func answer(ctx context.Context, r native.TurnRequest, sink native.EventSink) native.TurnResult {
	if err := sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted}); err != nil {
		return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
	}
	_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "answer", Phase: native.PhaseFinal, Delta: r.Input.Text, Complete: true}})
	return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
}

type testConfig struct {
	Limits         native.Limits // Limits 是待验证的本轮预算。
	CleanupTimeout time.Duration // CleanupTimeout 约束交互结束与关闭。
	Logger         *slog.Logger  // Logger 核验阶段诊断内容。
	Grants         Grants        // Grants 是不能被原生改写的授权上界。
}
type testRuntime struct {
	session *Session    // session 是实际被测的唯一产品所有者。
	config  testConfig  // config 是本测试的固定输入。
	driver  *fakeDriver // driver 是本测试的窄原生创建函数。
}

func started(t *testing.T, d *fakeDriver, configure func(*testConfig)) *testRuntime {
	t.Helper()
	cfg := testConfig{}
	if configure != nil {
		configure(&cfg)
	}
	m := NewManager(ManagerConfig{Limits: cfg.Limits, CleanupTimeout: cfg.CleanupTimeout, Logger: cfg.Logger})
	s, err := newSession(m, "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.declared.Target.RuntimeID = "fake"
	t.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return &testRuntime{session: s, config: cfg, driver: d}
}
func runTurn(rt *testRuntime, ctx context.Context, input executionSpec, options turnOptions) turnResult {
	return streamTurn(rt, ctx, input, options, false)
}
func invokeTurn(rt *testRuntime, ctx context.Context, input executionSpec) turnResult {
	return streamTurn(rt, ctx, input, turnOptions{}, true)
}

// streamTurn 仅装配私有 Turn 以验证流合同；真实冷热准备由 Manager 回归覆盖。
func streamTurn(rt *testRuntime, ctx context.Context, input executionSpec, options turnOptions, temporary bool) turnResult {
	compiled, err := compileExecution(input, rt.driver.caps, rt.config.Grants, rt.config.Limits)
	if err != nil {
		return failed("test", asFault(err, native.FaultPreparation, "prepare", "compile failed"))
	}
	frozen := compiled.Spec()
	s := rt.session
	s.revision++
	callCtx, cancel := context.WithCancelCause(ctx)
	turn := &Turn{ref: native.InvocationRef{ID: s.id + "/" + fmtRevision(s.revision), SessionID: s.id, Revision: s.revision}, ctx: callCtx, cancel: cancel, done: make(chan struct{}), bound: true, claimed: true, stopPreparation: func() bool { return false }}
	s.current = turn
	s.closeAfterRun = temporary
	defer s.finishTurn(turn)
	reused := s.connection != nil
	var elapsed time.Duration
	if !reused {
		started := time.Now()
		req := cloneExecution(frozen).OpenRequest
		req.Invocation = turn.ref
		req.Policies = native.SessionPolicies{Tools: frozen.Policies.Tools, NativeConfig: frozen.Policies.NativeConfig}
		req.Policies.Tools.Allow = slices.Clone(req.Policies.Tools.Allow)
		rt.driver.opens.Add(1)
		s.connection, err = rt.driver.open(callCtx, req)
		elapsed = time.Since(started)
	}
	logStage(s.manager.config.Logger, turn, s, "cli_initialization", elapsed, err == nil, reused)
	if err != nil {
		return failed(turn.ref.ID, asFault(err, native.FaultPreparation, "open", "open failed"))
	}
	return s.executeTurn(turn, frozen, options)
}

type observerFunc func(context.Context, observedEvent) error

func (f observerFunc) Accept(ctx context.Context, e observedEvent) error { return f(ctx, e) }

type interactionFunc func(context.Context, native.InteractionRequest) (native.InteractionResponse, error)

func (f interactionFunc) Handle(ctx context.Context, r native.InteractionRequest) (native.InteractionResponse, error) {
	return f(ctx, r)
}
func requireCode(t *testing.T, f *native.Fault, code native.FaultCode) {
	t.Helper()
	if f == nil || f.Code != code {
		t.Fatalf("fault=%#v want %s", f, code)
	}
}

func TestStageDiagnosticsDistinguishColdInitializationFromHotAcceptance(t *testing.T) {
	var logs bytes.Buffer
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, request native.TurnRequest, sink native.EventSink) native.TurnResult {
			result := answer(ctx, request, sink)
			result.NativeSession.Handle = "native-session"
			return result
		}}, nil
	}}
	rt := started(t, driver, func(config *testConfig) {
		config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	})
	input := spec()
	input.Input.Text = "private-input-never-log"
	for turn := 0; turn < 2; turn++ {
		result := runTurn(rt, context.Background(), input, turnOptions{})
		if result.Status != native.StatusCompleted {
			t.Fatalf("turn %d: %+v", turn, result)
		}
		input.Session = native.SessionDirective{Mode: native.SessionResume, NativeHandle: result.NativeSession.Handle}
	}
	if driver.opens.Load() != 1 {
		t.Fatalf("native opens = %d", driver.opens.Load())
	}
	var stages []string
	var reused []bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		stage, _ := entry["stage"].(string)
		stages = append(stages, stage)
		if stage == "cli_initialization" {
			value, _ := entry["reused"].(bool)
			reused = append(reused, value)
		}
		if entry["invocation_id"] == "" || entry["elapsed_ms"] == nil || entry["success"] != true {
			t.Fatalf("incomplete diagnostic: %s", line)
		}
	}
	if strings.Join(stages, ",") != "cli_initialization,input_acceptance,cli_initialization,input_acceptance" || len(reused) != 2 || reused[0] || !reused[1] {
		t.Fatalf("stages = %v, reused = %v", stages, reused)
	}
	if strings.Contains(logs.String(), input.Input.Text) {
		t.Fatal("diagnostic leaked input")
	}
}

func TestStageDiagnosticsRecordFailedInitializationWithoutInputOrRawError(t *testing.T) {
	var logs bytes.Buffer
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return nil, errors.New("private-native-error")
	}}
	rt := started(t, driver, func(config *testConfig) {
		config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	})
	input := spec()
	input.Input.Text = "private-failed-input"
	result := runTurn(rt, context.Background(), input, turnOptions{})
	if result.Status != native.StatusFailed {
		t.Fatalf("result = %+v", result)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["stage"] != "cli_initialization" || entry["success"] != false || entry["reused"] != false {
		t.Fatalf("diagnostic = %s", logs.String())
	}
	if strings.Contains(logs.String(), input.Input.Text) || strings.Contains(logs.String(), "private-native-error") {
		t.Fatal("diagnostic leaked input or raw error")
	}
}

func TestObserverFailurePreservesEffectsAndNeverRetries(t *testing.T) {
	var prompts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, s native.EventSink) native.TurnResult {
			prompts.Add(1)
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "1", Name: "read"}})
			return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowList, Allow: []string{"read"}}
	boom := errors.New("observer offline")
	result := runTurn(rt, context.Background(), in, turnOptions{Observer: observerFunc(func(_ context.Context, e observedEvent) error {
		if e.NativeEvent.Kind == native.EventTool {
			return boom
		}
		return nil
	})})
	requireCode(t, result.DeliveryFault, native.FaultDelivery)
	if !result.Effects.PromptAccepted || !result.Effects.ToolActivity || !result.Effects.OutcomeUnknown || prompts.Load() != 1 {
		t.Fatalf("effects %#v calls %d", result, prompts.Load())
	}
}

func TestBudgetFailureStillPreservesRealDeliveryFacts(t *testing.T) {
	for _, which := range []string{"acceptance", "tool"} {
		t.Run(which, func(t *testing.T) {
			d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
					_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					if which == "tool" {
						_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "1", Name: "read", Text: strings.Repeat("x", 1000)}})
					}
					return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
				}}, nil
			}}
			rt := started(t, d, func(cfg *testConfig) { cfg.Grants = Grants{Tools: []string{"read"}} })
			in := spec()
			in.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowList, Allow: []string{"read"}}
			in.Limits.MaxEventBytes = 100
			if which == "acceptance" {
				in.Limits.MaxEventBytes = 1
			}
			got := invokeTurn(rt, context.Background(), in)
			requireCode(t, got.Fault, native.FaultLimit)
			if !got.Effects.PromptAccepted || !got.Effects.OutcomeUnknown || (which == "tool" && !got.Effects.ToolActivity) {
				t.Fatalf("real facts lost %#v", got.Effects)
			}
		})
	}
}

func TestDriverCannotMutateEffectiveToolAuthorization(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(_ context.Context, req native.OpenRequest) (NativeConnection, error) {
		req.Policies.Tools.Allow[0] = "delete"
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "1", Name: "delete"}})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowList, Allow: []string{"read"}}
	got := invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, native.FaultProtocol)
	if got.Status == native.StatusCompleted {
		t.Fatal("driver rewrote runtime authorization")
	}
}

func TestDriverCannotClaimCompletionWithoutDelivery(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(context.Context, native.TurnRequest, native.EventSink) native.TurnResult {
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	got := invokeTurn(rt, context.Background(), spec())
	requireCode(t, got.Fault, native.FaultProtocol)
}

func TestObserverDeadlineAndInteractionRequiredAreDistinct(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, s native.EventSink) native.TurnResult {
			if err := s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted}); err == nil {
				_, _ = s.Interact(ctx, native.InteractionRequest{ID: "question", Kind: native.InteractionInput, Prompt: "which one?"})
			}
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Limits.DeliveryTimeout = 5 * time.Millisecond
	got := runTurn(rt, context.Background(), in, turnOptions{Observer: observerFunc(func(ctx context.Context, _ observedEvent) error { <-ctx.Done(); return ctx.Err() })})
	requireCode(t, got.DeliveryFault, native.FaultDelivery)
	got = invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, native.FaultInteractionRequired)
	if got.StopReason != native.StopInteractionRequired {
		t.Fatalf("%#v", got)
	}
}

func TestInteractionIgnoresDeliveryDeadlineAndRespectsTurnDeadline(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			_, err := s.Interact(ctx, native.InteractionRequest{ID: "q", Kind: native.InteractionInput, Prompt: "choose"})
			if err != nil {
				return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
			}
			m := native.AssistantMessage{MessageID: "a", Phase: native.PhaseFinal, Delta: "done"}
			_ = s.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &m})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = InteractionInteractive
	in.Limits.DeliveryTimeout = time.Millisecond
	handler := interactionFunc(func(ctx context.Context, req native.InteractionRequest) (native.InteractionResponse, error) {
		select {
		case <-time.After(10 * time.Millisecond):
			return native.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: native.InteractionAccept, Content: map[string]any{"answer": "yes"}}, nil
		case <-ctx.Done():
			return native.InteractionResponse{}, ctx.Err()
		}
	})
	got := runTurn(rt, context.Background(), in, turnOptions{InteractionHandler: handler})
	if got.Status != native.StatusCompleted {
		t.Fatalf("observer deadline incorrectly limited user input: %#v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	got = runTurn(rt, ctx, in, turnOptions{InteractionHandler: handler})
	if got.Status != native.StatusCancelled {
		t.Fatalf("turn deadline did not revoke human waiting: %#v", got)
	}
}

func TestToolStatusUpdateCanOmitPreviouslyAuthorizedName(t *testing.T) {
	for _, mode := range []native.ToolMode{native.ToolsAllowList, native.ToolsAllowAll} {
		t.Run(string(mode), func(t *testing.T) {
			d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, s native.EventSink) native.TurnResult {
					_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "call", Name: "read", Status: "started"}})
					_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "call", Status: "completed"}})
					m := native.AssistantMessage{MessageID: "a", Phase: native.PhaseFinal, Delta: "done"}
					_ = s.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &m})
					return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
				}}, nil
			}}
			rt := started(t, d, func(cfg *testConfig) {
				cfg.Grants = Grants{Tools: []string{"read"}, AllowAllTools: mode == native.ToolsAllowAll}
			})
			in := spec()
			in.Policies.Tools = native.ToolPolicy{Mode: mode, Allow: []string{"read"}}
			got := invokeTurn(rt, context.Background(), in)
			if got.Status != native.StatusCompleted || got.Output.Text != "done" {
				t.Fatalf("legal tool completion rejected: %#v", got)
			}
		})
	}
}

func TestUnknownToolIDCannotBorrowOmittedNameInAllowList(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "unknown", Status: "completed"}})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowList, Allow: []string{"read"}}
	got := invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, native.FaultProtocol)
}

func TestCancelledInteractionDoesNotCancelHealthyTurnOrAcceptLateReply(t *testing.T) {
	entered := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			requestCtx, cancel := context.WithCancel(ctx)
			go func() { <-entered; cancel() }()
			response, err := s.Interact(requestCtx, native.InteractionRequest{ID: "q", Kind: native.InteractionInput})
			if !errors.Is(err, context.Canceled) || response.Action == native.InteractionAccept {
				t.Errorf("revoked interaction accepted or wrongly classified: %#v %v", response, err)
			}
			if ctx.Err() != nil {
				t.Error("single revoked question cancelled healthy invocation")
			}
			m := native.AssistantMessage{MessageID: "a", Phase: native.PhaseFinal, Delta: "continued"}
			_ = s.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &m})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = InteractionInteractive
	got := runTurn(rt, context.Background(), in, turnOptions{InteractionHandler: interactionFunc(func(ctx context.Context, req native.InteractionRequest) (native.InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		return native.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: native.InteractionAccept, Content: map[string]any{"answer": "late"}}, nil
	})})
	if got.Status != native.StatusCompleted || got.Output.Text != "continued" {
		t.Fatalf("healthy turn was cancelled: %#v", got)
	}
}

func TestInteractionHandlerDoesNotBlockOrdinaryEvents(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			question := make(chan struct{})
			go func() {
				defer close(question)
				_, _ = s.Interact(ctx, native.InteractionRequest{ID: "q", Kind: native.InteractionInput})
			}()
			<-entered
			event := make(chan error, 1)
			go func() { event <- s.Accept(ctx, native.Event{Kind: native.EventThinking, Text: "progress"}) }()
			select {
			case err := <-event:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(20 * time.Millisecond):
				t.Error("ordinary event blocked behind external interaction handler")
			}
			close(release)
			<-question
			m := native.AssistantMessage{MessageID: "a", Phase: native.PhaseFinal, Delta: "done"}
			_ = s.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &m})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = InteractionInteractive
	got := runTurn(rt, context.Background(), in, turnOptions{InteractionHandler: interactionFunc(func(_ context.Context, req native.InteractionRequest) (native.InteractionResponse, error) {
		close(entered)
		<-release
		return native.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: native.InteractionAccept, Content: map[string]any{"answer": "yes"}}, nil
	})})
	if got.Status != native.StatusCompleted {
		t.Fatalf("%#v", got)
	}
}

func TestOversizedSeedRejectedBeforeNativePreparation(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		t.Error("oversized seed reached native preparation")
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	for _, seed := range [][]native.SeedMessage{make([]native.SeedMessage, 41), {{Role: "user", Text: strings.Repeat("中", 21846)}}} {
		for i := range seed {
			if seed[i].Role == "" {
				seed[i].Role = "user"
			}
		}
		in := spec()
		in.Seed = seed
		got := runTurn(rt, context.Background(), in, turnOptions{})
		requireCode(t, got.Fault, native.FaultInvalidSpec)
	}
	if d.opens.Load() != 0 {
		t.Fatalf("opens %d", d.opens.Load())
	}
}

func TestCancelledTurnPreservesDrainedDeliveryAndToolFacts(t *testing.T) {
	for _, which := range []string{"prompt accepted during drain", "tool during drain", "tool evidence without accepted event"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var observerCalls atomic.Int32
			d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(turnCtx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
					if which == "tool during drain" {
						_ = s.Accept(turnCtx, native.Event{Kind: native.EventPromptAccepted})
					}
					cancel()
					if which == "prompt accepted during drain" {
						_ = s.Accept(turnCtx, native.Event{Kind: native.EventPromptAccepted})
					} else {
						_ = s.Accept(turnCtx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "t", Name: "read"}})
					}
					return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
				}}, nil
			}}
			rt := started(t, d, nil)
			got := runTurn(rt, ctx, spec(), turnOptions{Observer: observerFunc(func(context.Context, observedEvent) error { observerCalls.Add(1); return nil })})
			if got.Status != native.StatusCancelled || !got.Effects.OutcomeUnknown {
				t.Fatalf("cancelled drain lost effect uncertainty: %#v", got)
			}
			if which == "prompt accepted during drain" && !got.Effects.PromptAccepted {
				t.Fatal("drained prompt delivery lost")
			}
			if which != "prompt accepted during drain" && !got.Effects.ToolActivity {
				t.Fatal("drained tool activity lost")
			}
			wantCalls := int32(0)
			if which == "tool during drain" {
				wantCalls = 1
			}
			if observerCalls.Load() != wantCalls {
				t.Fatalf("cancelled events delivered to observer: %d", observerCalls.Load())
			}
		})
	}
}

func TestObserverFailureStillPreservesSubsequentlyDrainedToolFacts(t *testing.T) {
	boom := errors.New("journal unavailable")
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			_ = s.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "t", Name: "read"}})
			return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, nil)
	got := runTurn(rt, context.Background(), spec(), turnOptions{Observer: observerFunc(func(context.Context, observedEvent) error { return boom })})
	requireCode(t, got.DeliveryFault, native.FaultDelivery)
	if !got.Effects.PromptAccepted || !got.Effects.ToolActivity || !got.Effects.OutcomeUnknown {
		t.Fatalf("first fault suppressed drained evidence: %#v", got.Effects)
	}
	if !errors.Is(got.Fault, boom) {
		t.Fatalf("first fault replaced: %#v", got.Fault)
	}
}

// TestRichToolContentUsesEventBudget 验证新增文件差异和公开工具参数受现有事件预算约束。
func TestRichToolContentUsesEventBudget(t *testing.T) {
	large := strings.Repeat("x", 1000)
	for name, event := range map[string]native.Event{
		"diff":            {Kind: native.EventTool, Tool: &native.ToolEvent{ID: "edit", Content: []native.ContentBlock{{Type: "diff", Diff: &native.Diff{Path: "file", NewText: large}}}}},
		"input":           {Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Input: []byte("\"" + large + "\"")}},
		"output":          {Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Output: []byte("\"" + large + "\"")}},
		"location":        {Kind: native.EventTool, Tool: &native.ToolEvent{ID: "tool", Locations: []native.Location{{Path: large}}}},
		"assistant image": {Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "answer", Content: []native.ContentBlock{{Type: "image", Data: large, MIMEType: "image/png"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, request native.TurnRequest, sink native.EventSink) native.TurnResult {
					_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					_ = sink.Accept(ctx, event)
					return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
				}}, nil
			}}
			runtime := started(t, driver, nil)
			input := spec()
			input.Limits.MaxEventBytes = 400
			result := invokeTurn(runtime, context.Background(), input)
			requireCode(t, result.Fault, native.FaultLimit)
			if event.Tool != nil && !result.Effects.ToolActivity {
				t.Fatal("observed tool side effect disappeared on budget failure")
			}
		})
	}
}

// TestUnattendedApprovalUsesExactPublicOption 验证无人值守授权仍回传实际公开选项身份。
func TestUnattendedApprovalUsesExactPublicOption(t *testing.T) {
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, request native.TurnRequest, sink native.EventSink) native.TurnResult {
			_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			response, err := sink.Interact(ctx, native.InteractionRequest{ID: "approval", Kind: native.InteractionApproval, Approval: &native.ApprovalRequest{Options: []native.ApprovalOption{{ID: "reject", Kind: "reject_once"}, {ID: "allow", Kind: "allow_once"}}}})
			if err != nil || response.Action != native.InteractionAccept || response.OptionID != "allow" {
				t.Errorf("wrong exact decision: %+v %v", response, err)
			}
			message := native.AssistantMessage{MessageID: "final", Phase: native.PhaseFinal, Delta: "done", Complete: true}
			_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &message})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	runtime := started(t, driver, func(config *testConfig) { config.Grants.AllowAllTools = true })
	input := spec()
	input.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowAll}
	result := invokeTurn(runtime, context.Background(), input)
	if result.Status != native.StatusCompleted {
		t.Fatal(result)
	}
}

// TestStructuredInputUsesInteractionBudget 验证表单结构和提交内容不会绕过交互预算。
func TestStructuredInputUsesInteractionBudget(t *testing.T) {
	for _, largeRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "request"}[largeRequest], func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, request native.TurnRequest, sink native.EventSink) native.TurnResult {
					_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					input := &native.InputRequest{Mode: "form"}
					if largeRequest {
						input.RequestedSchema = map[string]any{"description": strings.Repeat("x", 1000)}
					}
					_, _ = sink.Interact(ctx, native.InteractionRequest{ID: "question", Kind: native.InteractionInput, Input: input})
					return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
				}}, nil
			}}
			runtime := started(t, driver, nil)
			input := spec()
			input.Policies.Interaction = InteractionInteractive
			input.Limits.MaxEventBytes = 600
			result := runTurn(runtime, context.Background(), input, turnOptions{InteractionHandler: interactionFunc(func(ctx context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
				return native.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: native.InteractionAccept, Content: map[string]any{"answer": strings.Repeat("x", 1000)}}, nil
			})})
			requireCode(t, result.Fault, native.FaultLimit)
		})
	}
}

func (rt *testRuntime) Close(ctx context.Context) error { return rt.session.Close(ctx) }
func TestCleanupFailureDoesNotEraseCompletedAnswer(t *testing.T) {
	boom := errors.New("close failed")
	var attempts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return boom
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	got := invokeTurn(rt, context.Background(), spec())
	if got.Status != native.StatusCompleted || got.Output.Status != OutputAvailable || got.Output.Text != "first" || got.CleanupFault == nil || !errors.Is(got.CleanupFault, boom) {
		t.Fatalf("%#v", got)
	}
}

func TestCloseTimeoutCanBeRetriedWithoutDuplicateRelease(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var closes atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			close(entered)
			<-release
			return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
		}, close: func(context.Context) error { closes.Add(1); return nil }}, nil
	}}
	rt := started(t, d, nil)
	done := make(chan turnResult, 1)
	go func() { done <- invokeTurn(rt, context.Background(), spec()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := rt.Close(ctx); err == nil {
		t.Fatal("close did not respect caller deadline")
	}
	close(release)
	<-done
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := rt.Close(ctx2); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("session closes %d", closes.Load())
	}
}

func TestRuntimeCloseRespectsCallerWaitDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: answer, close: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	_ = runTurn(rt, context.Background(), spec(), turnOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- rt.Close(ctx) }()
	<-entered
	select {
	case err := <-returned:
		if err == nil {
			t.Error("close session ignored caller deadline")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Runtime.Close did not bound caller wait")
	}
	close(release)
}

func TestRuntimeCloseRetriesActiveRunCleanupWithoutErasingFault(t *testing.T) {
	entered := make(chan native.InvocationRef, 1)
	boom := errors.New("session close failed")
	var attempts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, s native.EventSink) native.TurnResult {
			_ = s.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			entered <- req.Invocation
			<-ctx.Done()
			return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
		}, close: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return boom
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	done := make(chan turnResult, 1)
	go func() { done <- runTurn(rt, context.Background(), spec(), turnOptions{}) }()
	<-entered
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if !errors.Is(got.CleanupFault, boom) || attempts.Load() != 2 {
		t.Fatalf("run cleanup=%v attempts=%d", got.CleanupFault, attempts.Load())
	}
}

// TestGrowingToolInputSnapshotsDoNotSpendRepeatedPrefixBudget 复现参数逐字符增长与推理分片交错，验证完整事件仍在默认预算内交付。
func TestGrowingToolInputSnapshotsDoNotSpendRepeatedPrefixBudget(t *testing.T) {
	const updates = 9000
	seen, latest := 0, 0
	seenThinking, seenEvents := 0, 0
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
			if err := sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted}); err != nil {
				return native.TurnResult{Status: native.StatusFailed}
			}
			for i := 1; i <= updates; i++ {
				if i%3 != 0 {
					if err := sink.Accept(ctx, native.Event{Kind: native.EventThinking, Text: "reason"}); err != nil {
						return native.TurnResult{Status: native.StatusFailed}
					}
				}
				input := []byte(`{"content":"` + strings.Repeat("x", i*2) + `"}`)
				event := native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "write", Name: "write", Update: i > 1, Status: "in_progress", Input: input, Content: []native.ContentBlock{{Type: "text", Text: "progress"}}}}
				if err := sink.Accept(ctx, event); err != nil {
					return native.TurnResult{Status: native.StatusFailed}
				}
			}
			if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: &native.ToolEvent{ID: "write", Update: true, Status: "completed", Output: []byte(`{"saved":true}`)}}); err != nil {
				return native.TurnResult{Status: native.StatusFailed}
			}
			_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "answer", Phase: native.PhaseFinal, Delta: "saved", Complete: true}})
			return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
		}}, nil
	}}
	runtime := started(t, driver, func(c *testConfig) { c.Grants.AllowAllTools = true })
	input := spec()
	input.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowAll}
	result := runTurn(runtime, context.Background(), input, turnOptions{Observer: observerFunc(func(_ context.Context, e observedEvent) error {
		seenEvents++
		if e.NativeEvent.Kind == native.EventThinking {
			seenThinking++
			if e.NativeEvent.Text != "reason" {
				t.Fatalf("thinking fragment changed at %d", seenThinking)
			}
		}
		if tool := e.NativeEvent.Tool; tool != nil && tool.Input != nil {
			seen++
			var data map[string]string
			if err := json.Unmarshal(tool.Input, &data); err != nil {
				return err
			}
			latest = len(data["content"])
			if latest != seen*2 || len(tool.Content) != 1 || tool.Content[0].Text != "progress" {
				t.Fatalf("snapshot or progress changed at %d", seen)
			}
			tool.Input[0] = '!'
		}
		return nil
	})})
	if result.Status != native.StatusCompleted || seen != updates || latest != 18000 || seenThinking != 6000 || seenEvents != 15003 {
		t.Fatalf("growing input failed: status=%s fault=%v observed=%d latest=%d thinking=%d events=%d", result.Status, result.Fault, seen, latest, seenThinking, seenEvents)
	}
	t.Logf("observed: events=%d tool_inputs=%d thinking=%d latest_content_bytes=%d", seenEvents, seen, seenThinking, latest)
}

// TestToolSnapshotAccountingStillBoundsNewData 单条大帧、独立参数、结果、正文及事件洪泛仍受原预算约束。
func TestToolSnapshotAccountingStillBoundsNewData(t *testing.T) {
	for _, scenario := range []string{"single", "replaced", "different-tools", "content", "output", "events", "clear-and-regrow"} {
		t.Run(scenario, func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
					_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					for i := 0; i < 100; i++ {
						tool := &native.ToolEvent{ID: "write", Name: "write", Update: i > 0, Input: []byte(`"` + strings.Repeat("x", 4000) + `"`)}
						switch scenario {
						case "single":
							if i > 0 {
								tool.Input = []byte(`"` + strings.Repeat("x", 9000) + `"`)
							}
						case "replaced":
							tool.Input = []byte(`"` + strings.Repeat(string(rune('a'+i%20)), 4000) + `"`)
						case "different-tools":
							tool.ID = fmtRevision(uint64(i + 1))
						case "content":
							tool.Content = []native.ContentBlock{{Type: "text", Text: strings.Repeat("x", 4000)}}
						case "output":
							tool.Output = tool.Input
						case "clear-and-regrow":
							if i%2 == 1 {
								tool.Input = []byte("null")
							}
						}
						if err := sink.Accept(ctx, native.Event{Kind: native.EventTool, Tool: tool}); err != nil {
							return native.TurnResult{Status: native.StatusFailed}
						}
					}
					return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
				}}, nil
			}}
			runtime := started(t, driver, func(c *testConfig) { c.Grants.AllowAllTools = true })
			input := spec()
			input.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowAll}
			input.Limits = native.Limits{MaxOutputBytes: 128, MaxEventBytes: 8192, MaxTotalEventBytes: 16384, MaxEvents: 1000}
			if scenario == "events" {
				input.Limits.MaxEvents = 16
			}
			result := invokeTurn(runtime, context.Background(), input)
			requireCode(t, result.Fault, native.FaultLimit)
			if !result.Effects.ToolActivity {
				t.Fatal("budget failure lost the real tool activity")
			}
		})
	}
}

// 人工决定不属于机器交付预算，只有所属执行或原生请求可以撤销等待。
func TestInteractionWaitsForHumanBeyondMachineDeadlines(t *testing.T) {
	for _, kind := range []native.InteractionKind{native.InteractionInput, native.InteractionApproval} {
		t.Run(string(kind), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
					return &fakeSession{prompt: func(ctx context.Context, req native.TurnRequest, sink native.EventSink) native.TurnResult {
						_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
						response, err := sink.Interact(ctx, native.InteractionRequest{ID: "human", Kind: kind, ToolName: "read"})
						if err != nil || response.Action != native.InteractionAccept {
							t.Errorf("valid human answer lost after waiting: response=%+v error=%v", response, err)
							return native.TurnResult{Status: native.StatusFailed}
						}
						_ = sink.Accept(ctx, native.Event{Kind: native.EventAssistantMessage, Message: &native.AssistantMessage{MessageID: "answer", Phase: native.PhaseFinal, Delta: "已采纳回答", Complete: true}})
						return native.TurnResult{Status: native.StatusCompleted, StopReason: native.StopEndTurn}
					}}, nil
				}}
				rt := started(t, d, func(config *testConfig) { config.Grants.AllowAllTools = true })
				in := spec()
				in.Policies.Interaction = InteractionInteractive
				in.Policies.Tools = native.ToolPolicy{Mode: native.ToolsAllowAll}
				result := runTurn(rt, context.Background(), in, turnOptions{InteractionHandler: interactionFunc(func(ctx context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
					select {
					case <-time.After(24 * time.Hour):
						return native.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: native.InteractionAccept}, nil
					case <-ctx.Done():
						return native.InteractionResponse{}, ctx.Err()
					}
				})})
				if result.Status != native.StatusCompleted || result.Fault != nil {
					t.Fatalf("human waiting was treated as machine failure: %+v", result)
				}
			})
		})
	}
}

func TestSessionCloseRevokesPendingHumanInteraction(t *testing.T) {
	entered := make(chan struct{})
	driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
			_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
			response, err := sink.Interact(ctx, native.InteractionRequest{ID: "pending", Kind: native.InteractionInput})
			if !errors.Is(err, context.Canceled) || response.Action == native.InteractionAccept {
				t.Errorf("closed turn accepted late answer: %+v %v", response, err)
			}
			return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
		}}, nil
	}}
	runtime := started(t, driver, nil)
	input := spec()
	input.Policies.Interaction = InteractionInteractive
	done := make(chan turnResult, 1)
	go func() {
		done <- runTurn(runtime, context.Background(), input, turnOptions{InteractionHandler: interactionFunc(func(ctx context.Context, request native.InteractionRequest) (native.InteractionResponse, error) {
			close(entered)
			<-ctx.Done()
			return native.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: native.InteractionAccept}, nil
		})})
	}()
	<-entered
	closing, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Close(closing); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.Status != native.StatusCancelled {
		t.Fatalf("close lost cancellation: %+v", result)
	}
}

func TestUndeliverableHumanInputFailsWithoutWaiting(t *testing.T) {
	for _, policy := range []InteractionPolicy{InteractionInteractive, InteractionUnattended} {
		t.Run(string(policy), func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, native.OpenRequest) (NativeConnection, error) {
				return &fakeSession{prompt: func(ctx context.Context, _ native.TurnRequest, sink native.EventSink) native.TurnResult {
					_ = sink.Accept(ctx, native.Event{Kind: native.EventPromptAccepted})
					_, _ = sink.Interact(ctx, native.InteractionRequest{ID: "undeliverable", Kind: native.InteractionInput})
					return native.TurnResult{Status: native.StatusCancelled, StopReason: native.StopCancelled}
				}}, nil
			}}
			runtime := started(t, driver, nil)
			input := spec()
			input.Policies.Interaction = policy
			result := runTurn(runtime, context.Background(), input, turnOptions{})
			requireCode(t, result.Fault, native.FaultInteractionRequired)
		})
	}
}

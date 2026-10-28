package acp_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// recordingSink 保存真实驱动交付，并控制观察背压与失败。
type recordingSink struct {
	mu       sync.Mutex                                                                               // mu 保护事件记录。
	events   []contract.DriverEvent                                                                   // events 按原始交付顺序保存事件。
	accept   func(context.Context, contract.DriverEvent) error                                        // accept 控制本次交付结果。
	interact func(context.Context, contract.InteractionRequest) (contract.InteractionResponse, error) // interact 控制本次审批决定。
}

// Accept 记录真实驱动事实后执行测试指定的交付行为。
func (s *recordingSink) Accept(ctx context.Context, e contract.DriverEvent) error {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	if s.accept != nil {
		return s.accept(ctx, e)
	}
	return nil
}

// Interact 通过本次测试的独立交互端口返回决定。
func (s *recordingSink) Interact(ctx context.Context, r contract.InteractionRequest) (contract.InteractionResponse, error) {
	if s.interact != nil {
		return s.interact(ctx, r)
	}
	return contract.InteractionResponse{}, errors.New("unexpected interaction")
}
func (s *recordingSink) snapshot() []contract.DriverEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.DriverEvent(nil), s.events...)
}
func openRequest(t *testing.T) contract.OpenRequest {
	t.Helper()
	return contract.OpenRequest{Invocation: contract.InvocationRef{ID: "call-1", SessionID: "runtime-session", Revision: 1}, Session: contract.SessionDirective{Mode: contract.SessionNew}, Workspace: contract.WorkspaceScope{Root: t.TempDir()}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}}
}
func newDriver(t *testing.T, a *fixtureAgent) *driver.Driver {
	t.Helper()
	d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return a, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return d
}

// TestUnsupportedPolicyNeverPreparesNative 捕获安全策略或未支持资源被静默忽略的错误。
func TestUnsupportedPolicyNeverPreparesNative(t *testing.T) {
	for _, policy := range []string{"explicit_only", "deny_all", "allow_list", "mcp", "directories", "seed", "resume"} {
		t.Run(policy, func(t *testing.T) {
			var prepared atomic.Bool
			d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) {
				prepared.Store(true)
				return newFixtureAgent(), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			r := openRequest(t)
			switch policy {
			case "explicit_only":
				r.Policies.NativeConfig = contract.NativeConfigExplicitOnly
			case "deny_all":
				r.Policies.Tools.Mode = contract.ToolsDenyAll
			case "allow_list":
				r.Policies.Tools = contract.ToolPolicy{Mode: contract.ToolsAllowList, Allow: []string{"read"}}
			case "directories":
				r.Workspace.Directories = []string{t.TempDir()}
			case "mcp":
				r.Contributions.MCP = []contract.MCP{{Name: "mcp"}}
			case "seed":
				r.Session.Seed = []contract.SeedMessage{{Role: "user", Text: "earlier"}}
			case "resume":
				r.Session.Mode = contract.SessionResume
			}
			_, err = d.Open(context.Background(), r)
			var f *contract.Fault
			if !errors.As(err, &f) || f.Code != contract.FaultUnsupported {
				t.Fatalf("want unsupported, got %v", err)
			}
			if prepared.Load() {
				t.Fatal("native prepared for unsupported request")
			}
		})
	}
}

// TestPromptDeliveryAndMetadata 捕获过早宣布开始、丢失消息身份或猜测缺失阶段的错误。
func TestPromptDeliveryAndMetadata(t *testing.T) {
	a := newFixtureAgent()
	sink := &recordingSink{}
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		if got := sink.snapshot(); len(got) != 1 || got[0].Kind != contract.EventPromptAccepted {
			t.Errorf("delivery order %#v", got)
		}
		for _, sample := range []struct {
			id    string // id 是公开消息身份。
			phase string // phase 是公开元数据阶段。
			text  string // text 是片段正文。
		}{{"progress", "commentary", "working"}, {"answer", "final", "result"}, {"unknown", "", "unverified"}} {
			var meta map[string]any
			if sample.phase != "" {
				meta = map[string]any{"acp-go/message": map[string]any{"phase": sample.phase, "source": "native_phase", "complete": false}}
			}
			if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &sample.id, Meta: meta, Content: sdk.TextBlock(sample.text)}}}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 20, Size: 100}}}); err != nil {
			return sdk.PromptResponse{}, err
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: 12, OutputTokens: 3}}, nil
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.snapshot()) != 0 {
		t.Fatal("Open announced delivery")
	}
	r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink)
	if r.Status != contract.StatusCompleted || r.StopReason != contract.StopEndTurn || r.Usage.InputTokens != 12 || r.Usage.ContextTokens != 20 {
		t.Fatalf("result %#v", r)
	}
	var messages []contract.AssistantMessage
	for _, e := range sink.snapshot() {
		if e.Message != nil {
			messages = append(messages, *e.Message)
		}
	}
	if len(messages) != 3 {
		t.Fatalf("messages %#v", messages)
	}
	if messages[0].MessageID != "progress" || messages[0].Phase != contract.PhaseCommentary || messages[1].MessageID != "answer" || messages[1].Phase != contract.PhaseFinal || messages[2].Phase != contract.PhaseUnknown || messages[2].CompletionVerified {
		t.Fatalf("metadata %#v", messages)
	}
}

// TestPromptWaitsForReceivedNotification 捕获终态越过已接收通知的错误。
func TestPromptWaitsForReceivedNotification(t *testing.T) {
	a := newFixtureAgent()
	entered := make(chan struct{})
	release := make(chan struct{})
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		id := "final"
		err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("answer")}}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{accept: func(ctx context.Context, e contract.DriverEvent) error {
		if e.Message != nil {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	done := make(chan contract.DriverResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("notification absent")
	}
	select {
	case <-done:
		t.Fatal("Prompt returned before drain")
	default:
	}
	close(release)
	select {
	case r := <-done:
		if r.Status != contract.StatusCompleted {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("Prompt blocked")
	}
}

// TestCancellationAndLateEvents 捕获取消时丢失已确认配置、取消后交付、重复释放和连接复用的错误。
func TestCancellationAndLateEvents(t *testing.T) {
	a := newFixtureAgent()
	a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: []sdk.SessionConfigOption{
			selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "agent-full-access", "agent-full-access"),
			selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "selected-model", "selected-model"),
			selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "off", "off"),
		}}, nil
	}
	started := make(chan struct{})
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		close(started)
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan contract.DriverResult, 1)
	sink := &recordingSink{}
	go func() { done <- s.Prompt(ctx, contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prompt absent")
	}
	cancel()
	select {
	case r := <-done:
		if r.Status != contract.StatusCancelled {
			t.Fatalf("cancel %#v", r)
		}
		want := contract.Configuration{ActualMode: "agent-full-access", ActualModel: "selected-model", ActualReasoning: "off", ThinkingDisabled: true}
		if r.Configuration != want {
			t.Fatalf("cancel lost confirmed configuration: got %+v, want %+v", r.Configuration, want)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	before := len(sink.snapshot())
	lateCtx, lateCancel := context.WithTimeout(context.Background(), time.Second)
	defer lateCancel()
	_ = a.connection.Load().SessionUpdate(lateCtx, sdk.SessionNotification{SessionId: "native-session", Update: sdk.UpdateAgentMessageText("late")})
	if len(sink.snapshot()) != before {
		t.Fatal("late event delivered")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.closes.Load() != 1 {
		t.Fatalf("close count %d", a.closes.Load())
	}
	if r := s.Prompt(context.Background(), contract.TurnRequest{}, sink); r.Status != contract.StatusFailed {
		t.Fatalf("spent connection reused %#v", r)
	}
}

// TestPreparationFailureClosesAgent 捕获初始化失败遗留原生 Adapter 的错误。
func TestPreparationFailureClosesAgent(t *testing.T) {
	a := newFixtureAgent()
	a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{}, errors.New("initialize failed")
	}
	d := newDriver(t, a)
	session, err := d.Open(context.Background(), openRequest(t))
	if err == nil || session == nil {
		t.Fatal("expected preparation failure with cleanup owner")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.closes.Load() != 1 {
		t.Fatalf("close count %d", a.closes.Load())
	}
}

// TestUnadvertisedModelAndReasoning 捕获模型选择被静默丢弃的错误。
func TestUnadvertisedModelAndReasoning(t *testing.T) {
	for _, kind := range []string{"model", "reasoning"} {
		t.Run(kind, func(t *testing.T) {
			a := newFixtureAgent()
			d := newDriver(t, a)
			request := openRequest(t)
			if kind == "model" {
				request.Profile.Model = "requested-model"
			} else {
				request.Profile.Reasoning = "high"
			}
			session, err := d.Open(context.Background(), request)
			var f *contract.Fault
			if !errors.As(err, &f) || f.Code != contract.FaultUnsupported {
				t.Fatalf("unadvertised option: %v", err)
			}
			if session == nil {
				t.Fatal("missing cleanup owner")
			}
			if err := session.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if a.closes.Load() != 1 {
				t.Fatalf("close count %d", a.closes.Load())
			}
		})
	}
}

// TestAdvertisedConfigurationMustBeConfirmed 捕获设置成功但实际配置未回显的错误。
func TestAdvertisedConfigurationMustBeConfirmed(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing confirmation", true: "confirmed"}[confirm], func(t *testing.T) {
			a := newFixtureAgent()
			modelCategory := sdk.SessionConfigOptionCategoryModel
			thoughtCategory := sdk.SessionConfigOptionCategoryThoughtLevel
			models := sdk.SessionConfigSelectOptionsUngrouped{{Value: "requested-model", Name: "Requested model"}}
			thoughts := sdk.SessionConfigSelectOptionsUngrouped{{Value: "high", Name: "High"}}
			options := []sdk.SessionConfigOption{{Select: &sdk.SessionConfigOptionSelect{Id: "model-choice", Name: "Model", Category: &modelCategory, CurrentValue: "default", Options: sdk.SessionConfigSelectOptions{Ungrouped: &models}}}, {Select: &sdk.SessionConfigOptionSelect{Id: "thought-choice", Name: "Thought", Category: &thoughtCategory, CurrentValue: "default", Options: sdk.SessionConfigSelectOptions{Ungrouped: &thoughts}}}}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			var selected []string
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				if r.ValueId == nil || r.ValueId.SessionId != "native-session" {
					t.Errorf("invalid setting %#v", r)
				}
				selected = append(selected, string(r.ValueId.ConfigId)+"="+string(r.ValueId.Value))
				if confirm {
					for _, option := range options {
						if option.Select.Id == r.ValueId.ConfigId {
							option.Select.CurrentValue = r.ValueId.Value
						}
					}
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newDriver(t, a)
			request := openRequest(t)
			request.Profile.Model = "requested-model"
			request.Profile.Reasoning = "high"
			s, err := d.Open(context.Background(), request)
			if confirm {
				if err != nil {
					t.Fatal(err)
				}
				if len(selected) != 2 || selected[0] != "model-choice=requested-model" || selected[1] != "thought-choice=high" {
					t.Fatalf("selected %#v", selected)
				}
				_ = s.Close(context.Background())
			} else {
				var f *contract.Fault
				if !errors.As(err, &f) || f.Code != contract.FaultProtocol {
					t.Fatalf("want protocol failure, got %v", err)
				}
			}
		})
	}
}

// TestObserverFailureCancelsWithoutReplaying 捕获通知失败后重发Prompt或继续运行的错误。
func TestObserverFailureCancelsWithoutReplaying(t *testing.T) {
	a := newFixtureAgent()
	var prompts atomic.Int32
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		prompts.Add(1)
		_ = a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.UpdateAgentMessageText("body")})
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{accept: func(_ context.Context, e contract.DriverEvent) error {
		if e.Kind == contract.EventAssistantMessage {
			return errors.New("delivery rejected")
		}
		return nil
	}}
	r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.Fault == nil || r.Fault.Code != contract.FaultDelivery || prompts.Load() != 1 || a.closes.Load() != 1 {
		t.Fatalf("failure %#v, prompts %d closes %d", r, prompts.Load(), a.closes.Load())
	}
}

// TestDriverCloseDuringFactory 捕获关闭与原生准备竞争时遗留连接或返回可用会话的错误。
func TestDriverCloseDuringFactory(t *testing.T) {
	a := newFixtureAgent()
	entered := make(chan struct{})
	d, err := driver.New(driver.Config{ID: "fixture", Factory: func(ctx context.Context, _ contract.OpenRequest) (sdk.Agent, error) {
		close(entered)
		<-ctx.Done()
		return a, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	owner := make(chan contract.DriverSession, 1)
	request := openRequest(t)
	go func() { session, err := d.Open(context.Background(), request); owner <- session; opened <- err }()
	<-entered
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-opened; err == nil {
		t.Fatal("closed Driver returned usable session")
	}
	session := <-owner
	if session == nil {
		t.Fatal("shutdown race lost native owner")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.closes.Load() != 1 {
		t.Fatalf("close count %d", a.closes.Load())
	}
}

// TestCodexFactoryRejectsUnsupportedBeforeExecutable 捕获首个Codex工厂暗中降级策略的错误。
func TestCodexFactoryRejectsUnsupportedBeforeExecutable(t *testing.T) {
	d, err := driver.NewCodex(driver.CodexConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CodexPath: "/missing/codex"})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	request := openRequest(t)
	request.Policies.Tools.Mode = contract.ToolsDenyAll
	_, err = d.Open(context.Background(), request)
	var f *contract.Fault
	if !errors.As(err, &f) || f.Code != contract.FaultUnsupported {
		t.Fatalf("policy failure %v", err)
	}
	request.Policies.Tools.Mode = contract.ToolsAllowAll
	_, err = d.Open(context.Background(), request)
	if !errors.As(err, &f) || f.Code != contract.FaultPreparation {
		t.Fatalf("executable failure %v", err)
	}
}

// TestCompletedPromptKeepsCleanupSeparate 捕获清理失败覆盖真实完成事实或Driver吞掉清理错误的错误。
func TestCompletedPromptKeepsCleanupSeparate(t *testing.T) {
	a := newFixtureAgent()
	a.closeErr = errors.New("native close failed")
	d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return a, nil }})
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{})
	if r.Status != contract.StatusCompleted || r.StopReason != contract.StopEndTurn {
		t.Fatalf("cleanup overwrote completion %#v", r)
	}
	if a.closes.Load() != 0 {
		t.Fatal("Prompt consumed session cleanup")
	}
	if err := s.Close(context.Background()); err == nil {
		t.Fatal("Session.Close hid native failure")
	}
	if err := d.Close(context.Background()); err == nil {
		t.Fatal("Driver.Close hid native failure")
	}
}

// TestPermissionIdentityAndToolFacts 捕获重复审批身份、审批串轮或工具事实丢失的错误。
func TestPermissionIdentityAndToolFacts(t *testing.T) {
	a := newFixtureAgent()
	request := openRequest(t)
	var previous string
	interactions := 0
	sink := &recordingSink{interact: func(_ context.Context, r contract.InteractionRequest) (contract.InteractionResponse, error) {
		interactions++
		if r.Invocation != request.Invocation || r.ID == "" || r.ID == previous {
			return contract.InteractionResponse{}, errors.New("permission identity reused or mismatched")
		}
		previous = r.ID
		return contract.InteractionResponse{Invocation: r.Invocation, ID: r.ID, Allowed: true}, nil
	}}
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.StartToolCall("tool-1", "Read file", sdk.WithStartStatus(sdk.ToolCallStatusPending))}); err != nil {
			return sdk.PromptResponse{}, err
		}
		for range 2 {
			response, err := a.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool-1", Title: sdk.Ptr("Read file")}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
			if err != nil {
				return sdk.PromptResponse{}, err
			}
			if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "allow" {
				return sdk.PromptResponse{}, errors.New("valid permission denied")
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), contract.TurnRequest{Invocation: request.Invocation, Input: contract.Input{Text: "read"}}, sink)
	if r.Status != contract.StatusCompleted || interactions != 2 {
		t.Fatalf("permission result %#v count %d", r, interactions)
	}
	events := sink.snapshot()
	if len(events) != 2 || events[1].Tool == nil || events[1].Tool.ID != "tool-1" || events[1].Tool.Status != "pending" {
		t.Fatalf("tool events %#v", events)
	}
}

// TestDriverCloseCancelsActivePrompt 捕获Driver关闭被误报为原生执行故障的错误。
func TestDriverCloseCancelsActivePrompt(t *testing.T) {
	a := newFixtureAgent()
	started := make(chan struct{})
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		close(started)
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan contract.DriverResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.Status != contract.StatusCancelled {
			t.Fatalf("close result %#v", r)
		}
	case <-ctx.Done():
		t.Fatal("Prompt outlived Driver.Close")
	}
}

// TestPromptWaitsForInFlightPermission 捕获Prompt结束后仍在使用旧调用交互端口的错误。
func TestPromptWaitsForInFlightPermission(t *testing.T) {
	a := newFixtureAgent()
	entered := make(chan struct{})
	release := make(chan struct{})
	permissionDone := make(chan struct{})
	permissionCtx, cancelPermission := context.WithCancel(context.Background())
	defer cancelPermission()
	sink := &recordingSink{interact: func(ctx context.Context, r contract.InteractionRequest) (contract.InteractionResponse, error) {
		close(entered)
		select {
		case <-release:
			return contract.InteractionResponse{Invocation: r.Invocation, ID: r.ID, Allowed: true}, nil
		case <-ctx.Done():
			return contract.InteractionResponse{}, ctx.Err()
		}
	}}
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		go func() {
			defer close(permissionDone)
			_, _ = a.connection.Load().RequestPermission(permissionCtx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool-1"}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
		}()
		<-entered
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan contract.DriverResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink)
	}()
	<-entered
	select {
	case r := <-done:
		close(release)
		t.Fatalf("Prompt returned with active permission %#v", r)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case r := <-done:
		if r.Status != contract.StatusCompleted {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("Prompt blocked")
	}
	<-permissionDone
}

// TestNativeErrorRemainsFailed 捕获普通原生错误被驱动自己的收尾取消掩盖的错误。
func TestNativeErrorRemainsFailed(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{}, errors.New("native execution error")
	}
	d := newDriver(t, a)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{})
	if result.Status != contract.StatusFailed || result.StopReason != contract.StopFailed || result.Fault == nil || result.Fault.Code != contract.FaultDriver {
		t.Fatalf("ordinary native failure became status=%s reason=%s fault=%v", result.Status, result.StopReason, result.Fault)
	}
	if result.Fault.Cause == nil || errors.Is(result.Fault.Cause, context.Canceled) {
		t.Fatalf("native failure cause was lost: %v", result.Fault.Cause)
	}
}

// TestPermissionRequestCancellationReleasesInteraction 捕获单项权限撤销阻塞整轮或接纳迟到审批的错误。
func TestPermissionRequestCancellationReleasesInteraction(t *testing.T) {
	for _, lateApproval := range []bool{false, true} {
		t.Run(map[bool]string{false: "request cancellation", true: "late approval"}[lateApproval], func(t *testing.T) {
			a := newFixtureAgent()
			entered := make(chan struct{})
			nativeReturned := make(chan struct{})
			sink := &recordingSink{interact: func(ctx context.Context, r contract.InteractionRequest) (contract.InteractionResponse, error) {
				close(entered)
				<-ctx.Done()
				if lateApproval {
					return contract.InteractionResponse{Invocation: r.Invocation, ID: "stale-approval", Allowed: true}, nil
				}
				return contract.InteractionResponse{}, ctx.Err()
			}}
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				permissionCtx, cancel := context.WithCancel(ctx)
				permissionDone := make(chan struct{})
				go func() {
					defer close(permissionDone)
					_, _ = a.connection.Load().RequestPermission(permissionCtx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
				}()
				<-entered
				cancel()
				<-permissionDone
				close(nativeReturned)
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			d := newDriver(t, a)
			session, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan contract.DriverResult, 1)
			go func() { done <- session.Prompt(ctx, contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink) }()
			select {
			case <-nativeReturned:
			case <-time.After(time.Second):
				cancel()
				t.Fatal("native Agent did not return")
			}
			select {
			case result := <-done:
				if result.Status != contract.StatusCompleted || result.StopReason != contract.StopEndTurn || result.Fault != nil {
					t.Fatalf("request cancellation affected healthy turn: %#v", result)
				}
			case <-time.After(200 * time.Millisecond):
				cancel()
				<-done
				t.Fatal("cancelled permission still blocks completed native Prompt")
			}
		})
	}
}

// TestInvalidUsageFailsTurn 捕获通知处理器只记日志而未使非法协议事实导致调用失败的错误。
func TestInvalidUsageFailsTurn(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: -1, Size: 100}}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	d := newDriver(t, a)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{})
	if result.Status != contract.StatusFailed || result.StopReason != contract.StopFailed || result.Fault == nil || result.Fault.Code != contract.FaultProtocol {
		t.Fatalf("invalid usage ignored: status=%s fault=%v", result.Status, result.Fault)
	}
}

// TestPartialPreparationReturnsCleanupOwner 捕获部分准备失败丢弃原生使用者及其清理故障的错误。
func TestPartialPreparationReturnsCleanupOwner(t *testing.T) {
	for _, stage := range []string{"factory", "initialize", "configuration"} {
		t.Run(stage, func(t *testing.T) {
			agent := newFixtureAgent()
			agent.closeNative = func(context.Context) error {
				if agent.closes.Load() == 1 {
					return errors.New("transient native close failure")
				}
				return nil
			}
			request := openRequest(t)
			if stage == "initialize" {
				agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
					return sdk.InitializeResponse{}, errors.New("initialize failed")
				}
			}
			if stage == "configuration" {
				request.Profile.Model = "unsupported-model"
			}
			d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) {
				if stage == "factory" {
					return agent, errors.New("factory partial failure")
				}
				return agent, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := d.Open(context.Background(), request)
			if err == nil || session == nil {
				t.Fatalf("partial native owner lost: session=%v error=%v closes=%d", session, err, agent.closes.Load())
			}
			if agent.closes.Load() != 0 {
				t.Fatal("Open consumed native cleanup without returning its result")
			}
			if result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{}); result.Status != contract.StatusFailed || result.Fault == nil || result.Fault.Code != contract.FaultClosed {
				t.Fatalf("partial owner accepted Prompt: %#v", result)
			}
			if err := session.Close(context.Background()); err == nil {
				t.Fatal("first native cleanup failure lost")
			}
			if err := d.Close(context.Background()); err != nil {
				t.Fatalf("Driver cannot retry partial cleanup: %v", err)
			}
			if err := session.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if agent.closes.Load() != 2 {
				t.Fatalf("native cleanup attempts=%d, want 2", agent.closes.Load())
			}
		})
	}
}

// TestSessionCloseRetriesPublicNativeCloser 捕获ACP边界结束后缓存旧错误而不重试原生清理的错误。
func TestSessionCloseRetriesPublicNativeCloser(t *testing.T) {
	agent := newFixtureAgent()
	agent.closeNative = func(context.Context) error {
		if agent.closes.Load() == 1 {
			return errors.New("transient native close failure")
		}
		return nil
	}
	d := newDriver(t, agent)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "hello"}}, &recordingSink{})
	if result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
	if err := session.Close(context.Background()); err == nil {
		t.Fatal("expected first native cleanup failure")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("public native cleanup not retried: %v", err)
	}
	if agent.closes.Load() != 2 {
		t.Fatalf("native cleanup attempts=%d, want 2", agent.closes.Load())
	}
}

// TestConcurrentDriverAndSessionCloseShareOwner 捕获Driver和Core同时关闭造成原生资源重复释放的错误。
func TestConcurrentDriverAndSessionCloseShareOwner(t *testing.T) {
	agent := newFixtureAgent()
	entered := make(chan struct{})
	release := make(chan struct{})
	agent.closeNative = func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	d := newDriver(t, agent)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- session.Close(ctx) }()
	<-entered
	go func() { second <- d.Close(ctx) }()
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if agent.closes.Load() != 1 {
		t.Fatalf("native cleanup attempts=%d, want 1", agent.closes.Load())
	}
}

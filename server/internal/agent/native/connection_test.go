package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureAgent 通过公开 ACP 接口控制会话准备、交付、通知和退出时序。
type fixtureAgent struct {
	// connection 保存 acpserver 注入的真实 SDK Agent 端连接。
	connection atomic.Pointer[sdk.AgentSideConnection]
	// bound 在连接注入后解除 Initialize 的构造窗口。
	bound chan struct{}
	// initialize 可覆盖初始化结果以验证准备失败的清理。
	initialize func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error)
	// newSession 可检查原生会话实际收到的工作区与配置。
	newSession func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error)
	// prompt 定义当前测试的原生调用与通知行为。
	prompt func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error)
	// configure 可检查模型参数是否通过公开配置接口应用。
	configure func(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error)
	// configureMode 控制标准模式目录的切换回执。
	configureMode func(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error)
	// closeNative 控制每次公开Close的实际执行与重试结果。
	closeNative func(context.Context) error
	// closeErr 控制原生资源释放失败。
	closeErr error
	// closes 记录原生 Adapter 实际资源释放次数。
	closes atomic.Int32
}

func newFixtureAgent() *fixtureAgent { return &fixtureAgent{bound: make(chan struct{})} }

// SetAgentConnection 接收真实acpserver注入的唯一SDK连接。
func (a *fixtureAgent) SetAgentConnection(connection *sdk.AgentSideConnection) {
	a.connection.Store(connection)
	close(a.bound)
}

// Initialize 完成公开ACP初始化并允许情景覆盖。
func (a *fixtureAgent) Initialize(ctx context.Context, request sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	select {
	case <-a.bound:
	case <-ctx.Done():
		return sdk.InitializeResponse{}, ctx.Err()
	}
	if a.initialize != nil {
		return a.initialize(ctx, request)
	}
	return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber}, nil
}

// NewSession 创建公开ACP测试会话并允许情景覆盖。
func (a *fixtureAgent) NewSession(ctx context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	if a.newSession != nil {
		return a.newSession(ctx, request)
	}
	return sdk.NewSessionResponse{SessionId: "native-session"}, nil
}

// Prompt 执行当前测试指定的原生调用情景。
func (a *fixtureAgent) Prompt(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
	if a.prompt != nil {
		return a.prompt(ctx, request)
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

// SetSessionConfigOption 验证公开配置请求并返回实际生效目录。
func (a *fixtureAgent) SetSessionConfigOption(ctx context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	if a.configure != nil {
		return a.configure(ctx, request)
	}
	return sdk.SetSessionConfigOptionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetConfigOption)
}

// Close 记录实际资源释放并返回预设清理结果。
func (a *fixtureAgent) Close(ctx context.Context) error {
	a.closes.Add(1)
	if a.closeNative != nil {
		return a.closeNative(ctx)
	}
	return a.closeErr
}

// Cancel 由SDK维护请求取消，测试Agent不再持有第二套取消状态。
func (a *fixtureAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

// Authenticate 拒绝测试未声明的认证操作。
func (a *fixtureAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodAuthenticate)
}

// Logout 拒绝测试未声明的退出登录操作。
func (a *fixtureAgent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

// CloseSession 拒绝测试未声明的单会话关闭操作。
func (a *fixtureAgent) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionClose)
}

// ListSessions 拒绝测试未声明的会话枚举操作。
func (a *fixtureAgent) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionList)
}

// ResumeSession 拒绝测试未声明的恢复操作。
func (a *fixtureAgent) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

// SetSessionMode 拒绝测试未声明的原生模式切换。
func (a *fixtureAgent) SetSessionMode(ctx context.Context, request sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	if a.configureMode != nil {
		return a.configureMode(ctx, request)
	}
	return sdk.SetSessionModeResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetMode)
}

// recordingSink 保存真实驱动交付，并控制观察背压与失败。
type recordingSink struct {
	mu       sync.Mutex                                                             // mu 保护事件记录。
	events   []Event                                                                // events 按原始交付顺序保存事件。
	accept   func(context.Context, Event) error                                     // accept 控制本次交付结果。
	interact func(context.Context, InteractionRequest) (InteractionResponse, error) // interact 控制本次审批决定。
}

// Accept 记录真实驱动事实后执行测试指定的交付行为。
func (s *recordingSink) Accept(ctx context.Context, e Event) error {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	if s.accept != nil {
		return s.accept(ctx, e)
	}
	return nil
}

// Interact 通过本次测试的独立交互端口返回决定。
func (s *recordingSink) Interact(ctx context.Context, r InteractionRequest) (InteractionResponse, error) {
	if s.interact != nil {
		return s.interact(ctx, r)
	}
	return InteractionResponse{}, errors.New("unexpected interaction")
}
func (s *recordingSink) snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}
func openRequest(t *testing.T) OpenRequest {
	t.Helper()
	return OpenRequest{Invocation: InvocationRef{ID: "call-1", SessionID: "runtime-session", Revision: 1}, Session: SessionDirective{Mode: SessionNew}, Workspace: WorkspaceScope{Root: t.TempDir()}, Policies: SessionPolicies{NativeConfig: NativeConfigInherit, Tools: ToolPolicy{Mode: ToolsAllowAll}}}
}
func newConnectionForAgent(t *testing.T, a *fixtureAgent) *connectionFixture {
	t.Helper()
	d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
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
	for _, policy := range []string{"explicit_only", "deny_all", "allow_list", "resume"} {
		t.Run(policy, func(t *testing.T) {
			var prepared atomic.Bool
			d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) {
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
				r.Policies.NativeConfig = NativeConfigExplicitOnly
			case "deny_all":
				r.Policies.Tools.Mode = ToolsDenyAll
			case "allow_list":
				r.Policies.Tools = ToolPolicy{Mode: ToolsAllowList, Allow: []string{"read"}}
			case "resume":
				r.Session.Mode = SessionResume
			}
			_, err = d.Open(context.Background(), r)
			var f *Fault
			if !errors.As(err, &f) || f.Code != FaultUnsupported {
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
		if got := sink.snapshot(); len(got) != 1 || got[0].Kind != EventPromptAccepted {
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.snapshot()) != 0 {
		t.Fatal("Open announced delivery")
	}
	r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, sink)
	if r.Status != StatusCompleted || r.StopReason != StopEndTurn || r.Usage.InputTokens != 12 || r.Usage.ContextTokens != 20 {
		t.Fatalf("result %#v", r)
	}
	var messages []AssistantMessage
	for _, e := range sink.snapshot() {
		if e.Message != nil {
			messages = append(messages, *e.Message)
		}
	}
	if len(messages) != 3 {
		t.Fatalf("messages %#v", messages)
	}
	if messages[0].MessageID != "progress" || messages[0].Phase != PhaseCommentary || messages[1].MessageID != "answer" || messages[1].Phase != PhaseFinal || messages[2].Phase != PhaseUnknown || messages[2].CompletionVerified {
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{accept: func(ctx context.Context, e Event) error {
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
	done := make(chan TurnResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, sink)
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
		if r.Status != StatusCompleted {
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan TurnResult, 1)
	sink := &recordingSink{}
	go func() { done <- s.Prompt(ctx, TurnRequest{Input: Input{Text: "hello"}}, sink) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prompt absent")
	}
	cancel()
	select {
	case r := <-done:
		if r.Status != StatusCancelled {
			t.Fatalf("cancel %#v", r)
		}
		want := Configuration{ActualMode: "agent-full-access", ActualModel: "selected-model", ActualReasoning: "off", ThinkingDisabled: true}
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
	if r := s.Prompt(context.Background(), TurnRequest{}, sink); r.Status != StatusFailed {
		t.Fatalf("spent connection reused %#v", r)
	}
}

// TestPreparationFailureClosesAgent 捕获初始化失败遗留原生 Adapter 的错误。
func TestPreparationFailureClosesAgent(t *testing.T) {
	a := newFixtureAgent()
	a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{}, errors.New("initialize failed")
	}
	d := newConnectionForAgent(t, a)
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
			d := newConnectionForAgent(t, a)
			request := openRequest(t)
			if kind == "model" {
				request.Profile.Model = "requested-model"
			} else {
				request.Profile.Reasoning = "high"
			}
			session, err := d.Open(context.Background(), request)
			var f *Fault
			if !errors.As(err, &f) || f.Code != FaultUnsupported {
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
			d := newConnectionForAgent(t, a)
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
				var f *Fault
				if !errors.As(err, &f) || f.Code != FaultProtocol {
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{accept: func(_ context.Context, e Event) error {
		if e.Kind == EventAssistantMessage {
			return errors.New("delivery rejected")
		}
		return nil
	}}
	r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, sink)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.Fault == nil || r.Fault.Code != FaultDelivery || prompts.Load() != 1 || a.closes.Load() != 1 {
		t.Fatalf("failure %#v, prompts %d closes %d", r, prompts.Load(), a.closes.Load())
	}
}

// TestOpenCancellationDuringFactory 验证准备被取消时仍返回已经取得的原生资源所有者。
func TestOpenCancellationDuringFactory(t *testing.T) {
	a := newFixtureAgent()
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	config := ConnectionConfig{ID: "fixture", Factory: func(ctx context.Context, _ OpenRequest) (sdk.Agent, error) {
		close(entered)
		<-ctx.Done()
		return a, nil
	}}
	opened := make(chan error, 1)
	owner := make(chan *Connection, 1)
	request := openRequest(t)
	go func() { connection, err := Open(ctx, config, request); owner <- connection; opened <- err }()
	<-entered
	cancel()
	if err := <-opened; err == nil {
		t.Fatal("cancelled Open returned usable connection")
	}
	connection := <-owner
	if connection == nil {
		t.Fatal("cancelled preparation lost native owner")
	}
	if err := connection.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.closes.Load() != 1 {
		t.Fatalf("close count %d", a.closes.Load())
	}
}

// TestCodexFactoryRejectsUnsupportedBeforeExecutable 捕获首个Codex工厂暗中降级策略的错误。
func TestCodexFactoryRejectsUnsupportedBeforeExecutable(t *testing.T) {
	d, err := newConnectionFixture(ConnectionConfig{ID: "codex", Factory: func(ctx context.Context, request OpenRequest) (sdk.Agent, error) {
		return NewAgent(ctx, AgentConfig{RuntimeID: "codex", Executable: "/missing/codex", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	request := openRequest(t)
	request.Policies.Tools.Mode = ToolsDenyAll
	_, err = d.Open(context.Background(), request)
	var f *Fault
	if !errors.As(err, &f) || f.Code != FaultUnsupported {
		t.Fatalf("policy failure %v", err)
	}
	request.Policies.Tools.Mode = ToolsAllowAll
	_, err = d.Open(context.Background(), request)
	if !errors.As(err, &f) || f.Code != FaultPreparation {
		t.Fatalf("executable failure %v", err)
	}
}

// TestCompletedPromptKeepsCleanupSeparate 捕获清理失败覆盖真实完成事实或Driver吞掉清理错误的错误。
func TestCompletedPromptKeepsCleanupSeparate(t *testing.T) {
	a := newFixtureAgent()
	a.closeErr = errors.New("native close failed")
	d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{})
	if r.Status != StatusCompleted || r.StopReason != StopEndTurn {
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
	sink := &recordingSink{interact: func(_ context.Context, r InteractionRequest) (InteractionResponse, error) {
		interactions++
		if r.Invocation != request.Invocation || r.ID == "" || r.ID == previous {
			return InteractionResponse{}, errors.New("permission identity reused or mismatched")
		}
		previous = r.ID
		return InteractionResponse{Invocation: r.Invocation, ID: r.ID, Action: InteractionAccept, OptionID: r.Approval.Options[0].ID}, nil
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), TurnRequest{Invocation: request.Invocation, Input: Input{Text: "read"}}, sink)
	if r.Status != StatusCompleted || interactions != 2 {
		t.Fatalf("permission result %#v count %d", r, interactions)
	}
	events := sink.snapshot()
	if len(events) != 2 || events[1].Tool == nil || events[1].Tool.ID != "tool-1" || events[1].Tool.Status != "pending" {
		t.Fatalf("tool events %#v", events)
	}
}

// TestConnectionCloseCancelsActivePrompt 捕获Driver关闭被误报为原生执行故障的错误。
func TestConnectionCloseCancelsActivePrompt(t *testing.T) {
	a := newFixtureAgent()
	started := make(chan struct{})
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		close(started)
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan TurnResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.Status != StatusCancelled {
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
	sink := &recordingSink{interact: func(ctx context.Context, r InteractionRequest) (InteractionResponse, error) {
		close(entered)
		select {
		case <-release:
			return InteractionResponse{Invocation: r.Invocation, ID: r.ID, Action: InteractionAccept, OptionID: r.Approval.Options[0].ID}, nil
		case <-ctx.Done():
			return InteractionResponse{}, ctx.Err()
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
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan TurnResult, 1)
	go func() {
		done <- s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, sink)
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
		if r.Status != StatusCompleted {
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
	d := newConnectionForAgent(t, a)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{})
	if result.Status != StatusFailed || result.StopReason != StopFailed || result.Fault == nil || result.Fault.Code != FaultDriver {
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
			sink := &recordingSink{interact: func(ctx context.Context, r InteractionRequest) (InteractionResponse, error) {
				close(entered)
				<-ctx.Done()
				if lateApproval {
					return InteractionResponse{Invocation: r.Invocation, ID: "stale-approval", Action: InteractionAccept, OptionID: r.Approval.Options[0].ID}, nil
				}
				return InteractionResponse{}, ctx.Err()
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
			d := newConnectionForAgent(t, a)
			session, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan TurnResult, 1)
			go func() { done <- session.Prompt(ctx, TurnRequest{Input: Input{Text: "hello"}}, sink) }()
			select {
			case <-nativeReturned:
			case <-time.After(time.Second):
				cancel()
				t.Fatal("native Agent did not return")
			}
			select {
			case result := <-done:
				if result.Status != StatusCompleted || result.StopReason != StopEndTurn || result.Fault != nil {
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
	d := newConnectionForAgent(t, a)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{})
	if result.Status != StatusFailed || result.StopReason != StopFailed || result.Fault == nil || result.Fault.Code != FaultProtocol {
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
			d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) {
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
			if result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{}); result.Status != StatusFailed || result.Fault == nil || result.Fault.Code != FaultClosed {
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
	d := newConnectionForAgent(t, agent)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "hello"}}, &recordingSink{})
	if result.Status != StatusCompleted {
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

// TestConcurrentConnectionCloseSharesOwner 捕获Driver和Core同时关闭造成原生资源重复释放的错误。
func TestConcurrentConnectionCloseSharesOwner(t *testing.T) {
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
	d := newConnectionForAgent(t, agent)
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

// TestChatInputKeepsSnapshotsAndNegotiatesImages 验证图片能力协商与不可变代码引用实际进入公开 Prompt。
func TestChatInputKeepsSnapshotsAndNegotiatesImages(t *testing.T) {
	for _, images := range []bool{false, true} {
		t.Run(map[bool]string{false: "text_fallback", true: "native_image"}[images], func(t *testing.T) {
			agent := newFixtureAgent()
			agent.initialize = func(_ context.Context, request sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{PromptCapabilities: sdk.PromptCapabilities{Image: images}}}, nil
			}
			image := filepath.Join(t.TempDir(), "image.png")
			if err := os.WriteFile(image, []byte("image-bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				want := 1
				if images {
					want = 2
				}
				if len(request.Prompt) != want {
					t.Fatalf("prompt blocks=%d want=%d", len(request.Prompt), want)
				}
				text := request.Prompt[0].Text.Text
				for _, part := range []string{"current task", "attachments/image.png", "sample.go:3-4", "fixed source snapshot", "selected skills: review", "selected MCP connections: docs"} {
					if !strings.Contains(text, part) {
						t.Errorf("missing %q in %q", part, text)
					}
				}
				if images && (request.Prompt[1].Image == nil || request.Prompt[1].Image.Data != base64.StdEncoding.EncodeToString([]byte("image-bytes"))) {
					t.Fatalf("image payload lost: %+v", request.Prompt[1])
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			fixture := newConnectionForAgent(t, agent)
			session, err := fixture.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "current task", Attachments: []Attachment{{Name: "image.png", Path: image, DisplayPath: "attachments/image.png", MIMEType: "image/png"}}, CodeReferences: []CodeReference{{Path: "sample.go", StartLine: 3, EndLine: 4, Content: "fixed source snapshot"}}, SelectedSkills: []string{"review"}, SelectedMCP: []string{"docs"}}}, &recordingSink{})
			if result.Status != StatusCompleted {
				t.Fatalf("input failed: %+v", result)
			}
		})
	}
}

// TestChatMCPUsesLeasedTypedConnections 验证三种传输、稳定秘密配置及原生能力拒绝。
func TestChatMCPUsesLeasedTypedConnections(t *testing.T) {
	agent := newFixtureAgent()
	agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{McpCapabilities: sdk.McpCapabilities{Http: true, Sse: true}}}, nil
	}
	agent.newSession = func(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		if len(request.McpServers) != 3 || request.McpServers[0].Stdio == nil || request.McpServers[1].Http == nil || request.McpServers[2].Sse == nil {
			t.Fatalf("MCP transport lost: %+v", request.McpServers)
		}
		if request.McpServers[0].Stdio.Env[0].Name != "A" || request.McpServers[1].Http.Headers[0].Value != "secret" {
			t.Fatalf("connection config lost: %+v", request.McpServers)
		}
		return sdk.NewSessionResponse{SessionId: "native-session"}, nil
	}
	request := openRequest(t)
	request.Contributions.MCP = []MCP{{Name: "local", ResourceID: "stdio"}, {Name: "remote", ResourceID: "http"}, {Name: "events", ResourceID: "sse"}}
	request.Resources = []ResourceBinding{{ID: "stdio", MCP: &MCPConnection{Transport: MCPStdio, Command: "node", Arguments: []string{"server.js"}, Environment: map[string]string{"Z": "last", "A": "first"}}}, {ID: "http", MCP: &MCPConnection{Transport: MCPHTTP, URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "secret"}}}, {ID: "sse", MCP: &MCPConnection{Transport: MCPSSE, URL: "https://example.test/events"}}}
	fixture := newConnectionForAgent(t, agent)
	if _, err := fixture.Open(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	unsupported := newConnectionForAgent(t, newFixtureAgent())
	if _, err := unsupported.Open(context.Background(), request); err == nil {
		t.Fatal("unadvertised HTTP/SSE transport accepted")
	}
}

// TestChatApprovalKeepsExactOptions 验证未知审批类别仍通过原始身份回传，不压缩成布尔授权。
func TestChatApprovalKeepsExactOptions(t *testing.T) {
	agent := newFixtureAgent()
	sink := &recordingSink{interact: func(_ context.Context, request InteractionRequest) (InteractionResponse, error) {
		if request.Approval == nil || request.Approval.ToolCallID != "tool" || request.Approval.Command != "git status" || len(request.Approval.Options) != 2 || request.Approval.Options[1].Kind != "vendor_restricted" {
			t.Fatalf("approval facts lost: %+v", request)
		}
		return InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: InteractionAccept, OptionID: "restricted"}, nil
	}}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool", Title: sdk.Ptr("Run status"), RawInput: map[string]any{"command": "git status"}}, Options: []sdk.PermissionOption{{OptionId: "always", Name: "Always", Kind: sdk.PermissionOptionKindAllowAlways}, {OptionId: "restricted", Name: "Restricted", Kind: "vendor_restricted"}}})
		if err != nil {
			return sdk.PromptResponse{}, err
		}
		if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "restricted" {
			t.Fatalf("exact approval option lost: %+v", response)
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink)
	if result.Status != StatusCompleted {
		t.Fatalf("approval failed: %+v", result)
	}
}

// TestChatFormAndURLElicitationRoundTrip 验证公开问题模式与提交、拒绝、取消三种决定。
func TestChatFormAndURLElicitationRoundTrip(t *testing.T) {
	for _, action := range []InteractionAction{InteractionAccept, InteractionDecline, InteractionCancel} {
		t.Run(string(action), func(t *testing.T) {
			agent := newFixtureAgent()
			var interactions int
			sink := &recordingSink{interact: func(_ context.Context, request InteractionRequest) (InteractionResponse, error) {
				interactions++
				if request.Input == nil || request.Prompt != "Choose" {
					t.Fatalf("input missing: %+v", request)
				}
				if interactions == 1 && request.Input.RequestedSchema["properties"] == nil || interactions == 2 && request.Input.URL != "https://example.test/authorize" {
					t.Fatalf("schema/URL lost: %+v", request.Input)
				}
				return InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: action, Content: map[string]any{"choice": "yes"}}, nil
			}}
			agent.prompt = func(ctx context.Context, prompt sdk.PromptRequest) (sdk.PromptResponse, error) {
				form := sdk.NewUnstableCreateElicitationRequestForm(sdk.UnstableElicitationSchema{Properties: map[string]any{"choice": map[string]any{"type": "string"}}})
				form.Form.Message = "Choose"
				url := sdk.NewUnstableCreateElicitationRequestUrl("url-request", "https://example.test/authorize")
				url.Url.Message = "Choose"
				for _, request := range []sdk.UnstableCreateElicitationRequest{form, url} {
					response, err := agent.connection.Load().UnstableCreateElicitation(ctx, request)
					if err != nil {
						return sdk.PromptResponse{}, err
					}
					if action == InteractionAccept && (response.Accept == nil || response.Accept.Content["choice"] != "yes") || action == InteractionDecline && response.Decline == nil || action == InteractionCancel && response.Cancel == nil {
						t.Fatalf("decision lost: %+v", response)
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			fixture := newConnectionForAgent(t, agent)
			session, err := fixture.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink)
			if result.Status != StatusCompleted || interactions != 2 {
				t.Fatalf("elicitation failed: %+v interactions=%d", result, interactions)
			}
		})
	}
}

// TestChatCanonicalToolOutputKeepsRichFacts 验证参数、输出、位置与差异通过同一规范事件到达宿主。
func TestChatCanonicalToolOutputKeepsRichFacts(t *testing.T) {
	agent := newFixtureAgent()
	sink := &recordingSink{}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		original := "before"
		call := sdk.SessionUpdateToolCall{ToolCallId: "edit", Title: "Edit file", Kind: sdk.ToolKindEdit, Status: sdk.ToolCallStatusCompleted, RawInput: map[string]any{"path": "main.go"}, RawOutput: map[string]any{"ok": true}, Locations: []sdk.ToolCallLocation{{Path: "main.go", Line: sdk.Ptr(4)}}, Content: []sdk.ToolCallContent{{Diff: &sdk.ToolCallContentDiff{Path: "main.go", OldText: &original, NewText: "after"}}, {Content: &sdk.ToolCallContentContent{Content: sdk.TextBlock("updated")}}}}
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{ToolCall: &call}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "edit"}}, sink)
	if result.Status != StatusCompleted {
		t.Fatal(result)
	}
	var tool *ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tool = event.Tool
		}
	}
	if tool == nil || tool.Title != "Edit file" || tool.Kind != "edit" || tool.Text != "updated" || len(tool.Content) != 2 || tool.Content[0].Diff == nil || tool.Content[0].Diff.OldText != "before" || len(tool.Locations) != 1 || *tool.Locations[0].Line != 4 {
		t.Fatalf("rich tool facts lost: %+v", tool)
	}
	var input, output map[string]any
	if json.Unmarshal(tool.Input, &input) != nil || json.Unmarshal(tool.Output, &output) != nil || input["path"] != "main.go" || output["ok"] != true {
		t.Fatalf("tool JSON lost: %+v", tool)
	}
}

// TestChatRichAssistantContentPreservesPublicResources 捕获图片地址、内嵌资源与音频在规范转换时丢失的错误。
func TestChatRichAssistantContentPreservesPublicResources(t *testing.T) {
	agent := newFixtureAgent()
	image := sdk.ImageBlock("aW1hZ2U=", "image/png")
	image.Image.Uri = sdk.Ptr("https://example.test/image.png")
	blocks := []sdk.ContentBlock{
		image,
		{Audio: &sdk.ContentBlockAudio{Type: "audio", Data: "YXVkaW8=", MimeType: "audio/wav"}},
		{ResourceLink: &sdk.ContentBlockResourceLink{Type: "resource_link", Uri: "file:///workspace/report.txt", Name: "report", MimeType: sdk.Ptr("text/plain")}},
		{Resource: &sdk.ContentBlockResource{Type: "resource", Resource: sdk.EmbeddedResourceResource{TextResourceContents: &sdk.TextResourceContents{Uri: "file:///workspace/note.txt", Text: "saved snapshot", MimeType: sdk.Ptr("text/plain")}}}},
		{Resource: &sdk.ContentBlockResource{Type: "resource", Resource: sdk.EmbeddedResourceResource{BlobResourceContents: &sdk.BlobResourceContents{Uri: "file:///workspace/archive.bin", Blob: "Ynl0ZXM=", MimeType: sdk.Ptr("application/octet-stream")}}}},
	}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		for _, block := range blocks {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: sdk.Ptr("answer"), Content: block}}}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "show resources"}}, sink)
	if result.Status != StatusCompleted {
		t.Fatal(result)
	}
	var content []ContentBlock
	for _, event := range sink.snapshot() {
		if event.Message != nil {
			content = append(content, event.Message.Content...)
		}
	}
	if len(content) != 5 || content[0].Type != "image" || content[0].URI != "https://example.test/image.png" || content[0].Data != "aW1hZ2U=" || content[1].Type != "audio" || content[1].MIMEType != "audio/wav" || content[2].Name != "report" || content[3].Text != "saved snapshot" || content[4].Data != "Ynl0ZXM=" {
		t.Fatalf("rich public resources lost: %+v", content)
	}
}

// TestChatPlanKeepsProgressAndRemoval 捕获标准计划未投射或完成后重复终态的错误。
func TestChatPlanKeepsProgressAndRemoval(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		updates := []sdk.SessionUpdate{
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "unknown"}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "inspect source", Priority: sdk.PlanEntryPriorityHigh, Status: sdk.PlanEntryStatusInProgress, Meta: map[string]any{"private": "must-not-cross"}}}}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "inspect source", Priority: sdk.PlanEntryPriorityHigh, Status: sdk.PlanEntryStatusCompleted}}}},
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "done"}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "verify", Priority: sdk.PlanEntryPriorityMedium, Status: sdk.PlanEntryStatusPending}}}},
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "active"}},
		}
		for _, update := range updates {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "plan"}}, sink)
	if result.Status != StatusCompleted {
		t.Fatal(result)
	}
	var tools []ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tools = append(tools, *event.Tool)
		}
	}
	if len(tools) != 4 || tools[0].ID != "acp-plan" || tools[0].Name != "update_plan" || tools[0].Update || tools[0].Status != "in_progress" || !tools[1].Update || tools[1].Status != "completed" || tools[2].Status != "in_progress" || tools[3].Status != "completed" {
		t.Fatalf("plan lifecycle lost: %+v", tools)
	}
	var input struct {
		Plan []struct {
			Content  string // Content 是计划条目正文。
			Priority string // Priority 是公开优先级。
			Status   string // Status 是公开执行状态。
		} // Plan 是公开计划条目的完整快照。
	}
	if err := json.Unmarshal(tools[0].Input, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Plan) != 1 || input.Plan[0].Content != "inspect source" || input.Plan[0].Priority != "high" || input.Plan[0].Status != "in_progress" || strings.Contains(string(tools[0].Input), "private") {
		t.Fatalf("plan content lost or metadata copied: %s", tools[0].Input)
	}
}

// TestChatContextUsagePreservesWindow 捕获上下文已用量到达宿主时丢失总窗口的错误。
func TestChatContextUsagePreservesWindow(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 900, Size: 64000}}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink)
	if result.Status != StatusCompleted || result.Usage.ContextWindowTokens != 64000 || result.Usage.ContextTokens != 900 {
		t.Fatalf("result context window lost: %+v", result)
	}
	var eventUsage *Usage
	for _, event := range sink.snapshot() {
		if event.Usage != nil {
			eventUsage = event.Usage
		}
	}
	if eventUsage == nil || !eventUsage.ContextKnown || eventUsage.ContextWindowTokens != 64000 || eventUsage.ContextTokens != 900 {
		t.Fatalf("live context window lost: %+v", eventUsage)
	}
}

// TestChatApprovalRoundTripsEveryPublicChoice 捕获始终允许、始终拒绝及未知选项被压缩为布尔决定的错误。
func TestChatApprovalRoundTripsEveryPublicChoice(t *testing.T) {
	for _, kind := range []sdk.PermissionOptionKind{sdk.PermissionOptionKindAllowOnce, sdk.PermissionOptionKindAllowAlways, sdk.PermissionOptionKindRejectOnce, sdk.PermissionOptionKindRejectAlways, "vendor_restricted"} {
		t.Run(string(kind), func(t *testing.T) {
			agent := newFixtureAgent()
			sink := &recordingSink{interact: func(_ context.Context, request InteractionRequest) (InteractionResponse, error) {
				if request.Approval == nil || len(request.Approval.Options) != 1 || request.Approval.Options[0].Kind != string(kind) {
					t.Error("public choice lost")
				}
				return InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: InteractionAccept, OptionID: "native-choice"}, nil
			}}
			agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "native-choice", Name: "Choice", Kind: kind}}})
				if err != nil {
					return sdk.PromptResponse{}, err
				}
				if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "native-choice" {
					t.Errorf("public choice not returned: %+v", response)
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			fixture := newConnectionForAgent(t, agent)
			session, err := fixture.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink)
			if result.Status != StatusCompleted {
				t.Fatal(result)
			}
		})
	}
}

// TestChatApprovalRejectsUnofferedChoice 捕获宿主返回不存在选项后被当作原生授权的错误。
func TestChatApprovalRejectsUnofferedChoice(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "offered", Name: "Once", Kind: sdk.PermissionOptionKindAllowOnce}}})
		if err != nil {
			return sdk.PromptResponse{}, err
		}
		if response.Outcome.Cancelled == nil || response.Outcome.Selected != nil {
			t.Errorf("unoffered choice accepted: %+v", response)
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	sink := &recordingSink{interact: func(_ context.Context, request InteractionRequest) (InteractionResponse, error) {
		return InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: InteractionAccept, OptionID: "not-offered"}, nil
	}}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink); result.Status != StatusCompleted {
		t.Fatal(result)
	}
}

// TestChatMCPRequiresMaterializedConnection 捕获必需MCP丢失及可选MCP缺失阻断准备的错误。
func TestChatMCPRequiresMaterializedConnection(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			agent := newFixtureAgent()
			var created bool
			agent.newSession = func(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				created = true
				if len(request.McpServers) != 0 {
					t.Errorf("missing resource materialized: %+v", request.McpServers)
				}
				return sdk.NewSessionResponse{SessionId: "native-session"}, nil
			}
			fixture := newConnectionForAgent(t, agent)
			request := openRequest(t)
			request.Contributions.MCP = []MCP{{Name: "docs", ResourceID: "docs", Optional: optional}}
			_, err := fixture.Open(context.Background(), request)
			if optional {
				if err != nil || !created {
					t.Fatalf("optional resource blocked native session: created=%v err=%v", created, err)
				}
			} else {
				var failure *Fault
				if !errors.As(err, &failure) || failure.Code != FaultPreparation || created {
					t.Fatalf("required resource ignored: created=%v err=%v", created, err)
				}
			}
		})
	}
}

// TestChatImageInputBudgetAndFallback 捕获原生图片读取不受限或文本回退仍读取附件的错误。
func TestChatImageInputBudgetAndFallback(t *testing.T) {
	for _, images := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "native image budget"}[images], func(t *testing.T) {
			agent := newFixtureAgent()
			agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{PromptCapabilities: sdk.PromptCapabilities{Image: images}}}, nil
			}
			image := filepath.Join(t.TempDir(), "large.png")
			if images {
				file, err := os.Create(image)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate((20 << 20) + 1); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var prompted bool
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				prompted = true
				if len(request.Prompt) != 1 || !strings.Contains(request.Prompt[0].Text.Text, "large.png") {
					t.Errorf("fallback file reference lost: %+v", request.Prompt)
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			fixture := newConnectionForAgent(t, agent)
			session, err := fixture.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "inspect", Attachments: []Attachment{{Name: "large.png", Path: image, MIMEType: "image/png"}}}}, &recordingSink{})
			if images {
				if result.Fault == nil || result.Fault.Code != FaultLimit || prompted {
					t.Fatalf("oversized image reached native prompt: %+v prompted=%v", result, prompted)
				}
			} else if result.Status != StatusCompleted || !prompted {
				t.Fatalf("text fallback read missing native file: %+v", result)
			}
		})
	}
}

// TestChatPromptUsageKeepsThoughtAndCache 捕获公开推理与缓存用量在Chat边界被丢失的错误。
func TestChatPromptUsageKeepsThoughtAndCache(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: 13, OutputTokens: 7, CachedReadTokens: sdk.Ptr(5), CachedWriteTokens: sdk.Ptr(3), ThoughtTokens: sdk.Ptr(2)}}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, &recordingSink{})
	if result.Status != StatusCompleted || !result.Usage.Known || result.Usage.InputTokens != 13 || result.Usage.OutputTokens != 7 || result.Usage.CachedReadTokens != 5 || result.Usage.CachedWriteTokens != 3 || result.Usage.ThoughtTokens != 2 {
		t.Fatalf("public usage lost: %+v", result)
	}
}

// TestChatRejectsNegativeThoughtUsage 捕获非法推理token作为成功用量交付的错误。
func TestChatRejectsNegativeThoughtUsage(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{ThoughtTokens: sdk.Ptr(-1)}}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, &recordingSink{})
	if result.Fault == nil || result.Fault.Code != FaultProtocol || result.Status != StatusFailed {
		t.Fatalf("negative thought usage accepted: %+v", result)
	}
}

// TestChatToolUpdatesKeepEmptyReplacements 捕获显式清空与省略字段被合并、旧工具内容无法清除的错误。
func TestChatToolUpdatesKeepEmptyReplacements(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		updates := []sdk.SessionUpdate{
			sdk.StartToolCall("tool", "Read file", sdk.WithStartContent([]sdk.ToolCallContent{sdk.ToolContent(sdk.TextBlock("previous"))}), sdk.WithStartLocations([]sdk.ToolCallLocation{{Path: "previous.go"}})),
			sdk.UpdateToolCall("tool", sdk.WithUpdateContent([]sdk.ToolCallContent{}), sdk.WithUpdateLocations([]sdk.ToolCallLocation{})),
			sdk.UpdateToolCall("tool", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted)),
		}
		for _, update := range updates {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	fixture := newConnectionForAgent(t, agent)
	session, err := fixture.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "work"}}, sink)
	if result.Status != StatusCompleted {
		t.Fatal(result)
	}
	var tools []ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tools = append(tools, *event.Tool)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("tool lifecycle lost: %+v", tools)
	}
	if tools[0].Update || len(tools[0].Content) != 1 || len(tools[0].Locations) != 1 {
		t.Fatalf("initial contents lost: %+v", tools[0])
	}
	if !tools[1].Update || tools[1].Content == nil || len(tools[1].Content) != 0 || tools[1].Locations == nil || len(tools[1].Locations) != 0 {
		t.Fatalf("explicit replacements lost: %+v", tools[1])
	}
	if !tools[2].Update || tools[2].Content != nil || tools[2].Locations != nil || tools[2].Status != "completed" {
		t.Fatalf("omitted replacements changed: %+v", tools[2])
	}
}

// TestNativeIdentityKeepsSkillSyntaxAndMaterializedInstructions 验证用户Profile仍使用基础CLI语法，原生系统指令不重复注入正文。
func TestNativeIdentityKeepsSkillSyntaxAndMaterializedInstructions(t *testing.T) {
	for base, prefix := range map[string]string{"codex": "$", "kimi": "/skill:", "pi": "/skill:", "claude": ""} {
		t.Run(base, func(t *testing.T) {
			agent := newFixtureAgent()
			prompts := 0
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				text := request.Prompt[0].Text.Text
				if !strings.Contains(text, "selected skills: "+prefix+"review") || strings.Contains(text, "MATERIALIZED_SYSTEM") {
					t.Errorf("native syntax or duplicate system instructions: %q", text)
				}
				if (prompts == 0) != strings.Contains(text, "MAINTENANCE_INSTRUCTION") {
					t.Errorf("static prefix replay: prompt=%d text=%q", prompts, text)
				}
				prompts++
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			nativeDriver, err := newConnectionFixture(ConnectionConfig{ID: "custom-profile", NativeID: base, MaterializedInstructions: []string{"system-0"}, Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return agent, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer nativeDriver.Close(context.Background())
			request := openRequest(t)
			request.Contributions.Instructions = []Instruction{{Name: "system-0", Text: "MATERIALIZED_SYSTEM"}, {Name: "maintenance", Text: "MAINTENANCE_INSTRUCTION"}}
			session, err := nativeDriver.Open(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "current task", SelectedSkills: []string{"review"}}}, &recordingSink{})
				if result.Status != StatusCompleted {
					t.Fatalf("prompt=%+v", result)
				}
			}
		})
	}
}

// TestPermissionModeRemainsConfirmedAfterLaterReceipts 防止模型或推理回执撤销权限后仍开始 Prompt 并报告原请求模式。
func TestPermissionModeRemainsConfirmedAfterLaterReceipts(t *testing.T) {
	for _, changedBy := range []string{"", "model", "effort"} {
		name := changedBy
		if name == "" {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "default", "default", "full"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "old", "old", "requested"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "effort", "low", "low", "high"),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(_ context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				for _, option := range options {
					if option.Select.Id == request.ValueId.ConfigId {
						option.Select.CurrentValue = request.ValueId.Value
					}
				}
				if string(request.ValueId.ConfigId) == changedBy {
					options[0].Select.CurrentValue = "default"
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newConnectionForAgent(t, a)
			request := openRequest(t)
			request.Profile.NativeMode, request.Profile.Model, request.Profile.Reasoning = "full", "requested", "high"
			s, err := d.Open(context.Background(), request)
			if changedBy != "" {
				var fault *Fault
				if !errors.As(err, &fault) || fault.Code != FaultProtocol {
					t.Fatalf("reverted permission was not rejected as protocol fault: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "input"}}, &recordingSink{})
			if result.Configuration.ActualMode != "full" {
				t.Fatalf("actual mode=%q", result.Configuration.ActualMode)
			}
		})
	}
}

// TestConfigurationSelectionKeepsModelThroughReasoningReceipt 捕获推理写入回执撤销已选择模型的错误。
func TestConfigurationSelectionKeepsModelThroughReasoningReceipt(t *testing.T) {
	for _, revert := range []bool{false, true} {
		t.Run(map[bool]string{false: "model remains selected", true: "reasoning reverts selected model"}[revert], func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategory("speed"), "fast", "false", "false", "true"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "old", "old", "requested"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "effort", "low", "low", "high"),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				for _, option := range options {
					if option.Select.Id == r.ValueId.ConfigId {
						option.Select.CurrentValue = r.ValueId.Value
					}
				}
				if revert && r.ValueId.ConfigId == "effort" {
					options[1].Select.CurrentValue = "old"
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newConnectionForAgent(t, a)
			r := openRequest(t)
			r.Profile.Model = "requested"
			r.Profile.Reasoning = "high"
			s, err := d.Open(context.Background(), r)
			if revert {
				if err == nil {
					t.Fatal("reasoning silently reverted selected model")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "input"}}, &recordingSink{})
			if result.Configuration.ActualModel != "requested" || result.Configuration.ActualReasoning != "high" || options[0].Select.CurrentValue != "false" {
				t.Fatalf("configuration=%+v", result.Configuration)
			}
		})
	}
}

// TestConfigurationSelectionUsesAdvertisedCategory 防止公共选择器按配置 ID 猜测语义或忽略标准模型类别。
func TestConfigurationSelectionUsesAdvertisedCategory(t *testing.T) {
	for _, tt := range []struct {
		// name 描述配置目录中的身份与类别关系。
		name string
		// options 是厂商公布的完整标准目录。
		options []sdk.SessionConfigOption
		// selected 是类别明确声明的真实模型配置身份。
		selected string
	}{
		{"opaque model id", []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryModel, "reasoning_effort", "old", "old", "requested")}, "reasoning_effort"},
		{"model id with another category", []sdk.SessionConfigOption{
			selectionOption(sdk.SessionConfigOptionCategory("custom"), "model", "off", "off", "on"),
			selectionOption(sdk.SessionConfigOptionCategoryModel, "provider-model", "old", "old", "requested"),
		}, "provider-model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: tt.options}, nil
			}
			var selected string
			a.configure = func(_ context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				selected = string(request.ValueId.ConfigId)
				for _, option := range tt.options {
					if option.Select.Id == request.ValueId.ConfigId {
						option.Select.CurrentValue = request.ValueId.Value
					}
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: tt.options}, nil
			}
			d := newConnectionForAgent(t, a)
			request := openRequest(t)
			request.Profile.Model = "requested"
			s, err := d.Open(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(context.Background())
			result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "input"}}, &recordingSink{})
			if selected != tt.selected || result.Configuration.ActualModel != "requested" {
				t.Fatalf("selected=%q configuration=%+v", selected, result.Configuration)
			}
		})
	}
}

// TestPermissionModeUsesAdvertisedMenuAndStrictLegacyFallback 防止模式候选和旧版兼容绕过真实权限目录。
func TestPermissionModeUsesAdvertisedMenuAndStrictLegacyFallback(t *testing.T) {
	for _, tt := range []struct {
		name     string            // name 描述原生模式目录。
		modes    []sdk.SessionMode // modes 是当前原生明确声明的目录。
		fallback string            // fallback 是唯一旧模式兼容值。
		want     string            // want 是允许实际选择的模式，空值表示必须拒绝。
	}{{"Qoder fallback", []sdk.SessionMode{{Id: "default"}, {Id: "bypassPermissions"}}, "", "bypassPermissions"}, {"legacy single default", []sdk.SessionMode{{Id: "default"}}, "default", "default"}, {"modern cannot silently use default", []sdk.SessionMode{{Id: "default"}, {Id: "auto"}}, "default", ""}} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", Modes: &sdk.SessionModeState{CurrentModeId: "default", AvailableModes: tt.modes}}, nil
			}
			selected := ""
			a.configureMode = func(_ context.Context, r sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
				selected = string(r.ModeId)
				return sdk.SetSessionModeResponse{}, nil
			}
			d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", ModeCandidates: []string{"yolo", "bypassPermissions"}, SingleModeFallback: tt.fallback, Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			s, err := d.Open(context.Background(), openRequest(t))
			if tt.want == "" {
				if err == nil || selected != "" {
					t.Fatalf("unsupported mode accepted: %q %v", selected, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "input"}}, &recordingSink{})
			if selected != tt.want || r.Configuration.ActualMode != tt.want {
				t.Fatalf("selected=%q actual=%q", selected, r.Configuration.ActualMode)
			}
		})
	}
}

// TestLastAssistantCompletionRequiresFinalActivity 捕获工具前正文、取消和缺失身份被提升为最终答案的错误。
func TestLastAssistantCompletionRequiresFinalActivity(t *testing.T) {
	for _, tt := range []struct {
		name     string         // name 是原生活动顺序场景。
		tail     string         // tail 是正文之后到达的活动类型。
		stop     sdk.StopReason // stop 是原生结束原因。
		verified bool           // verified 是是否允许验证最后正文的独立预期。
	}{{"last assistant", "", sdk.StopReasonEndTurn, true}, {"length preserves partial", "", sdk.StopReasonMaxTokens, true}, {"tool after text", "tool", sdk.StopReasonEndTurn, false}, {"tool then empty boundary", "tool-complete", sdk.StopReasonEndTurn, false}, {"known commentary stays nonfinal", "commentary-unknown", sdk.StopReasonEndTurn, false}, {"thought after text", "thought", sdk.StopReasonEndTurn, false}, {"cancelled", "", sdk.StopReasonCancelled, false}, {"refusal", "", sdk.StopReasonRefusal, false}, {"missing identity", "missing", sdk.StopReasonEndTurn, false}, {"error", "error", sdk.StopReasonEndTurn, false}} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				id := "answer"
				chunk := &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("native answer")}
				if tt.tail == "missing" {
					chunk.MessageId = nil
				}
				if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: chunk}}); err != nil {
					return sdk.PromptResponse{}, err
				}
				var tail sdk.SessionUpdate
				if tt.tail == "tool" || tt.tail == "tool-complete" {
					tail.ToolCall = &sdk.SessionUpdateToolCall{ToolCallId: "tool", Title: "run", Status: sdk.ToolCallStatusCompleted}
				}
				if tt.tail == "thought" {
					tail.AgentThoughtChunk = &sdk.SessionUpdateAgentThoughtChunk{Content: sdk.TextBlock("still thinking")}
				}
				if tail.ToolCall != nil || tail.AgentThoughtChunk != nil {
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: tail}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				if tt.tail == "tool-complete" || tt.tail == "commentary-unknown" {
					phase := "unknown"
					if tt.tail == "commentary-unknown" {
						phase = "commentary"
					}
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock(""), Meta: map[string]any{"acp-go/message": map[string]any{"phase": phase, "complete": false, "source": "native_message"}}}}}); err != nil {
						return sdk.PromptResponse{}, err
					}
					if tt.tail == "commentary-unknown" {
						if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("late process")}}}); err != nil {
							return sdk.PromptResponse{}, err
						}
					}
				}
				if tt.tail == "error" {
					return sdk.PromptResponse{}, sdk.NewInternalError(nil)
				}
				return sdk.PromptResponse{StopReason: tt.stop}, nil
			}
			d, err := newConnectionFixture(ConnectionConfig{ID: "generic", CompletionPolicy: CompletionLastAssistant, Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordingSink{}
			s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "input"}}, sink)
			verified := false
			for _, event := range sink.snapshot() {
				if event.Message != nil && event.Message.CompletionVerified {
					verified = true
					if event.Message.Phase != PhaseUnknown || !event.Message.Complete || event.Message.MessageID != "answer" {
						t.Fatalf("invented native phase: %+v", event.Message)
					}
				}
			}
			if verified != tt.verified {
				t.Fatalf("completion proof=%v, expected %v", verified, tt.verified)
			}
		})
	}
}

// TestWorkspaceAuthorizationNegotiatesNativeDirectories 验证宿主授权目录不因原生缺少扩目录能力而阻断创建或恢复。
func TestWorkspaceAuthorizationNegotiatesNativeDirectories(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		for _, method := range []string{"new", "resume", "load"} {
			t.Run(method+map[bool]string{false: " without native directories", true: " with native directories"}[advertised], func(t *testing.T) {
				a := &restoreAgent{fixtureAgent: newFixtureAgent()}
				r := openRequest(t)
				r.Workspace.Directories = []string{t.TempDir()}
				a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
					capabilities := sdk.AgentCapabilities{LoadSession: true}
					if advertised {
						capabilities.SessionCapabilities.AdditionalDirectories = &sdk.SessionAdditionalDirectoriesCapabilities{}
					}
					if method == "resume" {
						capabilities.SessionCapabilities.Resume = &sdk.SessionResumeCapabilities{}
					}
					return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: capabilities}, nil
				}
				calls := 0
				check := func(cwd string, directories []string) {
					t.Helper()
					calls++
					var want []string
					if advertised {
						want = r.Workspace.Directories
					}
					if cwd != r.Workspace.Root || !reflect.DeepEqual(directories, want) {
						t.Errorf("workspace negotiation: cwd=%q directories=%q, want %q", cwd, directories, want)
					}
				}
				a.newSession = func(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
					check(request.Cwd, request.AdditionalDirectories)
					return sdk.NewSessionResponse{SessionId: "native-session"}, nil
				}
				a.resume = func(_ context.Context, request sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
					check(request.Cwd, request.AdditionalDirectories)
					return sdk.ResumeSessionResponse{}, nil
				}
				a.load = func(_ context.Context, request sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
					check(request.Cwd, request.AdditionalDirectories)
					return sdk.LoadSessionResponse{}, nil
				}
				if method != "new" {
					r.Session = SessionDirective{Mode: SessionResume, NativeHandle: "native-session"}
				}
				d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close(context.Background())
				if _, err = d.Open(context.Background(), r); err != nil {
					t.Fatalf("host workspace blocked native %s: %v", method, err)
				}
				if calls != 1 || len(r.Workspace.Directories) != 1 {
					t.Fatalf("native calls=%d, host workspace=%+v", calls, r.Workspace)
				}
			})
		}
	}
}

// restoreAgent 使用公开恢复方法验证历史与本轮事件的真实隔离。
type restoreAgent struct {
	*fixtureAgent                                                                                    // fixtureAgent 提供公开初始化、Prompt 与资源清理边界。
	resume        func(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) // resume 控制无历史恢复。
	load          func(context.Context, sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error)     // load 控制会回放历史的恢复。
}

// ResumeSession 执行本次测试的公开无历史恢复。
func (a *restoreAgent) ResumeSession(ctx context.Context, request sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	if a.resume != nil {
		return a.resume(ctx, request)
	}
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

// LoadSession 执行本次测试的公开历史恢复。
func (a *restoreAgent) LoadSession(ctx context.Context, request sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
	if a.load != nil {
		return a.load(ctx, request)
	}
	return sdk.LoadSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionLoad)
}

// TestSessionKeepsFirstPrefixAndSeedOutOfSecondPrompt 防止连续回合重复注入静态指令或既有历史。
func TestSessionKeepsFirstPrefixAndSeedOutOfSecondPrompt(t *testing.T) {
	a := newFixtureAgent()
	var prompts []string
	a.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		prompts = append(prompts, request.Prompt[0].Text.Text)
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	d := newConnectionForAgent(t, a)
	r := openRequest(t)
	r.Contributions.Instructions = []Instruction{{Name: "profile", Text: "stable profile"}}
	r.Seed = []SeedMessage{{Role: "user", Text: "earlier user"}, {Role: "assistant", Text: "earlier answer"}}
	s, err := d.Open(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: text}}, &recordingSink{})
		if result.Status != StatusCompleted {
			t.Fatalf("%s: %#v", text, result)
		}
	}
	if len(prompts) != 2 || prompts[1] != "second" {
		t.Fatalf("continued prompt contains context: %#v", prompts)
	}
	for _, text := range []string{"stable profile", "earlier user", "earlier answer", "first"} {
		if !strings.Contains(prompts[0], text) {
			t.Fatalf("first prompt lost %q: %q", text, prompts[0])
		}
	}
}

// TestColdRestoreNegotiatesResumeBeforeLoadAndDropsReplay 防止把恢复历史当成新回合输出或隐式新建原生聊天。
func TestColdRestoreNegotiatesResumeBeforeLoadAndDropsReplay(t *testing.T) {
	for _, method := range []string{"resume", "load", "unsupported"} {
		t.Run(method, func(t *testing.T) {
			a := &restoreAgent{fixtureAgent: newFixtureAgent()}
			called := ""
			a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				capabilities := sdk.AgentCapabilities{LoadSession: method != "unsupported"}
				if method == "resume" {
					capabilities.SessionCapabilities.Resume = &sdk.SessionResumeCapabilities{}
				}
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: capabilities}, nil
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				t.Fatal("cold restore created a fresh native session")
				return sdk.NewSessionResponse{}, nil
			}
			a.resume = func(_ context.Context, request sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
				called = "resume"
				if request.SessionId != "persisted-native" {
					t.Fatalf("native id %q", request.SessionId)
				}
				return sdk.ResumeSessionResponse{}, nil
			}
			a.load = func(ctx context.Context, request sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
				called = "load"
				id := "old-final"
				err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("old answer")}}})
				return sdk.LoadSessionResponse{}, err
			}
			a.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				if request.SessionId != "persisted-native" || len(request.Prompt) != 1 || request.Prompt[0].Text.Text != "next" {
					t.Fatalf("restore input %#v", request)
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			r := openRequest(t)
			r.Session = SessionDirective{Mode: SessionResume, NativeHandle: "persisted-native"}
			r.Contributions.Instructions = []Instruction{{Name: "profile", Text: "must not be resent"}}
			s, err := d.Open(context.Background(), r)
			if method == "unsupported" {
				if err == nil {
					t.Fatal("unadvertised native restore accepted")
				}
				if called != "" {
					t.Fatalf("unsupported restore called %s", called)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordingSink{}
			result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "next"}}, sink)
			if result.Status != StatusCompleted || !result.NativeSession.Resumable || called != method {
				t.Fatalf("result %#v method %s", result, called)
			}
			for _, event := range sink.snapshot() {
				if event.Message != nil {
					t.Fatalf("restore replay entered current turn: %#v", event)
				}
			}
		})
	}
}

// pendingInputState 提供宿主已经拥有的外部人工等待事实。
type pendingInputState struct {
	pending atomic.Bool   // pending 表示真实问题尚未解决。
	changed chan struct{} // changed 仅合并等待状态变化。
}

func (s *pendingInputState) Pending() bool            { return s.pending.Load() }
func (s *pendingInputState) Changed() <-chan struct{} { return s.changed }

// TestHostInputWaitPausesAndRestartsInactivityBudget 防止内置工具的人工问题被误判为CLI静默，并保证回答后仍能发现真正静默。
func TestHostInputWaitPausesAndRestartsInactivityBudget(t *testing.T) {
	state := &pendingInputState{changed: make(chan struct{}, 1)}
	state.pending.Store(true)
	a := newFixtureAgent()
	entered := make(chan struct{})
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		close(entered)
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newConnectionForAgent(t, a)
	request := openRequest(t)
	connection, err := d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(context.Background())
	done := make(chan TurnResult, 1)
	go func() {
		done <- connection.Prompt(context.Background(), TurnRequest{Input: Input{Text: "wait for answer"}, Limits: Limits{InactivityTimeout: 20 * time.Millisecond}, UserInputState: state}, &recordingSink{})
	}()
	<-entered
	select {
	case result := <-done:
		t.Fatalf("external input wait timed out: %#v", result)
	case <-time.After(60 * time.Millisecond):
	}
	state.pending.Store(false)
	state.changed <- struct{}{}
	select {
	case result := <-done:
		if result.Fault == nil || result.Fault.Code != FaultInactivityTimeout {
			t.Fatalf("real inactivity was lost: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("answer did not restart inactivity timer")
	}
}

// TestPromptContextStrategyIsNewSessionOnly 防止宿主原生文件策略失效，或恢复时再次物化并注入静态指令。
func TestPromptContextStrategyIsNewSessionOnly(t *testing.T) {
	fresh := newFixtureAgent()
	restored := &restoreAgent{fixtureAgent: newFixtureAgent()}
	restored.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{SessionCapabilities: sdk.SessionCapabilities{Resume: &sdk.SessionResumeCapabilities{}}}}, nil
	}
	restored.resume = func(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
		return sdk.ResumeSessionResponse{}, nil
	}
	var prompts []string
	capture := func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		prompts = append(prompts, request.Prompt[0].Text.Text)
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	fresh.prompt, restored.prompt = capture, capture
	var prepared atomic.Int32
	d, err := newConnectionFixture(ConnectionConfig{ID: "fixture", Factory: func(_ context.Context, request OpenRequest) (sdk.Agent, error) {
		if request.Session.Mode == SessionNew {
			return fresh, nil
		}
		return restored, nil
	}, PromptContext: func(request OpenRequest) (string, error) {
		prepared.Add(1)
		return "native-file context\n\n", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	request := openRequest(t)
	request.Contributions.Instructions = []Instruction{{Name: "profile", Text: "default prefix must not be injected"}}
	request.Seed = []SeedMessage{{Role: "assistant", Text: "seeded answer"}}
	session, err := d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: text}}, &recordingSink{})
		if result.Status != StatusCompleted {
			t.Fatalf("hot prompt: %#v", result)
		}
	}
	request.Session = SessionDirective{Mode: SessionResume, NativeHandle: "persisted-native"}
	request.Seed = nil
	session, err = d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), TurnRequest{Input: Input{Text: "third"}}, &recordingSink{})
	if result.Status != StatusCompleted || prepared.Load() != 1 || len(prompts) != 3 || prompts[1] != "second" || prompts[2] != "third" {
		t.Fatalf("context strategy preparation=%d prompts=%#v result=%#v", prepared.Load(), prompts, result)
	}
	if !strings.Contains(prompts[0], "native-file context") || !strings.Contains(prompts[0], "seeded answer") || strings.Contains(prompts[0], "default prefix must not be injected") {
		t.Fatalf("host strategy prefix: %q", prompts[0])
	}
}

// TestCancellationDuringInteractionDrainKeepsCancelledState 防止原生已经响应但人工端口尚未释放时丢失本轮取消。
func TestCancellationDuringInteractionDrainKeepsCancelledState(t *testing.T) {
	agent := newFixtureAgent()
	entered := make(chan struct{})
	permissionDone := make(chan struct{})
	sink := &recordingSink{interact: func(ctx context.Context, _ InteractionRequest) (InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		return InteractionResponse{}, ctx.Err()
	}}
	agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		go func() {
			defer close(permissionDone)
			_, _ = agent.connection.Load().RequestPermission(context.Background(), sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
		}()
		<-entered
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	d := newConnectionForAgent(t, agent)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan TurnResult, 1)
	go func() { done <- session.Prompt(ctx, TurnRequest{Input: Input{Text: "hello"}}, sink) }()
	<-entered
	select {
	case result := <-done:
		t.Fatalf("prompt did not drain interaction: %#v", result)
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case result := <-done:
		if result.Status != StatusCancelled || result.Fault == nil || result.Fault.Code != FaultCancelled {
			t.Fatalf("cancelled drain result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled interaction drain remained active")
	}
	<-permissionDone
}

func selectionOption(category sdk.SessionConfigOptionCategory, id, current string, values ...string) sdk.SessionConfigOption {
	choices := make(sdk.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, value := range values {
		choices = append(choices, sdk.SessionConfigSelectOption{Value: sdk.SessionConfigValueId(value), Name: value})
	}
	return sdk.SessionConfigOption{Select: &sdk.SessionConfigOptionSelect{Id: sdk.SessionConfigId(id), Name: id, Category: &category, CurrentValue: sdk.SessionConfigValueId(current), Options: sdk.SessionConfigSelectOptions{Ungrouped: &choices}}}
}

// TestGenerationConfigurationUsesConfirmedNativeChoices 防止工具授权冒充原生权限或推理降级被虚报为关闭。
func TestGenerationConfigurationUsesConfirmedNativeChoices(t *testing.T) {
	for _, sample := range []struct {
		name     string   // name 标识当前推理情景。
		values   []string // values 是模型实际公布的推理档位。
		explicit string   // explicit 是用户明确覆盖的档位。
		want     string   // want 是确认后应生效的档位。
		disabled bool     // disabled 表示该档位真正关闭推理。
		fallback string   // fallback 是无法关闭时的真实降级说明。
	}{
		{name: "native off", values: []string{"medium", "off", "low"}, want: "off", disabled: true},
		{name: "lowest supported", values: []string{"high", "medium", "low"}, want: "low", fallback: "thinking_off_unsupported: selected low"},
		{name: "explicit wins", values: []string{"off", "high"}, explicit: "high", want: "high"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategoryMode, "native-mode", "agent", "agent", "agent-full-access"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "default", "selected"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "medium", sample.values...),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			var selected []string
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				selected = append(selected, string(r.ValueId.ConfigId)+"="+string(r.ValueId.Value))
				for _, option := range options {
					if option.Select.Id == r.ValueId.ConfigId {
						option.Select.CurrentValue = r.ValueId.Value
					}
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newConnectionForAgent(t, a)
			r := openRequest(t)
			no := false
			r.Profile = Profile{Model: "selected", NativeMode: "agent-full-access", Thinking: &no, Reasoning: sample.explicit}
			s, err := d.Open(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "answer"}}, &recordingSink{})
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := []string{"native-mode=agent-full-access", "model=selected", "reasoning=" + sample.want}
			if !reflect.DeepEqual(selected, want) {
				t.Fatalf("native configuration = %#v, want %#v", selected, want)
			}
			c := result.Configuration
			if c.ActualMode != "agent-full-access" || c.ActualModel != "selected" || c.ActualReasoning != sample.want || c.ThinkingDisabled != sample.disabled || c.ReasoningFallback != sample.fallback {
				t.Fatalf("unconfirmed diagnostics: %+v", c)
			}
		})
	}
}

// TestNativeModeMustBeAdvertisedAndConfirmed 防止 full-access 请求被静默忽略或在回执不符时继续调用模型。
func TestNativeModeMustBeAdvertisedAndConfirmed(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "unconfirmed"}[advertised], func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{}
			if advertised {
				options = append(options, selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "agent", "agent-full-access"))
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newConnectionForAgent(t, a)
			r := openRequest(t)
			r.Profile.NativeMode = "agent-full-access"
			s, err := d.Open(context.Background(), r)
			if s != nil {
				_ = s.Close(context.Background())
			}
			code := FaultUnsupported
			if advertised {
				code = FaultProtocol
			}
			var f *Fault
			if !errors.As(err, &f) || f.Code != code {
				t.Fatalf("mode selection error = %v, want %s", err, code)
			}
		})
	}
}

// TestPromptUsagePreservesUnknownAndCacheComponents 防止未知用量变成零值或缓存被丢失及重复计入。
func TestPromptUsagePreservesUnknownAndCacheComponents(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "known"}[known], func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 17}}}); err != nil {
					return sdk.PromptResponse{}, err
				}
				r := sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}
				if known {
					read, write := 7, 2
					r.Usage = &sdk.Usage{InputTokens: 11, OutputTokens: 5, CachedReadTokens: &read, CachedWriteTokens: &write}
				}
				return r, nil
			}
			d := newConnectionForAgent(t, a)
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "answer"}}, &recordingSink{})
			_ = s.Close(context.Background())
			if r.Usage.Known != known || !r.Usage.ContextKnown || r.Usage.ContextTokens != 17 {
				t.Fatalf("usage certainty = %+v", r.Usage)
			}
			if known && (r.Usage.InputTokens != 11 || r.Usage.OutputTokens != 5 || r.Usage.CachedReadTokens != 7 || r.Usage.CachedWriteTokens != 2) {
				t.Fatalf("cache usage changed = %+v", r.Usage)
			}
		})
	}
}

// TestPromptInactivityIsDistinctFromCancellation 防止静默原生调用无限占用资源或超时被误报为用户取消。
func TestPromptInactivityIsDistinctFromCancellation(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := s.Prompt(ctx, TurnRequest{Input: Input{Text: "answer"}, Limits: Limits{InactivityTimeout: 20 * time.Millisecond}}, &recordingSink{})
	_ = s.Close(context.Background())
	if r.Status != StatusFailed || r.Fault == nil || r.Fault.Code != FaultInactivityTimeout {
		t.Fatalf("inactivity result = %+v", r)
	}
}

// TestNativeActivityAndApprovalPauseInactivity 防止原生更新未刷新计时器或审批等待被无活动监视器中断。
func TestNativeActivityAndApprovalPauseInactivity(t *testing.T) {
	for _, approval := range []bool{false, true} {
		t.Run(map[bool]string{false: "native progress", true: "approval wait"}[approval], func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				if approval {
					_, err := a.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "t", Title: sdk.Ptr("Read")}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
					return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
				}
				for range 5 {
					select {
					case <-time.After(15 * time.Millisecond):
					case <-ctx.Done():
						return sdk.PromptResponse{}, ctx.Err()
					}
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{}}}}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			d := newConnectionForAgent(t, a)
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordingSink{interact: func(ctx context.Context, r InteractionRequest) (InteractionResponse, error) {
				select {
				case <-time.After(80 * time.Millisecond):
					return InteractionResponse{Invocation: r.Invocation, ID: r.ID, Action: InteractionAccept, OptionID: r.Approval.Options[0].ID}, nil
				case <-ctx.Done():
					return InteractionResponse{}, ctx.Err()
				}
			}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			r := s.Prompt(ctx, TurnRequest{Input: Input{Text: "answer"}, Limits: Limits{InactivityTimeout: 35 * time.Millisecond}}, sink)
			_ = s.Close(context.Background())
			if r.Status != StatusCompleted || r.Fault != nil {
				t.Fatalf("healthy prompt interrupted = %+v", r)
			}
		})
	}
}

// TestNegativePromptUsageFailsInsteadOfReportingCost 防止无效上游统计进入宿主计费与诊断。
func TestNegativePromptUsageFailsInsteadOfReportingCost(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: -1}}, nil
	}
	d := newConnectionForAgent(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "answer"}}, &recordingSink{})
	_ = s.Close(context.Background())
	if r.Fault == nil || r.Fault.Code != FaultProtocol {
		t.Fatalf("invalid usage accepted: %+v", r)
	}
}

// TestACPInitialContextSnapshotIsDeterministic 捕获工厂或调用方改写贡献影响已打开会话、资源顺序影响提示词的错误。
func TestACPInitialContextSnapshotIsDeterministic(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "ally")
	var previous string
	for _, reverse := range []bool{false, true} {
		agent := newFixtureAgent()
		request := openRequest(t)
		request.Contributions = Contributions{Instructions: []Instruction{{Name: "maintenance", Version: "v1", Text: "fixed instructions"}}, Skills: []Skill{{Name: "ally", Version: "v2", ResourceID: "ally"}, {Name: "optional", ResourceID: "optional", Optional: true}}}
		request.Resources = []ResourceBinding{{ID: "ally", Path: skillDir}, {ID: "optional", Path: filepath.Join(skillDir, "optional")}}
		if reverse {
			request.Resources[0], request.Resources[1] = request.Resources[1], request.Resources[0]
		}
		var prompt string
		agent.prompt = func(_ context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
			prompt = p.Prompt[0].Text.Text
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
		d, err := newConnectionFixture(ConnectionConfig{ID: "snapshot", Factory: func(_ context.Context, r OpenRequest) (sdk.Agent, error) {
			r.Contributions.Instructions[0].Text = "factory mutated instructions"
			r.Contributions.Skills[0].Name = "factory mutated skill"
			r.Resources[0].Path = "/factory-mutated"
			return agent, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Close(context.Background()) })
		s, err := d.Open(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		request.Contributions.Instructions[0].Text = "caller mutated instructions"
		request.Contributions.Skills[0].Name = "caller mutated skill"
		request.Resources[0].Path = "/caller-mutated"
		result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: "task body"}}, &recordingSink{})
		if result.Status != StatusCompleted || strings.Contains(prompt, "mutated") || strings.Count(prompt, "fixed instructions") != 1 || strings.Count(prompt, filepath.Join(skillDir, "SKILL.md")) != 1 || !strings.HasSuffix(prompt, "task body") {
			t.Fatalf("context snapshot changed: result=%+v prompt=%q", result, prompt)
		}
		if reverse && prompt != previous {
			t.Fatalf("resource binding order changed prompt:\n%q\n%q", previous, prompt)
		}
		previous = prompt
	}
}

// TestACPEmptyContributionsKeepPromptBytes 捕获纯生成被加入静态包装或空可选技能说明的错误。
func TestACPEmptyContributionsKeepPromptBytes(t *testing.T) {
	for _, optional := range []bool{false, true} {
		agent := newFixtureAgent()
		want := "  original task\n中文\n\t"
		agent.prompt = func(_ context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
			if len(p.Prompt) != 1 || p.Prompt[0].Text == nil || p.Prompt[0].Text.Text != want {
				t.Errorf("prompt bytes changed: %+v", p.Prompt)
			}
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
		d := newConnectionForAgent(t, agent)
		request := openRequest(t)
		if optional {
			request.Contributions.Skills = []Skill{{Name: "missing optional", ResourceID: "missing", Optional: true}}
		}
		s, err := d.Open(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		result := s.Prompt(context.Background(), TurnRequest{Input: Input{Text: want}}, &recordingSink{})
		if result.Status != StatusCompleted {
			t.Fatal(result)
		}
	}
}

// TestACPContributionValidationPrecedesNativeLaunch 捕获缺失、冲突、相对或超限资源被静默使用的错误。
func TestACPContributionValidationPrecedesNativeLaunch(t *testing.T) {
	for _, sample := range []struct {
		name   string             // name 标识无效贡献场景。
		change func(*OpenRequest) // change 只构造当前边界的无效请求。
		code   FaultCode          // code 是预期错误类别。
	}{
		{name: "missing binding", change: func(r *OpenRequest) { r.Resources = nil }, code: FaultPreparation},
		{name: "relative path", change: func(r *OpenRequest) { r.Resources[0].Path = "relative/ally" }, code: FaultInvalidSpec},
		{name: "empty path", change: func(r *OpenRequest) { r.Resources[0].Path = "" }, code: FaultInvalidSpec},
		{name: "conflicting binding", change: func(r *OpenRequest) {
			r.Resources = append(r.Resources, ResourceBinding{ID: "ally", Path: "/different"})
		}, code: FaultPreparation},
		{name: "unrequested binding", change: func(r *OpenRequest) {
			r.Resources = append(r.Resources, ResourceBinding{ID: "unrequested", Path: "/different"})
		}, code: FaultUnsupported},
		{name: "missing identity", change: func(r *OpenRequest) { r.Contributions.Skills[0].ResourceID = "" }, code: FaultInvalidSpec},
		{name: "oversized instruction", change: func(r *OpenRequest) {
			r.Contributions.Instructions = []Instruction{{Name: "too-large", Text: strings.Repeat("x", (1<<20)+1)}}
		}, code: FaultInvalidSpec},
		{name: "oversized aggregate", change: func(r *OpenRequest) {
			r.Contributions.Instructions = []Instruction{{Name: "first", Text: strings.Repeat("x", 600<<10)}, {Name: "second", Text: strings.Repeat("y", 600<<10)}}
		}, code: FaultInvalidSpec},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var prepared atomic.Bool
			d, err := newConnectionFixture(ConnectionConfig{ID: "validation", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) {
				prepared.Store(true)
				return newFixtureAgent(), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close(context.Background()) })
			request := openRequest(t)
			request.Contributions.Skills = []Skill{{Name: "ally", ResourceID: "ally"}}
			request.Resources = []ResourceBinding{{ID: "ally", Path: filepath.Join(t.TempDir(), "installed")}}
			sample.change(&request)
			_, err = d.Open(context.Background(), request)
			var f *Fault
			if !errors.As(err, &f) || f.Code != sample.code || prepared.Load() {
				t.Fatalf("invalid contribution reached native launch: error=%v prepared=%v", err, prepared.Load())
			}
		})
	}
}

// connectionFixture 保存当前测试的启动配置与最近一次真实连接，不参与生产生命周期。
type connectionFixture struct {
	config     ConnectionConfig // config 是本测试传给 Open 的冻结协议配置。
	connection *Connection      // connection 是最近一次 Open 交回的实际资源所有者。
}

func newConnectionFixture(config ConnectionConfig) (*connectionFixture, error) {
	return &connectionFixture{config: config}, nil
}
func (f *connectionFixture) Open(ctx context.Context, request OpenRequest) (*Connection, error) {
	connection, err := Open(ctx, f.config, request)
	if connection != nil {
		f.connection = connection
	}
	return connection, err
}
func (f *connectionFixture) Close(ctx context.Context) error {
	if f.connection != nil {
		return f.connection.Close(ctx)
	}
	return nil
}

// TestOpenRejectsInvalidConnectionConfig 验证空工厂在任何原生资源取得前被拒绝。
func TestOpenRejectsInvalidConnectionConfig(t *testing.T) {
	connection, err := Open(context.Background(), ConnectionConfig{}, OpenRequest{})
	var failure *Fault
	if connection != nil || !errors.As(err, &failure) || failure.Code != FaultInvalidSpec {
		t.Fatalf("invalid connection config accepted: connection=%v error=%v", connection, err)
	}
}

// TestConnectionCloseDeadlineRetainsNativeOwner 验证原生关闭超时后仍能用同一连接重试。
func TestConnectionCloseDeadlineRetainsNativeOwner(t *testing.T) {
	agent := newFixtureAgent()
	agent.closeNative = func(ctx context.Context) error {
		if agent.closes.Load() == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	connection, err := Open(context.Background(), ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return agent, nil }}, openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := connection.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close deadline lost: %v", err)
	}
	if err := connection.Close(context.Background()); err != nil {
		t.Fatalf("retained owner cannot retry: %v", err)
	}
	if agent.closes.Load() != 2 {
		t.Fatalf("native close attempts=%d, want 2", agent.closes.Load())
	}
	if err := connection.Close(context.Background()); err != nil || agent.closes.Load() != 2 {
		t.Fatalf("successful close repeated: attempts=%d error=%v", agent.closes.Load(), err)
	}
}

// TestConnectionRejectsConcurrentPrompt 验证一次连接最多只有一个原生回合，拒绝重入不会取消当前回合。
func TestConnectionRejectsConcurrentPrompt(t *testing.T) {
	agent := newFixtureAgent()
	entered, release := make(chan struct{}), make(chan struct{})
	agent.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		close(entered)
		select {
		case <-release:
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		case <-ctx.Done():
			return sdk.PromptResponse{}, ctx.Err()
		}
	}
	connection, err := Open(context.Background(), ConnectionConfig{ID: "fixture", Factory: func(context.Context, OpenRequest) (sdk.Agent, error) { return agent, nil }}, openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(context.Background())
	done := make(chan TurnResult, 1)
	go func() {
		done <- connection.Prompt(context.Background(), TurnRequest{Input: Input{Text: "first"}}, &recordingSink{})
	}()
	<-entered
	rejected := connection.Prompt(context.Background(), TurnRequest{Input: Input{Text: "second"}}, &recordingSink{})
	if rejected.Fault == nil || rejected.Fault.Code != FaultSessionBusy {
		t.Fatalf("concurrent prompt admitted: %+v", rejected)
	}
	close(release)
	select {
	case result := <-done:
		if result.Status != StatusCompleted {
			t.Fatalf("busy rejection damaged active turn: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("active prompt did not finish")
	}
}

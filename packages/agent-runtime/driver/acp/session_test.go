package acp_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

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
	d := newDriver(t, a)
	r := openRequest(t)
	r.Contributions.Instructions = []contract.Instruction{{Name: "profile", Text: "stable profile"}}
	r.Session.Seed = []contract.SeedMessage{{Role: "user", Text: "earlier user"}, {Role: "assistant", Text: "earlier answer"}}
	s, err := d.Open(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: text}}, &recordingSink{})
		if result.Status != contract.StatusCompleted {
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
			d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			r := openRequest(t)
			r.Session = contract.SessionDirective{Mode: contract.SessionResume, Checkpoint: &contract.SessionCheckpoint{NativeHandle: "persisted-native"}}
			r.Contributions.Instructions = []contract.Instruction{{Name: "profile", Text: "must not be resent"}}
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
			result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "next"}}, sink)
			if result.Status != contract.StatusCompleted || !result.NativeSession.Resumable || called != method {
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
	d := newDriver(t, a)
	request := openRequest(t)
	rt, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{request.Workspace.Root}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	input := contract.ExecutionSpec{Profile: contract.Profile{DriverID: "fixture"}, Input: contract.Input{Text: "wait for answer"}, Workspace: request.Workspace, Policies: request.Policies, Limits: contract.Limits{InactivityTimeout: 20 * time.Millisecond}}
	done := make(chan contract.Result, 1)
	go func() {
		done <- rt.Run(context.Background(), input, contract.RunOptions{UserInputState: state})
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
		if result.Fault == nil || result.Fault.Code != contract.FaultInactivityTimeout {
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
	d, err := driver.New(driver.Config{ID: "fixture", Factory: func(_ context.Context, request contract.OpenRequest) (sdk.Agent, error) {
		if request.Session.Mode == contract.SessionNew {
			return fresh, nil
		}
		return restored, nil
	}, PromptContext: func(request contract.OpenRequest) (string, error) {
		prepared.Add(1)
		return "native-file context\n\n", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	request := openRequest(t)
	request.Contributions.Instructions = []contract.Instruction{{Name: "profile", Text: "default prefix must not be injected"}}
	request.Session.Seed = []contract.SeedMessage{{Role: "assistant", Text: "seeded answer"}}
	session, err := d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: text}}, &recordingSink{})
		if result.Status != contract.StatusCompleted {
			t.Fatalf("hot prompt: %#v", result)
		}
	}
	request.Session = contract.SessionDirective{Mode: contract.SessionResume, Checkpoint: &contract.SessionCheckpoint{NativeHandle: "persisted-native"}}
	session, err = d.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "third"}}, &recordingSink{})
	if result.Status != contract.StatusCompleted || prepared.Load() != 1 || len(prompts) != 3 || prompts[1] != "second" || prompts[2] != "third" {
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
	sink := &recordingSink{interact: func(ctx context.Context, _ contract.InteractionRequest) (contract.InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		return contract.InteractionResponse{}, ctx.Err()
	}}
	agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		go func() {
			defer close(permissionDone)
			_, _ = agent.connection.Load().RequestPermission(context.Background(), sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
		}()
		<-entered
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	d := newDriver(t, agent)
	session, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan contract.DriverResult, 1)
	go func() { done <- session.Prompt(ctx, contract.TurnRequest{Input: contract.Input{Text: "hello"}}, sink) }()
	<-entered
	select {
	case result := <-done:
		t.Fatalf("prompt did not drain interaction: %#v", result)
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case result := <-done:
		if result.Status != contract.StatusCancelled || result.Fault == nil || result.Fault.Code != contract.FaultCancelled {
			t.Fatalf("cancelled drain result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled interaction drain remained active")
	}
	<-permissionDone
}

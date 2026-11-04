package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// nativeIntegrationAgent 通过公开 ACP 接口控制会话准备、交付、通知和退出时序。
type nativeIntegrationAgent struct {
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

func newNativeIntegrationAgent() *nativeIntegrationAgent {
	return &nativeIntegrationAgent{bound: make(chan struct{})}
}

// SetAgentConnection 接收真实acpserver注入的唯一SDK连接。
func (a *nativeIntegrationAgent) SetAgentConnection(connection *sdk.AgentSideConnection) {
	a.connection.Store(connection)
	close(a.bound)
}

// Initialize 完成公开ACP初始化并允许情景覆盖。
func (a *nativeIntegrationAgent) Initialize(ctx context.Context, request sdk.InitializeRequest) (sdk.InitializeResponse, error) {
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
func (a *nativeIntegrationAgent) NewSession(ctx context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	if a.newSession != nil {
		return a.newSession(ctx, request)
	}
	return sdk.NewSessionResponse{SessionId: "native-session", Modes: &sdk.SessionModeState{CurrentModeId: "agent-full-access", AvailableModes: []sdk.SessionMode{{Id: "agent-full-access", Name: "Full access"}}}}, nil
}

// Prompt 执行当前测试指定的原生调用情景。
func (a *nativeIntegrationAgent) Prompt(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
	if a.prompt != nil {
		return a.prompt(ctx, request)
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

// SetSessionConfigOption 验证公开配置请求并返回实际生效目录。
func (a *nativeIntegrationAgent) SetSessionConfigOption(ctx context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	if a.configure != nil {
		return a.configure(ctx, request)
	}
	return sdk.SetSessionConfigOptionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetConfigOption)
}

// Close 记录实际资源释放并返回预设清理结果。
func (a *nativeIntegrationAgent) Close(ctx context.Context) error {
	a.closes.Add(1)
	if a.closeNative != nil {
		return a.closeNative(ctx)
	}
	return a.closeErr
}

// Cancel 由SDK维护请求取消，测试Agent不再持有第二套取消状态。
func (a *nativeIntegrationAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

// Authenticate 拒绝测试未声明的认证操作。
func (a *nativeIntegrationAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodAuthenticate)
}

// Logout 拒绝测试未声明的退出登录操作。
func (a *nativeIntegrationAgent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

// CloseSession 拒绝测试未声明的单会话关闭操作。
func (a *nativeIntegrationAgent) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionClose)
}

// ListSessions 拒绝测试未声明的会话枚举操作。
func (a *nativeIntegrationAgent) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionList)
}

// ResumeSession 拒绝测试未声明的恢复操作。
func (a *nativeIntegrationAgent) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

// SetSessionMode 确认测试会话公开的全访问模式。
func (a *nativeIntegrationAgent) SetSessionMode(ctx context.Context, request sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	if a.configureMode != nil {
		return a.configureMode(ctx, request)
	}
	if request.ModeId != "agent-full-access" {
		return sdk.SetSessionModeResponse{}, sdk.NewInvalidParams(nil)
	}
	return sdk.SetSessionModeResponse{}, nil
}

// newNativeIntegrationManager 只替换原生 Agent 工厂，产品会话及公开 ACP 传输均为真实实现。
func newNativeIntegrationManager(t *testing.T, factory native.Factory, configure func(*native.ConnectionConfig)) *Manager {
	t.Helper()
	manager := NewManager(ManagerConfig{
		Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
			return LaunchFacts{BaseRuntimeID: "codex", Executable: "fixture"}, nil
		},
		Open: func(ctx context.Context, config native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
			config.Factory = factory
			if configure != nil {
				configure(&config)
			}
			return native.Open(ctx, config, request)
		},
	})
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager
}

// nativeIntegrationPlan 直接声明产品稳定计划，工具与工作范围由同一 Session 校验。
func nativeIntegrationPlan(root string) SessionPlan {
	return SessionPlan{Target: DeclareTarget(RuntimeTarget{RuntimeID: "codex", Isolated: true}), Detection: DetectionResult{Runtime: RuntimeSummary{ID: "codex", BaseRuntimeID: "codex"}}, Workspace: Workspace{Path: root}, Policy: RunPolicy{Execution: ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}}}
}

// runNativeIntegration 经过真实 PrepareSession、Bind、Run；包内结果保留原始输出证据和三个错误通道。
func runNativeIntegration(t *testing.T, manager *Manager, ctx context.Context, declaration SessionPlan, directive SessionDirective, input TurnInput, actual SessionResources, options resourcePreparation, observer turnObserver, strict bool) turnResult {
	t.Helper()
	prepared, err := manager.PrepareSession(ctx, declaration, directive)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	turn := prepared.(*PreparedTurn)
	if prepared.NeedsResources() {
		if actual.Target.RuntimeID == "" {
			actual.Target = RuntimeTarget{RuntimeID: declaration.Target.RuntimeID, Isolated: true}
		}
		if err := turn.bind(prepared.Context(), actual, options); err != nil {
			t.Fatal(err)
		}
	}
	return turn.run(ctx, input, observer, strict)
}

// nativeApprovalFunc 通过产品交互端口交付审批，取消由真实 Session 与 ACP 协议共同传播。
type nativeApprovalFunc func(context.Context, ApprovalRequest) (ApprovalDecision, error)

func (f nativeApprovalFunc) Request(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	return f(ctx, request)
}

// TestRuntimeConsumesACPToolProgress 验证真实内核消费公开ACP工具增量后仍得到最终结果。
func TestRuntimeConsumesACPToolProgress(t *testing.T) {
	agent := newNativeIntegrationAgent()
	agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		id := "answer-1"
		updates := []sdk.SessionUpdate{
			sdk.StartToolCall("tool-1", "Read file", sdk.WithStartStatus(sdk.ToolCallStatusInProgress)),
			sdk.UpdateToolCall("tool-1", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted)),
			{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "source": "native_phase", "complete": true}}, Content: sdk.TextBlock("done")}},
		}
		for _, update := range updates {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: update}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	root := t.TempDir()
	runtime := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return agent, nil }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	runtime.Start(ctx)
	result := runNativeIntegration(t, runtime, ctx, nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "read and answer"}}, SessionResources{}, resourcePreparation{}, nil, true)
	if result.Status != native.StatusCompleted || result.Fault != nil || result.CleanupFault != nil {
		t.Fatalf("execution failed: %+v", result)
	}
	if result.Output.Status != OutputAvailable || result.Output.Text != "done" || !result.Effects.PromptAccepted || !result.Effects.ToolActivity {
		t.Fatalf("missing final output or effects: %+v", result)
	}
	if agent.closes.Load() != 0 {
		t.Fatalf("hot native session released before owner close: %d", agent.closes.Load())
	}
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if agent.closes.Load() != 1 {
		t.Fatalf("native session not released after owner close: %d", agent.closes.Load())
	}
}

// TestRuntimeConsumesACPRequestCancellation 验证单项原生审批撤销不会取消仍可完成的整轮。
func TestRuntimeConsumesACPRequestCancellation(t *testing.T) {
	entered := make(chan struct{})
	agent := newNativeIntegrationAgent()
	agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		permissionCtx, cancelPermission := context.WithCancel(ctx)
		permissionDone := make(chan struct{})
		go func() {
			defer close(permissionDone)
			_, _ = agent.connection.Load().RequestPermission(permissionCtx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "approval-1", Title: sdk.Ptr("Read file")}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			cancelPermission()
			<-permissionDone
			return sdk.PromptResponse{}, ctx.Err()
		}
		cancelPermission()
		<-permissionDone
		id := "final"
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "source": "native_phase", "complete": true}}, Content: sdk.TextBlock("continued")}}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	root := t.TempDir()
	runtime := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return agent, nil }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	runtime.Start(ctx)
	handler := nativeApprovalFunc(func(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
		close(entered)
		<-ctx.Done()
		// 故意在请求撤销后交回允许；内核必须丢弃这一迟到授权。
		return ApprovalDecision{OptionID: request.Options[0].ID}, nil
	})
	result := runNativeIntegration(t, runtime, ctx, nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "continue after approval is withdrawn"}, ApprovalHandler: handler}, SessionResources{}, resourcePreparation{}, nil, false)
	if result.Status != native.StatusCompleted || result.Fault != nil || result.DeliveryFault != nil || result.Output.Text != "continued" {
		t.Fatalf("single request cancellation damaged turn: %+v", result)
	}
}

// TestRuntimeConsumesACPMessageEvidence 捕获伪 final、迟到阶段和消息完成被误用为最终证明的错误。
func TestRuntimeConsumesACPMessageEvidence(t *testing.T) {
	chunk := func(id, text string, meta map[string]any) sdk.SessionUpdateAgentMessageChunk {
		return sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock(text), Meta: map[string]any{"acp-go/message": meta}}
	}
	cases := []struct {
		name        string                               // name 标识需要保护的公开协议行为。
		chunks      []sdk.SessionUpdateAgentMessageChunk // chunks 是按原生顺序到达的公开 ACP 消息。
		policy      OutputPolicy                         // policy 是本次执行的最终证据要求。
		status      OutputStatus                         // status 是独立推导的输出状态。
		text        string                               // text 是独立推导的最终正文。
		reliability Reliability                          // reliability 是预期采用的最终证据。
		phase       native.MessagePhase                  // phase 是最后一条规范消息的预期阶段。
		complete    bool                                 // complete 是最后一条消息是否携带真实完成边界。
	}{
		{name: "missing source", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "final", "complete": true})}, policy: OutputStrict, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseUnknown},
		{name: "weak source cannot assert final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "final", "complete": true, "source": "adapter_segment"})}, policy: OutputStrict, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseUnknown},
		{name: "wrong phase type", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": 1, "complete": true, "source": "native_phase"})}, policy: OutputStrict, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseUnknown},
		{name: "message completion alone is not final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "unknown", "complete": true, "source": "native_message"})}, policy: OutputStrict, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseUnknown, complete: true},
		{name: "completion best effort stays unverified", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "unverified", map[string]any{"phase": "unknown", "complete": true, "source": "native_message"})}, policy: OutputBestEffort, status: OutputAvailable, text: "unverified", reliability: ReliabilityUnverified, phase: native.PhaseUnknown, complete: true},
		{name: "verified final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: OutputStrict, status: OutputAvailable, text: "answer", reliability: ReliabilityVerifiedPhase, phase: native.PhaseFinal, complete: true},
		{name: "late final keeps body", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", nil), chunk("a", "", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: OutputStrict, status: OutputAvailable, text: "answer", reliability: ReliabilityVerifiedPhase, phase: native.PhaseFinal, complete: true},
		{name: "late commentary revokes candidate", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "process", nil), chunk("a", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_message"})}, policy: OutputBestEffort, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseCommentary, complete: true},
		{name: "verified empty final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: OutputStrict, status: OutputEmpty, reliability: ReliabilityVerifiedPhase, phase: native.PhaseFinal, complete: true},
		{name: "wrong completion type", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": "true", "source": "native_phase"})}, policy: OutputStrict, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseUnknown},
		{name: "late commentary preserves unrelated final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": true, "source": "native_phase"}), chunk("b", "process", nil), chunk("b", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_message"})}, policy: OutputBestEffort, status: OutputAvailable, text: "answer", reliability: ReliabilityVerifiedPhase, phase: native.PhaseCommentary, complete: true},
		{name: "completion changes final classification", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "process", map[string]any{"phase": "final", "complete": false, "source": "native_phase"}), chunk("a", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_phase"})}, policy: OutputBestEffort, status: OutputUnavailable, reliability: ReliabilityUnverified, phase: native.PhaseCommentary, complete: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			agent := newNativeIntegrationAgent()
			agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				for _, message := range tt.chunks {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &message}}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			root := t.TempDir()
			runtime := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return agent, nil }, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			defer func() {
				if err := runtime.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			runtime.Start(ctx)
			var messages []native.AssistantMessage
			observer := observerFunc(func(_ context.Context, event observedEvent) error {
				if event.NativeEvent.Message != nil {
					messages = append(messages, *event.NativeEvent.Message)
				}
				return nil
			})
			result := runNativeIntegration(t, runtime, ctx, nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "answer"}}, SessionResources{}, resourcePreparation{}, observer, tt.policy == OutputStrict)
			if result.Status != native.StatusCompleted || result.StopReason != native.StopEndTurn || result.Fault != nil || result.DeliveryFault != nil || result.CleanupFault != nil {
				t.Fatalf("message evidence damaged execution: %+v", result)
			}
			if result.Output.Status != tt.status || result.Output.Text != tt.text || result.Output.Reliability != tt.reliability {
				t.Errorf("output=%+v, want status=%s text=%q reliability=%s", result.Output, tt.status, tt.text, tt.reliability)
			}
			if len(messages) != len(tt.chunks) {
				t.Fatalf("message boundaries lost: %+v", messages)
			}
			for i, message := range messages {
				if message.MessageID != *tt.chunks[i].MessageId || message.CompletionVerified {
					t.Errorf("identity or final proof changed: %+v", message)
				}
			}
			last := messages[len(messages)-1]
			if last.Phase != tt.phase || last.Complete != tt.complete {
				t.Errorf("last message=%+v, want phase=%s complete=%v", last, tt.phase, tt.complete)
			}
		})
	}
}

// credentialCleanupFixture 验证原生使用者未停止时凭据租约仍被Session保留。
type credentialCleanupFixture struct {
	closes atomic.Int32 // closes 统计实际凭据归还次数。
}

// Close 记录凭据实际归还。
func (p *credentialCleanupFixture) Close(context.Context) error { p.closes.Add(1); return nil }

// resourceCleanupFixture 验证原生关闭之前不会释放其依赖资源租约。
type resourceCleanupFixture struct {
	closes atomic.Int32 // closes 统计实际资源归还次数。
}

// Acquire 提供持有空资源列表的真实测试租约。
func (p *resourceCleanupFixture) Acquire(context.Context, ResourceRequest) (ResourceLease, error) {
	return p, nil
}

// Resources 返回空列表，不启用额外能力资源。
func (p *resourceCleanupFixture) Resources() []native.ResourceBinding { return nil }

// Close 记录资源实际归还。
func (p *resourceCleanupFixture) Close(context.Context) error { p.closes.Add(1); return nil }

// TestRuntimeKeepsACPDependenciesUntilNativeCloseSucceeds 验证真实 Session 与 SDK 组合保留失败清理的依赖，重试确认后只释放一次。
func TestRuntimeKeepsACPDependenciesUntilNativeCloseSucceeds(t *testing.T) {
	for _, stage := range []string{"factory", "initialize", "completed"} {
		t.Run(stage, func(t *testing.T) {
			agent := newNativeIntegrationAgent()
			agent.closeNative = func(context.Context) error {
				if agent.closes.Load() == 1 {
					return errors.New("transient native cleanup failure")
				}
				return nil
			}
			if stage == "initialize" {
				agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
					return sdk.InitializeResponse{}, errors.New("initialize failed")
				}
			}
			factory := func(context.Context, native.OpenRequest) (sdk.Agent, error) {
				if stage == "factory" {
					return agent, errors.New("factory partial failure")
				}
				return agent, nil
			}
			credentials := &credentialCleanupFixture{}
			resources := &resourceCleanupFixture{}
			root := t.TempDir()
			runtime := newNativeIntegrationManager(t, factory, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			runtime.Start(ctx)
			defer func() {
				if err := runtime.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			actual := SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true, CloseAfterRun: true}, Close: credentials.Close}
			lease, err := resources.Acquire(ctx, ResourceRequest{Workspace: native.WorkspaceScope{Root: root}})
			if err != nil {
				t.Fatal(err)
			}
			result := runNativeIntegration(t, runtime, ctx, nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "hello"}}, actual, resourcePreparation{taskLease: lease}, nil, true)
			if result.CleanupFault == nil || credentials.closes.Load() != 0 || resources.closes.Load() != 0 || agent.closes.Load() != 1 {
				t.Fatalf("native owner or dependencies lost: %#v native=%d credentials=%d resources=%d", result, agent.closes.Load(), credentials.closes.Load(), resources.closes.Load())
			}
			if err := runtime.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if credentials.closes.Load() != 1 || resources.closes.Load() != 1 || agent.closes.Load() != 2 {
				t.Fatalf("cleanup retry counts native=%d credentials=%d resources=%d", agent.closes.Load(), credentials.closes.Load(), resources.closes.Load())
			}
			if err := runtime.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if credentials.closes.Load() != 1 || resources.closes.Load() != 1 || agent.closes.Load() != 2 {
				t.Fatal("successful cleanup repeated")
			}
		})
	}
}

// skillResourceFixture 提供宿主已物化的技能目录，记录真实租约归还。
type skillResourceFixture struct {
	bindings []native.ResourceBinding // bindings 是宿主固定的技能目录视图。
	closes   atomic.Int32             // closes 统计依赖使用结束后的归还次数。
}

// Acquire 将当前宿主资源租约交给真实执行内核。
func (f *skillResourceFixture) Acquire(context.Context, ResourceRequest) (ResourceLease, error) {
	return f, nil
}

// Resources 返回由当前租约保护的技能目录。
func (f *skillResourceFixture) Resources() []native.ResourceBinding { return f.bindings }

// Close 记录执行内核归还资源的事实。
func (f *skillResourceFixture) Close(context.Context) error { f.closes.Add(1); return nil }

// TestRuntimeACPContributionsReachToolCycleOnce 捕获贡献未交付、工具循环重复注入或过程正文冒充最终输出的错误。
func TestRuntimeACPContributionsReachToolCycleOnce(t *testing.T) {
	for _, policy := range []native.CompletionPolicy{native.CompletionNativePhase, native.CompletionLastAssistant} {
		t.Run(string(policy), func(t *testing.T) {
			agent := newNativeIntegrationAgent()
			skillDir := filepath.Join(t.TempDir(), "installed ally")
			resources := &skillResourceFixture{bindings: []native.ResourceBinding{{ID: "ally-skill", Path: skillDir}}}
			var prompts atomic.Int32
			agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				prompts.Add(1)
				if len(p.Prompt) != 1 || p.Prompt[0].Text == nil {
					t.Errorf("expected one initial text block: %+v", p.Prompt)
					return sdk.PromptResponse{}, sdk.NewInvalidParams(nil)
				}
				text := p.Prompt[0].Text.Text
				for _, once := range []string{"MAINTENANCE_INSTRUCTION", filepath.Join(skillDir, "SKILL.md"), "MAINTENANCE_TASK"} {
					if strings.Count(text, once) != 1 {
						t.Errorf("initial context must contain %q exactly once: %q", once, text)
					}
				}
				if !strings.Contains(text, "ally") || !strings.Contains(text, "2026.10") {
					t.Errorf("skill identity missing: %q", text)
				}
				id := "progress"
				updates := []sdk.SessionUpdate{
					{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("tool preparation")}},
					sdk.StartToolCall("knowledge-call", "Update Wiki", sdk.WithStartStatus(sdk.ToolCallStatusInProgress)),
					sdk.UpdateToolCall("knowledge-call", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted)),
				}
				for _, update := range updates {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				id = "final"
				var meta map[string]any
				if policy == native.CompletionNativePhase {
					meta = map[string]any{"acp-go/message": map[string]any{"phase": "final", "complete": true, "source": "native_phase"}}
				}
				err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("Wiki maintenance completed"), Meta: meta}}})
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
			}
			agent.closeNative = func(context.Context) error {
				if resources.closes.Load() != 0 {
					return errors.New("skill lease released before native consumer stopped")
				}
				return nil
			}
			manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return agent, nil }, func(config *native.ConnectionConfig) { config.CompletionPolicy = policy })
			root := t.TempDir()
			manager.Start(context.Background())
			lease, err := resources.Acquire(context.Background(), ResourceRequest{Workspace: native.WorkspaceScope{Root: root}})
			if err != nil {
				t.Fatal(err)
			}
			actual := SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true, CloseAfterRun: true}}
			options := resourcePreparation{taskLease: lease, contributions: native.Contributions{Instructions: []native.Instruction{{Name: "maintenance", Version: "v1", Text: "MAINTENANCE_INSTRUCTION"}}, Skills: []native.Skill{{Name: "ally", Version: "2026.10", ResourceID: "ally-skill"}}}}
			result := runNativeIntegration(t, manager, context.Background(), nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "MAINTENANCE_TASK"}}, actual, options, nil, true)
			if result.Status != native.StatusCompleted || result.Fault != nil || result.CleanupFault != nil || result.Output.Status != OutputAvailable || result.Output.Text != "Wiki maintenance completed" || !result.Effects.ToolActivity {
				t.Fatalf("tool cycle lost final output: %+v", result)
			}
			if prompts.Load() != 1 || agent.closes.Load() != 1 || resources.closes.Load() != 1 {
				t.Fatalf("replayed prompt or incorrect cleanup: prompts=%d native=%d resources=%d", prompts.Load(), agent.closes.Load(), resources.closes.Load())
			}
		})
	}
}

func nativeIntegrationSelectionOption(category sdk.SessionConfigOptionCategory, id, current string, values ...string) sdk.SessionConfigOption {
	choices := make(sdk.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, value := range values {
		choices = append(choices, sdk.SessionConfigSelectOption{Value: sdk.SessionConfigValueId(value), Name: value})
	}
	return sdk.SessionConfigOption{Select: &sdk.SessionConfigOptionSelect{Id: sdk.SessionConfigId(id), Name: id, Category: &category, CurrentValue: sdk.SessionConfigValueId(current), Options: sdk.SessionConfigSelectOptions{Ungrouped: &choices}}}
}

// TestInvokePreservesConfirmedConfigurationAndUsage 防止内核丢失原生确认的选择或把上下文统计混入最终生成用量。
func TestInvokePreservesConfirmedConfigurationAndUsage(t *testing.T) {
	a := newNativeIntegrationAgent()
	options := []sdk.SessionConfigOption{nativeIntegrationSelectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "agent", "agent-full-access")}
	a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
	}
	a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
		options[0].Select.CurrentValue = r.ValueId.Value
		return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
	}
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		id := "final"
		err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "source": "native_phase", "complete": true}}, Content: sdk.TextBlock("done")}}})
		read := 7
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: 11, OutputTokens: 5, CachedReadTokens: &read}}, err
	}
	root := t.TempDir()
	r := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
	defer r.Close(context.Background())
	r.Start(context.Background())
	r.config.Open = func(ctx context.Context, config native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
		config.Factory = func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }
		config.ModeCandidates = []string{"agent-full-access"}
		return native.Open(ctx, config, request)
	}
	result := runNativeIntegration(t, r, context.Background(), nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "answer"}}, SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true, CloseAfterRun: true}}, resourcePreparation{}, nil, true)
	if result.Status != native.StatusCompleted || result.Output.Text != "done" || result.Configuration.ActualMode != "agent-full-access" || !result.Usage.Known || result.Usage.InputTokens != 11 || result.Usage.CachedReadTokens != 7 || result.Usage.ContextKnown {
		t.Fatalf("lost native facts: %+v", result)
	}
}

// TestInvokeForwardsInactivityBudget 防止内核仅规范化无活动预算却没有传给原生调用。
func TestInvokeForwardsInactivityBudget(t *testing.T) {
	a := newNativeIntegrationAgent()
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	root := t.TempDir()
	r := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
	defer r.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.Start(ctx)
	r.config.Limits.InactivityTimeout = 20 * time.Millisecond
	result := runNativeIntegration(t, r, ctx, nativeIntegrationPlan(root), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "answer"}}, SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true, CloseAfterRun: true}}, resourcePreparation{}, nil, true)
	if result.Fault == nil || result.Fault.Code != native.FaultInactivityTimeout || result.Status != native.StatusFailed || !result.Effects.PromptAccepted || !result.Effects.OutcomeUnknown {
		t.Fatalf("wrong inactivity outcome: %+v", result)
	}
}

// TestUsageFoundationFinalObserverTerminals 验证真实 Session 在失败和取消后通过原 observer 交付消费及上下文。
func TestUsageFoundationFinalObserverTerminals(t *testing.T) {
	for _, terminal := range []string{"error", "cancel", "delivery_error"} {
		t.Run(terminal, func(t *testing.T) {
			a := newNativeIntegrationAgent()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			delivered := make(chan struct{})
			a.prompt = func(nativeCtx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				reported := acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Source: "fixture.requests", Coverage: "observed_requests", ContextKnown: true}
				if err := a.connection.Load().SessionUpdate(nativeCtx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 900, Size: 64000, Meta: acpmeta.WithTurnUsage(nil, reported)}}}); err != nil {
					return sdk.PromptResponse{}, err
				}
				select {
				case <-delivered:
				case <-nativeCtx.Done():
					return sdk.PromptResponse{}, nativeCtx.Err()
				}
				if terminal == "error" {
					return sdk.PromptResponse{}, errors.New("native failed")
				}
				<-nativeCtx.Done()
				return sdk.PromptResponse{}, nativeCtx.Err()
			}
			manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
			manager.Start(ctx)
			var usages []UsageData
			observer := observerFunc(func(delivery context.Context, event observedEvent) error {
				if event.NativeEvent.Usage == nil {
					return nil
				}
				if err := delivery.Err(); err != nil {
					return err
				}
				usages = append(usages, chatUsage(*event.NativeEvent.Usage))
				if len(usages) == 1 {
					close(delivered)
					if terminal == "cancel" {
						cancel()
					}
					if terminal == "delivery_error" {
						return errors.New("observer rejected update")
					}
				}
				return nil
			})
			result := runNativeIntegration(t, manager, ctx, nativeIntegrationPlan(t.TempDir()), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "work"}}, SessionResources{}, resourcePreparation{}, observer, false)
			wantStatus := native.StatusFailed
			if terminal == "cancel" {
				wantStatus = native.StatusCancelled
			}
			if result.Status != wantStatus || result.Usage.InputTokens != 100 || !result.Usage.ContextKnown {
				t.Fatalf("terminal changed or usage lost: %+v", result)
			}
			if terminal == "delivery_error" && result.DeliveryFault == nil {
				t.Fatalf("delivery fault lost: %+v", result)
			}
			if len(usages) != 2 {
				t.Fatalf("final usage not delivered: %+v", usages)
			}
			final := usages[1]
			if final.Turn == nil || final.Context == nil || final.Turn.InputTokens != 100 || final.Turn.OutputTokens != 10 || final.Turn.Complete == nil || *final.Turn.Complete || final.Turn.Coverage != "observed_requests" || final.Context.UsedTokens != 900 || final.Context.WindowTokens != 64000 {
				t.Fatalf("final metadata lost: %+v", final)
			}
		})
	}
}

// TestUsageFinalFixObserverOwnership 验证仅最终回执的用量交付副本不允许观察者改写返回结果。
func TestUsageFinalFixObserverOwnership(t *testing.T) {
	a := newNativeIntegrationAgent()
	var prompts atomic.Int32
	a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		prompts.Add(1)
		reported := acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Source: "fixture.final", Coverage: "native_turn", Complete: true}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Meta: acpmeta.WithTurnUsage(nil, reported)}, nil
	}
	manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
	manager.Start(context.Background())
	observed := 0
	observer := observerFunc(func(_ context.Context, event observedEvent) error {
		if usage := event.NativeEvent.Usage; usage != nil {
			observed++
			usage.Reported.Source = "mutated"
			*usage.Reported.OutputTokens = 999
			usage.InputTokens, usage.OutputTokens, usage.Known = 888, 777, false
		}
		return nil
	})
	result := runNativeIntegration(t, manager, context.Background(), nativeIntegrationPlan(t.TempDir()), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "work"}}, SessionResources{}, resourcePreparation{}, observer, false)
	if observed != 1 || prompts.Load() != 1 || result.Status != native.StatusCompleted || !result.Usage.Known || result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 10 || result.Usage.Reported == nil || result.Usage.Reported.Source != "fixture.final" || *result.Usage.Reported.OutputTokens != 10 {
		t.Fatalf("final observer mutated authoritative usage: prompts=%d observed=%d result=%+v", prompts.Load(), observed, result)
	}
}

// TestUsageFinalFixMetadataBudget 验证仅最终回执的元数据受单事件和累计字节预算约束，原终态与消费不被清除。
func TestUsageFinalFixMetadataBudget(t *testing.T) {
	for _, budget := range []string{"single", "total"} {
		for _, terminal := range []string{"completed", "cancelled", "failed"} {
			t.Run(budget+"/"+terminal, func(t *testing.T) {
				a := newNativeIntegrationAgent()
				var prompts atomic.Int32
				source := strings.Repeat("s", 600)
				wantLimit := native.LimitSingleEventBytes
				if budget == "total" {
					source, wantLimit = strings.Repeat("s", 300), native.LimitTotalEventBytes
				}
				a.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
					prompts.Add(1)
					if budget == "total" {
						if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{AgentThoughtChunk: &sdk.SessionUpdateAgentThoughtChunk{Content: sdk.TextBlock(strings.Repeat("x", 100))}}}); err != nil {
							return sdk.PromptResponse{}, err
						}
					}
					stop := sdk.StopReasonEndTurn
					if terminal == "cancelled" {
						stop = sdk.StopReasonCancelled
					} else if terminal == "failed" {
						stop = sdk.StopReason("future_reason")
					}
					reported := acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Source: source, Coverage: "native_turn", Complete: true}
					return sdk.PromptResponse{StopReason: stop, Meta: acpmeta.WithTurnUsage(nil, reported)}, nil
				}
				manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
				manager.config.Limits = native.Limits{MaxOutputBytes: 128, MaxEventBytes: 512, MaxTotalEventBytes: 512}
				manager.Start(context.Background())
				observed := 0
				observer := observerFunc(func(_ context.Context, event observedEvent) error {
					if event.NativeEvent.Usage != nil {
						observed++
					}
					return nil
				})
				result := runNativeIntegration(t, manager, context.Background(), nativeIntegrationPlan(t.TempDir()), SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "work"}}, SessionResources{}, resourcePreparation{}, observer, false)
				wantStatus := native.StatusCompleted
				if terminal == "cancelled" {
					wantStatus = native.StatusCancelled
				} else if terminal == "failed" {
					wantStatus = native.StatusFailed
				}
				if observed != 0 || result.DeliveryFault == nil || result.DeliveryFault.Code != native.FaultDelivery {
					t.Fatalf("oversize final metadata delivered: observed=%d result=%+v", observed, result)
				}
				var limit *native.Fault
				if !errors.As(result.DeliveryFault.Cause, &limit) || limit.Code != native.FaultLimit || limit.Limit != wantLimit {
					t.Fatalf("wrong final budget fault: %+v", result.DeliveryFault)
				}
				if prompts.Load() != 1 || result.Status != wantStatus || !result.Usage.Known || result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 10 || result.Usage.Reported == nil || result.Usage.Reported.Source != source {
					t.Fatalf("budget erased consumption or terminal: prompts=%d result=%+v", prompts.Load(), result)
				}
				if terminal == "failed" && (result.Fault == nil || result.Fault.Code != native.FaultProtocol) {
					t.Fatalf("original failure replaced: %+v", result)
				}
			})
		}
	}
}

// TestUsageTerminalPublicResult 验证公开 Run 的终态交接独立于最终事件预算及观察者交付。
func TestUsageTerminalPublicResult(t *testing.T) {
	for _, partial := range []bool{false, true} {
		for _, delivery := range []string{"ok", "events", "single", "total", "observer"} {
			for _, terminal := range []string{"completed", "cancelled", "failed"} {
				t.Run(fmt.Sprintf("partial=%t/%s/%s", partial, delivery, terminal), func(t *testing.T) {
					a := newNativeIntegrationAgent()
					var prompts atomic.Int32
					source := "fixture.final"
					if delivery == "single" {
						source = strings.Repeat("s", 600)
					}
					if delivery == "total" {
						source = strings.Repeat("s", 300)
					}
					a.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
						prompts.Add(1)
						if partial {
							reported := acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Source: "fixture.partial", Coverage: "observed_requests", ContextKnown: true}
							if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 900, Size: 64000, Meta: acpmeta.WithTurnUsage(nil, reported)}}}); err != nil {
								return sdk.PromptResponse{}, err
							}
						}
						if delivery == "total" {
							if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{AgentThoughtChunk: &sdk.SessionUpdateAgentThoughtChunk{Content: sdk.TextBlock(strings.Repeat("x", 100))}}}); err != nil {
								return sdk.PromptResponse{}, err
							}
						}
						stop := sdk.StopReasonEndTurn
						if terminal == "cancelled" {
							stop = sdk.StopReasonCancelled
						}
						if terminal == "failed" {
							stop = sdk.StopReason("future_reason")
						}
						input, output := int64(100), int64(10)
						if partial {
							input, output = 250, 30
						}
						reported := acpmeta.TurnUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: sdk.Ptr(int64(0)), ReasoningTokens: sdk.Ptr(int64(0)), Source: source, Coverage: "native_turn", Complete: true, Model: "fixture-model"}
						return sdk.PromptResponse{StopReason: stop, Meta: acpmeta.WithTurnUsage(nil, reported)}, nil
					}
					manager := newNativeIntegrationManager(t, func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }, nil)
					switch delivery {
					case "events":
						manager.config.Limits.MaxEvents = 1
						if partial {
							manager.config.Limits.MaxEvents = 2
						}
					case "single":
						manager.config.Limits = native.Limits{MaxOutputBytes: 128, MaxEventBytes: 512}
					case "total":
						manager.config.Limits = native.Limits{MaxOutputBytes: 128, MaxEventBytes: 512, MaxTotalEventBytes: 512}
						if partial {
							manager.config.Limits.MaxTotalEventBytes = 700
						}
					}
					manager.Start(context.Background())
					prepared, err := manager.PrepareSession(context.Background(), nativeIntegrationPlan(t.TempDir()), SessionDirective{Mode: SessionModeNew})
					if err != nil {
						t.Fatal(err)
					}
					defer prepared.Close(context.Background())
					if err = prepared.Bind(prepared.Context(), SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true}}); err != nil {
						t.Fatal(err)
					}
					accepted, usageEvents := 0, 0
					var observed UsageData
					sink := productSinkFunc(func(_ context.Context, event Event) error {
						accepted++
						if event.Type == EventTypeUsage {
							usageEvents++
							if err := json.Unmarshal(event.Data, &observed); err != nil {
								t.Fatal(err)
							}
							if observed.Turn != nil && observed.Turn.Complete != nil && *observed.Turn.Complete && delivery == "observer" {
								return errors.New("observer rejected final")
							}
						}
						return nil
					})
					result := prepared.Run(context.Background(), TurnInput{Message: Message{Text: "work"}}, sink)
					if prompts.Load() != 1 {
						t.Fatalf("Prompt replayed: %d", prompts.Load())
					}
					wantInput, wantOutput := int64(100), int64(10)
					if partial {
						wantInput, wantOutput = 250, 30
					}
					if result.Usage == nil || result.Usage.Turn == nil {
						t.Fatalf("public terminal consumption missing: %+v", result)
					}
					usage := result.Usage
					if usage.Turn.InputTokens != wantInput || usage.Turn.OutputTokens != wantOutput || usage.Turn.Source != source || usage.Turn.Coverage != "native_turn" || usage.Turn.Model != "fixture-model" || usage.Turn.Complete == nil || !*usage.Turn.Complete || !reflect.DeepEqual(usage.Turn.KnownFields, []string{"input_tokens", "cache_read_tokens", "output_tokens", "reasoning_output_tokens"}) {
						t.Fatalf("terminal consumption changed: %+v", usage.Turn)
					}
					if partial && (usage.Context == nil || usage.Context.UsedTokens != 900 || usage.Context.WindowTokens != 64000) {
						t.Fatalf("context erased: %+v", usage)
					}
					if !partial && usage.Context != nil {
						t.Fatalf("context fabricated: %+v", usage.Context)
					}
					if terminal == "failed" {
						if result.Failure == nil || result.Failure.Code == FailureCodeEventDelivery || result.TerminationReason == constants.RuntimeTerminationReasonCancelled {
							t.Fatalf("original native failure replaced: %+v", result)
						}
					} else if terminal == "cancelled" {
						if result.TerminationReason != constants.RuntimeTerminationReasonCancelled || result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
							t.Fatalf("cancel lost: %+v", result)
						}
					} else if delivery != "ok" && (result.Failure == nil || result.Failure.Code != FailureCodeEventDelivery) {
						t.Fatalf("delivery failure missing: %+v", result)
					}
					if delivery == "events" && accepted > manager.config.Limits.MaxEvents {
						t.Fatalf("events bypass budget: %d", accepted)
					}
					wantEvents := 0
					if partial {
						wantEvents++
					}
					if delivery == "ok" || delivery == "observer" {
						wantEvents++
					}
					if usageEvents != wantEvents {
						t.Fatalf("unexpected usage delivery: %d want %d", usageEvents, wantEvents)
					}
					usage.Turn.InputTokens = 999
					usage.Turn.KnownFields[0] = "mutated"
					*usage.Turn.Complete = false
					if delivery == "ok" && (observed.Turn.InputTokens != wantInput || observed.Turn.KnownFields[0] != "input_tokens" || !*observed.Turn.Complete) {
						t.Fatal("public result shares observer snapshot")
					}
				})
			}
		}
	}
}

// TestUsageTerminalResultOwnership 验证公开结果不能反向修改 Session 已冻结的消费与上下文。
func TestUsageTerminalResultOwnership(t *testing.T) {
	reported := &acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Complete: true, Source: "fixture.final"}
	original := turnResult{Usage: native.Usage{Known: true, InputTokens: 100, OutputTokens: 10, ContextKnown: true, ContextTokens: 900, ContextWindowTokens: 64000, Reported: reported}}
	first := chatResult(SessionPlan{}, SessionDirective{}, original, &chatObserver{})
	first.Usage.Turn.InputTokens = 999
	first.Usage.Turn.KnownFields[0] = "mutated"
	*first.Usage.Turn.Complete = false
	first.Usage.Context.UsedTokens = 0
	second := chatResult(SessionPlan{}, SessionDirective{}, original, &chatObserver{})
	if original.Usage.InputTokens != 100 || *reported.InputTokens != 100 || !reported.Complete || second.Usage.Turn.InputTokens != 100 || second.Usage.Context.UsedTokens != 900 || second.Usage.Turn.KnownFields[0] != "input_tokens" || !*second.Usage.Turn.Complete {
		t.Fatal("consumer changed authoritative terminal")
	}
}

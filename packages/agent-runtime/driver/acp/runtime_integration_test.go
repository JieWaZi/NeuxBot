package acp_test

import (
	"context"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// TestRuntimeConsumesACPToolProgress 验证真实内核消费公开ACP工具增量后仍得到最终结果。
func TestRuntimeConsumesACPToolProgress(t *testing.T) {
	agent := newFixtureAgent()
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
	driver := newDriver(t, agent)
	root := t.TempDir()
	runtime, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{driver}, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	result := runtime.Run(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: driver.ID()}, Input: contract.Input{Text: "read and answer"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}}, contract.RunOptions{})
	if result.Status != contract.StatusCompleted || result.Fault != nil || result.CleanupFault != nil {
		t.Fatalf("execution failed: %+v", result)
	}
	if result.Output.Status != contract.OutputAvailable || result.Output.Text != "done" || !result.Effects.PromptAccepted || !result.Effects.ToolActivity {
		t.Fatalf("missing final output or effects: %+v", result)
	}
	if agent.closes.Load() != 1 {
		t.Fatalf("non-hot native session not released: %d", agent.closes.Load())
	}
}

// runtimeInteractionFunc 为组合测试提供真实可撤销的交互处理端口。
type runtimeInteractionFunc func(context.Context, contract.InteractionRequest) (contract.InteractionResponse, error)

// Handle 将请求上下文交给本次测试指定的处理函数。
func (f runtimeInteractionFunc) Handle(ctx context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
	return f(ctx, request)
}

// TestRuntimeConsumesACPRequestCancellation 验证单项原生审批撤销不会取消仍可完成的整轮。
func TestRuntimeConsumesACPRequestCancellation(t *testing.T) {
	entered := make(chan struct{})
	agent := newFixtureAgent()
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
	driver := newDriver(t, agent)
	root := t.TempDir()
	runtime, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{driver}, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	handler := runtimeInteractionFunc(func(ctx context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		// 故意在请求撤销后交回允许；内核必须丢弃这一迟到授权。
		return contract.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Allowed: true}, nil
	})
	result := runtime.Run(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: driver.ID()}, Input: contract.Input{Text: "continue after approval is withdrawn"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}, Interaction: contract.InteractionInteractive}}, contract.RunOptions{InteractionHandler: handler})
	if result.Status != contract.StatusCompleted || result.Fault != nil || result.DeliveryFault != nil || result.Output.Text != "continued" {
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
		policy      contract.OutputPolicy                // policy 是本次执行的最终证据要求。
		status      contract.OutputStatus                // status 是独立推导的输出状态。
		text        string                               // text 是独立推导的最终正文。
		reliability contract.Reliability                 // reliability 是预期采用的最终证据。
		phase       contract.MessagePhase                // phase 是最后一条规范消息的预期阶段。
		complete    bool                                 // complete 是最后一条消息是否携带真实完成边界。
	}{
		{name: "missing source", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "final", "complete": true})}, policy: contract.OutputStrict, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown},
		{name: "weak source cannot assert final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "final", "complete": true, "source": "adapter_segment"})}, policy: contract.OutputStrict, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown},
		{name: "wrong phase type", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": 1, "complete": true, "source": "native_phase"})}, policy: contract.OutputStrict, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown},
		{name: "message completion alone is not final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "not proven", map[string]any{"phase": "unknown", "complete": true, "source": "native_message"})}, policy: contract.OutputStrict, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown, complete: true},
		{name: "completion best effort stays unverified", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "unverified", map[string]any{"phase": "unknown", "complete": true, "source": "native_message"})}, policy: contract.OutputBestEffort, status: contract.OutputAvailable, text: "unverified", reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown, complete: true},
		{name: "verified final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: contract.OutputStrict, status: contract.OutputAvailable, text: "answer", reliability: contract.ReliabilityVerifiedPhase, phase: contract.PhaseFinal, complete: true},
		{name: "late final keeps body", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", nil), chunk("a", "", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: contract.OutputStrict, status: contract.OutputAvailable, text: "answer", reliability: contract.ReliabilityVerifiedPhase, phase: contract.PhaseFinal, complete: true},
		{name: "late commentary revokes candidate", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "process", nil), chunk("a", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_message"})}, policy: contract.OutputBestEffort, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseCommentary, complete: true},
		{name: "verified empty final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "", map[string]any{"phase": "final", "complete": true, "source": "native_phase"})}, policy: contract.OutputStrict, status: contract.OutputEmpty, reliability: contract.ReliabilityVerifiedPhase, phase: contract.PhaseFinal, complete: true},
		{name: "wrong completion type", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": "true", "source": "native_phase"})}, policy: contract.OutputStrict, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseUnknown},
		{name: "late commentary preserves unrelated final", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "answer", map[string]any{"phase": "final", "complete": true, "source": "native_phase"}), chunk("b", "process", nil), chunk("b", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_message"})}, policy: contract.OutputBestEffort, status: contract.OutputAvailable, text: "answer", reliability: contract.ReliabilityVerifiedPhase, phase: contract.PhaseCommentary, complete: true},
		{name: "completion changes final classification", chunks: []sdk.SessionUpdateAgentMessageChunk{chunk("a", "process", map[string]any{"phase": "final", "complete": false, "source": "native_phase"}), chunk("a", "", map[string]any{"phase": "commentary", "complete": true, "source": "native_phase"})}, policy: contract.OutputBestEffort, status: contract.OutputUnavailable, reliability: contract.ReliabilityUnverified, phase: contract.PhaseCommentary, complete: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			agent := newFixtureAgent()
			agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				for _, message := range tt.chunks {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &message}}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			driver := newDriver(t, agent)
			root := t.TempDir()
			runtime, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{driver}, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			defer func() {
				if err := runtime.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			if err := runtime.Start(ctx); err != nil {
				t.Fatal(err)
			}
			var messages []contract.AssistantMessage
			observer := contract.ObserverFunc(func(_ context.Context, event contract.Event) error {
				if event.DriverEvent.Message != nil {
					messages = append(messages, *event.DriverEvent.Message)
				}
				return nil
			})
			result := runtime.Run(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: driver.ID()}, Input: contract.Input{Text: "answer"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}, Output: tt.policy}}, contract.RunOptions{Observer: observer})
			if result.Status != contract.StatusCompleted || result.StopReason != contract.StopEndTurn || result.Fault != nil || result.DeliveryFault != nil || result.CleanupFault != nil {
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

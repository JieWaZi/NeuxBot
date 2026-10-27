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
			{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"codex": map[string]any{"phase": "final_answer"}}, Content: sdk.TextBlock("done")}},
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
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"codex": map[string]any{"phase": "final_answer"}}, Content: sdk.TextBlock("continued")}}})
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

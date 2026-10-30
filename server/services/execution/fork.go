package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// ForkChat 把冻结执行配置交给 Runtime；Chat 仍独占历史、幂等和发布事务。
func (m *service) ForkChat(ctx context.Context, input chatservice.ForkInput) (string, error) {
	var prepared runtimecontract.ForkSessionRequest
	var preparedReady bool
	buildRequest := func(ctx context.Context, input chatservice.NativeForkInput) (runtimecontract.ForkSessionRequest, error) {
		environment, err := m.runtimeEnvironments.Get(ctx, input.Invocation.RuntimeID)
		if err != nil {
			return runtimecontract.ForkSessionRequest{}, err
		}
		if !environment.Enabled || (input.Invocation.BaseRuntimeID != "" && environment.BaseRuntimeID != input.Invocation.BaseRuntimeID) {
			return runtimecontract.ForkSessionRequest{}, status_error.RuntimeUnavailable
		}
		detection := environment.Detection
		detection.ResolvedBinary = input.Invocation.Binary
		detection.Version = input.Invocation.Version
		return runtimecontract.ForkSessionRequest{
			ReceiptDirectory: input.ReceiptDirectory, ReconcileOnly: input.ReconcileOnly, SourceSessionID: input.Invocation.SessionOutputID, Position: input.Invocation.ForkPosition, SourceWorkspace: input.Invocation.WorkingDirectory,
			Plan: runtimecontract.SessionPlan{
				Target: runtimecontract.SessionTarget{RuntimeID: input.Invocation.RuntimeID, Binary: input.Invocation.Binary, PrefixArgs: input.Invocation.PrefixArgs, ConfigHome: input.Invocation.ConfigHome}, Detection: detection,
				Workspace: runtimecontract.Workspace{Path: input.Workspace, Managed: true, AdditionalWritableDirectories: input.Invocation.AdditionalWritableDirectories},
				Policy:    runtimecontract.RunPolicy{Model: input.Invocation.Model, Reasoning: input.Invocation.Reasoning, Execution: runtimecontract.ExecutionPolicy{PermissionLevel: input.Invocation.PermissionLevel, WorkspaceReadOnly: input.Invocation.WorkspaceReadOnly}},
			},
		}, nil
	}
	return m.journal.ForkChat(ctx, input, chatservice.NativeFork{
		Preflight: func(ctx context.Context, input chatservice.NativeForkInput) error {
			var err error
			prepared, err = buildRequest(ctx, input)
			if err != nil {
				return err
			}
			// 先释放源消费者，再由 Chat 持久化原生派发边界；关闭失败仍可按同一请求安全重试。
			if err = m.runtime.CloseWorkspace(ctx, prepared.SourceWorkspace); err != nil {
				return err
			}
			preparedReady = true
			return nil
		},
		Execute: func(ctx context.Context, input chatservice.NativeForkInput) (string, error) {
			request := prepared
			if !preparedReady {
				var err error
				request, err = buildRequest(ctx, input)
				if err != nil {
					return "", err
				}
			}
			request.ReconcileOnly = input.ReconcileOnly
			result, err := m.runtime.ForkSession(ctx, request)
			return result.SessionID, err
		},
	})
}

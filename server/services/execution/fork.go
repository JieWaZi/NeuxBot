package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// ForkChat 把冻结执行配置交给 Runtime；Chat 仍独占历史、幂等和发布事务。
func (m *service) ForkChat(ctx context.Context, input chatservice.ForkInput) (string, error) {
	runner := m.runtime
	if m.chatRuntime != nil {
		state, err := m.journal.LoadExecutionContext(ctx, input.ChatID)
		if err != nil {
			return "", err
		}
		if state.ThreadWorkspaceID == "" {
			runner = m.chatRuntime
		}
	}
	var prepared runtimes.ForkSessionRequest
	var preparedReady bool
	buildRequest := func(ctx context.Context, input chatservice.NativeForkInput) (runtimes.ForkSessionRequest, error) {
		environment, err := m.runtimeEnvironments.Get(ctx, input.Invocation.RuntimeID)
		if err != nil {
			return runtimes.ForkSessionRequest{}, err
		}
		if !environment.Enabled || (input.Invocation.BaseRuntimeID != "" && environment.BaseRuntimeID != input.Invocation.BaseRuntimeID) {
			return runtimes.ForkSessionRequest{}, status_error.RuntimeUnavailable
		}
		detection := environment.Detection
		detection.ResolvedBinary = input.Invocation.Binary
		detection.Version = input.Invocation.Version
		return runtimes.ForkSessionRequest{
			ReceiptDirectory: input.ReceiptDirectory, ReconcileOnly: input.ReconcileOnly, SourceSessionID: input.Invocation.SessionOutputID, Position: input.Invocation.ForkPosition, SourceWorkspace: input.Invocation.WorkingDirectory,
			Run: runtimes.RunRequest{
				Target: runtimes.RuntimeTarget{RuntimeID: input.Invocation.RuntimeID, Binary: input.Invocation.Binary, PrefixArgs: input.Invocation.PrefixArgs, ConfigHome: input.Invocation.ConfigHome}, Detection: detection,
				Workspace: runtimes.Workspace{Path: input.Workspace, Managed: true, AdditionalWritableDirectories: input.Invocation.AdditionalWritableDirectories},
				Policy:    runtimes.RunPolicy{Model: input.Invocation.Model, Reasoning: input.Invocation.Reasoning, Execution: runtimes.ExecutionPolicy{PermissionLevel: input.Invocation.PermissionLevel, WorkspaceReadOnly: input.Invocation.WorkspaceReadOnly}},
			},
		}, nil
	}
	return m.journal.ForkChat(ctx, input, chatservice.NativeFork{
		Preflight: func(ctx context.Context, input chatservice.NativeForkInput) error {
			var err error
			prepared, err = buildRequest(ctx, input)
			preparedReady = err == nil
			return err
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
			result, err := runner.ForkSession(ctx, request)
			return result.SessionID, err
		},
	})
}

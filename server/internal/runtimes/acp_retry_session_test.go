package runtimes

import (
	"context"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestACPTransientFailureKeepsResumableSessionAcrossCLIs 验证五种 CLI 的无输出拒绝均保留可续接句柄，重试不新建会话。
func TestACPTransientFailureKeepsResumableSessionAcrossCLIs(t *testing.T) {
	for _, runtimeID := range []string{RuntimeIDCodex, RuntimeIDClaude, RuntimeIDCursor, RuntimeIDKimi, RuntimeIDPi} {
		t.Run(runtimeID, func(t *testing.T) {
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{sessionID: acp.SessionId(runtimeID + "-retry-session"), selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, promptError: acp.NewInternalError(map[string]any{"errorKind": "rate_limit"}), simplePrompt: true}
			replaceACPAgentFactory(backend, runtimeID, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
			t.Cleanup(func() {
				if err := backend.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			request := matrixACPRunRequest(t, runtimeID, SessionDirective{Mode: SessionModeNew})
			failed := backend.Run(context.Background(), request)
			if failed.Failure == nil || failed.Failure.Code != FailureCodeRateLimited || failed.Session.OutputID == "" || failed.Session.State != SessionStateCreated || failed.SideEffects.Any() || failed.Failure.SideEffects.Any() {
				t.Fatalf("限流没有产生可安全续接事实：%+v", failed)
			}
			agent.mutex.Lock()
			agent.promptError = nil
			agent.mutex.Unlock()
			request.InvocationID = runtimeID + "-retry-invocation"
			request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: failed.Session.OutputID}
			retried := backend.Run(context.Background(), request)
			if retried.Failure != nil || retried.Session.State != SessionStateResumed || retried.Session.InputID != failed.Session.OutputID || retried.Session.OutputID != failed.Session.OutputID {
				t.Fatalf("重试没有继续原会话：%+v", retried)
			}
			agent.mutex.Lock()
			newCalls := agent.newCalls
			agent.mutex.Unlock()
			if newCalls != 1 {
				t.Fatalf("重试重新建立了 %d 个会话", newCalls)
			}
		})
	}
}

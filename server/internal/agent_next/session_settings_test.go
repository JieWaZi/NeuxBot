package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

type sessionModeSetter struct {
	// AgentClient 提供本测试不调用的原生操作。
	AgentClient
	// set 控制原生模式设置调用的回执与观察顺序。
	set func(context.Context, string, string) error
}

func (c sessionModeSetter) SetSessionMode(ctx context.Context, sessionID, mode string) error {
	return c.set(ctx, sessionID, mode)
}

func TestThreadModeReceiptFollowsNativeObservationOrder(t *testing.T) {
	const (
		nativeID  = "native-session"
		initial   = "agent"
		requested = "read-only"
	)
	for _, tc := range []struct {
		// name 描述模式设置回执与通知的相对顺序。
		name string
		// observeBeforeReceipt 模拟设置调用尚未返回时收到同代模式通知。
		observeBeforeReceipt bool
		// rejected 表示原生拒绝设置请求。
		rejected bool
		// want 是回执处理后的公开模式。
		want string
	}{
		{name: "accepted without mode notification", want: requested},
		{name: "current generation observed before receipt", observeBeforeReceipt: true, want: initial},
		{name: "rejected without mode notification", rejected: true, want: initial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lifecycle := NewThreadLifecycle(nil, nil)
			conn := &providerConnection{}
			state := &threadState{
				connection: conn,
				generation: 2,
				effective: AgentSession{ID: nativeID, Mode: initial, Modes: []AgentChoice{
					{ID: initial}, {ID: requested},
				}},
			}
			turn := &Turn{
				lifecycle: lifecycle, thread: state, ctx: t.Context(), conn: conn,
				binding:   NativeBinding{Session: NativeSessionKey{SessionID: nativeID}},
				effective: state.effective,
			}
			scope := AgentScope{SessionID: nativeID}
			setter := sessionModeSetter{set: func(_ context.Context, id, mode string) error {
				if id != nativeID || mode != requested {
					t.Fatalf("mode setter received session %q and mode %q", id, mode)
				}
				if tc.rejected {
					return status_error.RuntimePolicyUnsupported
				}
				observed := make(chan struct{})
				go func() {
					defer close(observed)
					lifecycle.applySessionNotification(state, 1, scope, AgentEvent{Kind: "mode", Mode: "retired-owner"})
					lifecycle.applySessionNotification(state, 2, scope, AgentEvent{Kind: "settings", Options: []AgentSessionOption{{ID: "model"}}})
					if tc.observeBeforeReceipt {
						lifecycle.applySessionNotification(state, 2, scope, AgentEvent{Kind: "mode", Mode: initial})
					}
				}()
				<-observed
				return nil
			}}
			err := turn.applySettings(setter, ThreadRequest{Mode: requested})
			if tc.rejected && !errors.Is(err, status_error.RuntimePolicyUnsupported) || !tc.rejected && err != nil {
				t.Fatalf("mode setter result: %v", err)
			}
			if got := turn.observedSession().Mode; got != tc.want {
				t.Fatalf("published mode = %q, want %q", got, tc.want)
			}
			lifecycle.applySessionNotification(state, 2, scope, AgentEvent{Kind: "mode", Mode: "provider-later"})
			if got := turn.observedSession().Mode; got != "provider-later" {
				t.Fatalf("published mode after later notification = %q", got)
			}
		})
	}
}

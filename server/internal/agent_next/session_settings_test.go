package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

type sessionModeSetter struct {
	// AgentClient supplies native operations outside the scope of this test.
	AgentClient
	// set controls the order of the native mode receipt and observations.
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
		// name describes the order of the mode receipt and notifications.
		name string
		// observeBeforeReceipt delivers a notification from the active connection before the setter returns.
		observeBeforeReceipt bool
		// rejected reports that the native provider declined the requested mode.
		rejected bool
		// want is the published mode after handling the receipt.
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

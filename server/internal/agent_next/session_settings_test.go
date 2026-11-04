package agent

import (
	"context"
	"errors"
	"reflect"
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

// selectionClient models native menus that change after the model setter's receipt.
type selectionClient struct {
	// AgentClient supplies unrelated operations outside this boundary.
	AgentClient
	// calls records the actual order crossing the native client boundary.
	calls []string
	// options is the latest native receipt, not the host's requested tuple.
	options []AgentSessionOption
}

func (c *selectionClient) SetSessionOption(_ context.Context, _, id string, value AgentOptionValue) ([]AgentSessionOption, error) {
	c.calls = append(c.calls, id+"="+value.ID)
	for i := range c.options {
		if c.options[i].ID == id {
			c.options[i].Value = value
		}
	}
	return cloneOptions(c.options), nil
}

func (c *selectionClient) SetSessionMode(_ context.Context, _, mode string) error {
	c.calls = append(c.calls, "mode="+mode)
	return nil
}

func TestProviderSelectionUsesNativeOptionTypesAndAppliesModeAfterModel(t *testing.T) {
	for _, provider := range []string{"claude", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			fastOn, fastOff := "on", "off"
			if provider == "cursor" {
				fastOn, fastOff = "true", "false"
			}
			client := &selectionClient{options: []AgentSessionOption{
				{ID: "model", Category: "model", Value: AgentOptionValue{ID: "old"}, Choices: []AgentChoice{{ID: "old"}, {ID: "next"}}},
				{ID: "effort", Category: "thought_level", Value: AgentOptionValue{ID: "low"}, Choices: []AgentChoice{{ID: "low"}, {ID: "high"}}},
				{ID: "fast", Category: "model_config", Value: AgentOptionValue{ID: fastOff}, Choices: []AgentChoice{{ID: fastOn}, {ID: fastOff}}},
			}}
			state := &threadState{effective: AgentSession{ID: "native", Mode: "default", Options: cloneOptions(client.options)}}
			turn := &Turn{ctx: t.Context(), lifecycle: NewThreadLifecycle(nil, nil), thread: state,
				binding: NativeBinding{Session: NativeSessionKey{SessionID: "native"}}, effective: state.effective}
			fast := true
			err := turn.applySettings(client, ThreadRequest{
				Connection: ProviderConnectionRequest{Account: AccountRequest{Provider: provider, Model: "next"}},
				Selection:  &TurnSelection{Reasoning: "high", Fast: &fast}, Mode: "auto",
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"model=next", "effort=high", "fast=" + fastOn, "mode=auto"}
			if !reflect.DeepEqual(client.calls, want) {
				t.Fatalf("native calls = %v, want %v", client.calls, want)
			}
			if got := turn.RequestedFast(); got == nil || !*got {
				t.Fatal("accepted Fast intent lost")
			}
		})
	}
}

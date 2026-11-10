package agent_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

func TestThreadRealAccountSteeringAndForkDoNotDiagnose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENAI_API_KEY", "")
	accounts := aiaccount.New(nil)
	req.Connection.Account.AccountID = agent.SystemAccountID
	req.Connection.Account.Executable = req.Connection.Launch.Executable
	req.Connection.Account.PrefixArgs = req.Connection.Launch.PrefixArgs
	selected, err := accounts.ResolveAccount(ctx, req.Connection.Account, home)
	if err != nil {
		t.Fatal(err)
	}
	req.Connection.Account = selected
	req.Connection.Launch.ConfigHome = selected.ConfigHome
	provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: accounts.AcquireAccount})
	lifecycle := agent.NewThreadLifecycle(provider, store)
	t.Cleanup(func() {
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := provider.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	first := chatTurn(0, "hold-native")
	ports := chatPorts(req.ChatID, first, true)
	observed := make(chan struct{}, 1)
	ports.Observe = func(_ context.Context, _ agent.AgentScope, event agent.AgentEvent) error {
		if event.Kind == "text" {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
		return nil
	}
	turn, err := lifecycle.Prepare(ctx, req, first, ports)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan agent.TurnResult, 1)
	go func() { done <- turn.Run(ctx) }()
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	steer := chatTurn(1, "only one steering input")
	receipt, err := turn.Steer(ctx, steer, chatPorts(req.ChatID, steer, false))
	if err != nil || receipt.Outcome != "injected" {
		t.Fatalf("real account steering: %+v %v", receipt, err)
	}
	var result agent.TurnResult
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if result.OperationError != nil || result.StorageError != nil || result.Outcome == nil {
		t.Fatalf("steering terminal: %+v", result)
	}
	fork := req
	fork.ChatID = "private-account-fork"
	fork.Connection.Launch.PrefixArgs = []string{"-test.run=^TestProviderRuntimeChild$", "--"}
	fork.Connection.Launch.Environment = map[string]string{"FIXTURE_PROVIDER": "codex", "FIXTURE_TRACE": trace, "GORACE": "atexit_sleep_ms=0"}
	fork.Connection.Account.PrefixArgs = fork.Connection.Launch.PrefixArgs
	fork.Fork = &agent.AgentForkRequest{SourceSessionID: result.Binding.Session.SessionID, SourceCWD: req.Connection.Account.CWD,
		ReceiptDirectory: t.TempDir(), Position: "source-turn"}
	binding, err := lifecycle.ForkSession(ctx, fork, nil)
	if err != nil || binding.Session.SessionID != "fixture-child" {
		t.Fatalf("real account fork: %+v %v", binding, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Count(string(data), `"method":"turn/steer"`) != 1 || strings.Count(string(data), `"method":"thread/fork"`) != 1 ||
		strings.Contains(string(data), `"method":"account/read"`) {
		t.Fatalf("unexpected account diagnostic or missing native operation: %v\n%s", err, data)
	}
}

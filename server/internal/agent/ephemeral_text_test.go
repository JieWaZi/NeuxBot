package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestEphemeralClaudeLaunchAlwaysDisablesNativeHistory(t *testing.T) {
	original := []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--tools=", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`}
	args, ok := ephemeralClaudeArgs(original)
	if !ok || !slices.Contains(args, "--no-session-persistence") || slices.Contains(original, "--no-session-persistence") {
		t.Fatalf("unexpected private launch arguments: %v", args)
	}
	if _, ok = ephemeralClaudeArgs([]string{"--version"}); !ok {
		t.Fatal("adapter version lookup must still work")
	}
	if _, ok = ephemeralClaudeArgs([]string{"--resume", "old-session"}); ok {
		t.Fatal("ephemeral runner must reject session reuse")
	}
}

func TestEphemeralOutcomeOnlyRetainsUsageFields(t *testing.T) {
	private := NativeResult{FinalMessage: &NativeFinalMessage{ID: "source", Text: "private-answer"}, StopReason: "private-error", ForkPosition: "private-history"}
	safe := ephemeralUsageResult(private)
	if safe.FinalMessage != nil || safe.StopReason != "" || safe.ForkPosition != "" || private.FinalMessage.Text != "private-answer" {
		t.Fatalf("private outcome was not separated: %#v", safe)
	}
}

func TestEphemeralScratchCanBeCreatedAfterStartupRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "review-scratch")
	stop := errors.New("fixture stops before native startup")
	expectResolution := func(context.Context, AccountRequest, string) (AccountRequest, error) {
		return AccountRequest{}, stop
	}
	lifecycle := NewThreadLifecycle(NewProviderRuntime(ProviderRuntimeOptions{}), nil)
	_, err := lifecycle.prepareDetached(t.Context(), ThreadRequest{}, TurnRequest{}, TurnPorts{}, expectResolution, root, nil)
	if !errors.Is(err, stop) {
		t.Fatalf("request could not prepare its scratch root: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed preparation left scratch entries: %v, %v", entries, err)
	}
}

func TestContinuedTextIdleDeadlineOnlyRetiresTheCapturedIdleSeat(t *testing.T) {
	lifecycle := NewThreadLifecycle(NewProviderRuntime(ProviderRuntimeOptions{}), nil)
	owner := &threadState{id: "text:owner", textSession: "owner", textIdleGeneration: 2}
	timer := time.NewTimer(time.Hour)
	owner.textIdle = timer
	t.Cleanup(func() { timer.Stop() })
	lifecycle.threads[owner.id] = owner
	lifecycle.expireTextThread(owner, 1)
	if lifecycle.threads[owner.id] != owner || owner.closing {
		t.Fatal("an older deadline retired the current owner")
	}
	owner.active = &Turn{}
	lifecycle.expireTextThread(owner, 2)
	if lifecycle.threads[owner.id] != owner || owner.closing {
		t.Fatal("the idle deadline retired an active turn")
	}
	owner.active = nil
	lifecycle.expireTextThread(owner, 2)
	if lifecycle.threads[owner.id] != nil {
		t.Fatal("the current idle owner remained indexed")
	}
}

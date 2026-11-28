package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const acpWatchdogApprovalQuestion = "acp-watchdog-approval"

func TestExecutionWatchdogNativeStopPreservesApprovalAndWaitsForReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Second)
	defer cancel()
	t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", "opencode")
	gate := filepath.Join(t.TempDir(), "confirm-stop")
	t.Setenv("NEUX_EXECUTION_FIXTURE_WATCHDOG_GATE", gate)
	fixture := newNativeProductFixture(t, ctx, "opencode", "TestNativeExecutionACPChild", constants.PermissionLevelDefault)
	core, chats, store, closeRuntime := fixture.open()
	defer closeRuntime()
	defer func() { _ = os.WriteFile(gate, nil, 0600) }()
	registry := core.(*service).executions
	options := WorkOptions{RuntimeID: "opencode", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelDefault,
		SessionPolicy: constants.RuntimeSessionPolicyContinue, EntryPoint: MainConversationSourceEntryPoint}
	start := func(target ChatTarget, submission, text string) (AcceptedExecution, error) {
		intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text, SubmissionID: submission}, Options: options}
		if target.Create != nil {
			intent.ClientRequestID = submission
		}
		return core.Start(ctx, intent)
	}
	create := ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}
	stalled, err := start(create, "stalled", acpHeldQuestion)
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductLive(t, ctx, chats, stalled, acpFixturePartial)
	waiting, err := start(create, "waiting", acpWatchdogApprovalQuestion)
	if err != nil {
		t.Fatal(err)
	}
	var approvalID string
	waitWatchdogCondition(t, ctx, "original native approval", func() bool {
		chat, err := chats.LoadExecutionChat(ctx, waiting.Chat.ID)
		if err != nil || len(chat.PendingApprovals) != 1 {
			return false
		}
		approvalID = chat.PendingApprovals[0].ApprovalID
		return true
	})

	// Move the same native observations across the limit without waiting hours.
	for _, work := range []AcceptedExecution{stalled, waiting} {
		attempt := registry.watchdogFor(work.Chat.ID, work.Execution.ID)
		if attempt == nil {
			t.Fatal("accepted prompt was not registered for the watchdog")
		}
		attempt.mutex.Lock()
		attempt.lastProgress = time.Now().Add(-registry.watchdogPolicy.maxQuiet - time.Minute)
		attempt.mutex.Unlock()
	}
	registry.checkWatchdogs()
	registry.checkWatchdogs()
	waitWatchdogCondition(t, ctx, "native cancellation signal", func() bool {
		raw, _ := os.ReadFile(fixture.trace)
		return strings.Count(string(raw), `"method":"session/cancel"`) == 1
	})
	if !core.IsExecutionActive(stalled.Chat.ID, stalled.Execution.ID) || !core.IsExecutionActive(waiting.Chat.ID, waiting.Execution.ID) {
		t.Fatal("a stop request or unrelated wait prematurely released an execution")
	}
	if _, err := start(ChatTarget{ChatID: stalled.Chat.ID}, "before-cancel", acpResumeQuestion); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("second input bypassed the original seat before confirmation: %v", err)
	}
	readStop := func() {
		t.Helper()
		events, err := chats.ListEvents(ctx, stalled.Chat.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, event := range events {
			if event.Type == constants.ChatEventTypeExecutionWatchdogStopRequested {
				count++
				if event.Message == nil || event.Message.Status != constants.ExecutionStatusRunning || event.Message.ErrorCode != status_error.RuntimeWatchdogStall.Key() {
					t.Fatalf("stop request projected premature terminal or lost cause: %+v", event)
				}
			}
		}
		if count != 1 {
			t.Fatalf("stop request count = %d", count)
		}
	}
	readStop()
	history, err := chats.ReadMessageHistory(ctx, stalled.Chat.ID, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[1].ErrorCode != status_error.RuntimeWatchdogStall.Key() || history.Resources[1].Status != constants.ExecutionStatusRunning {
		t.Fatalf("restart of observer lost the pending stop: %+v %v", history, err)
	}
	if err := core.ResolveApproval(ctx, waiting.Chat.ID, approvalID, "native-allow"); err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, waiting, constants.ExecutionStatusCompleted)

	// The next submission waits at the same host seat boundary as the durable input queue.
	nextDone := make(chan executionStartResult, 1)
	go func() {
		if err := core.WaitForChatIdle(ctx, stalled.Chat.ID); err != nil {
			nextDone <- executionStartResult{err: err}
			return
		}
		next, err := start(ChatTarget{ChatID: stalled.Chat.ID}, "queued", acpResumeQuestion)
		nextDone <- executionStartResult{chat: next.Chat, execution: next.Execution, err: err}
	}()
	select {
	case next := <-nextDone:
		t.Fatalf("next input started before the adapter acknowledged stop: %+v", next)
	default:
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, stalled, constants.ExecutionStatusFailed)
	var next executionStartResult
	select {
	case next = <-nextDone:
	case <-ctx.Done():
		t.Fatal("confirmed stop did not release the waiting input")
	}
	if next.err != nil {
		t.Fatal(next.err)
	}
	waitNativeProductStatus(t, ctx, store, AcceptedExecution{Chat: next.chat, Execution: next.execution}, constants.ExecutionStatusCompleted)
	history, err = chats.ReadMessageHistory(ctx, stalled.Chat.ID, 0, 10)
	if err != nil || len(history.Resources) != 4 || history.Resources[1].ErrorCode != status_error.RuntimeWatchdogStall.Key() ||
		history.Resources[1].Status != constants.ExecutionStatusFailed || history.Resources[1].ErrorParams["Confirmed"] != true {
		t.Fatalf("stop confirmation was not projected on the original work: %+v %v", history, err)
	}
	raw, _ := os.ReadFile(fixture.trace)
	if strings.Count(string(raw), acpResumeQuestion) != 1 {
		t.Fatalf("next input did not reach the native prompt exactly once: %s", raw)
	}
}

func waitWatchdogCondition(t *testing.T, ctx context.Context, label string, passed func() bool) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !passed() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", label)
		}
	}
}

func TestExecutionWatchdogChecksProgressAndWaitsForOriginalOwner(t *testing.T) {
	registry, now, stops := newWatchdogTestRegistry(t)
	stalled := reserveWatchdogAttempt(t, registry, "stalled", "a")
	waiting := reserveWatchdogAttempt(t, registry, "waiting", "b")
	progressing := reserveWatchdogAttempt(t, registry, "progressing", "c")
	resume, allowed := registry.pauseWatchdog("waiting", "b")
	if !allowed {
		t.Fatal("live execution rejected its original interaction")
	}

	*now = now.Add(11 * time.Minute)
	registry.observeWatchdog("progressing", "c", agent.AgentEvent{Kind: "text", Text: "working"})
	registry.checkWatchdogs()
	registry.checkWatchdogs()
	if len(*stops) != 1 || (*stops)[0].ExecutionID != "a" || (*stops)[0].ReasonCode != status_error.RuntimeWatchdogStall.Key() {
		t.Fatalf("stop requests = %+v", *stops)
	}
	if !errors.Is(stalled.Err(), context.Canceled) || waiting.Err() != nil || progressing.Err() != nil {
		t.Fatalf("contexts = stalled %v, waiting %v, progressing %v", stalled.Err(), waiting.Err(), progressing.Err())
	}
	if _, err := registry.reserve("stalled", "next", "next-input"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("stop request released the active execution seat: %v", err)
	}
	registry.finishWatchdog("stalled", "a")
	registry.complete("stalled", "a", nil)
	if _, err := registry.reserve("stalled", "next", "next-input"); err != nil {
		t.Fatalf("confirmed terminal kept the seat: %v", err)
	}
	registry.release("stalled", "next")
	*now = now.Add(2 * time.Hour)
	resume()
	registry.observeWatchdog("progressing", "c", agent.AgentEvent{Kind: "text", Text: "still working"})
	registry.checkWatchdogs()
	if waiting.Err() != nil {
		t.Fatalf("approval wait counted as running time after resolution: %v", waiting.Err())
	}
}

func TestExecutionWatchdogTriggersIndependentLimitsOnNativeEvidence(t *testing.T) {
	usage, reasoning := int64(50), int64(45)
	tool := func(id, args string, update bool) agent.AgentEvent {
		title := "shell command"
		return agent.AgentEvent{Kind: "tool", Tool: &agent.AgentTool{ID: id, Title: &title, Input: json.RawMessage(args), Update: update}}
	}
	tests := []struct {
		// name identifies the independent limit or evidence boundary.
		name string
		// events are scoped native observations for one execution.
		events []agent.AgentEvent
		// elapsed is the simulated running time before the scan.
		elapsed time.Duration
		// reason is the only expected stop code, or empty to continue.
		reason string
	}{
		{name: "elapsed despite fresh text", events: []agent.AgentEvent{{Kind: "text", Text: "working"}}, elapsed: 2 * time.Hour, reason: status_error.RuntimeWatchdogElapsed.Key()},
		{name: "same arguments", events: []agent.AgentEvent{tool("one", `{"a":1,"b":2}`, false), tool("two", `{ "b":2,"a":1 }`, false), tool("three", `{"a":1,"b":2}`, false)}, reason: status_error.RuntimeWatchdogToolLoop.Key()},
		{name: "updates are not another tool call", events: []agent.AgentEvent{tool("one", `{"a":1}`, false), tool("one", `{"a":1}`, true), tool("one", `{"a":1}`, true)}},
		{name: "different arguments", events: []agent.AgentEvent{tool("one", `{"a":1}`, false), tool("two", `{"a":2}`, false), tool("three", `{"a":1}`, false)}},
		{name: "replaceable budget snapshots", events: []agent.AgentEvent{{Kind: "usage", Usage: &acpmeta.TurnUsage{InputTokens: &usage, OutputTokens: &usage}}, {Kind: "usage", Usage: &acpmeta.TurnUsage{InputTokens: &usage, OutputTokens: &usage}}}, reason: status_error.RuntimeWatchdogTokenBudget.Key()},
		{name: "reasoning is a subset", events: []agent.AgentEvent{{Kind: "usage", Usage: &acpmeta.TurnUsage{OutputTokens: &usage, ReasoningTokens: &reasoning}}, {Kind: "usage", Usage: &acpmeta.TurnUsage{OutputTokens: &usage, ReasoningTokens: &reasoning}}}},
		{name: "context size is not consumption", events: []agent.AgentEvent{{Kind: "usage", ContextUsedTokens: &usage, ContextWindowTokens: &usage}}},
		{name: "mode is not progress", events: []agent.AgentEvent{{Kind: "mode", Mode: "default"}}, elapsed: 11 * time.Minute, reason: status_error.RuntimeWatchdogStall.Key()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, now, stops := newWatchdogTestRegistry(t)
			reserveWatchdogAttempt(t, registry, "chat", "execution")
			*now = now.Add(test.elapsed)
			for _, event := range test.events {
				registry.observeWatchdog("chat", "execution", event)
			}
			registry.checkWatchdogs()
			actual := ""
			if len(*stops) == 1 {
				actual = (*stops)[0].ReasonCode
			}
			if len(*stops) > 1 || actual != test.reason {
				t.Fatalf("requests = %+v, expected %q", *stops, test.reason)
			}
		})
	}
}

func TestExecutionWatchdogRechecksCandidateAfterScan(t *testing.T) {
	for _, change := range []string{"progress", "approval", "terminal", "replacement", "manual stop", "ticket moved"} {
		t.Run(change, func(t *testing.T) {
			registry, now, stops := newWatchdogTestRegistry(t)
			original := reserveWatchdogAttempt(t, registry, "chat", "old")
			*now = now.Add(11 * time.Minute)
			candidates := registry.watchdogCandidates()
			if len(candidates) != 1 {
				t.Fatalf("candidates = %+v", candidates)
			}
			switch change {
			case "progress":
				registry.observeWatchdog("chat", "old", agent.AgentEvent{Kind: "text", Text: "fresh"})
			case "approval":
				resume, _ := registry.pauseWatchdog("chat", "old")
				defer resume()
			case "terminal":
				registry.finishWatchdog("chat", "old")
			case "replacement":
				registry.finishWatchdog("chat", "old")
				registry.complete("chat", "old", nil)
				replacement := reserveWatchdogAttempt(t, registry, "chat", "new")
				*now = now.Add(11 * time.Minute)
				defer func() {
					if replacement.Err() != nil {
						t.Fatalf("stale observation canceled replacement: %v", replacement.Err())
					}
				}()
			case "manual stop":
				registry.cancel("chat")
			case "ticket moved":
				candidates[0].guard = func(context.Context, func() error) error {
					return status_error.WorkTicketAttemptUnavailable
				}
			}
			registry.requestWatchdogStop(candidates[0])
			if len(*stops) != 0 || (change != "manual stop" && original.Err() != nil) {
				t.Fatalf("stale candidate triggered a stop: %+v, %v", *stops, original.Err())
			}
		})
	}
}

func newWatchdogTestRegistry(t *testing.T) (*executionRegistry, *time.Time, *[]eventlog.WatchdogStopRequestedData) {
	t.Helper()
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	var stops []eventlog.WatchdogStopRequestedData
	registry := newExecutionRegistry()
	registry.watchdogPolicy = watchdogPolicy{interval: time.Hour, maxElapsed: time.Hour, maxQuiet: 10 * time.Minute, maxToolQuiet: 30 * time.Minute, maxRepeatedTools: 3, maxTokens: 100}
	registry.watchdogNow = func() time.Time { return now }
	registry.recordWatchdogStop = func(_ string, stop eventlog.WatchdogStopRequestedData) error {
		stops = append(stops, stop)
		return nil
	}
	registry.start(t.Context())
	t.Cleanup(registry.close)
	return registry, &now, &stops
}

func reserveWatchdogAttempt(t *testing.T, registry *executionRegistry, chatID, executionID string) context.Context {
	t.Helper()
	ctx, err := registry.reserve(chatID, executionID, executionID+"-input")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.release(chatID, executionID) })
	registry.startWatchdog(chatID, executionID, nil)
	return ctx
}

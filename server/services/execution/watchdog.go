package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

type watchdogPolicy struct {
	// interval controls how often the host checks in-flight work.
	interval time.Duration
	// maxElapsed limits running time, excluding waits for a user decision.
	maxElapsed time.Duration
	// maxQuiet limits silence between meaningful observations.
	maxQuiet time.Duration
	// maxToolQuiet allows a longer quiet period while the CLI reports an open tool.
	maxToolQuiet time.Duration
	// maxRepeatedTools limits consecutive calls with an identical tool and input.
	maxRepeatedTools int
	// maxTokens bounds known, nonoverlapping invocation consumption.
	maxTokens int64
}

func defaultWatchdogPolicy() watchdogPolicy {
	return watchdogPolicy{interval: 15 * time.Second, maxElapsed: 45 * time.Minute, maxQuiet: 12 * time.Minute,
		maxToolQuiet: 30 * time.Minute, maxRepeatedTools: 4, maxTokens: 2_000_000}
}

type watchdogAttempt struct {
	// mutex orders observations, waiting transitions, stop persistence and native return.
	mutex sync.Mutex
	// chatID is the exact Chat holding this native invocation.
	chatID string
	// executionID identifies the original execution and its bound ticket attempt, if present.
	executionID string
	// context is the execution reservation context, including manual cancellation.
	context context.Context
	// cancel is the reservation's exact native cancellation path.
	cancel context.CancelFunc
	// guard protects the matching durable work-ticket attempt where one exists.
	guard func(context.Context, func() error) error
	// started establishes the admitted invocation's running-time baseline.
	started time.Time
	// lastProgress tracks the last meaningful native or interaction observation.
	lastProgress time.Time
	// waitStarted marks the start of the current overlapping user waits.
	waitStarted time.Time
	// paused accumulates the duration of resolved user waits.
	paused time.Duration
	// waiting counts simultaneous interactions for this invocation.
	waiting int
	// finished makes a returned invocation ineligible before business settlement.
	finished bool
	// requested preserves the first durable stop request until terminal settlement.
	requested eventlog.WatchdogStopRequestedData
	// tokens holds the greatest known subtotal from one replaceable usage snapshot.
	tokens int64
	// planFingerprint ignores duplicate plan snapshots.
	planFingerprint [sha256.Size]byte
	// toolSignature stores the fingerprint of the most recent complete tool call.
	toolSignature [sha256.Size]byte
	// repeatedTools counts consecutive distinct tool calls with that fingerprint.
	repeatedTools int
	// tools retains recent update fingerprints and open state by native call ID.
	tools map[string]watchdogTool
	// toolOrder bounds the retained tool observations.
	toolOrder []string
}

type watchdogTool struct {
	// fingerprint identifies the most recent substantive update for this call.
	fingerprint [sha256.Size]byte
	// open reports explicit pending or in-progress native status.
	open bool
}

// startWatchdog enables observation only after the journal has admitted the native prompt.
func (r *executionRegistry) startWatchdog(chatID, executionID string, guard func(context.Context, func() error) error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active := r.active[chatID]
	if active.executionID != executionID || active.watchdog != nil {
		return
	}
	now := r.watchdogNow()
	active.watchdog = &watchdogAttempt{chatID: chatID, executionID: executionID, context: active.context, cancel: active.cancel,
		guard: guard, started: now, lastProgress: now, tools: make(map[string]watchdogTool)}
	r.active[chatID] = active
}

func (r *executionRegistry) watchdogFor(chatID, executionID string) *watchdogAttempt {
	if r == nil {
		return nil
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active := r.active[chatID]
	if active.executionID != executionID {
		return nil
	}
	return active.watchdog
}

func (r *executionRegistry) observeWatchdog(chatID, executionID string, event agent.AgentEvent) {
	attempt := r.watchdogFor(chatID, executionID)
	if attempt == nil {
		return
	}
	attempt.mutex.Lock()
	defer attempt.mutex.Unlock()
	if attempt.finished || attempt.requested.ReasonCode != "" {
		return
	}
	var progress bool
	switch event.Kind {
	case "text", "reasoning":
		progress = strings.TrimSpace(event.Text) != "" || event.Content != nil
	case "tool":
		progress = attempt.observeTool(event.Tool)
	case "plan":
		if event.Plan != nil {
			encoded, err := json.Marshal(event.Plan)
			fingerprint := sha256.Sum256(encoded)
			if err == nil && fingerprint != attempt.planFingerprint {
				attempt.planFingerprint = fingerprint
				progress = true
			}
		}
	case "usage":
		tokens := knownWatchdogTokens(usage.ReadTokens(event.Usage))
		progress = tokens > attempt.tokens
		attempt.tokens = max(attempt.tokens, tokens)
	}
	if progress {
		attempt.lastProgress = r.watchdogNow()
	}
}

func knownWatchdogTokens(tokens usage.Tokens) int64 {
	counters := []*int64{tokens.Input, tokens.CacheWrite, tokens.CacheRead, tokens.Output}
	if tokens.Input == nil && tokens.ReportedInput != nil {
		// The provider's aggregate input may already include cache traffic.
		counters = []*int64{tokens.ReportedInput, tokens.Output}
	}
	total := int64(0)
	for _, counter := range counters {
		if counter == nil {
			continue
		}
		if *counter > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += *counter
	}
	return total
}

func (a *watchdogAttempt) observeTool(tool *agent.AgentTool) bool {
	if tool == nil || tool.ID == "" {
		return false
	}
	if tool.Update && tool.Status == nil && len(tool.Input) == 0 && len(tool.Output) == 0 && len(tool.Content) == 0 && tool.TerminalDelta == "" {
		return false
	}
	state, known := a.tools[tool.ID]
	encoded, err := json.Marshal(tool)
	if err != nil {
		return false
	}
	fingerprint := sha256.Sum256(encoded)
	progress := !known || fingerprint != state.fingerprint || tool.TerminalDelta != ""
	if tool.Status != nil {
		state.open = *tool.Status == "pending" || *tool.Status == "in_progress"
	}
	state.fingerprint = fingerprint
	if !known {
		const retainedTools = 128
		if len(a.toolOrder) == retainedTools {
			delete(a.tools, a.toolOrder[0])
			a.toolOrder = a.toolOrder[1:]
		}
		a.toolOrder = append(a.toolOrder, tool.ID)
		if !tool.Update {
			signature, ok := watchdogToolSignature(tool)
			switch {
			case !ok:
				a.repeatedTools = 0
			case a.toolSignature == signature:
				a.repeatedTools++
			default:
				a.toolSignature, a.repeatedTools = signature, 1
			}
		}
	}
	a.tools[tool.ID] = state
	return progress
}

func watchdogToolSignature(tool *agent.AgentTool) ([sha256.Size]byte, bool) {
	var input any
	decoder := json.NewDecoder(bytes.NewReader(tool.Input))
	decoder.UseNumber()
	if decoder.Decode(&input) != nil {
		return [sha256.Size]byte{}, false
	}
	title, kind := "", ""
	if tool.Title != nil {
		title = *tool.Title
	}
	if tool.Kind != nil {
		kind = *tool.Kind
	}
	if title == "" && kind == "" {
		return [sha256.Size]byte{}, false
	}
	encoded, err := json.Marshal([]any{kind, title, input})
	return sha256.Sum256(encoded), err == nil
}

// pauseWatchdog enters the user wait before the request becomes visible. The same lock
// decides whether the wait or a competing stop request owns the interaction.
func (r *executionRegistry) pauseWatchdog(chatID, executionID string) (func(), bool) {
	attempt := r.watchdogFor(chatID, executionID)
	if attempt == nil {
		return func() {}, true
	}
	attempt.mutex.Lock()
	defer attempt.mutex.Unlock()
	if attempt.finished || attempt.requested.ReasonCode != "" || attempt.context.Err() != nil {
		return func() {}, false
	}
	if attempt.waiting == 0 {
		attempt.waitStarted = r.watchdogNow()
	}
	attempt.waiting++
	return func() {
		attempt.mutex.Lock()
		defer attempt.mutex.Unlock()
		attempt.waiting--
		if attempt.waiting == 0 {
			now := r.watchdogNow()
			attempt.paused += now.Sub(attempt.waitStarted)
			attempt.lastProgress = now
			attempt.repeatedTools = 0
		}
	}, true
}

func (r *executionRegistry) finishWatchdog(chatID, executionID string) eventlog.WatchdogStopRequestedData {
	attempt := r.watchdogFor(chatID, executionID)
	if attempt == nil {
		return eventlog.WatchdogStopRequestedData{}
	}
	attempt.mutex.Lock()
	defer attempt.mutex.Unlock()
	attempt.finished = true
	return attempt.requested
}

func (r *executionRegistry) watchExecutions(ctx context.Context) {
	ticker := time.NewTicker(r.watchdogPolicy.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkWatchdogs()
		}
	}
}

func (r *executionRegistry) checkWatchdogs() {
	for _, attempt := range r.watchdogCandidates() {
		r.requestWatchdogStop(attempt)
	}
}

func (r *executionRegistry) watchdogCandidates() []*watchdogAttempt {
	r.mutex.Lock()
	active := make([]*watchdogAttempt, 0, len(r.active))
	for _, execution := range r.active {
		if execution.watchdog != nil {
			active = append(active, execution.watchdog)
		}
	}
	r.mutex.Unlock()
	now := r.watchdogNow()
	candidates := active[:0]
	for _, attempt := range active {
		attempt.mutex.Lock()
		reason := attempt.stopReason(now, r.watchdogPolicy)
		attempt.mutex.Unlock()
		if reason != "" {
			candidates = append(candidates, attempt)
		}
	}
	return candidates
}

func (a *watchdogAttempt) stopReason(now time.Time, policy watchdogPolicy) string {
	if a.finished || a.waiting > 0 || a.context.Err() != nil || a.requested.ReasonCode != "" {
		return ""
	}
	if now.Sub(a.started)-a.paused >= policy.maxElapsed {
		return status_error.RuntimeWatchdogElapsed.Key()
	}
	quiet := policy.maxQuiet
	for _, tool := range a.tools {
		if tool.open {
			quiet = policy.maxToolQuiet
			break
		}
	}
	if now.Sub(a.lastProgress) >= quiet {
		return status_error.RuntimeWatchdogStall.Key()
	}
	if a.repeatedTools >= policy.maxRepeatedTools {
		return status_error.RuntimeWatchdogToolLoop.Key()
	}
	if a.tokens >= policy.maxTokens {
		return status_error.RuntimeWatchdogTokenBudget.Key()
	}
	return ""
}

func (r *executionRegistry) requestWatchdogStop(attempt *watchdogAttempt) {
	if r.recordWatchdogStop == nil || r.watchdogFor(attempt.chatID, attempt.executionID) != attempt {
		return
	}
	attempt.mutex.Lock()
	defer attempt.mutex.Unlock()
	if attempt.stopReason(r.watchdogNow(), r.watchdogPolicy) == "" {
		return
	}
	stop := func() error {
		now := r.watchdogNow()
		reason := attempt.stopReason(now, r.watchdogPolicy)
		if reason == "" {
			return nil
		}
		fact := eventlog.WatchdogStopRequestedData{ExecutionID: attempt.executionID, ReasonCode: reason,
			RequestedAt: now.UnixMilli(), LastProgressAt: attempt.lastProgress.UnixMilli()}
		if err := r.recordWatchdogStop(attempt.chatID, fact); err != nil {
			return err
		}
		attempt.requested = fact
		// Only the original reservation accepts this signal. The native owner retains
		// the seat until its outcome (or transport retirement) is settled.
		attempt.cancel()
		return nil
	}
	var err error
	if attempt.guard == nil {
		err = stop()
	} else {
		err = attempt.guard(attempt.context, stop)
	}
	if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		attempt.finished = true
	} else if err != nil {
		logx.Warnf("record execution watchdog stop: chat_id=%s execution_id=%s error=%v", attempt.chatID, attempt.executionID, err)
	}
}

func watchdogExecutionOutcome(outcome nativeExecutionStatus, result agent.TurnResult, stop eventlog.WatchdogStopRequestedData) nativeExecutionStatus {
	if stop.ReasonCode == "" || outcome.status == constants.ExecutionStatusCompleted {
		return outcome
	}
	confirmed := result.Outcome != nil && result.Outcome.Turn.Status == acpmeta.NativeTurnCancelled
	if confirmed || outcome.status == constants.ExecutionStatusCanceled || result.Outcome == nil {
		outcome.status = constants.ExecutionStatusFailed
		outcome.code = stop.ReasonCode
		outcome.messageSource = constants.ErrorMessageSourceLocalized
		outcome.message = ""
		outcome.params = map[string]interface{}{"Confirmed": confirmed, "Settled": true}
	}
	return outcome
}

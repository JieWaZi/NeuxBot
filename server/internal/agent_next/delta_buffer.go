package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const (
	deltaBudget        = 8 << 10
	deltaDelay         = 2 * time.Second
	liveQueueSize      = 64
	maxObservedBytes   = 32 << 10
	maxCorrectionBytes = 512 << 10
	maxTrackedTools    = 64
)

// deltaTarget selects the durable native stream and its optional public reference.
type deltaTarget struct {
	// store is the shared authority for the native and public streams.
	store ConversationStore
	// session freezes the actual producer's provider, subject and session.
	session NativeSessionKey
	// chatID is present only for an admitted host-owned message.
	chatID string
	// hostID identifies the host-owned delivery lane when present.
	hostID string
	// messageID identifies the public assistant message when present.
	messageID string
	// executionID scopes the public reference to its business execution.
	executionID string
	// committed announces only confirmed public writes.
	committed func(CommitReceipt)
}

type toolSnapshot struct {
	// AgentTool contains the most recent values of reported snapshot fields.
	AgentTool
	// TerminalOutput accumulates independent terminal bytes in native order.
	TerminalOutput string `json:"terminalOutput,omitempty"`
}

// DeltaBuffer owns the current bounded segment and its exact pending commit.
type DeltaBuffer struct {
	// mu serializes observations, timed flushes and settlement for this one Turn.
	mu sync.Mutex
	// closed excludes any observation or timer after final settlement.
	closed bool
	// timer bounds the age of uncommitted content without rewriting earlier fragments.
	timer *time.Timer
	// timerGeneration excludes a timer fired before a later segment was opened.
	timerGeneration uint64
	// chatVersion is the last committed public cursor observed by this buffer.
	chatVersion int64
	// revision is the most recent successfully committed operation.
	revision int64
	// key identifies the native invocation owning the pending segment.
	key NativeResultKey
	// pending is the current operation, retained across failed writes.
	pending MessageChange
	// text accumulates only the bytes missing from the durable stream.
	text strings.Builder
	// retry preserves the current segment byte-for-byte after a partial flush.
	retry bool
	// tools retains the latest bounded tool state needed to merge partial native updates.
	tools map[string]toolSnapshot
	// failure records the first durable or normalization error.
	failure error
}

func (b *DeltaBuffer) observe(observation context.Context, target deltaTarget, scope AgentScope, event AgentEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	change, ok, err := b.messageChange(event)
	if !ok && err == nil {
		return nil
	}
	if err != nil {
		b.failure = errors.Join(b.failure, err)
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(observation), turnSettlementTimeout)
	defer cancel()
	key := NativeResultKey{Session: target.session, InvocationID: scope.NativeInvocationID}
	if change.MessageID == "" {
		// Unidentified payloads share a lane only within this proven native invocation.
		change.MessageID = key.InvocationID + ":" + event.Kind
	}
	if b.retry || change.Kind != MessageAppend || key != b.key ||
		change.MessageID != b.pending.MessageID || change.ContentType != b.pending.ContentType {
		if err := b.flush(ctx, target); err != nil {
			return err
		}
	}
	knownPhase := event.Kind == "text" && event.Message != nil && event.Message.Complete &&
		event.Message.Source == acpmeta.MessageSourceNativePhase && event.Message.Phase != acpmeta.MessagePhaseUnknown
	if change.Kind != MessageAppend {
		b.pending, b.key = change, key
		return b.flush(ctx, target)
	}
	text := change.Text
	change.Text = ""
	for len(text) > 0 {
		b.pending, b.key = change, key
		n := fragmentPrefixBytes(text, deltaBudget-b.text.Len())
		b.text.WriteString(text[:n])
		text = text[n:]
		if len(text) > 0 || b.text.Len() == deltaBudget {
			if err := b.flush(ctx, target); err != nil {
				return err
			}
		}
	}
	if event.Message != nil && event.Message.Complete {
		if err := b.flush(ctx, target); err != nil {
			return err
		}
		if knownPhase {
			return b.classify(ctx, target, key, change)
		}
		return nil
	}
	if b.pending.Kind != "" && b.timer == nil {
		generation := b.timerGeneration
		b.timer = time.AfterFunc(deltaDelay, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.closed || b.timerGeneration != generation {
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(observation), turnSettlementTimeout)
			defer cancel()
			_ = b.flush(ctx, target)
		})
	}
	return nil
}

func fragmentPrefixBytes(text string, budget int) int {
	n := min(len(text), budget)
	for n < len(text) && !utf8.RuneStart(text[n]) {
		n--
	}
	return n
}

func (b *DeltaBuffer) classify(ctx context.Context, target deltaTarget, key NativeResultKey, text MessageChange) error {
	// Native completion can classify text after earlier chunks passed a
	// commit boundary. The metadata applies to the whole exact lane.
	b.pending = MessageChange{MessageID: text.MessageID, Kind: MessageClassify, ContentType: text.ContentType}
	b.key = key
	return b.flush(ctx, target)
}

func (b *DeltaBuffer) mergeTool(update AgentTool) ([]byte, error) {
	tool, known := b.tools[update.ID]
	terminal := update.Status != nil && (*update.Status == "completed" || *update.Status == "failed" || *update.Status == "cancelled")
	if !known && !terminal && len(b.tools) >= maxTrackedTools {
		return nil, status_error.RuntimeEventQueueOverflow
	}
	tool.ID = update.ID
	if update.Kind != nil {
		tool.Kind = update.Kind
	}
	if update.Title != nil {
		tool.Title = update.Title
	}
	if update.Status != nil {
		tool.Status = update.Status
	}
	if update.Input != nil {
		tool.Input = update.Input
	}
	if update.Output != nil {
		tool.Output = update.Output
	}
	if update.Content != nil {
		tool.Content = update.Content
	}
	if update.Locations != nil {
		tool.Locations = update.Locations
	}
	tool.TerminalOutput += update.TerminalDelta
	data, err := json.Marshal(tool)
	if err != nil {
		return nil, status_error.RuntimeEventEncodingFailed
	}
	if len(data) > maxObservedBytes {
		return nil, status_error.RuntimeEventQueueOverflow
	}
	if terminal {
		delete(b.tools, tool.ID)
	} else {
		if b.tools == nil {
			b.tools = make(map[string]toolSnapshot)
		}
		b.tools[tool.ID] = tool
	}
	return data, nil
}

func (b *DeltaBuffer) messageChange(event AgentEvent) (MessageChange, bool, error) {
	change := MessageChange{MessageID: event.MessageID}
	switch event.Kind {
	case "text":
		change.ContentType = constants.RuntimeEventTypeTextDelta
		if event.Message != nil && event.Message.Phase == acpmeta.MessagePhaseCommentary {
			change.ContentType = constants.RuntimeEventTypeCommentaryDelta
		}
	case "reasoning":
		change.ContentType = constants.RuntimeEventTypeThinkingDelta
	case "tool":
		if event.Tool == nil || event.Tool.ID == "" {
			return MessageChange{}, false, status_error.RuntimeProtocolIncompatible
		}
		change.Kind, change.ContentType = MessageToolSnapshot, constants.RuntimeEventTypeToolUpdate
		change.ToolCallID = event.Tool.ID
		if change.MessageID == "" {
			change.MessageID = event.Tool.ID
		}
		data, err := b.mergeTool(*event.Tool)
		if err != nil {
			return MessageChange{}, false, err
		}
		change.Data = data
		return change, true, nil
	default:
		return MessageChange{}, false, nil
	}
	if event.Kind == "text" && event.Message != nil && event.Message.ReplacementText != nil {
		if len(*event.Message.ReplacementText) > maxCorrectionBytes {
			return MessageChange{}, false, status_error.RuntimeEventQueueOverflow
		}
		change.Kind, change.Text = MessageReplace, *event.Message.ReplacementText
		if event.Message.Phase == acpmeta.MessagePhaseUnknown {
			change.ContentType = ""
		}
		return change, true, nil
	}
	if event.Content != nil {
		data, err := json.Marshal(event.Content)
		if err != nil {
			return MessageChange{}, false, status_error.RuntimeEventEncodingFailed
		}
		if len(data) > maxObservedBytes {
			return MessageChange{}, false, status_error.RuntimeEventQueueOverflow
		}
		change.Kind, change.Data = MessageEvent, data
		return change, true, nil
	}
	change.Kind, change.Text = MessageAppend, event.Text
	return change, true, nil
}

func (b *DeltaBuffer) flush(ctx context.Context, target deltaTarget) error {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
		b.timerGeneration++
	}
	if b.pending.Kind == "" {
		return nil
	}
	change := b.pending
	if change.Kind == MessageAppend {
		change.Text = b.text.String()
		if change.Text == "" {
			b.pending = MessageChange{}
			return nil
		}
	}
	messageID := change.MessageID
	change.MessageID = ""
	change.Revision = b.revision + 1
	changes := []MessageChange{change}
	if change.Kind == MessageReplace && len(change.Text) > deltaBudget {
		// Commit one whole correction atomically. Each appended record is
		// bounded, and an interrupted retry uses the same original body.
		n := fragmentPrefixBytes(change.Text, deltaBudget)
		changes[0].Text = change.Text[:n]
		for remainder := change.Text[n:]; len(remainder) > 0; {
			n := fragmentPrefixBytes(remainder, deltaBudget)
			changes = append(changes, MessageChange{Kind: MessageAppend, Text: remainder[:n],
				ContentType: change.ContentType, Revision: change.Revision + int64(len(changes))})
			remainder = remainder[n:]
		}
	}
	revision := changes[len(changes)-1].Revision
	commitKey := stableTurnKey("fragment", target.hostID, b.key.InvocationID, strconv.FormatInt(change.Revision, 10))
	rng, err := target.store.CommitNativeFragments(ctx, NativeFragmentBatch{
		Key: b.key, CommitKey: commitKey, MessageID: messageID, Changes: changes,
	})
	if err != nil {
		b.retry = true
		b.failure = err
		return err
	}
	if target.messageID != "" {
		receipt, err := commitConversation(ctx, target.store, ConversationBatch{
			ChatID: target.chatID, Key: commitKey, ExpectedVersion: b.chatVersion, Changes: []ConversationChange{{Message: &MessageChange{
				MessageID: target.messageID, ExecutionID: target.executionID,
				Kind: MessageNativeFragment, Fragments: &rng, Revision: revision,
			}}},
		})
		if err != nil {
			b.retry = true
			b.failure = err
			return err
		}
		b.chatVersion = receipt.Version
		if target.committed != nil {
			target.committed(receipt)
		}
	}
	b.revision = revision
	b.retry = false
	b.pending = MessageChange{}
	b.text.Reset()
	return nil
}

func (b *DeltaBuffer) finish(ctx context.Context, target deltaTarget) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return errors.Join(b.failure, b.flush(ctx, target))
}

// pendingWrite reports whether a failed segment still needs an exact retry.
func (b *DeltaBuffer) pendingWrite() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending.Kind != ""
}

type liveEvent struct {
	// scope is the exact source captured at observation.
	scope AgentScope
	// event is the transient normalized observation.
	event AgentEvent
}

type liveTurnObserver struct {
	// mu guards the queue and its first delivery failure.
	mu sync.Mutex
	// queue holds a bounded ordered set of transient observations.
	queue chan liveEvent
	// done closes when delivery finishes or stops after a failure.
	done chan struct{}
	// cancel stops delivery independently of native execution.
	cancel context.CancelFunc
	// err records the first delivery failure.
	err error
}

func (o *liveTurnObserver) start(ctx context.Context, observe func(context.Context, AgentScope, AgentEvent) error) {
	if observe == nil {
		return
	}
	ctx, o.cancel = context.WithCancel(ctx)
	queue, done := make(chan liveEvent, liveQueueSize), make(chan struct{})
	o.queue, o.done = queue, done
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case next, ok := <-queue:
				if !ok || ctx.Err() != nil {
					return
				}
				if err := observe(ctx, next.scope, next.event); err != nil {
					o.mu.Lock()
					if o.err == nil {
						o.err = err
					}
					o.mu.Unlock()
					return
				}
			}
		}
	}()
}

func (o *liveTurnObserver) offer(scope AgentScope, event AgentEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.queue == nil || o.err != nil {
		return
	}
	if len(event.Text) > maxObservedBytes ||
		event.Message != nil && event.Message.ReplacementText != nil && len(*event.Message.ReplacementText) > maxObservedBytes {
		o.err = status_error.RuntimeEventQueueOverflow
		o.cancel()
		return
	}
	select {
	case o.queue <- liveEvent{scope: scope, event: event}:
	default:
		o.err = status_error.RuntimeEventQueueOverflow
		o.cancel()
	}
}

func (o *liveTurnObserver) finish(ctx context.Context) error {
	o.mu.Lock()
	if o.queue == nil {
		o.mu.Unlock()
		return nil
	}
	close(o.queue)
	o.queue = nil
	o.mu.Unlock()
	defer o.cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-o.done:
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.err
	}
}

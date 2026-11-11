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
)

// deltaTarget selects the durable native stream and its optional public reference.
type deltaTarget struct {
	// store is the authoritative store shared by native and public message streams.
	store ConversationStore
	// session freezes the actual producer's provider, account, and native session.
	session NativeSessionKey
	// chatID identifies the admitted public chat.
	chatID string
	// hostID identifies host-submitted input.
	hostID string
	// messageID identifies the associated public assistant message.
	messageID string
	// executionID identifies the business execution owning the message.
	executionID string
	// committed notifies successfully written public records.
	committed func(CommitReceipt)
	// appendLive delivers normalized text fragments before persistence.
	appendLive func(MessageChange)
	// checkpointLive delivers persisted coverage and tail changes.
	checkpointLive func(context.Context, CommitReceipt, MessageChange)
}

type pendingToolFrame struct {
	// scope freezes the producer of the unfinished native frame.
	scope AgentScope
	// event retains message identity and unconsumed terminal bytes from one frame.
	event AgentEvent
	// tool retains fields not yet accepted into the pending durable operation.
	tool AgentTool
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
	// toolFrame retains at most one bounded native frame across a segmented write failure.
	toolFrame *pendingToolFrame
	// failure records the first durable or normalization error.
	failure error
}

func (b *DeltaBuffer) observe(observation context.Context, target deltaTarget, scope AgentScope, event AgentEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	if err := b.drainToolFrame(observation, target); err != nil {
		return err
	}
	if event.Kind == "tool" && event.Tool != nil && len(event.Tool.TerminalDelta) > deltaBudget {
		b.toolFrame = &pendingToolFrame{scope: scope, event: event, tool: *event.Tool}
		b.toolFrame.event.Tool = nil
		return b.drainToolFrame(observation, target)
	}
	return b.observeLocked(observation, target, scope, event)
}

func (b *DeltaBuffer) drainToolFrame(ctx context.Context, target deltaTarget) error {
	for b.toolFrame != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.flush(ctx, target); err != nil {
			return err
		}
		frame := b.toolFrame
		event := frame.event
		event.Tool = &frame.tool
		change, _, err := b.messageChange(event)
		if err != nil {
			return err
		}
		message := change.MessageID
		change.MessageID = ""
		change.Revision = b.revision + 1
		key := NativeResultKey{Session: target.session, InvocationID: frame.scope.NativeInvocationID}
		prepared, err := target.store.PrepareNativeToolUpdate(ctx, NativeFragmentBatch{
			Key: key, CommitKey: stableTurnKey("fragment", target.hostID, key.InvocationID, strconv.FormatInt(change.Revision, 10)),
			MessageID: message, Changes: []MessageChange{change},
		})
		if err != nil {
			return err
		}
		change = prepared.Changes[0]
		n := len(change.Text)
		change.MessageID = message
		b.pending, b.key = change, key
		// Freeze the accepted logical operation before releasing any original
		// bytes. Its exact pending batch survives a native/public partial write.
		remaining := frame.tool.TerminalDelta[n:]
		if remaining == "" {
			b.toolFrame = nil
		} else {
			frame.tool = AgentTool{ID: frame.tool.ID, Update: true, Status: frame.tool.Status, TerminalDelta: remaining}
		}
		if err := b.flush(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (b *DeltaBuffer) observeLocked(observation context.Context, target deltaTarget, scope AgentScope, event AgentEvent) error {
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
	if change.Kind == MessageToolUpdate {
		if b.retry || b.key != key || b.pending.Kind != MessageToolUpdate || b.pending.MessageID != change.MessageID ||
			b.pending.ToolCallID != change.ToolCallID {
			if err := b.flush(ctx, target); err != nil {
				return err
			}
		}
		if b.pending.Kind == MessageToolUpdate {
			data, err := mergePendingTool(b.pending.Data, change.Data)
			if err != nil {
				return err
			}
			if len(data)+len(b.pending.Text)+len(change.Text) > deltaBudget {
				if err := b.flush(ctx, target); err != nil {
					return err
				}
			} else {
				change.Data, change.Text = data, b.pending.Text+change.Text
			}
		}
		b.pending, b.key = change, key
		// Announcements and lifecycle changes remain immediate boundaries.
		if !event.Tool.Update || event.Tool.Status != nil || len(change.Data)+len(change.Text) >= deltaBudget {
			return b.flush(ctx, target)
		}
		b.armTimer(observation, target)
		return nil
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
		n := fragmentPrefixBytes(text, deltaBudget-b.text.Len())
		if n == 0 {
			if err := b.flush(ctx, target); err != nil {
				return err
			}
			continue
		}
		if target.appendLive != nil {
			change.Revision = max(b.pending.Revision, b.revision) + 1
		}
		b.pending, b.key = change, key
		b.text.WriteString(text[:n])
		if target.appendLive != nil {
			part := change
			part.Text = text[:n]
			target.appendLive(part)
		}
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
	b.armTimer(observation, target)
	return nil
}

// armTimer bounds only uncommitted operations, independently of cumulative output.
func (b *DeltaBuffer) armTimer(observation context.Context, target deltaTarget) {
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
}

func mergePendingTool(before, after json.RawMessage) (json.RawMessage, error) {
	var fields, update map[string]json.RawMessage
	if json.Unmarshal(before, &fields) != nil || json.Unmarshal(after, &update) != nil {
		return nil, status_error.RuntimeEventEncodingFailed
	}
	for name, value := range update {
		fields[name] = value
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, status_error.RuntimeEventEncodingFailed
	}
	return data, nil
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
		change.Kind, change.ContentType = MessageToolUpdate, constants.RuntimeEventTypeToolUpdate
		change.ToolCallID = event.Tool.ID
		if change.MessageID == "" {
			change.MessageID = event.Tool.ID
		}
		metadata := *event.Tool
		metadata.TerminalDelta = ""
		data, err := json.Marshal(metadata)
		if err != nil {
			return MessageChange{}, false, status_error.RuntimeEventEncodingFailed
		}
		change.Data, change.Text = data, event.Tool.TerminalDelta
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
	change.Revision = max(change.Revision, b.revision+1)
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
		if target.checkpointLive != nil {
			if change.Kind == MessageToolUpdate {
				// Live projection merges the same partial fields; durable readers
				// resolve the Store's complete snapshot at the returned boundary.
				change.Kind = MessageToolSnapshot
			}
			change.MessageID, change.Revision = target.messageID, revision
			target.checkpointLive(ctx, receipt, change)
		} else if target.committed != nil {
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
	err := b.drainToolFrame(ctx, target)
	b.closed = true
	return errors.Join(b.failure, err, b.flush(ctx, target))
}

// pendingWrite reports whether a failed segment still needs an exact retry.
func (b *DeltaBuffer) pendingWrite() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending.Kind != "" || b.toolFrame != nil
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

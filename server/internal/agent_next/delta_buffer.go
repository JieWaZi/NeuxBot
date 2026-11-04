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
	deltaBudget      = 8 << 10
	deltaDelay       = 2 * time.Second
	liveQueueSize    = 64
	maxObservedBytes = 32 << 10
	maxTrackedTools  = 64
)

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
	tools map[string]AgentTool
	// failure records the first durable or normalization error.
	failure error
}

func (b *DeltaBuffer) observe(_ context.Context, t *Turn, scope AgentScope, event AgentEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	if event.Tool != nil {
		tool, err := b.mergeTool(*event.Tool)
		if err != nil {
			b.failure = errors.Join(b.failure, err)
			return err
		}
		event.Tool = &tool
	}
	change, ok, err := messageChange(event)
	if !ok && err == nil {
		return nil
	}
	if err != nil {
		b.failure = errors.Join(b.failure, err)
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), turnSettlementTimeout)
	defer cancel()
	key := NativeResultKey{Session: t.binding.Session, InvocationID: scope.NativeInvocationID}
	if change.MessageID == "" {
		// Unidentified payloads share a lane only within this proven native invocation.
		change.MessageID = key.InvocationID + ":" + event.Kind
	}
	if !b.retry && key == b.key && change.Kind == MessageAppend && b.pending.Kind == MessageAppend &&
		change.MessageID == b.pending.MessageID && change.ContentType == constants.RuntimeEventTypeCommentaryDelta {
		// Some providers disclose the phase only on the message-complete frame.
		b.pending.ContentType = change.ContentType
	}
	if b.retry || change.Kind != MessageAppend || key != b.key ||
		change.MessageID != b.pending.MessageID || change.ContentType != b.pending.ContentType {
		if err := b.flush(ctx, t); err != nil {
			return err
		}
	}
	if change.Kind != MessageAppend {
		b.pending, b.key = change, key
		return b.flush(ctx, t)
	}
	text := change.Text
	change.Text = ""
	for len(text) > 0 {
		b.pending, b.key = change, key
		n := min(len(text), deltaBudget-b.text.Len())
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		b.text.WriteString(text[:n])
		text = text[n:]
		if len(text) > 0 || b.text.Len() == deltaBudget {
			if err := b.flush(ctx, t); err != nil {
				return err
			}
		}
	}
	if event.Message != nil && event.Message.Complete {
		return b.flush(ctx, t)
	}
	if b.pending.Kind != "" && b.timer == nil {
		generation := b.timerGeneration
		b.timer = time.AfterFunc(deltaDelay, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.closed || b.timerGeneration != generation {
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), turnSettlementTimeout)
			defer cancel()
			_ = b.flush(ctx, t)
		})
	}
	return nil
}

func (b *DeltaBuffer) mergeTool(update AgentTool) (AgentTool, error) {
	tool, known := b.tools[update.ID]
	terminal := update.Status != nil && (*update.Status == "completed" || *update.Status == "failed")
	if !known && !terminal && len(b.tools) >= maxTrackedTools {
		return AgentTool{}, status_error.RuntimeEventQueueOverflow
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
	data, err := json.Marshal(tool)
	if err != nil {
		return AgentTool{}, status_error.RuntimeEventEncodingFailed
	}
	if len(data) > maxObservedBytes {
		return AgentTool{}, status_error.RuntimeEventQueueOverflow
	}
	if terminal {
		delete(b.tools, tool.ID)
	} else {
		if b.tools == nil {
			b.tools = make(map[string]AgentTool)
		}
		b.tools[tool.ID] = tool
	}
	return tool, nil
}

func messageChange(event AgentEvent) (MessageChange, bool, error) {
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
		data, err := json.Marshal(event.Tool)
		if err != nil {
			return MessageChange{}, false, status_error.RuntimeEventEncodingFailed
		}
		if len(data) > maxObservedBytes {
			return MessageChange{}, false, status_error.RuntimeEventQueueOverflow
		}
		change.Data = data
		return change, true, nil
	default:
		return MessageChange{}, false, nil
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

func (b *DeltaBuffer) flush(ctx context.Context, t *Turn) error {
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
	commitKey := stableTurnKey("fragment", t.input.HostInvocationID, b.key.InvocationID, strconv.FormatInt(change.Revision, 10))
	rng, err := t.lifecycle.store.CommitNativeFragments(ctx, NativeFragmentBatch{
		Key: b.key, CommitKey: commitKey, MessageID: messageID, Changes: []MessageChange{change},
	})
	if err != nil {
		b.retry = true
		b.failure = err
		return err
	}
	if t.input.AssistantMessageID != "" {
		receipt, err := commitConversation(ctx, t.lifecycle.store, ConversationBatch{
			ChatID: t.thread.chatID, Key: commitKey, ExpectedVersion: t.chatVersion, Changes: []ConversationChange{{Message: &MessageChange{
				MessageID: t.input.AssistantMessageID, ExecutionID: t.input.ExecutionID,
				Kind: MessageNativeFragment, Fragments: &rng, Revision: change.Revision,
			}}},
		})
		if err != nil {
			b.retry = true
			b.failure = err
			return err
		}
		t.chatVersion = receipt.Version
		if t.ports.Committed != nil {
			t.ports.Committed(receipt)
		}
	}
	b.revision = change.Revision
	b.retry = false
	b.pending = MessageChange{}
	b.text.Reset()
	return nil
}

func (b *DeltaBuffer) finish(ctx context.Context, t *Turn) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return errors.Join(b.failure, b.flush(ctx, t))
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
	if len(event.Text) > maxObservedBytes {
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

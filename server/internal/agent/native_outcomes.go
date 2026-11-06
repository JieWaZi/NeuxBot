package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func stableTurnKey(kind string, parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return kind + ":" + hex.EncodeToString(sum[:])
}

func (l *ThreadLifecycle) commitAndAcknowledge(ctx context.Context, client AgentClient, session NativeSessionKey, outcome AgentNativeOutcome, committed func(CommitReceipt)) error {
	if outcome.Turn.InvocationID == "" || outcome.Turn.Status == "" {
		return status_error.ChatLogNativeOutcomePending
	}
	key := NativeResultKey{Session: session, InvocationID: outcome.Turn.InvocationID}
	result := NativeResult{Key: key, Turn: outcome.Turn, StopReason: outcome.StopReason,
		ForkPosition: outcome.ForkPosition, Unavailable: outcome.Unavailable,
	}
	if outcome.FinalMessage != nil {
		result.FinalMessage = &NativeFinalMessage{ID: outcome.FinalMessage.ID, Text: outcome.FinalMessage.Text}
	}
	if outcome.NativeFailure != nil {
		result.Failure = &acpmeta.NativeTurnFailure{
			Code: outcome.NativeErrorCode, Kind: outcome.NativeErrorKind,
			ObservedOutput: outcome.ObservedOutput, PossibleSideEffects: outcome.PossibleSideEffects,
		}
		var public statuserror.CommonError
		if errors.As(outcome.NativeFailure, &public) {
			result.Failure.Message = public.Value()
		}
	}
	if err := l.store.CommitNativeResult(ctx, result); err != nil {
		return err
	}
	if slices.Contains(result.Unavailable, acpmeta.ReceiptConsumedInputsUnavailable) {
		return status_error.ChatLogNativeOutcomePending
	}
	inputs, err := l.store.ReadNativeInputs(ctx, session, result.Turn.ConsumedInputIDs)
	if err != nil {
		return err
	}
	refs := make([]HostInputRef, 0, len(inputs))
	for _, item := range inputs {
		ref := item.Ref
		refs = append(refs, ref)
		if ref.ChatID == "" {
			continue
		}
		changes := []ConversationChange{{Consumption: &InputConsumption{
			HostInvocationID: ref.HostInvocationID, Result: key,
		}}}
		if id := item.Input.AssistantMessageID; id != "" {
			changes = append(changes, terminalMessageChanges(key, id, item.Input.ExecutionID, result.FinalMessage != nil)...)
		}
		receipt, err := commitConversation(ctx, l.store, ConversationBatch{
			ChatID: ref.ChatID, Key: stableTurnKey("consume", session.Provider, session.Subject, session.SessionID, ref.HostInvocationID, key.InvocationID),
			Changes: changes,
		})
		if err != nil {
			return err
		}
		if committed != nil {
			committed(receipt)
		}
	}
	if err := l.store.ConfirmNativeResult(ctx, key, refs); err != nil {
		return err
	}
	if client.Capabilities().NativeOutcomes {
		_, err = client.ReadNativeOutcomes(ctx, session.SessionID, []string{key.InvocationID}, false)
	}
	return err
}

type nativeOutcomeReader struct {
	// client borrows the adapter's receipt ledger through process shutdown and ACK retry.
	client AgentClient
	// session freezes the exact native receipt address.
	session NativeSessionKey
	// cancel stops the long read without canceling native work.
	cancel context.CancelFunc
	// done closes after the reader leaves the native and durable stores.
	done chan struct{}
	// target is the native-only destination for verified autonomous observations.
	target deltaTarget
	// mu protects the bounded set of native invocation buffers.
	mu sync.Mutex
	// autonomous contains only unfinalized, proven native invocation lanes.
	autonomous map[string]*DeltaBuffer
}

func (l *ThreadLifecycle) startNativeOutcomeReader(client AgentClient, state *threadState, session NativeSessionKey) *nativeOutcomeReader {
	ctx, cancel := context.WithCancel(context.Background())
	r := &nativeOutcomeReader{client: client, session: session, cancel: cancel, done: make(chan struct{}),
		target: deltaTarget{store: l.store, session: session}, autonomous: make(map[string]*DeltaBuffer)}
	go func() {
		defer close(r.done)
		for ctx.Err() == nil {
			page, err := r.client.ReadNativeOutcomes(ctx, session.SessionID, nil, true)
			if err == nil && len(page.Outcomes) == 0 {
				return
			}
			if err == nil {
				for _, outcome := range page.Outcomes {
					// The foreground owns the final flush of observed fragments. A
					// receipt can wake earlier, so let that exact input finish first.
					if waitErr := l.awaitForeground(ctx, state, outcome.Turn); waitErr != nil {
						err = errors.Join(err, waitErr)
						break
					}
					write, cancelWrite := context.WithTimeout(ctx, turnSettlementTimeout)
					commitErr := l.persistReceipt(write, r, outcome, nil)
					cancelWrite()
					err = errors.Join(err, commitErr)
				}
			}
			if err != nil {
				timer := time.NewTimer(time.Second)
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
				timer.Stop()
			}
		}
	}()
	return r
}

func (l *ThreadLifecycle) awaitForeground(ctx context.Context, state *threadState, native acpmeta.NativeTurn) error {
	l.mu.Lock()
	active := state.active
	l.mu.Unlock()
	if active == nil {
		return nil
	}
	active.mu.Lock()
	if active.summary != nil {
		child := active.summary
		active.mu.Unlock()
		active = child
		active.mu.Lock()
	}
	wait := native.InvocationID != "" && active.nativeInvocationID == native.InvocationID ||
		active.adapterInputID != "" && slices.Contains(native.ConsumedInputIDs, active.adapterInputID)
	active.mu.Unlock()
	if !wait {
		return nil
	}
	select {
	case <-active.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *nativeOutcomeReader) stop(ctx context.Context) error {
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *ThreadLifecycle) drainNativeOutcomes(ctx context.Context, reader *nativeOutcomeReader) error {
	if reader == nil {
		return nil
	}
	for {
		page, err := reader.client.ReadNativeOutcomes(ctx, reader.session.SessionID, nil, false)
		if err != nil || len(page.Outcomes) == 0 {
			return err
		}
		var pending error
		for _, outcome := range page.Outcomes {
			pending = errors.Join(pending, l.persistReceipt(ctx, reader, outcome, nil))
		}
		if pending != nil || !page.More {
			return pending
		}
	}
}

func (r *nativeOutcomeReader) observe(ctx context.Context, scope AgentScope, event AgentEvent) error {
	switch event.Kind {
	case "text", "reasoning", "tool":
	default:
		return nil
	}
	const maxObservedInvocations = 128
	r.mu.Lock()
	buffer := r.autonomous[scope.NativeInvocationID]
	if buffer == nil {
		if len(r.autonomous) >= maxObservedInvocations {
			r.mu.Unlock()
			return status_error.RuntimeEventQueueOverflow
		}
		buffer = &DeltaBuffer{}
		r.autonomous[scope.NativeInvocationID] = buffer
	}
	r.mu.Unlock()
	return buffer.observe(ctx, r.target, scope, event)
}

func (r *nativeOutcomeReader) flushBeforeHost(ctx context.Context, id string) error {
	r.mu.Lock()
	buffer := r.autonomous[id]
	r.mu.Unlock()
	if buffer == nil {
		return nil
	}
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
	defer cancel()
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.flush(flush, r.target)
}

func (r *nativeOutcomeReader) flushInvocation(ctx context.Context, id string) (error, bool) {
	r.mu.Lock()
	buffer := r.autonomous[id]
	r.mu.Unlock()
	if buffer == nil {
		return nil, false
	}
	err := buffer.finish(ctx, r.target)
	pending := buffer.pendingWrite()
	if !pending {
		r.mu.Lock()
		if r.autonomous[id] == buffer {
			delete(r.autonomous, id)
		}
		r.mu.Unlock()
	}
	return err, pending
}

func (r *nativeOutcomeReader) flushRemaining(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	ids := make([]string, 0, len(r.autonomous))
	for id := range r.autonomous {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	var err error
	for _, id := range ids {
		if flushErr, pending := r.flushInvocation(ctx, id); pending {
			err = errors.Join(err, flushErr)
		}
	}
	return err
}

func (l *ThreadLifecycle) persistReceipt(ctx context.Context, reader *nativeOutcomeReader, outcome AgentNativeOutcome, committed func(CommitReceipt)) error {
	// Both native adapters publish the receipt after draining its observer.
	// Close this exact invocation's buffer before the immutable terminal row.
	if err, pending := reader.flushInvocation(ctx, outcome.Turn.InvocationID); pending {
		return err
	}
	return l.commitAndAcknowledge(ctx, reader.client, reader.session, outcome, committed)
}

func terminalMessageChanges(key NativeResultKey, messageID, executionID string, final bool) []ConversationChange {
	var changes []ConversationChange
	if final {
		changes = append(changes, ConversationChange{Message: &MessageChange{
			Kind: MessageNativeFinal, MessageID: messageID, ExecutionID: executionID, Result: &key,
		}})
	}
	return append(changes, ConversationChange{Message: &MessageChange{
		Kind: MessageComplete, MessageID: messageID, ExecutionID: executionID,
	}})
}

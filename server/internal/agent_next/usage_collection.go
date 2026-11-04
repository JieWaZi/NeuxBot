package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func stableTurnKey(kind string, parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return kind + ":" + hex.EncodeToString(sum[:])
}

func (l *ThreadLifecycle) commitAndAcknowledge(ctx context.Context, client AgentClient, session NativeSessionKey, outcome AgentNativeOutcome, committed func(CommitReceipt)) error {
	if outcome.Turn.InvocationID == "" || outcome.Turn.Status == "" {
		return nil
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
	// client is borrowed until the process owner completes closing.
	client AgentClient
	// session freezes the exact native receipt address.
	session NativeSessionKey
	// cancel stops the long read without canceling native work.
	cancel context.CancelFunc
	// done closes after the reader leaves the native and durable stores.
	done chan struct{}
}

func (l *ThreadLifecycle) startNativeOutcomeReader(client AgentClient, state *threadState, session NativeSessionKey) *nativeOutcomeReader {
	ctx, cancel := context.WithCancel(context.Background())
	r := &nativeOutcomeReader{client: client, session: session, cancel: cancel, done: make(chan struct{})}
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
					if waitErr := l.awaitForeground(ctx, state, outcome.Turn.ConsumedInputIDs); waitErr != nil {
						err = errors.Join(err, waitErr)
						break
					}
					write, cancelWrite := context.WithTimeout(ctx, turnSettlementTimeout)
					commitErr := l.commitAndAcknowledge(write, r.client, session, outcome, nil)
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

func (l *ThreadLifecycle) awaitForeground(ctx context.Context, state *threadState, inputs []string) error {
	l.mu.Lock()
	active := state.active
	l.mu.Unlock()
	if active == nil || len(inputs) == 0 {
		return nil
	}
	active.mu.Lock()
	if active.summary != nil {
		child := active.summary
		active.mu.Unlock()
		active = child
		active.mu.Lock()
	}
	wait := slices.Contains(inputs, active.adapterInputID) && active.adapterInputID != ""
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
			pending = errors.Join(pending, l.commitAndAcknowledge(ctx, reader.client, reader.session, outcome, nil))
		}
		if pending != nil || !page.More {
			return pending
		}
	}
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

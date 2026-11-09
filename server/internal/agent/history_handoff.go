package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const handoffInstructions = "Prepare a concise private handoff for another assistant. Summarize the user's goals, decisions, work completed, outstanding issues and essential file references. Use only the conversation already available. Do not call tools or change anything."

func (t *Turn) summarize(conn *providerConnection, binding NativeBinding, previous AgentSession, previousRequest *ThreadRequest) *NativeResultKey {
	client, err := conn.Client()
	if err != nil {
		return nil
	}
	child := newTurn(t.ctx, t.lifecycle, t.thread, TurnRequest{HostInvocationID: newTurnID(), Text: handoffInstructions, UsageSource: "handoff", UsageWakerID: t.input.UsageWakerID}, TurnPorts{})
	defer child.cancel()
	child.setSession(conn, binding)
	child.effective = previous
	if previousRequest != nil && child.applySettings(client, *previousRequest) != nil {
		return nil
	}
	var mode string
	for _, candidate := range []string{"read-only", "plan"} {
		if slices.ContainsFunc(previous.Modes, func(choice AgentChoice) bool { return choice.ID == candidate }) {
			mode = candidate
			break
		}
	}
	if mode == "" || client.SetSessionMode(t.ctx, binding.Session.SessionID, mode) != nil {
		return nil
	}
	child.phase = turnRunning
	t.mu.Lock()
	t.summary = child
	t.mu.Unlock()

	result := child.runPrompt()
	close(child.done)
	t.mu.Lock()
	t.summary = nil
	t.mu.Unlock()

	child.mu.Lock()
	toolAttempt := child.handoffToolAttempt
	child.mu.Unlock()
	if toolAttempt || turnExecutionError(result) != nil || result.Outcome.FinalMessage == nil ||
		strings.TrimSpace(result.Outcome.FinalMessage.Text) == "" {
		return nil
	}
	return &NativeResultKey{Session: binding.Session, InvocationID: result.Outcome.Turn.InvocationID}
}

func (t *Turn) addPrefix(prefix string) {
	if prefix == "" {
		return
	}
	prefix += "\n\nCurrent user message:\n"
	if len(t.input.Content) == 0 {
		t.input.Text = prefix + t.input.Text
	} else {
		t.input.Content = append([]AgentContent{{Kind: "text", Text: prefix}}, t.input.Content...)
	}
}

func (t *Turn) applyHandoff(handoff *NativeHandoff) error {
	if handoff == nil || handoff.Claim != nil {
		return nil
	}
	if handoff.Summary != nil {
		result, err := t.lifecycle.store.ReadNativeResult(t.ctx, *handoff.Summary)
		if err != nil {
			return err
		}
		if result.FinalMessage == nil || result.Turn.Status != acpmeta.NativeTurnCompleted || strings.TrimSpace(result.FinalMessage.Text) == "" {
			return status_error.RuntimeHandoffInvalid
		}
		t.addPrefix("Private handoff from the previous assistant:\n" + boundedHistoryTail(strings.TrimSpace(result.FinalMessage.Text), constants.RuntimeHandoffMaximumBytes))
		return nil
	}
	var turns [][2]TextMessage
	for _, pair := range handoff.History {
		input, err := t.lifecycle.store.ReadMessage(t.ctx, t.thread.chatID, pair.UserMessageID)
		if err != nil {
			return err
		}
		reply, err := t.lifecycle.store.ReadMessage(t.ctx, t.thread.chatID, pair.AssistantMessageID)
		if err != nil {
			return err
		}
		turns = append(turns, handoffPairMessages(input, reply))
	}
	prefix, err := encodeHandoffHistory(turns)
	if err != nil {
		return err
	}
	t.addPrefix(prefix)
	return nil
}

func (l *ThreadLifecycle) selectHistory(ctx context.Context, chatID string, version int64, limit int) ([]HandoffPair, string, error) {
	if limit <= 0 {
		limit = constants.RuntimeHistoryDefaultTurns
	}
	limit = min(limit, constants.RuntimeHistoryMaximumTurns)
	var pairs []HandoffPair
	var turns [][2]TextMessage
	var inheritedReply *StoredMessage
	total := 0
	before := version + 1
	for before != 0 && len(pairs) < limit {
		candidates, next, err := l.store.ReadHistoryCandidates(ctx, MessageQuery{ChatID: chatID, Before: before, Limit: limit * 2})
		if err != nil {
			return nil, "", err
		}
		for _, candidate := range candidates {
			var input, reply StoredMessage
			if candidate.Inherited {
				message, err := l.store.ReadMessage(ctx, chatID, candidate.MessageID)
				if err != nil {
					return nil, "", err
				}
				switch {
				case message.Role == constants.MessageRoleAssistant:
					inheritedReply = &message
					continue
				case message.Role == constants.MessageRoleUser && inheritedReply != nil:
					input, reply = message, *inheritedReply
				default:
					inheritedReply = nil
					continue
				}
				inheritedReply = nil
			} else {
				inheritedReply = nil
				if candidate.UserMessageID == "" || candidate.MessageID == "" {
					continue
				}
				input, err = l.store.ReadMessage(ctx, chatID, candidate.UserMessageID)
				if err != nil {
					return nil, "", err
				}
				reply, err = l.store.ReadMessage(ctx, chatID, candidate.MessageID)
				if err != nil {
					return nil, "", err
				}
				if !reply.Complete {
					continue
				}
			}
			if input.Role != constants.MessageRoleUser || reply.Role != constants.MessageRoleAssistant ||
				strings.TrimSpace(input.Text) == "" || strings.TrimSpace(reply.Text) == "" {
				continue
			}
			pair := handoffPairMessages(input, reply)
			total += len(pair[0].Content) + len(pair[1].Content)
			if total > constants.RuntimeHistoryMaximumBytes {
				next = 0
				break
			}
			pairs = append(pairs, HandoffPair{UserMessageID: input.ID, AssistantMessageID: reply.ID})
			turns = append(turns, pair)
			if len(pairs) == limit {
				break
			}
		}
		if next >= before {
			return nil, "", status_error.ChatLogCorrupted
		}
		before = next
	}
	slices.Reverse(pairs)
	slices.Reverse(turns)
	prefix, err := encodeHandoffHistory(turns)
	return pairs, prefix, err
}

func handoffPairMessages(input, reply StoredMessage) [2]TextMessage {
	const half = constants.RuntimeHistoryMaximumBytes / 2
	return [2]TextMessage{
		{Role: "user", Content: boundedHistoryTail(input.Text, half)},
		{Role: "assistant", Content: boundedHistoryTail(reply.Text, half)},
	}
}

func encodeHandoffHistory(turns [][2]TextMessage) (string, error) {
	if len(turns) == 0 {
		return "", nil
	}
	ordered := make([]TextMessage, 0, len(turns)*2)
	var bytes int
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		bytes += len(turn[0].Content) + len(turn[1].Content)
		if bytes > constants.RuntimeHistoryMaximumBytes {
			turns = turns[i+1:]
			break
		}
	}
	for _, turn := range turns {
		ordered = append(ordered, turn[:]...)
	}
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return "", status_error.RuntimeEventEncodingFailed
	}
	return "Recent conversation history:\n" + string(encoded), nil
}

func cloneBinding(binding NativeBinding) NativeBinding {
	binding.Configuration = slices.Clone(binding.Configuration)
	if binding.Handoff != nil {
		copy := *binding.Handoff
		copy.History = slices.Clone(copy.History)
		if copy.Summary != nil {
			key := *copy.Summary
			copy.Summary = &key
		}
		if copy.Claim != nil {
			claim := *copy.Claim
			copy.Claim = &claim
		}
		binding.Handoff = &copy
	}
	return binding
}

func handoffClaimed(handoff *NativeHandoff) bool {
	return handoff != nil && handoff.Claim != nil
}

// failedHandoffForReplacement preserves frozen history without acknowledging or replaying the failed input.
func (l *ThreadLifecycle) failedHandoffForReplacement(ctx context.Context, chatID string, binding NativeBinding) (*NativeHandoff, error) {
	if !handoffClaimed(binding.Handoff) {
		return nil, nil
	}
	claim := binding.Handoff.Claim
	input, consumption, err := l.store.ReadHostInputConsumption(ctx, HostInputRef{ChatID: chatID, HostInvocationID: claim.HostInvocationID})
	if err != nil {
		return nil, err
	}
	if input.Session != binding.Session || input.AdapterInputID != claim.AdapterInputID {
		return nil, status_error.ChatLogCorrupted
	}
	if consumption != nil || input.ExecutionID == "" {
		return nil, nil
	}
	records, err := l.store.ReadTurn(ctx, chatID, input.ExecutionID)
	if err != nil {
		return nil, err
	}
	for _, record := range slices.Backward(records) {
		fact := record.Change.Fact
		if fact == nil || fact.ExecutionID != input.ExecutionID {
			continue
		}
		switch fact.Type {
		case constants.ChatEventTypeExecutionFailed:
			handoff := cloneBinding(binding).Handoff
			handoff.Claim = nil
			return handoff, nil
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionCanceled:
			return nil, nil
		}
	}
	return nil, nil
}

func (l *ThreadLifecycle) reconcileHandoff(ctx context.Context, chatID string, binding NativeBinding, reader *nativeOutcomeReader) (NativeBinding, error) {
	if !handoffClaimed(binding.Handoff) {
		return binding, nil
	}
	claim := binding.Handoff.Claim
	ref := HostInputRef{ChatID: chatID, HostInvocationID: claim.HostInvocationID}
	check := func() error {
		input, consumption, err := l.store.ReadHostInputConsumption(ctx, ref)
		if err != nil {
			return err
		}
		if input.Session != binding.Session || input.HostInvocationID != claim.HostInvocationID || input.AdapterInputID != claim.AdapterInputID {
			return status_error.ChatLogCorrupted
		}
		if consumption == nil {
			return status_error.ChatLogNativeOutcomePending
		}
		if consumption.HostInvocationID != claim.HostInvocationID || consumption.Result.Session != binding.Session {
			return status_error.ChatLogCorrupted
		}
		return nil
	}
	err := check()
	if errors.Is(err, status_error.ChatLogNativeOutcomePending) && reader != nil && reader.session == binding.Session {
		drainErr := l.drainNativeOutcomes(ctx, reader, false)
		if err = check(); err != nil {
			err = errors.Join(err, drainErr)
		}
	}
	if err == nil {
		binding.Handoff = nil
	}
	return binding, err
}

func (t *Turn) claimHandoff(ctx context.Context, submitted AgentPromptSubmission) error {
	if t.binding.Handoff.Claim != nil {
		return status_error.ChatLogNativeOutcomePending
	}
	t.handoffClaim = &HandoffClaim{HostInvocationID: submitted.HostInvocationID, AdapterInputID: submitted.InputID}
	claimed := *t.binding.Handoff
	claimed.Claim = t.handoffClaim
	return t.replaceHandoff(ctx, t.binding.Handoff, &claimed, "claim-handoff")
}

func (t *Turn) restoreHandoff(ctx context.Context) error {
	pending := *t.binding.Handoff
	pending.Claim = nil
	claimed := pending
	claimed.Claim = t.handoffClaim
	return t.replaceHandoff(ctx, &claimed, &pending, "restore-handoff")
}

func (t *Turn) replaceHandoff(ctx context.Context, from, to *NativeHandoff, operation string) error {
	store, chatID := t.lifecycle.store, t.thread.chatID
	key := stableTurnKey(operation, t.binding.Session.Provider, t.binding.Session.Subject,
		t.binding.Session.SessionID, t.handoffClaim.HostInvocationID, t.handoffClaim.AdapterInputID)
	next := t.binding
	next.Handoff = to
	for ctx.Err() == nil {
		version, err := store.ReadVersion(ctx, chatID)
		if err != nil {
			return err
		}
		current, err := store.ReadBinding(ctx, chatID)
		if err != nil {
			return err
		}
		if current == nil || current.Session != t.binding.Session || string(current.Configuration) != string(t.binding.Configuration) {
			return status_error.ChatLogVersionConflict
		}
		if reflect.DeepEqual(current.Handoff, to) {
			t.binding = next
			t.publishBinding()
			return nil
		}
		if !reflect.DeepEqual(current.Handoff, from) {
			return status_error.ChatLogVersionConflict
		}
		receipt, err := store.Commit(ctx, ConversationBatch{
			ChatID: chatID, Key: key, ExpectedVersion: version, Changes: []ConversationChange{{Binding: &next}},
		})
		if errors.Is(err, status_error.ChatLogVersionConflict) {
			continue
		}
		if err != nil {
			return err
		}
		t.binding = next
		t.publishBinding()
		t.buffer.mu.Lock()
		t.buffer.chatVersion = receipt.Version
		t.buffer.mu.Unlock()
		if t.ports.Committed != nil {
			t.ports.Committed(receipt)
		}
		return nil
	}
	return ctx.Err()
}

func boundedHistoryTail(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	const marker = "[Earlier text omitted]\n"
	start := len(text) - maxBytes + len(marker)
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return marker + text[start:]
}

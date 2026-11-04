package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const handoffInstructions = "Prepare a concise private handoff for another assistant. Summarize the user's goals, decisions, work completed, outstanding issues and essential file references. Use only the conversation already available. Do not call tools or change anything."

// TextMessage carries one role in an independent generation request.
type TextMessage struct {
	// Role is system, user or assistant.
	Role string `json:"role"`
	// Content is the role's textual material.
	Content string `json:"content"`
}

// TextRequest generates text with no application resources or tool access.
type TextRequest struct {
	// Connection freezes the native account, executable and requested launch model.
	Connection ProviderConnectionRequest
	// Messages contains a leading system instruction and ordered conversation material.
	Messages []TextMessage
	// Settings contains the exact native option IDs requested by this caller.
	Settings []SessionSetting
	// MaxTokens is reported as a limitation when nonzero.
	MaxTokens int
	// Temperature is reported as a limitation when nonzero.
	Temperature float64
}

// TextResult returns provider-proven final text and its completion and usage evidence.
type TextResult struct {
	// Text is the native final answer, present only with a final-message receipt.
	Text string
	// Turn retains native completion and independent local faults.
	Turn TurnResult
	// Limitations names requested generation controls that cannot be applied.
	Limitations []string
}

// TaskRequest runs a detached task with explicitly selected resources and policy.
type TaskRequest struct {
	// Thread freezes the account, workspace and authorized capabilities.
	Thread ThreadRequest
	// Input holds the independent task payload.
	Input TurnRequest
	// Ports supplies the task's explicit resource and interaction grants.
	Ports TurnPorts
}

// GenerateText uses the same turn as Chat with strict native tool restrictions.
func (l *ThreadLifecycle) GenerateText(ctx context.Context, request TextRequest) (TextResult, error) {
	var result TextResult
	if request.MaxTokens != 0 {
		result.Limitations = append(result.Limitations, "max_tokens_unsupported")
	}
	if request.Temperature != 0 {
		result.Limitations = append(result.Limitations, "temperature_unsupported")
	}
	instructions, text, err := generationInput(request.Messages)
	if err != nil {
		return result, err
	}
	if len(request.Connection.Resources.Plugins) != 0 || len(request.Connection.Resources.ConnectorIDs) != 0 {
		return result, status_error.RuntimePolicyUnsupported
	}
	cwd, err := os.MkdirTemp("", "neux-text-")
	if err != nil {
		return result, status_error.RuntimeConfigurationInvalid
	}
	closeFiles := func(context.Context) error { return os.RemoveAll(cwd) }
	request.Connection.Account.CWD = cwd
	request.Connection.Launch.RequireNoTools = true
	turn, err := l.prepare(ctx, ThreadRequest{
		Connection: request.Connection, Settings: request.Settings, Session: AgentSessionOptions{DeveloperInstructions: instructions},
	}, TurnRequest{Text: text}, TurnPorts{}, closeFiles)
	if err != nil {
		return result, err
	}
	result.Turn = turn.Run(ctx)
	if outcome := result.Turn.Outcome; outcome != nil && outcome.FinalMessage != nil &&
		outcome.Turn.Status == acpmeta.NativeTurnCompleted && outcome.NativeFailure == nil {
		result.Text = outcome.FinalMessage.Text
	}
	err = turnExecutionError(result.Turn)
	if err == nil && (result.Turn.Outcome == nil || result.Turn.Outcome.FinalMessage == nil) {
		err = status_error.RuntimeExecutionFailed
	}
	return result, err
}

// ExecuteTask applies an explicit tool policy through the same detached Turn path.
func (l *ThreadLifecycle) ExecuteTask(ctx context.Context, request TaskRequest) (TurnResult, error) {
	if request.Thread.ChatID != "" || request.Thread.Fork != nil {
		return TurnResult{}, status_error.RuntimePolicyUnsupported
	}
	launch := &request.Thread.Connection.Launch
	if !launch.RequireNoTools {
		// Tool-enabled detached work must have a native allowlist as well as
		// the host approval port. Approval alone cannot constrain auto-run tools.
		if request.Thread.Connection.Account.Provider != constants.RuntimeIDClaude ||
			launch.Claude.Tools == nil || request.Ports.Approve == nil {
			return TurnResult{}, status_error.RuntimePolicyUnsupported
		}
		for _, tool := range launch.Claude.Tools {
			if tool == "" || tool == "default" || strings.ContainsAny(tool, " ,*()\t\r\n") {
				return TurnResult{}, status_error.RuntimeConfigurationInvalid
			}
		}
		launch.Claude.SettingSources = []string{}
	}
	turn, err := l.Prepare(ctx, request.Thread, request.Input, request.Ports)
	if err != nil {
		return TurnResult{}, err
	}
	result := turn.Run(ctx)
	return result, turnExecutionError(result)
}

func turnExecutionError(r TurnResult) error {
	var nativeError error
	if r.Outcome != nil {
		nativeError = r.Outcome.NativeFailure
		if nativeError == nil {
			switch r.Outcome.Turn.Status {
			case acpmeta.NativeTurnCompleted:
			case acpmeta.NativeTurnCancelled:
				nativeError = context.Canceled
			default:
				nativeError = status_error.RuntimeExecutionFailed
			}
		}
	} else if r.OperationError == nil {
		nativeError = status_error.RuntimeExecutionFailed
	}
	return errors.Join(r.OperationError, nativeError, r.DeliveryError, r.StorageError, r.CleanupError)
}

func generationInput(messages []TextMessage) (*string, string, error) {
	var system []string
	i := 0
	for ; i < len(messages) && messages[i].Role == "system"; i++ {
		system = append(system, messages[i].Content)
	}
	var instructions *string
	if len(system) > 0 {
		value := strings.Join(system, "\n\n")
		instructions = &value
	}
	remaining := messages[i:]
	if len(remaining) == 0 || remaining[len(remaining)-1].Role != "user" || strings.TrimSpace(remaining[len(remaining)-1].Content) == "" {
		return nil, "", status_error.AgentInputInvalid
	}
	for _, message := range remaining {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, "", status_error.AgentInputInvalid
		}
	}
	if len(remaining) == 1 {
		return instructions, remaining[0].Content, nil
	}
	content, err := json.Marshal(remaining)
	if err != nil {
		return nil, "", status_error.RuntimeEventEncodingFailed
	}
	return instructions, "Conversation messages in chronological order:\n" + string(content), nil
}

func (t *Turn) summarize(conn *providerConnection, binding NativeBinding, previous AgentSession, previousRequest *ThreadRequest) *NativeResultKey {
	client, err := conn.Client()
	if err != nil {
		return nil
	}
	child := newTurn(t.ctx, t.lifecycle, t.thread, TurnRequest{HostInvocationID: newTurnID(), Text: handoffInstructions}, TurnPorts{})
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
		drainErr := l.drainNativeOutcomes(ctx, reader)
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

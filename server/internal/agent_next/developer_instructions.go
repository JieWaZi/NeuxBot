package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func (t *Turn) summarize(conn *providerConnection, binding NativeBinding, previous AgentSession) string {
	client, err := conn.Client()
	if err != nil {
		return ""
	}
	var mode string
	for _, candidate := range []string{"read-only", "plan"} {
		if slices.ContainsFunc(previous.Modes, func(choice AgentChoice) bool { return choice.ID == candidate }) {
			mode = candidate
			break
		}
	}
	if mode == "" || client.SetSessionMode(t.ctx, binding.Session.SessionID, mode) != nil {
		return ""
	}
	child := newTurn(t.ctx, t.lifecycle, t.thread, TurnRequest{HostInvocationID: newTurnID(), Text: handoffInstructions}, TurnPorts{})
	child.setSession(conn, binding)
	child.phase = turnRunning
	t.mu.Lock()
	t.summary = child
	t.mu.Unlock()

	result := child.runPrompt()
	child.cancel()
	close(child.done)
	t.mu.Lock()
	t.summary = nil
	t.mu.Unlock()

	if turnExecutionError(result) != nil || result.Outcome.FinalMessage == nil {
		return ""
	}
	text := strings.TrimSpace(result.Outcome.FinalMessage.Text)
	if text == "" {
		return ""
	}
	return "Private handoff from the previous assistant:\n" + boundedHistoryTail(text, constants.RuntimeHandoffMaximumBytes)
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

func (l *ThreadLifecycle) recentHistory(ctx context.Context, chatID string, limit int) (string, error) {
	if limit <= 0 {
		limit = constants.RuntimeHistoryDefaultTurns
	}
	limit = min(limit, constants.RuntimeHistoryMaximumTurns)
	candidates, _, err := l.store.ReadHistoryCandidates(ctx, MessageQuery{ChatID: chatID, Limit: limit * 2})
	if err != nil {
		return "", err
	}
	var turns [][]TextMessage
	var total int
	// Candidates arrive newest first. Only complete public input and final reply pairs seed a replacement.
	for _, candidate := range candidates {
		if len(turns) == limit {
			break
		}
		if candidate.UserMessageID == "" || candidate.MessageID == "" {
			continue
		}
		input, err := l.store.ReadMessage(ctx, chatID, candidate.UserMessageID)
		if err != nil {
			return "", err
		}
		reply, err := l.store.ReadMessage(ctx, chatID, candidate.MessageID)
		if err != nil {
			return "", err
		}
		if reply.Text == "" || !reply.Complete {
			continue
		}
		half := constants.RuntimeHistoryMaximumBytes / 2
		user, assistant := boundedHistoryTail(input.Text, half), boundedHistoryTail(reply.Text, half)
		if total+len(user)+len(assistant) > constants.RuntimeHistoryMaximumBytes {
			break
		}
		turns = append(turns, []TextMessage{{Role: "user", Content: user}, {Role: "assistant", Content: assistant}})
		total += len(user) + len(assistant)
	}
	if len(turns) == 0 {
		return "", nil
	}
	ordered := make([]TextMessage, 0, len(turns)*2)
	for i := len(turns) - 1; i >= 0; i-- {
		ordered = append(ordered, turns[i]...)
	}
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return "", status_error.RuntimeEventEncodingFailed
	}
	return "Recent conversation history:\n" + string(encoded), nil
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

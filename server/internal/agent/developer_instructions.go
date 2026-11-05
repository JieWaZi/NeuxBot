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

// TextMessage 是一次独立生成中的角色消息。
type TextMessage struct {
	// Role 是 system、user 或 assistant。
	Role string `json:"role"`
	// Content 是该角色的消息正文。
	Content string `json:"content"`
}

// DetachedAccountResolver 在隔离工作目录中冻结所选账号的真实凭据身份。
type DetachedAccountResolver func(context.Context, AccountRequest, string) (AccountRequest, error)

// TextRequest 指定无应用资源和工具的独立文本调用。
type TextRequest struct {
	// Connection 指定原生账号、程序和模型。
	Connection ProviderConnectionRequest
	// ResolveAccount 在隔离工作目录准备后解析账号。
	ResolveAccount DetachedAccountResolver
	// Messages 按顺序传入系统指令和消息。
	Messages []TextMessage
	// Settings 传入调用方明确指定的原生选项。
	Settings []SessionSetting
	// Selection 指定推理选项。
	Selection *TurnSelection
	// UsageSource 记录本次调用的业务来源。
	UsageSource string
	// MaxTokens 非零时会作为未支持的生成限制报告。
	MaxTokens int
	// Temperature 非零时会作为未支持的生成限制报告。
	Temperature float64
}

// TextResult 返回原生确认的文本、终态和用量证据。
type TextResult struct {
	// Text 仅在原生确认完整消息时返回。
	Text string
	// Turn 保存原生终态和独立的宿主故障。
	Turn TurnResult
	// ToolsDisabled 表示运行时确实确认了无工具启动。
	ToolsDisabled bool
	// Limitations 记录未能应用的请求控制。
	Limitations []string
}

// TaskRequest 通过显式选择的资源和权限执行独立任务。
type TaskRequest struct {
	// Thread 固定账号、工作范围和能力选择。
	Thread ThreadRequest
	// ResolveAccount 在隔离工作目录准备后解析账号。
	ResolveAccount DetachedAccountResolver
	// Input 是独立任务输入。
	Input TurnRequest
	// Ports 提供显式资源和交互授权。
	Ports TurnPorts
}

// SupportsDetachedPolicy 报告适配器是否能强制执行独立调用的权限边界。
func SupportsDetachedPolicy(provider string) bool {
	return provider == constants.RuntimeIDClaude
}

// GenerateText 通过正式 Turn 执行无工具的独立生成。
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
	if !SupportsDetachedPolicy(request.Connection.Account.Provider) || len(request.Connection.Resources.Plugins) != 0 || len(request.Connection.Resources.ConnectorIDs) != 0 {
		return result, status_error.RuntimePolicyUnsupported
	}
	source := request.UsageSource
	if source == "" {
		source = "generate_text"
	}
	request.Connection.Launch.RequireNoTools = true
	turn, err := l.prepareDetached(ctx, ThreadRequest{
		Connection: request.Connection, Settings: request.Settings, Selection: request.Selection,
		Session: AgentSessionOptions{DeveloperInstructions: instructions},
	}, TurnRequest{Text: text, UsageSource: source}, TurnPorts{}, request.ResolveAccount)
	if err != nil {
		return result, err
	}
	result.ToolsDisabled = true
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

// ExecuteTask 通过正式 Turn 应用明确的独立工具权限。
func (l *ThreadLifecycle) ExecuteTask(ctx context.Context, request TaskRequest) (TurnResult, error) {
	if request.Thread.ChatID != "" || request.Thread.Fork != nil || !SupportsDetachedPolicy(request.Thread.Connection.Account.Provider) {
		return TurnResult{}, status_error.RuntimePolicyUnsupported
	}
	launch := &request.Thread.Connection.Launch
	if !launch.RequireNoTools {
		// 审批回调不能约束原生自动执行的工具，因此还必须提供原生工具白名单。
		if launch.Claude.Tools == nil || request.Ports.Approve == nil {
			return TurnResult{}, status_error.RuntimePolicyUnsupported
		}
		for _, tool := range launch.Claude.Tools {
			if tool == "" || tool == "default" || strings.ContainsAny(tool, " ,*()\t\r\n") {
				return TurnResult{}, status_error.RuntimeConfigurationInvalid
			}
		}
		launch.Claude.SettingSources = []string{}
	}
	if request.Input.UsageSource == "" {
		request.Input.UsageSource = "background"
	}
	turn, err := l.prepareDetached(ctx, request.Thread, request.Input, request.Ports, request.ResolveAccount)
	if err != nil {
		return TurnResult{}, err
	}
	result := turn.Run(ctx)
	return result, turnExecutionError(result)
}

func (l *ThreadLifecycle) prepareDetached(ctx context.Context, thread ThreadRequest, input TurnRequest, ports TurnPorts, resolve DetachedAccountResolver) (*Turn, error) {
	cwd, err := os.MkdirTemp("", "neux-detached-")
	if err != nil {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	closeFiles := func(context.Context) error { return os.RemoveAll(cwd) }
	account := &thread.Connection.Account
	account.CWD = cwd
	if resolve != nil {
		account.CWD, err = resolveAgentDirectory(cwd)
		if err == nil {
			*account, err = resolve(ctx, *account, thread.Connection.Launch.ConfigHome)
		}
		if err != nil {
			return nil, errors.Join(err, closeFiles(context.WithoutCancel(ctx)))
		}
		if account.ConfigHome != "" {
			thread.Connection.Launch.ConfigHome = account.ConfigHome
		}
	}
	return l.prepare(ctx, thread, input, ports, closeFiles)
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

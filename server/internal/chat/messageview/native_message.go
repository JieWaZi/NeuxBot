package messageview

import (
	"encoding/hex"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// ReplaceStoredContent 投影已提交的原生内容并保留业务元数据。
func (p *MessageViewBuilder) ReplaceStoredContent(stored agent.StoredMessage, cursor, recordedAt int64) error {
	index, ok := p.messageIndexes[stored.ID]
	if !ok {
		return nil
	}
	message := &p.messages[index]
	message.Content, message.DisplayParts = stored.Text, nil
	delete(p.textBuilders, message.ID)
	delete(p.pendingTextIndexes, message.ID)
	delete(p.provisionalTextIndexes, message.ID)
	delete(p.toolIndexes, message.ID)
	delete(p.toolStates, message.ID)
	delete(p.latestToolKeys, message.ID)
	delete(p.runningTools, message.ID)
	for _, part := range stored.Parts {
		if err := p.applyNativePart(message, part, cursor, recordedAt); err != nil {
			return err
		}
	}
	message.Revision = max(message.Revision, cursor, stored.Revision)
	message.LiveRevision = max(message.LiveRevision, stored.Revision)
	message.UpdatedAt = max(message.UpdatedAt, recordedAt)
	return nil
}

// SeedNativeLiveMessage 初始化已接纳助手消息的实时投影，版本来自执行开始事实。
func (p *MessageViewBuilder) SeedNativeLiveMessage(messageID string, revision int64) {
	p.messageIndexes[messageID] = len(p.messages)
	p.messages = append(p.messages, Message{ID: messageID, ExecutionID: messageID, LiveRevision: revision})
}

// ApplyNativeIncrement 投影本次规范文本，只收集新增部件字节。
func (p *MessageViewBuilder) ApplyNativeIncrement(messageID string, change agent.MessageChange, recordedAt int64) (MessageIncrement, error) {
	update := MessageIncrement{Revision: change.Revision, UpdatedAt: recordedAt}
	index, exists := p.messageIndexes[messageID]
	if !exists {
		return update, status_error.ChatLogError
	}
	part := agent.MessagePart{Type: change.ContentType, Text: change.Text, ToolCallID: change.ToolCallID, Data: change.Data}
	p.increment = &update
	err := p.applyNativePart(&p.messages[index], part, change.Revision, recordedAt)
	p.increment = nil
	return update, err
}

// NativeToolMovesPart 判断最新工具快照是否会跨过已经显示的部件。
func (p *MessageViewBuilder) NativeToolMovesPart(messageID, toolCallID string) bool {
	index, exists := p.toolIndexes[messageID][toolProjectionKey("", toolCallID)]
	return exists && index < len(p.messages[p.messageIndexes[messageID]].DisplayParts)-1
}

func (p *MessageViewBuilder) applyNativePart(message *Message, part agent.MessagePart, cursor, recordedAt int64) error {
	switch part.Type {
	case constants.RuntimeEventTypeTextDelta:
		p.appendText(message, constants.MessageDisplayPartText, part.Text, false)
	case constants.RuntimeEventTypeCommentaryDelta:
		p.appendText(message, constants.MessageDisplayPartProcessCommentary, part.Text, false)
	case constants.RuntimeEventTypeThinkingDelta:
		p.appendText(message, constants.MessageDisplayPartReasoning, part.Text, false)
	case constants.RuntimeEventTypeToolUse, constants.RuntimeEventTypeToolUpdate, constants.RuntimeEventTypeToolInputSnapshot, constants.RuntimeEventTypeToolResult:
		var data map[string]any
		if json.Unmarshal(part.Data, &data) != nil || data == nil {
			return status_error.ChatLogCorrupted
		}
		data["toolCallId"] = part.ToolCallID
		if input, exists := data["input"]; exists {
			data["rawInput"] = input
		}
		event := MessageRuntimeEvent{Sequence: cursor, RecordedAt: recordedAt, Type: part.Type}
		status, _ := data["status"].(string)
		if output, exists := data["output"]; exists {
			data["rawOutput"] = output
		}
		switch acp.ToolCallStatus(status) {
		case acp.ToolCallStatusCompleted, acp.ToolCallStatusFailed:
			p.applyToolResult(message, event, data)
		default:
			if part.Type == constants.RuntimeEventTypeToolResult {
				p.applyToolResult(message, event, data)
			} else {
				p.applyToolUse(message, event, data)
			}
		}
	}
	return nil
}

// SetNativeResult 投影原生调用用量，并将末次请求样本与调用用量分开。
func (p *MessageViewBuilder) SetNativeResult(messageID string, result *agent.NativeResult) {
	if result == nil {
		return
	}
	p.SetNativeUsage(messageID, result.Turn.Usage)
	index, ok := p.messageIndexes[messageID]
	if !ok {
		return
	}
	message := &p.messages[index]
	p.setForkBoundary(message, agent.NativeForkBoundary{Session: result.Key.Session, Mode: result.ForkMode, Position: result.ForkPosition})
	if message.RunMetrics != nil {
		message.RunMetrics.TokenUsage = nil
		message.RunMetrics.LatestRequestOutputTokens = nil
	}
	observed := result.Turn.Usage
	tokens := ledger.ReadTokens(observed)
	var latestOutput *int64
	if observed != nil && result.Key.Session.Provider == constants.RuntimeIDWorkBuddy {
		for _, sample := range observed.Observations {
			if sample.Source == "codebuddy/usage_update" && sample.Coverage == "latest_model_request" {
				latestOutput = ledger.ReadModelTokens(sample.Tokens).Output
				break
			}
		}
	}
	showUsage := tokens != (ledger.Tokens{}) || result.Key.Session.Provider == constants.RuntimeIDCodex
	if !showUsage && latestOutput == nil {
		return
	}
	if message.RunMetrics == nil {
		message.RunMetrics = &MessageRunMetrics{}
	}
	message.RunMetrics.LatestRequestOutputTokens = latestOutput
	if !showUsage {
		return
	}
	digest := result.Key.Digest()
	view := &MessageTokenUsage{InvocationID: hex.EncodeToString(digest[:])}
	var totals ledger.TokenAccumulator
	if observed != nil {
		view.Source, view.Coverage = observed.Source, observed.Coverage
	}
	if err := totals.Add(tokens, observed != nil && observed.Complete); err == nil {
		view.TokenMetrics, _ = totals.Metrics()
	}
	view.Complete = view.TotalWithCacheRead != nil
	message.RunMetrics.TokenUsage = view
}

// SetNativeUsage exposes primary counters only when the native result covers the turn.
func (p *MessageViewBuilder) SetNativeUsage(messageID string, usage *acpmeta.TurnUsage) {
	index, ok := p.messageIndexes[messageID]
	if !ok {
		return
	}
	message := &p.messages[index]
	complete := usage != nil && usage.Complete
	if message.RunMetrics == nil {
		if !complete {
			return
		}
		message.RunMetrics = &MessageRunMetrics{}
	}
	if !complete {
		// The public numeric fields have no coverage annotation. Clear only the
		// token counters when new evidence no longer covers the whole turn.
		usage = &acpmeta.TurnUsage{}
	}
	metrics := message.RunMetrics
	metrics.InputTokens = knownCounter(usage.InputTokens)
	metrics.OutputTokens = knownCounter(usage.OutputTokens)
	metrics.ReasoningOutputTokens = knownCounter(usage.ReasoningTokens)
	metrics.CacheReadTokens = knownCounter(usage.CacheReadTokens)
	metrics.CacheCreationTokens = knownCounter(usage.CacheWriteTokens)
}

func knownCounter(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

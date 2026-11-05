package messageview

import (
	"encoding/hex"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// ReplaceStoredContent projects the effective native lanes while retaining business metadata.
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
	}
	message.Revision = max(message.Revision, cursor)
	message.LiveRevision = max(message.LiveRevision, cursor)
	message.UpdatedAt = max(message.UpdatedAt, recordedAt)
	return nil
}

// SetNativeResult projects the canonical Codex usage with its opaque deduplication identity.
func (p *MessageViewBuilder) SetNativeResult(messageID string, result *agent.NativeResult) {
	if result == nil {
		return
	}
	p.SetNativeUsage(messageID, result.Turn.Usage)
	index, ok := p.messageIndexes[messageID]
	if !ok || result.Key.Session.Provider != "codex" {
		return
	}
	digest := result.Key.Digest()
	view := &MessageTokenUsage{InvocationID: hex.EncodeToString(digest[:])}
	var totals ledger.TokenAccumulator
	observed := result.Turn.Usage
	if observed != nil {
		view.Source, view.Coverage = observed.Source, observed.Coverage
	}
	if err := totals.Add(ledger.ReadTokens(observed), observed != nil && observed.Complete); err == nil {
		view.TokenMetrics, _ = totals.Metrics()
	}
	view.Complete = view.TotalWithCacheRead != nil
	message := &p.messages[index]
	if message.RunMetrics == nil {
		message.RunMetrics = &MessageRunMetrics{}
	}
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

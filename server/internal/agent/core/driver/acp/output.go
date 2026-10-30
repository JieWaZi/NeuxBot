package acp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// SessionUpdate 将公开 ACP 内容转换为规范事实，消息身份与最终阶段只采纳明确证据。
func (s *session) SessionUpdate(ctx context.Context, n sdk.SessionNotification) error {
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if turn == nil || n.SessionId != s.nativeID {
		return nil
	}
	s.markActivity(turn)
	if chunk := n.Update.AgentMessageChunk; chunk != nil {
		message := &contract.AssistantMessage{Phase: contract.PhaseUnknown}
		if info, ok := acpmeta.ReadMessageInfo(chunk.Meta); ok {
			message.Complete = info.Complete
			switch info.Phase {
			case acpmeta.MessagePhaseCommentary:
				message.Phase = contract.PhaseCommentary
			case acpmeta.MessagePhaseFinal:
				message.Phase = contract.PhaseFinal
			}
		}
		if chunk.MessageId != nil {
			message.MessageID = *chunk.MessageId
		}
		if chunk.Content.Text != nil {
			message.Delta = chunk.Content.Text.Text
		} else if block, ok := canonicalContent(chunk.Content); ok {
			message.Content = []contract.ContentBlock{block}
		}
		return s.deliver(contract.DriverEvent{Kind: contract.EventAssistantMessage, Message: message})
	}
	if usage := n.Update.UsageUpdate; usage != nil {
		if usage.Used < 0 || usage.Size < 0 {
			err := fault(contract.FaultProtocol, "usage", "ACP context usage or window is negative", nil)
			s.failTurn(turn, err)
			return err
		}
		s.mu.Lock()
		if s.turn == turn {
			turn.contextTokens = int64(usage.Used)
			turn.contextWindowTokens = int64(usage.Size)
			turn.contextKnown = true
		}
		s.mu.Unlock()
		return s.deliver(contract.DriverEvent{Kind: contract.EventUsage, Usage: &contract.Usage{ContextKnown: true, ContextTokens: int64(usage.Used), ContextWindowTokens: int64(usage.Size)}})
	}
	if chunk := n.Update.AgentThoughtChunk; chunk != nil {
		if chunk.Content.Text != nil {
			return s.deliver(contract.DriverEvent{Kind: contract.EventThinking, Text: chunk.Content.Text.Text})
		}
		return nil
	}
	if plan := n.Update.Plan; plan != nil {
		return s.deliverPlan(turn, plan)
	}
	if n.Update.PlanRemoved != nil {
		s.mu.Lock()
		active := turn.planStarted && !turn.planCompleted
		if active {
			turn.planCompleted = true
		}
		s.mu.Unlock()
		if active {
			return s.deliver(contract.DriverEvent{Kind: contract.EventTool, Tool: &contract.ToolEvent{ID: "acp-plan", Name: "update_plan", Title: "Update plan", Kind: string(sdk.ToolKindThink), Status: string(sdk.ToolCallStatusCompleted), Update: true}})
		}
		return nil
	}
	var tool *contract.ToolEvent
	var rawInput, rawOutput any
	if call := n.Update.ToolCall; call != nil {
		tool = &contract.ToolEvent{ID: string(call.ToolCallId), Title: call.Title, Kind: string(call.Kind), Status: string(call.Status), Content: canonicalToolContent(call.Content), Locations: canonicalLocations(call.Locations)}
		rawInput, rawOutput = call.RawInput, call.RawOutput
	} else if update := n.Update.ToolCallUpdate; update != nil {
		tool = &contract.ToolEvent{ID: string(update.ToolCallId), Update: true, Content: canonicalToolContent(update.Content), Locations: canonicalLocations(update.Locations)}
		if update.Title != nil {
			tool.Title = *update.Title
		}
		if update.Kind != nil {
			tool.Kind = string(*update.Kind)
		}
		if update.Status != nil {
			tool.Status = string(*update.Status)
		}
		rawInput, rawOutput = update.RawInput, update.RawOutput
	}
	if tool == nil {
		return nil
	}
	var err error
	if rawInput != nil {
		tool.Input, err = json.Marshal(rawInput)
		if err != nil {
			err = fault(contract.FaultProtocol, "tool", "ACP tool input is not JSON", err)
			s.failTurn(turn, err)
			return err
		}
	}
	if rawOutput != nil {
		tool.Output, err = json.Marshal(rawOutput)
		if err != nil {
			err = fault(contract.FaultProtocol, "tool", "ACP tool output is not JSON", err)
			s.failTurn(turn, err)
			return err
		}
	}
	var text []string
	for _, block := range tool.Content {
		if block.Text != "" {
			text = append(text, block.Text)
		}
	}
	tool.Text = strings.Join(text, "\n")
	return s.deliver(contract.DriverEvent{Kind: contract.EventTool, Tool: tool})
}

func canonicalLocations(locations []sdk.ToolCallLocation) []contract.Location {
	if locations == nil {
		return nil
	}
	result := make([]contract.Location, 0, len(locations))
	for _, location := range locations {
		item := contract.Location{Path: location.Path}
		if location.Line != nil {
			line := *location.Line
			item.Line = &line
		}
		result = append(result, item)
	}
	return result
}

func canonicalToolContent(content []sdk.ToolCallContent) []contract.ContentBlock {
	if content == nil {
		return nil
	}
	result := make([]contract.ContentBlock, 0, len(content))
	for _, item := range content {
		switch {
		case item.Content != nil:
			if block, ok := canonicalContent(item.Content.Content); ok {
				result = append(result, block)
			}
		case item.Diff != nil:
			diff := &contract.Diff{Path: item.Diff.Path, NewText: item.Diff.NewText}
			if item.Diff.OldText != nil {
				diff.OldText = *item.Diff.OldText
			}
			result = append(result, contract.ContentBlock{Type: "diff", Diff: diff})
		case item.Terminal != nil:
			result = append(result, contract.ContentBlock{Type: "terminal", TerminalID: item.Terminal.TerminalId})
		}
	}
	return result
}

func canonicalContent(content sdk.ContentBlock) (contract.ContentBlock, bool) {
	switch {
	case content.Text != nil:
		return contract.ContentBlock{Type: "text", Text: content.Text.Text}, true
	case content.Image != nil:
		result := contract.ContentBlock{Type: "image", Data: content.Image.Data, MIMEType: content.Image.MimeType}
		if content.Image.Uri != nil {
			result.URI = *content.Image.Uri
		}
		return result, true
	case content.Audio != nil:
		return contract.ContentBlock{Type: "audio", Data: content.Audio.Data, MIMEType: content.Audio.MimeType}, true
	case content.ResourceLink != nil:
		resource := content.ResourceLink
		result := contract.ContentBlock{Type: "resource_link", URI: resource.Uri, Name: resource.Name}
		if resource.MimeType != nil {
			result.MIMEType = *resource.MimeType
		}
		return result, true
	case content.Resource != nil:
		resource := content.Resource.Resource
		result := contract.ContentBlock{Type: "resource"}
		if text := resource.TextResourceContents; text != nil {
			result.Text = text.Text
			result.URI = text.Uri
			if text.MimeType != nil {
				result.MIMEType = *text.MimeType
			}
		} else if blob := resource.BlobResourceContents; blob != nil {
			result.Data = blob.Blob
			result.URI = blob.Uri
			if blob.MimeType != nil {
				result.MIMEType = *blob.MimeType
			}
		} else {
			return contract.ContentBlock{}, false
		}
		return result, true
	}
	return contract.ContentBlock{}, false
}

// deliverPlan 只提取标准计划条目，独立保留进度与终态，不把协议元数据带到宿主。
func (s *session) deliverPlan(turn *turnState, plan *sdk.SessionUpdatePlan) error {
	entries := make([]map[string]string, 0, len(plan.Entries))
	completed := len(plan.Entries) > 0
	for _, entry := range plan.Entries {
		entries = append(entries, map[string]string{"content": entry.Content, "priority": string(entry.Priority), "status": string(entry.Status)})
		completed = completed && entry.Status == sdk.PlanEntryStatusCompleted
	}
	input, err := json.Marshal(map[string]any{"plan": entries})
	if err != nil {
		return err
	}
	s.mu.Lock()
	update := turn.planStarted
	turn.planStarted, turn.planCompleted = true, completed
	s.mu.Unlock()
	status := sdk.ToolCallStatusInProgress
	if completed {
		status = sdk.ToolCallStatusCompleted
	}
	return s.deliver(contract.DriverEvent{Kind: contract.EventTool, Tool: &contract.ToolEvent{ID: "acp-plan", Name: "update_plan", Title: "Update plan", Kind: string(sdk.ToolKindThink), Status: string(status), Update: update, Input: input}})
}

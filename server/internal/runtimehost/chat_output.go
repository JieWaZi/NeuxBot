package runtimehost

import (
	"context"
	"encoding/json"
	"errors"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// chatObserver 把同一技术调用的规范事实交付给既有实时与持久消费者。
type chatObserver struct {
	// sink 是业务执行唯一的有序事件入口。
	sink runtimes.EventSink
	// effects 保留失败重试判断需要的已观察副作用。
	effects runtimes.SideEffectSummary
	// thinkingStarted 保证每轮只产生一次已有思考开始事实。
	thinkingStarted bool
}

func (o *chatObserver) emit(ctx context.Context, kind runtimes.EventType, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return o.sink.Accept(ctx, runtimes.Event{Type: kind, Data: data})
}
func (o *chatObserver) Accept(ctx context.Context, event c.Event) error {
	fact := event.DriverEvent
	switch fact.Kind {
	case c.EventPromptAccepted:
		return o.emit(ctx, runtimes.EventTypeInvocationStarted, runtimes.InvocationStartedData{InvocationID: event.Invocation.ID})
	case c.EventAssistantMessage:
		if fact.Message == nil || fact.Message.Delta == "" {
			return nil
		}
		o.effects.VisibleOutput = true
		kind := runtimes.EventTypeTextDelta
		if fact.Message.Phase == c.PhaseCommentary {
			kind = runtimes.EventTypeCommentaryDelta
		}
		return o.emit(ctx, kind, map[string]any{"text": fact.Message.Delta})
	case c.EventThinking:
		o.effects.ReasoningOutput = true
		if !o.thinkingStarted {
			o.thinkingStarted = true
			if err := o.emit(ctx, runtimes.EventTypeThinkingStart, map[string]any{}); err != nil {
				return err
			}
		}
		if fact.Text != "" {
			return o.emit(ctx, runtimes.EventTypeThinkingDelta, map[string]any{"text": fact.Text})
		}
	case c.EventTool:
		if fact.Tool == nil {
			return nil
		}
		tool := fact.Tool
		o.effects.ToolUse = true
		if tool.Kind == "edit" || tool.Kind == "delete" || tool.Kind == "move" {
			o.effects.FileWrite = true
		}
		payload := map[string]any{"toolCallId": tool.ID}
		if tool.Name != "" {
			payload["name"] = tool.Name
		}
		if tool.Title != "" {
			payload["title"] = tool.Title
		}
		if tool.Kind != "" {
			payload["kind"] = tool.Kind
		}
		if tool.Status != "" {
			payload["status"] = tool.Status
		}
		if tool.Input != nil {
			payload["rawInput"] = tool.Input
			payload["input"] = tool.Input
		}
		if tool.Output != nil {
			payload["rawOutput"] = tool.Output
			payload["output"] = tool.Output
		}
		if tool.Locations != nil {
			locations := make([]map[string]any, 0, len(tool.Locations))
			for _, location := range tool.Locations {
				item := map[string]any{"path": location.Path}
				if location.Line != nil {
					item["line"] = *location.Line
				}
				locations = append(locations, item)
			}
			payload["locations"] = locations
		}
		if tool.Content != nil {
			content := make([]map[string]any, 0, len(tool.Content))
			for _, block := range tool.Content {
				item := map[string]any{"type": block.Type}
				if block.Diff != nil {
					item["path"] = block.Diff.Path
					item["oldText"] = block.Diff.OldText
					item["newText"] = block.Diff.NewText
					o.effects.FileWrite = true
				}
				if block.Text != "" {
					item["text"] = block.Text
				}
				if block.URI != "" {
					item["uri"] = block.URI
				}
				if block.Data != "" {
					item["data"] = block.Data
				}
				if block.MIMEType != "" {
					item["mimeType"] = block.MIMEType
				}
				if block.Name != "" {
					item["name"] = block.Name
				}
				if block.TerminalID != "" {
					item["terminalId"] = block.TerminalID
				}
				if block.Type == "resource" {
					resource := map[string]any{"uri": block.URI}
					if block.MIMEType != "" {
						resource["mimeType"] = block.MIMEType
					}
					if block.Data != "" {
						resource["blob"] = block.Data
					} else {
						resource["text"] = block.Text
					}
					item = map[string]any{"type": "resource", "resource": resource}
				}
				if block.Type != "diff" && block.Type != "terminal" {
					item = map[string]any{"type": "content", "content": item}
				}
				content = append(content, item)
			}
			payload["content"] = content
		}
		terminal := tool.Status == "completed" || tool.Status == "failed"
		kind := runtimes.EventTypeToolUse
		if tool.Update {
			kind = runtimes.EventTypeToolUpdate
			if terminal {
				kind = runtimes.EventTypeToolResult
			} else if tool.Input != nil && tool.Content == nil && tool.Output == nil {
				kind = runtimes.EventTypeToolInputSnapshot
			}
		}
		if err := o.emit(ctx, kind, payload); err != nil {
			return err
		}
		if !tool.Update && terminal {
			return o.emit(ctx, runtimes.EventTypeToolResult, payload)
		}
	case c.EventUsage:
		if fact.Usage != nil {
			return o.emit(ctx, runtimes.EventTypeUsage, chatUsage(*fact.Usage))
		}
	}
	return nil
}
func chatUsage(usage c.Usage) runtimes.UsageData {
	data := runtimes.UsageData{}
	if usage.Known {
		data.Turn = &runtimes.TurnUsageData{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CachedReadTokens, CacheCreationTokens: usage.CachedWriteTokens, ReasoningOutputTokens: usage.ThoughtTokens}
	}
	if usage.ContextKnown {
		data.Context = &runtimes.ContextUsageData{UsedTokens: usage.ContextTokens, WindowTokens: usage.ContextWindowTokens}
	}
	return data
}

func chatResult(request runtimes.RunRequest, result c.Result, observer *chatObserver) runtimes.Result {
	output := runtimes.Result{ExitCode: 0, StopReason: string(result.StopReason), ProtocolHealthy: result.Effects.PromptAccepted, SideEffects: observer.effects, Diagnostics: request.Detection.Diagnostics, Session: runtimes.SessionResult{Mode: request.Session.Mode, InputID: request.Session.ID, OutputID: result.NativeSession.Handle, ForkMode: result.NativeSession.ForkMode, ForkPosition: result.NativeSession.ForkPosition, State: runtimes.SessionStateUnknown}, Facts: runtimes.InvocationFacts{InvocationID: result.Invocation.ID, RuntimeID: request.Target.RuntimeID, BaseRuntimeID: request.Detection.Runtime.BaseRuntimeID, Binary: request.Detection.ResolvedBinary, Version: request.Detection.Version, Model: result.Configuration.ActualModel, Reasoning: result.Configuration.ActualReasoning, Policy: request.Policy.Execution, PrefixArgs: append([]string(nil), request.Target.PrefixArgs...), ConfigHome: request.Target.ConfigHome, Capabilities: request.Detection.Capabilities}}
	if output.Session.OutputID != "" {
		output.Session.State = runtimes.SessionStateCreated
		if request.Session.Mode == runtimes.SessionModeResume {
			output.Session.State = runtimes.SessionStateResumed
		}
	}
	switch result.StopReason {
	case c.StopEndTurn:
		output.TerminationReason = constants.RuntimeTerminationReasonNormal
	case c.StopCancelled:
		output.TerminationReason = constants.RuntimeTerminationReasonCancelled
	case c.StopRefused:
		output.TerminationReason = constants.RuntimeTerminationReasonRefusal
	case c.StopMaxTokens:
		output.TerminationReason = constants.RuntimeTerminationReasonTruncated
	default:
		output.TerminationReason = constants.RuntimeTerminationReasonUnknown
	}
	fault := result.Fault
	if fault == nil {
		fault = result.DeliveryFault
	}
	if fault != nil {
		output.ExitCode = -1
		code := runtimes.ClassifyFailure(request.Detection.Runtime.BaseRuntimeID, fault)
		stage := constants.RuntimeACPFailureStagePrompt
		switch fault.Code {
		case c.FaultSessionMissing:
			code = runtimes.FailureCodeSessionNotFound
			output.Session.State = runtimes.SessionStateMissing
		case c.FaultInvalidSpec:
			code = runtimes.FailureCodeInvalidRequest
		case c.FaultUnsupported:
			code = runtimes.FailureCodeUnsupportedCapability
		case c.FaultDelivery:
			code = runtimes.FailureCodeEventDelivery
		case c.FaultLimit:
			code = runtimes.FailureCodeFrameTooLarge
		case c.FaultInactivityTimeout:
			code = runtimes.FailureCodeInactivityTimeout
		case c.FaultCancelled:
			code = runtimes.FailureCodeContextCanceled
		}
		if !result.Effects.PromptAccepted {
			stage = constants.RuntimeRunFailureStagePrepare
		}
		transience := runtimes.FailureTransiencePermanent
		if code == runtimes.FailureCodeRateLimited || code == runtimes.FailureCodeUpstreamUnavailable {
			transience = runtimes.FailureTransienceTransient
		}
		output.Failure = &runtimes.Failure{Code: code, Stage: stage, Message: fault.Error(), DisplayMessage: runtimes.FailureDisplayMessage(fault), SessionState: output.Session.State, SideEffects: output.SideEffects, Transience: transience}
		if errors.Is(fault, context.Canceled) {
			output.TerminationReason = constants.RuntimeTerminationReasonCancelled
		}
	} else if result.Status == c.StatusCancelled || result.StopReason == c.StopCancelled {
		output.ExitCode = -1
		output.TerminationReason = constants.RuntimeTerminationReasonCancelled
		output.Failure = &runtimes.Failure{Code: runtimes.FailureCodeContextCanceled, Stage: constants.RuntimeACPFailureStagePrompt, Message: context.Canceled.Error(), DisplayMessage: runtimes.FailureDisplayMessage(context.Canceled), SessionState: output.Session.State, SideEffects: output.SideEffects, Transience: runtimes.FailureTransiencePermanent}
	}
	return output
}

package agent

import (
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// RecoverCompletedAnswer verifies exact native completion and the evidence required to deliver its answer.
func (l *ThreadLifecycle) RecoverCompletedAnswer(ctx context.Context, chatID, executionID string) (bool, string, error) {
	ref := HostInputRef{ChatID: chatID, HostInvocationID: executionID}
	input, consumption, err := l.store.ReadHostInputConsumption(ctx, ref)
	if err != nil {
		return false, "", err
	}
	if input.ExecutionID != executionID || input.AssistantMessageID != executionID {
		return false, "", status_error.ChatLogCorrupted
	}
	var result *NativeResult
	if consumption != nil {
		terminal, err := l.store.ReadNativeResult(ctx, consumption.Result)
		if err != nil {
			return false, "", err
		}
		result = &terminal
	} else if reader, ok := l.store.(interface {
		ReadNativeResultForInput(context.Context, HostInput) (*NativeResult, error)
	}); ok {
		result, err = reader.ReadNativeResultForInput(ctx, input)
		if err != nil {
			return false, "", err
		}
	}
	if result == nil || result.Turn.Status != acpmeta.NativeTurnCompleted || result.Failure != nil || len(result.Turn.ConsumedInputIDs) != 1 ||
		slices.Contains(result.Unavailable, acpmeta.ReceiptConsumedInputsUnavailable) {
		return false, "", nil
	}
	if result.Key.Session != input.Session || result.Turn.ConsumedInputIDs[0] != input.AdapterInputID {
		return false, "", status_error.ChatLogCorrupted
	}
	if consumption == nil {
		if _, err := l.linkNativeConsumption(ctx, DurableHostInput{Ref: ref, Input: input}, *result); err != nil {
			return false, "", err
		}
	}
	if result.FinalMessage == nil && !result.StreamAnswerEligible {
		return true, "", nil
	}
	outcome := &AgentNativeOutcome{Turn: result.Turn, StopReason: result.StopReason}
	if final := result.FinalMessage; final != nil {
		outcome.FinalMessage = &AgentFinalMessage{ID: final.ID, Text: final.Text}
	}
	answer, err := l.DirectAnswer(ctx, chatID, executionID, TurnResult{
		BusinessAdmitted: true,
		Binding:          NativeBinding{Session: input.Session},
		Input:            AgentPromptSubmission{HostInvocationID: input.HostInvocationID, InputID: input.AdapterInputID},
		Outcome:          outcome,
	})
	return true, answer, err
}

// DirectAnswer reads only the native invocation that consumed this admitted host input.
// An empty result means the turn did not prove a final answer for the user.
func (l *ThreadLifecycle) DirectAnswer(ctx context.Context, chatID, executionID string, result TurnResult) (string, error) {
	outcome := result.Outcome
	if !result.BusinessAdmitted || outcome == nil || outcome.Turn.Status != acpmeta.NativeTurnCompleted ||
		result.Input.HostInvocationID != executionID || result.Input.InputID == "" ||
		len(outcome.Turn.ConsumedInputIDs) != 1 || outcome.Turn.ConsumedInputIDs[0] != result.Input.InputID {
		return "", nil
	}
	key := NativeResultKey{Session: result.Binding.Session, InvocationID: outcome.Turn.InvocationID}
	// WorkBuddy's ACP end_turn can omit finalMessage while identifying the assistant's message lane.
	streamOnly := outcome.FinalMessage == nil
	if streamOnly && (key.Session.Provider != constants.RuntimeIDWorkBuddy || outcome.StopReason != "end_turn" || result.InteractionRequested) {
		return "", nil
	}
	if !streamOnly && (outcome.FinalMessage.ID == "" || strings.TrimSpace(outcome.FinalMessage.Text) == "") {
		return "", nil
	}
	rows, err := l.store.ReadTurn(ctx, chatID, executionID)
	if err != nil {
		return "", err
	}
	var answer strings.Builder
	var lane string
	sawComplete := false
	sawNativeFinal := false
	for _, row := range rows {
		if fact := row.Change.Fact; streamOnly && fact != nil {
			switch fact.Type {
			case constants.ChatEventTypeApprovalRequested, constants.ChatEventTypeApprovalResolved,
				constants.ChatEventTypeElicitationRequested, constants.ChatEventTypeElicitationResolved,
				constants.ChatEventTypeAgentCommandCommitted, constants.ChatEventTypeToolMessage:
				return "", nil
			}
		}
		msg := row.Change.Message
		if msg == nil {
			continue
		}
		if msg.MessageID != executionID || msg.ExecutionID != executionID {
			return "", nil
		}
		switch msg.Kind {
		case MessageComplete:
			sawComplete = true
			continue
		case MessageNativeFinal:
			if msg.Result == nil || *msg.Result != key {
				return "", nil
			}
			sawNativeFinal = true
			continue
		case MessageNativeFragment:
		default:
			return "", nil
		}
		ref := msg.Fragments
		if ref == nil || ref.Key != key {
			return "", nil
		}
		// The provider identifies the final answer separately from tool output and commentary.
		// Only the stream-only fallback needs to inspect individual fragments.
		if !streamOnly {
			continue
		}
		changes, err := l.store.ReadNativeFragments(ctx, *ref)
		if err != nil {
			return "", err
		}
		for _, change := range changes {
			switch change.ContentType {
			case constants.RuntimeEventTypeTextDelta:
				if change.Kind != MessageAppend || ref.MessageID == "" || (lane != "" && lane != ref.MessageID) {
					return "", nil
				}
				lane = ref.MessageID
				answer.WriteString(change.Text)
			case constants.RuntimeEventTypeThinkingDelta:
				// Only an uninterrupted closing assistant lane is deliverable without a native final.
				if lane != "" {
					return "", nil
				}
			default:
				return "", nil
			}
		}
	}
	if !sawComplete || (!streamOnly && !sawNativeFinal) {
		return "", nil
	}
	if !streamOnly {
		return outcome.FinalMessage.Text, nil
	}
	if strings.TrimSpace(answer.String()) == "" {
		return "", nil
	}
	return answer.String(), nil
}

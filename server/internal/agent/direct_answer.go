package agent

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// DirectAnswer reads only the native invocation that consumed this admitted host input.
// An empty result means the turn did not prove a final answer for the user.
func (l *ThreadLifecycle) DirectAnswer(ctx context.Context, chatID, executionID string, result TurnResult) (string, error) {
	outcome := result.Outcome
	if !result.BusinessAdmitted || result.InteractionRequested || outcome == nil || outcome.Turn.Status != acpmeta.NativeTurnCompleted ||
		result.Input.HostInvocationID != executionID || result.Input.InputID == "" ||
		len(outcome.Turn.ConsumedInputIDs) != 1 || outcome.Turn.ConsumedInputIDs[0] != result.Input.InputID {
		return "", nil
	}
	key := NativeResultKey{Session: result.Binding.Session, InvocationID: outcome.Turn.InvocationID}
	// WorkBuddy's ACP end_turn can omit finalMessage while identifying the assistant's message lane.
	streamOnly := outcome.FinalMessage == nil
	if streamOnly && (key.Session.Provider != constants.RuntimeIDWorkBuddy || outcome.StopReason != "end_turn") {
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
		if fact := row.Change.Fact; fact != nil {
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

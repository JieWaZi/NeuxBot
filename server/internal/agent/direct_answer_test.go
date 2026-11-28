package agent

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

type directAnswerStore struct {
	// ConversationStore supplies unused ports.
	ConversationStore
	// rows holds business facts and exact native fragment references for this input.
	rows []ConversationRecord
	// fragments holds adapter-normalized segments by native cursor.
	fragments []MessageChange
	// input is the exact admitted input read by restart reconciliation.
	input HostInput
	// terminal is the terminal native evidence retained for that input.
	terminal NativeResult
}

func (s *directAnswerStore) ReadTurn(context.Context, string, string) ([]ConversationRecord, error) {
	return s.rows, nil
}

func (s *directAnswerStore) ReadNativeFragments(_ context.Context, ref NativeFragmentRange) ([]MessageChange, error) {
	return []MessageChange{s.fragments[ref.First-1]}, nil
}

func (s *directAnswerStore) ReadHostInputConsumption(context.Context, HostInputRef) (HostInput, *InputConsumption, error) {
	return s.input, &InputConsumption{HostInvocationID: s.input.HostInvocationID, Result: s.terminal.Key}, nil
}

func (s *directAnswerStore) ReadNativeResult(context.Context, NativeResultKey) (NativeResult, error) {
	return s.terminal, nil
}

func TestDirectAnswerRequiresOneProvenNativeReply(t *testing.T) {
	for _, test := range []struct {
		// name identifies the native evidence.
		name string
		// want is the only text permitted for delivery.
		want string
	}{
		{"workbuddy split text", "two chunks"},
		{"native final", "provider answer"},
		{"native final after commentary", "provider answer"},
		{"native final after approval", "provider answer"},
		{"native final after host command", "provider answer"},
		{"native empty", ""},
		{"other provider without final", ""},
		{"multiple native messages", ""},
		{"thought after body", ""},
		{"mixed commentary", ""},
		{"all reasoning", ""},
		{"incomplete native turn", ""},
		{"without end turn", ""},
		{"two inputs", ""},
		{"other invocation", ""},
		{"native approval refused without record", ""},
		{"approval request only", ""},
		{"approval rejected without tool part", ""},
		{"elicitation only", ""},
		{"command only", ""},
		{"completed file tool with final", "provider answer"},
		{"native final without durable reference", ""},
		{"tool with streamed body", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := NativeResultKey{Session: NativeSessionKey{Provider: constants.RuntimeIDWorkBuddy, Subject: "account", SessionID: "s"}, InvocationID: "n"}
			outcome := &AgentNativeOutcome{Turn: acpmeta.NativeTurn{Status: acpmeta.NativeTurnCompleted, InvocationID: "n", ConsumedInputIDs: []string{"input"}}, StopReason: "end_turn"}
			result := TurnResult{BusinessAdmitted: true, Outcome: outcome, Binding: NativeBinding{Session: key.Session}, Input: AgentPromptSubmission{HostInvocationID: "work", InputID: "input"}}
			store := &directAnswerStore{}
			add := func(lane string, contentType constants.RuntimeEventType, body string) {
				pos := int64(len(store.fragments) + 1)
				store.fragments = append(store.fragments, MessageChange{Kind: MessageAppend, ContentType: contentType, Text: body})
				store.rows = append(store.rows, ConversationRecord{Change: ConversationChange{Message: &MessageChange{Kind: MessageNativeFragment, MessageID: "work", ExecutionID: "work", Fragments: &NativeFragmentRange{Key: key, MessageID: lane, First: pos, Last: pos}}}})
			}
			fact := func(k constants.ChatEventType) {
				store.rows = append(store.rows, ConversationRecord{Change: ConversationChange{Fact: &ChatFact{Type: k, ExecutionID: "work"}}})
			}
			add("thought", constants.RuntimeEventTypeThinkingDelta, "private thought")
			if test.name != "all reasoning" {
				add("message", constants.RuntimeEventTypeTextDelta, "two ")
			}
			switch test.name {
			case "native final", "native empty", "native final after commentary", "native final after approval", "native final after host command",
				"completed file tool with final", "native final without durable reference":
				outcome.FinalMessage = &AgentFinalMessage{ID: "message", Text: "provider answer"}
			case "other provider without final":
				result.Binding.Session.Provider = constants.RuntimeIDCodex
			case "multiple native messages":
				add("another", constants.RuntimeEventTypeTextDelta, "second message")
			case "thought after body":
				add("thought", constants.RuntimeEventTypeThinkingDelta, "private")
			case "incomplete native turn":
				outcome.Turn.Status = acpmeta.NativeTurnCancelled
			case "without end turn":
				outcome.StopReason = "max_tokens"
			case "two inputs":
				outcome.Turn.ConsumedInputIDs = append(outcome.Turn.ConsumedInputIDs, "other")
			case "other invocation":
				outcome.Turn.InvocationID = "other"
			case "native approval refused without record":
				result.InteractionRequested = true
			case "approval request only":
				fact(constants.ChatEventTypeApprovalRequested)
			case "approval rejected without tool part":
				fact(constants.ChatEventTypeApprovalResolved)
			case "elicitation only":
				fact(constants.ChatEventTypeElicitationRequested)
			case "command only":
				fact(constants.ChatEventTypeAgentCommandCommitted)
			}
			switch test.name {
			case "native empty":
				outcome.FinalMessage.Text = ""
			case "mixed commentary", "native final after commentary":
				add("comment", constants.RuntimeEventTypeCommentaryDelta, "progress")
			case "completed file tool with final", "tool with streamed body":
				add("file", constants.RuntimeEventTypeToolResult, "tool output")
			case "native final after approval":
				result.InteractionRequested = true
				fact(constants.ChatEventTypeApprovalRequested)
				fact(constants.ChatEventTypeApprovalResolved)
			case "native final after host command":
				fact(constants.ChatEventTypeAgentCommandCommitted)
			}
			if test.name != "all reasoning" {
				add("message", constants.RuntimeEventTypeTextDelta, "chunks")
			}
			if outcome.FinalMessage != nil && test.name != "native final without durable reference" {
				store.rows = append(store.rows, ConversationRecord{Change: ConversationChange{Message: &MessageChange{Kind: MessageNativeFinal, MessageID: "work", ExecutionID: "work", Result: &key}}})
			}
			store.rows = append(store.rows, ConversationRecord{Change: ConversationChange{Message: &MessageChange{Kind: MessageComplete, MessageID: "work", ExecutionID: "work"}}})
			l := &ThreadLifecycle{store: store}
			got, err := l.DirectAnswer(t.Context(), "chat", "work", result)
			if err != nil || got != test.want {
				t.Fatalf("answer=%q,err=%v", got, err)
			}
			if test.name == "workbuddy split text" || test.name == "mixed commentary" {
				store.input = HostInput{HostInvocationID: "work", AdapterInputID: "input", Session: key.Session, ExecutionID: "work", AssistantMessageID: "work"}
				store.terminal = NativeResult{Key: key, Turn: outcome.Turn, StopReason: outcome.StopReason, StreamAnswerEligible: true}
				complete, answer, err := l.RecoverCompletedAnswer(t.Context(), "chat", "work")
				if err != nil || !complete || answer != test.want {
					t.Fatalf("recovered answer=%q,complete=%t,err=%v", answer, complete, err)
				}
				store.terminal.StreamAnswerEligible = false
				complete, answer, err = l.RecoverCompletedAnswer(t.Context(), "chat", "work")
				if err != nil || !complete || answer != "" {
					t.Fatalf("unverified stream recovered answer=%q,complete=%t,err=%v", answer, complete, err)
				}
			}
		})
	}
}

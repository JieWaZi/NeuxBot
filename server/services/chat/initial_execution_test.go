package chat

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestReadExecutionOutcomeRecoversLegacyAndLastMatchingInvocation(t *testing.T) {
	m, chatID := forkFixture(t)
	legacy, err := m.ReadExecutionOutcome(context.Background(), chatID, "before")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.TerminationReason != constants.RuntimeTerminationReasonUnknown || legacy.StopReason != "" || legacy.Execution.Status != constants.ExecutionStatusCompleted {
		t.Fatalf("legacy guessed complete: %+v", legacy)
	}
	_, err = m.logStore.Append(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "target", Data: marshalData(eventlog.ExecutionCreatedData{})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "target", InvocationID: "retry-1"},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "target", InvocationID: "retry-1", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, StopReason: "end_turn", TerminationReason: constants.RuntimeTerminationReasonNormal})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "target", InvocationID: "retry-2"},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "target", InvocationID: "retry-2", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, StopReason: "max_tokens", TerminationReason: constants.RuntimeTerminationReasonTruncated})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "target", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "later", Data: marshalData(eventlog.ExecutionCreatedData{})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "later", InvocationID: "unrelated"},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "later", InvocationID: "unrelated", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, StopReason: "end_turn", TerminationReason: constants.RuntimeTerminationReasonNormal})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "later", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 替换进程内日志 Store，重新从同一路径的权威 JSONL 恢复。
	reopened, err := eventlog.NewStore(m.logStore.Root())
	if err != nil {
		t.Fatal(err)
	}
	m.logStore = reopened
	outcome, err := m.ReadExecutionOutcome(context.Background(), chatID, "target")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.InvocationID != "retry-2" || outcome.TerminationReason != constants.RuntimeTerminationReasonTruncated || outcome.StopReason != "max_tokens" {
		t.Fatalf("wrong invocation: %+v", outcome)
	}
}

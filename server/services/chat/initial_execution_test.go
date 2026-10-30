package chat

import (
	"context"
	"fmt"
	"os"
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

// TestExecutionReadsIgnoreLongMessagePrefix 验证单执行状态读取不解码其他回合正文。
func TestExecutionReadsIgnoreLongMessagePrefix(t *testing.T) {
	measure := func(count int) float64 {
		m, chatID := forkFixture(t)
		inputs := make([]eventlog.AppendInput, 0, count*3+2)
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("prefix-%d", i)
			inputs = append(inputs,
				eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: marshalData(eventlog.ExecutionCreatedData{})},
				eventlog.AppendInput{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: id, InvocationID: id, Data: marshalData(eventlog.RuntimeMessageData{MessageRevision: 1})},
				eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
			)
		}
		inputs = append(inputs, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "wanted", Data: marshalData(eventlog.ExecutionCreatedData{})}, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "wanted", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})})
		if _, err := m.logStore.Append(chatID, inputs); err != nil {
			t.Fatal(err)
		}
		if _, err := m.ReadExecutionOutcome(context.Background(), chatID, "wanted"); err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(1, func() {
			outcome, err := m.ReadExecutionOutcome(context.Background(), chatID, "wanted")
			if err != nil || outcome.Execution.Status != constants.ExecutionStatusCompleted {
				t.Fatalf("outcome = %+v, %v", outcome, err)
			}
		})
	}
	small, large := measure(8), measure(256)
	if large > small*2 {
		t.Fatalf("single execution decoded long prefix: %.0f -> %.0f allocations", small, large)
	}
}

// TestExactExecutionReadsRepairIndexesAndKeepRequestedMessages 覆盖文件索引缺失、SQLite定位缺失及首执行消息精确范围。
func TestExactExecutionReadsRepairIndexesAndKeepRequestedMessages(t *testing.T) {
	m, id := forkFixture(t)
	ctx := context.Background()
	before, err := m.ReadExecutionOutcome(ctx, id, "before")
	if err != nil {
		t.Fatal(err)
	}
	path, err := m.logStore.IndexPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := m.queryIndexes.chatController.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	recovered, err := m.ReadExecutionOutcome(ctx, id, "before")
	if err != nil || recovered.Execution.ID != "before" || recovered.StartSequence != before.StartSequence || recovered.LatestSequence != before.LatestSequence {
		t.Fatalf("recovered=%+v %v", recovered, err)
	}
	messages, err := m.ReadExecutionMessages(ctx, id, "before")
	if err != nil || len(messages) != 2 || messages[0].ID != "user-before" || messages[1].ID != "before" {
		t.Fatalf("exact messages=%+v %v", messages, err)
	}
	receipt, err := m.ReadSubmission(ctx, id, "new-unknown-submission")
	if err != nil || receipt != nil {
		t.Fatalf("missing receipt=%+v %v", receipt, err)
	}
}

// TestExecutionPositionTailKeepsFinishedInvocationAcrossGrowingBody 验证只增量推进位置并区分未完成新调用与已持久调用结论。
func TestExecutionPositionTailKeepsFinishedInvocationAcrossGrowingBody(t *testing.T) {
	m, id := forkFixture(t)
	ctx := context.Background()
	appendFacts := func(facts ...eventlog.AppendInput) {
		t.Helper()
		if _, err := m.logStore.Append(id, facts); err != nil {
			t.Fatal(err)
		}
	}
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "growing", Data: marshalData(eventlog.ExecutionCreatedData{SubmissionID: "growing-input"})}, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "growing"})
	pending, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || pending.Execution.Status != constants.ExecutionStatusRunning || pending.TerminationReason != constants.RuntimeTerminationReasonUnknown || pending.InvocationID != "" {
		t.Fatalf("unfinished guessed result: %+v %v", pending, err)
	}
	for _, count := range []int{8, 1024} {
		facts := make([]eventlog.AppendInput, 0, count)
		for i := 0; i < count; i++ {
			facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeAssistantMessage, InvocationID: "body-only", Data: marshalData(eventlog.RuntimeMessageData{MessageRevision: int64(i + 1)})})
		}
		appendFacts(facts...)
		outcome, err := m.ReadExecutionOutcome(ctx, id, "growing")
		if err != nil || outcome.Execution.Revision != 2 || outcome.InvocationID != "" || outcome.TerminationReason != constants.RuntimeTerminationReasonUnknown {
			t.Fatalf("body changed lifecycle: %+v %v", outcome, err)
		}
		location, covered, err := m.locateExecution(ctx, id, "growing", "")
		if err != nil || len(location.LifecycleSequences) != 1 || location.InvocationFinishedSequence != 0 || location.InvocationBoundarySequence == 0 || covered != outcome.LatestSequence {
			t.Fatalf("tail positions=%+v covered=%d outcome=%+v err=%v", location, covered, outcome, err)
		}
	}
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeInvocationFinished, InvocationID: "body-only", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, TerminationReason: constants.RuntimeTerminationReasonNormal, StopReason: "end_turn"})})
	first, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || first.InvocationID != "body-only" || first.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("first finished=%+v %v", first, err)
	}
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "growing", InvocationID: "next-incomplete"})
	later, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || later.InvocationID != "body-only" || later.Execution.Revision != 3 {
		t.Fatalf("new unfinished call overwrote durable result: %+v %v", later, err)
	}
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeInvocationFinished, InvocationID: "next-incomplete", Data: marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, TerminationReason: constants.RuntimeTerminationReasonTruncated, StopReason: "max_tokens"})}, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "growing", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: "DELIVERY_FAILED"})})
	final, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || final.InvocationID != "next-incomplete" || final.TerminationReason != constants.RuntimeTerminationReasonTruncated || final.StopReason != "max_tokens" || final.Execution.Status != constants.ExecutionStatusFailed || final.Execution.ErrorCode != "DELIVERY_FAILED" {
		t.Fatalf("last invocation/terminal=%+v %v", final, err)
	}
	receipt, err := m.ReadSubmission(ctx, id, "growing-input")
	if err != nil || receipt == nil || receipt.ID != "growing" || receipt.Status != constants.ExecutionStatusFailed {
		t.Fatalf("exact receipt=%+v %v", receipt, err)
	}
}

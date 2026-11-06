package chat

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestReadExecutionOutcomeSelectsLastMatchingInvocation(t *testing.T) {
	root := t.TempDir()
	_, m := newRecoveryTestService(t, root)
	chatID := "12345678901234567890123456789012"
	_, err := m.logStore.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "before", Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "before", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "target", Data: []byte(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	appendNativeOutcome(t, m, chatID, "target", "retry-1", "end_turn")
	appendNativeOutcome(t, m, chatID, "target", "retry-2", "max_tokens")
	if _, err := m.logStore.Append(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "target", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "later", Data: []byte(`{}`)},
	}); err != nil {
		t.Fatal(err)
	}
	appendNativeOutcome(t, m, chatID, "later", "unrelated", "end_turn")
	first, err := m.ReadExecutionOutcome(context.Background(), chatID, "before")
	if err != nil {
		t.Fatal(err)
	}
	if first.TerminationReason != constants.RuntimeTerminationReasonUnknown || first.StopReason != "" || first.Execution.Status != constants.ExecutionStatusCompleted {
		t.Fatalf("unreported termination was inferred: %+v", first)
	}
	outcome, err := m.ReadExecutionOutcome(context.Background(), chatID, "target")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.InvocationID != "retry-2" || outcome.TerminationReason != constants.RuntimeTerminationReasonTruncated || outcome.StopReason != "max_tokens" {
		t.Fatalf("wrong invocation: %+v", outcome)
	}
}

func appendNativeOutcome(t *testing.T, m *service, chatID, executionID, invocationID, stopReason string) {
	t.Helper()
	ctx := t.Context()
	store := m.logStore.(*conversationJournal).Store
	session := agent.NativeSessionKey{Provider: "codex", Subject: "fixture", SessionID: chatID}
	key := agent.NativeResultKey{Session: session, InvocationID: invocationID}
	hostID, inputID := "host-"+invocationID, "input-"+invocationID
	if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, StopReason: stopReason, Turn: acpmeta.NativeTurn{
		InvocationID: invocationID, Origin: acpmeta.NativeTurnOriginHost,
		Status: acpmeta.NativeTurnCompleted, ConsumedInputIDs: []string{inputID},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := conversationfixture.Commit(ctx, store, chatID, []agent.ConversationChange{
		{Binding: &agent.NativeBinding{Session: session}},
		{Input: &agent.HostInput{Session: session, HostInvocationID: hostID, AdapterInputID: inputID, ExecutionID: executionID, AssistantMessageID: executionID}},
		{Consumption: &agent.InputConsumption{HostInvocationID: hostID, Result: key}},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestExecutionReadsIgnoreLongMessagePrefix 验证单执行状态读取不解码其他回合正文。
func TestExecutionReadsIgnoreLongMessagePrefix(t *testing.T) {
	measure := func(count int) float64 {
		m, chatID := forkFixture(t)
		store := m.logStore.(*conversationJournal).Store
		var inputs []agent.ConversationChange
		for i := range count {
			id := fmt.Sprintf("prefix-%d", i)
			inputs = append(inputs,
				agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: []byte(`{}`)}},
				conversationfixture.NativeMessage(t, store, chatID, id, agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "historical answer"}),
				agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})}},
			)
		}
		inputs = append(inputs, conversationfixture.Facts([]eventlog.AppendInput{
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "wanted", Data: []byte(`{}`)},
			{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "wanted", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		})...)
		if _, err := conversationfixture.Commit(t.Context(), store, chatID, inputs); err != nil {
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
	m.logStore.(*conversationJournal).Store.Close()
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
		store := m.logStore.(*conversationJournal).Store
		changes := make([]agent.ConversationChange, count)
		for i := range changes {
			changes[i] = agent.ConversationChange{Message: &agent.MessageChange{
				MessageID: "growing", ExecutionID: "growing", Kind: agent.MessageComplete, Revision: int64(i + 1),
			}}
		}
		if _, err := conversationfixture.Commit(t.Context(), store, id, changes); err != nil {
			t.Fatal(err)
		}
	}
	appendNativeOutcome(t, m, id, "growing", "body-only", "end_turn")
	first, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || first.InvocationID != "body-only" || first.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("first finished=%+v %v", first, err)
	}
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "growing", InvocationID: "next-incomplete"})
	later, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || later.InvocationID != "body-only" || later.Execution.Revision != 3 {
		t.Fatalf("new unfinished call overwrote durable result: %+v %v", later, err)
	}
	appendNativeOutcome(t, m, id, "growing", "next-incomplete", "max_tokens")
	appendFacts(eventlog.AppendInput{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "growing", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: "DELIVERY_FAILED"})})
	final, err := m.ReadExecutionOutcome(ctx, id, "growing")
	if err != nil || final.InvocationID != "next-incomplete" || final.TerminationReason != constants.RuntimeTerminationReasonTruncated || final.StopReason != "max_tokens" || final.Execution.Status != constants.ExecutionStatusFailed || final.Execution.ErrorCode != "DELIVERY_FAILED" {
		t.Fatalf("last invocation/terminal=%+v %v", final, err)
	}
	receipt, err := m.ReadSubmission(ctx, id, "growing-input")
	if err != nil || receipt == nil || receipt.ID != "growing" || receipt.Status != constants.ExecutionStatusFailed {
		t.Fatalf("exact receipt=%+v %v", receipt, err)
	}
}

func TestExecutionOutcomeKeepsApplicationAndRuntimeFailureSeparate(t *testing.T) {
	_, m := newRecoveryTestService(t, t.TempDir())
	chatID := "12345678901234567890123456789012"
	_, err := m.logStore.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "target", Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "target", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeWorkspaceScopeUnsupported", ErrorParams: map[string]interface{}{"MaxDirectories": 2}, ErrorMessageSource: constants.ErrorMessageSourceLocalized, FailureCode: constants.RuntimeFailureCodeEventDelivery})},
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := m.ReadExecutionOutcome(t.Context(), chatID, "target")
	if err != nil {
		t.Fatal(err)
	}
	event := executionOutcomeEvent(outcome)
	if outcome.FailureCode != constants.RuntimeFailureCodeEventDelivery || event.ErrorCode != "RuntimeWorkspaceScopeUnsupported" || event.ErrorParams["MaxDirectories"] != float64(2) || event.ErrorMessageSource != "localized" || event.ErrorMessage != "" {
		t.Fatalf("outcome=%+v event=%+v", outcome, event)
	}
}

func TestExecutionReadsPreserveExplicitFailureSource(t *testing.T) {
	for _, source := range []constants.ErrorMessageSource{constants.ErrorMessageSourceUnspecified, constants.ErrorMessageSourceLocalized, constants.ErrorMessageSourcePublic} {
		_, m := newRecoveryTestService(t, t.TempDir())
		ctx := t.Context()
		chatID := "12345678901234567890123456789012"
		_, err := m.logStore.Create(chatID, []eventlog.AppendInput{
			{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "target", Data: []byte(`{}`)},
		})
		if err != nil {
			t.Fatal(err)
		}
		store := m.logStore.(*conversationJournal).Store
		session := agent.NativeSessionKey{Provider: "codex", Subject: "fixture", SessionID: chatID}
		key := agent.NativeResultKey{Session: session, InvocationID: "native-failure"}
		if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, Failure: &acpmeta.NativeTurnFailure{Message: "Native quota exhausted"}, Turn: acpmeta.NativeTurn{InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnFailed, ConsumedInputIDs: []string{"native-input"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := conversationfixture.Commit(ctx, store, chatID, []agent.ConversationChange{
			{Binding: &agent.NativeBinding{Session: session}},
			{Input: &agent.HostInput{Session: session, HostInvocationID: "host", AdapterInputID: "native-input", ExecutionID: "target", AssistantMessageID: "target"}},
			{Consumption: &agent.InputConsumption{HostInvocationID: "host", Result: key}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "target", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeQuotaExhausted", ErrorMessage: "Native quota exhausted", ErrorMessageSource: source, FailureCode: constants.RuntimeFailureCodeQuotaExhausted})}}); err != nil {
			t.Fatal(err)
		}
		initial, err := m.ReadInitialExecution(ctx, chatID)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := m.ReadExecutionOutcome(ctx, chatID, "target")
		if err != nil {
			t.Fatal(err)
		}
		if initial.InitialExecution == nil || initial.InitialExecution.ErrorMessageSource != source || outcome.ErrorMessageSource != source || outcome.FailureCode != constants.RuntimeFailureCodeQuotaExhausted {
			t.Fatalf("source=%q initial=%+v outcome=%+v", source, initial.InitialExecution, outcome)
		}
	}
}

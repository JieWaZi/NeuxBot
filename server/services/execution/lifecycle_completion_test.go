package execution

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// terminalJournal 记录执行终态的 durable 与公开两阶段交接。
type terminalJournal struct {
	// Journal 保留本测试不使用的执行查询边界。
	chatservice.Journal
	// terminal 是最近一次先持久的执行终态。
	terminal chatservice.TerminalFact
	// published 在注册表交出执行占用后接收公开通知。
	published chan struct{}
}

// ForkChat 不参与终态交接测试。
func (j *terminalJournal) ForkChat(context.Context, chatservice.ForkInput, chatservice.NativeFork) (string, error) {
	panic("unexpected ForkChat call")
}

// LoadExecutionContext 不参与终态交接测试。
func (j *terminalJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	panic("unexpected LoadExecutionContext call")
}

// CommitStartedTurn 不参与终态交接测试。
func (j *terminalJournal) CommitStartedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitStartedTurn call")
}

// CommitAcceptedTurn 不参与终态交接测试。
func (j *terminalJournal) CommitAcceptedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitAcceptedTurn call")
}

// MarkExecutionStarted 不参与终态交接测试。
func (j *terminalJournal) MarkExecutionStarted(context.Context, models.Execution, string) (models.Execution, error) {
	panic("unexpected MarkExecutionStarted call")
}

// ReadRecords 不参与终态交接测试。
func (j *terminalJournal) ReadRecords(string) ([]eventlog.Record, error) {
	panic("unexpected ReadRecords call")
}

// AppendExecutionFacts 不参与终态交接测试。
func (j *terminalJournal) AppendExecutionFacts(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error) {
	panic("unexpected AppendExecutionFacts call")
}

// AppendApprovalFact 不参与终态交接测试。
func (j *terminalJournal) AppendApprovalFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendApprovalFact call")
}

// AppendElicitationFact 不参与终态交接测试。
func (j *terminalJournal) AppendElicitationFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendElicitationFact call")
}

// PublishApprovalFact 不参与终态交接测试。
func (j *terminalJournal) PublishApprovalFact(context.Context, eventlog.Record, []models.PendingApproval) {
	panic("unexpected PublishApprovalFact call")
}

// PublishElicitationFact 不参与终态交接测试。
func (j *terminalJournal) PublishElicitationFact(context.Context, eventlog.Record, []models.PendingElicitation) {
	panic("unexpected PublishElicitationFact call")
}

// CommitTerminal 记录已经 durable 但尚未公开的终态。
func (j *terminalJournal) CommitTerminal(_ context.Context, fact chatservice.TerminalFact) (chatservice.TerminalCommit, error) {
	j.terminal = fact
	return chatservice.TerminalCommit{}, nil
}

// PublishTerminal 在执行占用交出时公开终态。
func (j *terminalJournal) PublishTerminal(chatservice.TerminalCommit) {
	j.published <- struct{}{}
}

// PublishLive 不参与终态交接测试。
func (j *terminalJournal) PublishLive(string, eventstream.PublicEvent) {
	panic("unexpected PublishLive call")
}

func TestFinishExecutionReleasesReservationAndPublishesDurableTerminal(t *testing.T) {
	chatID := "0123456789abcdef0123456789abcdef"
	executionID := "11111111111111111111111111111111"
	registry := newExecutionRegistry()
	registry.start(context.Background())
	t.Cleanup(registry.close)
	journal := &terminalJournal{published: make(chan struct{}, 1)}
	executionCore := &service{journal: journal, executions: registry}
	if _, err := registry.reserve(chatID, executionID); err != nil {
		t.Fatalf("first reserve() error = %v", err)
	}

	executionCore.finishExecution(
		models.Chat{ID: chatID},
		models.Execution{ID: executionID, ChatID: chatID},
		constants.ExecutionStatusCompleted,
		"",
		"",
		0,
		"",
		nil,
		0,
		false,
	)
	select {
	case <-journal.published:
	default:
		t.Fatal("durable terminal was not published")
	}
	if journal.terminal.Status != constants.ExecutionStatusCompleted {
		t.Fatalf("terminal status = %q", journal.terminal.Status)
	}
	if _, err := registry.reserve(
		chatID,
		"22222222222222222222222222222222",
	); err != nil {
		t.Fatalf("reserve after visible terminal event error = %v", err)
	}
	registry.release(chatID, "22222222222222222222222222222222")
}

func TestExecutionOutcomeIsDurableAndKeepsChatCompletedSemantics(t *testing.T) {
	cases := []struct {
		// raw 是 Runtime 报告的协议停止原因。
		raw string
		// reason 是 Runtime 归一化的完整性结论。
		reason constants.RuntimeTerminationReason
	}{{"end_turn", constants.RuntimeTerminationReasonNormal}, {"max_tokens", constants.RuntimeTerminationReasonTruncated}, {"max_turn_requests", constants.RuntimeTerminationReasonTurnLimit}, {"refusal", constants.RuntimeTerminationReasonRefusal}, {"future", constants.RuntimeTerminationReasonUnknown}, {"", constants.RuntimeTerminationReasonUnspecified}}
	for _, test := range cases {
		t.Run(test.raw, func(t *testing.T) {
			runner := &retryTestRuntime{result: runtimecontract.Result{StopReason: test.raw, TerminationReason: test.reason}}
			manager := newMessageTestService(t, t.TempDir(), runner)
			accepted := retryTestStart(t, manager, retryTestIntent(t))
			outcome, err := manager.chats.ReadExecutionOutcome(context.Background(), accepted.Chat.ID, accepted.Execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := test.reason
			if want == constants.RuntimeTerminationReasonUnspecified {
				want = constants.RuntimeTerminationReasonUnknown
			}
			if outcome.Execution.Status != constants.ExecutionStatusCompleted || outcome.InvocationID == "" || outcome.StopReason != test.raw || outcome.TerminationReason != want {
				t.Fatalf("durable outcome = %+v", outcome)
			}
			// 后续聊天正常完成，不能覆盖指定旧执行的截断或未知结论。
			runner.mutex.Lock()
			runner.result = runtimecontract.Result{StopReason: "end_turn", TerminationReason: constants.RuntimeTerminationReasonNormal}
			runner.mutex.Unlock()
			intent := retryTestIntent(t)
			intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
			intent.ClientRequestID = ""
			retryTestStart(t, manager, intent)
			old, err := manager.chats.ReadExecutionOutcome(context.Background(), accepted.Chat.ID, accepted.Execution.ID)
			if err != nil || old.InvocationID != outcome.InvocationID || old.TerminationReason != want {
				t.Fatalf("later turn overwrote outcome: %+v %v", old, err)
			}
			if _, err := manager.chats.ReadExecutionOutcome(context.Background(), accepted.Chat.ID, "missing"); !errors.Is(err, status_error.ExecutionNotFound) {
				t.Fatalf("missing execution = %v", err)
			}
		})
	}
}

func TestExecutionOutcomeKeepsFailureAlongsideStopReason(t *testing.T) {
	runner := &retryTestRuntime{result: runtimecontract.Result{StopReason: "end_turn", TerminationReason: constants.RuntimeTerminationReasonNormal, Failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeEventDelivery, Stage: "sink", Message: "delivery failed"}}}
	manager := newMessageTestService(t, t.TempDir(), runner)
	accepted := retryTestStart(t, manager, retryTestIntent(t))
	outcome, err := manager.chats.ReadExecutionOutcome(context.Background(), accepted.Chat.ID, accepted.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Execution.Status != constants.ExecutionStatusFailed || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal || outcome.FailureCode != constants.RuntimeFailureCodeEventDelivery || outcome.FailureMessage != "delivery failed" || outcome.FailureStage != "sink" {
		t.Fatalf("failure evidence lost: %+v", outcome)
	}
}

func TestExecutionOutcomePreservesConfirmedCancellation(t *testing.T) {
	runner := &retryTestRuntime{result: runtimecontract.Result{StopReason: "cancelled", TerminationReason: constants.RuntimeTerminationReasonCancelled, Failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeContextCanceled, Stage: "prompt", Message: "cancelled by user"}}}
	manager := newMessageTestService(t, t.TempDir(), runner)
	accepted := retryTestStart(t, manager, retryTestIntent(t))
	outcome, err := manager.chats.ReadExecutionOutcome(context.Background(), accepted.Chat.ID, accepted.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Execution.Status != constants.ExecutionStatusCanceled || outcome.StopReason != "cancelled" || outcome.TerminationReason != constants.RuntimeTerminationReasonCancelled || outcome.FailureMessage != "cancelled by user" {
		t.Fatalf("cancellation facts lost: %+v", outcome)
	}
}

// LoadExecutionChat 不参与此日志边界测试。
func (j *terminalJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	panic("unexpected LoadExecutionChat call")
}

// ReadExecutionRecords 不参与此日志边界测试。
func (j *terminalJournal) ReadExecutionRecords(string, string) ([]eventlog.Record, error) {
	panic("unexpected ReadExecutionRecords call")
}

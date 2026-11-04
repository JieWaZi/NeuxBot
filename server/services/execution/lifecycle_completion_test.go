package execution

import (
	"context"

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

func (j *terminalJournal) ForkChat(context.Context, chatservice.ForkInput, chatservice.NativeFork) (string, error) {
	panic("unexpected ForkChat call")
}

func (j *terminalJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	panic("unexpected LoadExecutionContext call")
}

func (j *terminalJournal) CommitStartedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitStartedTurn call")
}

func (j *terminalJournal) CommitAcceptedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitAcceptedTurn call")
}

func (j *terminalJournal) MarkExecutionStarted(context.Context, models.Execution, string) (models.Execution, error) {
	panic("unexpected MarkExecutionStarted call")
}

func (j *terminalJournal) ReadRecords(string) ([]eventlog.Record, error) {
	panic("unexpected ReadRecords call")
}

func (j *terminalJournal) AppendExecutionFacts(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error) {
	panic("unexpected AppendExecutionFacts call")
}

func (j *terminalJournal) AppendApprovalFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendApprovalFact call")
}

func (j *terminalJournal) AppendElicitationFact(string, eventlog.AppendInput) (eventlog.Record, error) {
	panic("unexpected AppendElicitationFact call")
}

func (j *terminalJournal) PublishApprovalFact(context.Context, eventlog.Record, []models.PendingApproval) {
	panic("unexpected PublishApprovalFact call")
}

func (j *terminalJournal) PublishElicitationFact(context.Context, eventlog.Record, []models.PendingElicitation) {
	panic("unexpected PublishElicitationFact call")
}

func (j *terminalJournal) CommitTerminal(_ context.Context, fact chatservice.TerminalFact) (chatservice.TerminalCommit, error) {
	j.terminal = fact
	return chatservice.TerminalCommit{}, nil
}

func (j *terminalJournal) PublishTerminal(chatservice.TerminalCommit) {
	j.published <- struct{}{}
}

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
	registry.release(chatID, executionID)
}

func (j *terminalJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	panic("unexpected LoadExecutionChat call")
}

func (j *terminalJournal) ReadExecutionRecords(string, string) ([]eventlog.Record, error) {
	panic("unexpected ReadExecutionRecords call")
}

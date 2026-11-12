package execution

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// approvalOutcome 保存审批回调测试收到的最终选择。
type approvalOutcome struct {
	// decision 是协调器返回给 Runtime 的审批选择。
	decision agentapi.ApprovalDecision
	// err 是审批等待或持久化失败。
	err error
}

// approvalTestJournal 为协调器测试保存 durable 事实。
type approvalTestJournal struct {
	// Journal 保留本测试不使用的执行查询边界。
	chatservice.Journal
	// mutex 保护记录序号和事实。
	mutex sync.Mutex
	// records 保存按提交顺序形成的审批事实。
	records []eventlog.Record
}

// ForkChat 不参与审批协调器测试。
func (j *approvalTestJournal) ForkChat(context.Context, chatservice.ForkInput, chatservice.NativeFork) (string, error) {
	panic("unexpected ForkChat call")
}

// LoadExecutionContext 不参与审批协调器测试。
func (j *approvalTestJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	panic("unexpected LoadExecutionContext call")
}

// CommitAcceptedTurn 不参与审批协调器测试。
func (j *approvalTestJournal) CommitAcceptedTurn(context.Context, chatservice.StartedTurn) (chatservice.AcceptedTurn, error) {
	panic("unexpected CommitAcceptedTurn call")
}

// ReadRecords 返回当前测试事实副本。
func (j *approvalTestJournal) ReadRecords(string) ([]eventlog.Record, error) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	return append([]eventlog.Record(nil), j.records...), nil
}

// AppendExecutionFacts 不参与审批协调器测试。
func (j *approvalTestJournal) AppendExecutionFacts(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error) {
	panic("unexpected AppendExecutionFacts call")
}

// AppendInteractionFact 生成连续序号并保存一条 durable 审批事实。
func (j *approvalTestJournal) AppendInteractionFact(chatID string, fact eventlog.AppendInput) (eventlog.Record, error) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	record := eventlog.Record{
		SchemaVersion: 1,
		ChatID:        chatID,
		Sequence:      int64(len(j.records) + 1),
		RecordedAt:    time.Now().UnixMilli(),
		Type:          fact.Type,
		ExecutionID:   fact.ExecutionID,
		Data:          fact.Data,
	}
	j.records = append(j.records, record)
	return record, nil
}

func (*approvalTestJournal) PublishInteractionFact(context.Context, eventlog.Record) {}

// CommitTerminal 不参与审批协调器测试。
func (j *approvalTestJournal) CommitTerminal(context.Context, chatservice.TerminalFact) (chatservice.TerminalCommit, error) {
	panic("unexpected CommitTerminal call")
}

// PublishTerminal 不参与审批协调器测试。
func (j *approvalTestJournal) PublishTerminal(chatservice.TerminalCommit) {
	panic("unexpected PublishTerminal call")
}

// PublishLive 不参与审批协调器测试。
func (j *approvalTestJournal) PublishLive(string, eventstream.PublicEvent) {
	panic("unexpected PublishLive call")
}

func TestApprovalCoordinatorKeepsArrivalOrderAndFirstResponseWins(t *testing.T) {
	core, _ := newApprovalTestService()
	chatID := "abababababababababababababababab"
	first := startApprovalRequest(core, chatID, "execution-1", "tool-1", context.Background())
	firstPending := waitPendingApprovals(t, core.approvals, chatID, 1)
	second := startApprovalRequest(core, chatID, "execution-1", "tool-2", context.Background())
	pending := waitPendingApprovals(t, core.approvals, chatID, 2)
	if pending[0].ToolCallID != "tool-1" || pending[1].ToolCallID != "tool-2" {
		t.Fatalf("pending order = %+v", pending)
	}
	if err := core.ResolveApproval(context.Background(), chatID, pending[1].ApprovalID, "allow-once"); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("out-of-order ResolveApproval() error = %v", err)
	}
	if err := core.ResolveApproval(context.Background(), chatID, firstPending[0].ApprovalID, "allow-once"); err != nil {
		t.Fatalf("first ResolveApproval() error = %v", err)
	}
	if result := <-first; result.err != nil || result.decision.OptionID != "allow-once" {
		t.Fatalf("first outcome = %+v", result)
	}
	if err := core.ResolveApproval(context.Background(), chatID, firstPending[0].ApprovalID, "allow-once"); !errors.Is(err, status_error.ApprovalAlreadyResolved) {
		t.Fatalf("duplicate ResolveApproval() error = %v", err)
	}
	remaining := waitPendingApprovals(t, core.approvals, chatID, 1)
	if err := core.ResolveApproval(context.Background(), chatID, remaining[0].ApprovalID, "reject-once"); err != nil {
		t.Fatalf("second ResolveApproval() error = %v", err)
	}
	if result := <-second; result.err != nil || result.decision.OptionID != "reject-once" {
		t.Fatalf("second outcome = %+v", result)
	}
	waitPendingApprovals(t, core.approvals, chatID, 0)
}

func TestApprovalCoordinatorPreservesUnknownOptionKind(t *testing.T) {
	core, _ := newApprovalTestService()
	chatID := "abababababababababababababababab"
	outcome := make(chan approvalOutcome, 1)
	go func() {
		decision, err := core.approvals.handler(chatID, "execution-1").Request(
			context.Background(),
			agentapi.ApprovalRequest{
				ToolCallID: "tool-custom",
				Title:      "自定义审批",
				Kind:       "other",
				Options: []agentapi.ApprovalOption{{
					ID: "custom-scope", Name: "Allow this repo clone", Kind: "allow_repo",
				}},
			},
		)
		outcome <- approvalOutcome{decision: decision, err: err}
	}()
	pending := waitPendingApprovals(t, core.approvals, chatID, 1)
	if len(pending[0].Options) != 1 || pending[0].Options[0].Kind != "allow_repo" {
		t.Fatalf("pending options = %+v", pending[0].Options)
	}
	if err := core.ResolveApproval(context.Background(), chatID, pending[0].ApprovalID, "custom-scope"); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if result := <-outcome; result.err != nil || result.decision.OptionID != "custom-scope" {
		t.Fatalf("unknown option outcome = %+v", result)
	}
}

func TestApprovalCoordinatorInvalidatesPendingRequestWhenExecutionStops(t *testing.T) {
	core, journal := newApprovalTestService()
	chatID := "abababababababababababababababab"
	ctx, cancel := context.WithCancel(context.Background())
	outcome := startApprovalRequest(core, chatID, "execution-1", "tool-1", ctx)
	waitPendingApprovals(t, core.approvals, chatID, 1)
	cancel()
	if result := <-outcome; result.err != nil || !result.decision.Cancelled {
		t.Fatalf("cancelled outcome = %+v", result)
	}
	waitPendingApprovals(t, core.approvals, chatID, 0)
	records, err := journal.ReadRecords(chatID)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if records[len(records)-1].Type != constants.ChatEventTypeApprovalResolved {
		t.Fatalf("last event = %s", records[len(records)-1].Type)
	}
}

func newApprovalTestService() (*service, *approvalTestJournal) {
	journal := &approvalTestJournal{}
	return &service{approvals: newApprovalCoordinator(journal)}, journal
}

func startApprovalRequest(
	core *service,
	chatID string,
	executionID string,
	toolCallID string,
	ctx context.Context,
) <-chan approvalOutcome {
	outcome := make(chan approvalOutcome, 1)
	go func() {
		decision, err := core.approvals.handler(chatID, executionID).Request(ctx, approvalRequest(toolCallID))
		outcome <- approvalOutcome{decision: decision, err: err}
	}()
	return outcome
}

func approvalRequest(toolCallID string) agentapi.ApprovalRequest {
	return agentapi.ApprovalRequest{
		ToolCallID: toolCallID,
		Title:      "执行测试命令",
		Kind:       "command",
		Command:    "curl https://chatgpt.com/",
		Options: []agentapi.ApprovalOption{
			{ID: "allow-once", Name: "Allow once", Kind: "allow_once"},
			{ID: "reject-once", Name: "Reject", Kind: "reject_once"},
		},
	}
}

func waitPendingApprovals(t *testing.T, coordinator *approvalCoordinator, chatID string, want int) []eventlog.ApprovalRequestedData {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		coordinator.mutex.Lock()
		order := coordinator.orderByChat[chatID]
		pending := make([]eventlog.ApprovalRequestedData, 0, len(order))
		for _, id := range order {
			pending = append(pending, coordinator.byID[id].requested)
		}
		coordinator.mutex.Unlock()
		if len(pending) == want {
			return pending
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pending approval count did not become %d", want)
	return nil
}

// LoadExecutionChat 不参与此日志边界测试。
func (j *approvalTestJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	panic("unexpected LoadExecutionChat call")
}

// ReadExecutionRecords 不参与此日志边界测试。
func (j *approvalTestJournal) ReadExecutionRecords(string, string) ([]eventlog.Record, error) {
	panic("unexpected ReadExecutionRecords call")
}

package execution

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// readBudgetJournal 观察正常续发的真实读取边界，其他方法继续使用真实日志。
type readBudgetJournal struct {
	// Journal 是真实 Chat 权威读写模块。
	chatservice.Journal
	// contexts 统计当前执行上下文读取次数。
	contexts atomic.Int64
	// histories 统计显式历史窗口读取次数。
	histories atomic.Int64
	// historyBodies 统计冷交接实际展开的成功执行正文数量。
	historyBodies atomic.Int64
	// fullReads 统计整日志读取次数。
	fullReads atomic.Int64
}

func (j *readBudgetJournal) LoadExecutionContext(ctx context.Context, id string) (chatservice.ExecutionContext, error) {
	j.contexts.Add(1)
	return j.Journal.LoadExecutionContext(ctx, id)
}
func (j *readBudgetJournal) ReadHistoryCandidates(ctx context.Context, id string, before int64, limit int) (eventlog.HistoryCandidateWindow, error) {
	j.histories.Add(1)
	return j.Journal.ReadHistoryCandidates(ctx, id, before, limit)
}
func (j *readBudgetJournal) ReadExecutionMessages(ctx context.Context, id, executionID string) ([]messageview.Message, error) {
	j.historyBodies.Add(1)
	return j.Journal.ReadExecutionMessages(ctx, id, executionID)
}
func (j *readBudgetJournal) ReadRecords(id string) ([]eventlog.Record, error) {
	j.fullReads.Add(1)
	return j.Journal.ReadRecords(id)
}

// TestHotContinuationReadsOneCurrentContextAndNoHistory 覆盖带新 submission 的健康热续发与同键回执重放。
func TestHotContinuationReadsOneCurrentContextAndNoHistory(t *testing.T) {
	runtime := &leasedChatRuntime{}
	runtime.result.Session = runtimecontract.SessionResult{OutputID: "native-budget", State: runtimecontract.SessionStateCreated}
	manager := newMessageTestService(t, t.TempDir(), &runtime.accountRuntime)
	core := manager.execution.(*service)
	core.runtime = runtime
	ctx := context.Background()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "budget-first", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "first"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	observed := &readBudgetJournal{Journal: core.journal}
	core.journal = observed
	intent := WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", SubmissionID: "budget-next", Content: "next"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}}
	accepted, err := core.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if observed.contexts.Load() != 1 || observed.histories.Load() != 0 || observed.fullReads.Load() != 0 {
		t.Fatalf("hot reads: context=%d history=%d full=%d", observed.contexts.Load(), observed.histories.Load(), observed.fullReads.Load())
	}
	if len(runtime.requests) != 2 || runtime.requests[1].Session.Mode != runtimecontract.SessionModeResume || len(runtime.requests[1].History) != 0 {
		t.Fatal("healthy native continuation lost its incremental input")
	}
	replay, err := core.Start(ctx, intent)
	if err != nil || !replay.Replayed || replay.Execution.ID != accepted.Execution.ID || observed.contexts.Load() != 1 || len(runtime.requests) != 2 {
		t.Fatalf("receipt replay: %+v %v contexts=%d runs=%d", replay, err, observed.contexts.Load(), len(runtime.requests))
	}
	_ = runtime.CloseWorkspace(ctx, first.Chat.WorkspaceDirectory)
}

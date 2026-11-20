package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestChatWorkflowIsolationSkipsStoreForOrdinaryTurns(t *testing.T) {
	s, _ := workflowTestService(t)
	runs := &chatIsolationRunStore{WorkflowRunController: s.runs}
	threads := &chatIsolationThreadStore{}
	s.runs, s.channelThreads = runs, threads
	for _, current := range []models.Chat{
		{ID: "new-partner"},
		{ID: "existing-partner", LatestSequence: 12},
		{ID: "team-lead", HiddenFromWakerHistory: true, LatestSequence: 1},
		{ID: "team-member", HiddenFromWakerHistory: true, LatestSequence: 15},
	} {
		t.Run(current.ID, func(t *testing.T) {
			bound, err := s.BindWorkflowTurn(t.Context(), current, "ordinary-execution", nil)
			if err != nil || bound.Reference != nil || bound.Capability != nil || bound.Instruction != "" {
				t.Fatalf("ordinary chat depends on workflow state: %+v, %v", bound, err)
			}
		})
	}
	if runs.reads.Load() != 0 || threads.reads.Load() != 0 {
		t.Fatalf("ordinary chat read workflow history %d times and team ownership %d times", runs.reads.Load(), threads.reads.Load())
	}
}

func TestChatWorkflowIsolationOrdinaryStopSkipsCommandLock(t *testing.T) {
	s, _ := workflowTestService(t)
	runs := &chatIsolationRunStore{WorkflowRunController: s.runs}
	s.runs = runs
	s.RestoreChatRunIndex(t.Context())
	assertChatStopDoesNotWaitForWorkflowLock(t, s, "ordinary-chat", "ordinary-execution")
	if runs.reads.Load() != 0 {
		t.Fatalf("ordinary stop read the workflow store %d times", runs.reads.Load())
	}
}

func TestChatWorkflowIsolationTeamStopSkipsFinishedHistory(t *testing.T) {
	s, _ := workflowTestService(t)
	stoppingAt := int64(100)
	s.channelThreads = &chatIsolationThreadStore{stopAt: &stoppingAt}
	s.chatTurns.restored.Store(true)
	s.chatTurns.teamStopsReady.Store(true)
	s.chatTurns.remember("chat", "")
	grant := s.chatTurns.bind("chat", "ordinary", false)
	if err := s.StopTeamWorkflowTurns(t.Context(), "team", []string{"chat"}, stoppingAt-1); err != nil || grant.state.Load() != 0 {
		t.Fatalf("old stop revoked the current grant: %v, %d", err, grant.state.Load())
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s.state.Commands.Lock()
	done := make(chan error, 1)
	go func() { done <- s.StopTeamWorkflowTurns(ctx, "team", []string{"chat"}, stoppingAt) }()
	select {
	case err := <-done:
		s.state.Commands.Unlock()
		if err != nil || grant.state.Load()&chatTurnStopped == 0 {
			t.Fatalf("ordinary team stop did not revoke its current grant: %v, %d", err, grant.state.Load())
		}
	case <-ctx.Done():
		s.state.Commands.Unlock()
		<-done
		t.Fatal("terminal workflow history delayed the team stop")
	}
}

func TestChatWorkflowIsolationHistoryOnlyStopRevokesNewStartsWithoutCommandLock(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	s.RestoreChatRunIndex(ctx)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "existing-chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "original-execution", "original-input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "input"}})
	if err != nil {
		t.Fatal(err)
	}
	original := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	if original.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("source run did not finish: %+v", original)
	}
	actual, lease = bindConsumerFixture(t, s, conversation, "", "history-question", "later-input")
	assertChatStopDoesNotWaitForWorkflowLock(t, s, conversation.ID, actual.ID)
	if _, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: original.ID, RequestID: "late-repeat"}); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("stale observation was able to start a run after stop: %v", err)
	}
}

func assertChatStopDoesNotWaitForWorkflowLock(t *testing.T, s *Service, chatID, executionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s.state.Commands.Lock()
	done := make(chan error, 1)
	go func() { done <- s.StopChatWorkflowTurn(ctx, chatID, executionID) }()
	select {
	case err := <-done:
		s.state.Commands.Unlock()
		if err != nil {
			t.Fatalf("ordinary stop accessed workflow state: %v", err)
		}
	case <-ctx.Done():
		s.state.Commands.Unlock()
		<-done
		t.Fatal("ordinary stop waited on the workflow command lock")
	}
}

func TestChatWorkflowIsolationOptionalHistoryFailureKeepsOrdinaryTurn(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "existing-chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "original-execution", "original-input")
	if _, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "input"}}); err != nil {
		t.Fatal(err)
	}
	s.consumers = chatIsolationUnavailableConsumers{WorkflowConsumerController: s.consumers}
	bound, err := s.BindWorkflowTurn(ctx, conversation, "ordinary-follow-up", nil)
	if err != nil || bound.Reference != nil {
		t.Fatalf("unavailable history capability refused an ordinary turn: %+v, %v", bound, err)
	}
	if _, err := s.BindWorkflowTurn(ctx, conversation, "explicit-workflow", workinput.SelectWorkflow(definition.ID)); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("explicit selection did not report durable authorization failure: %v", err)
	}
}

func TestChatWorkflowIsolationRestoresHistoryAndExactStop(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	conversation := consumerFixtureChat(t, "existing-chat")
	s.RestoreChatRunIndex(ctx)
	if s.MayHaveChatRunSources(conversation.ID) {
		t.Fatal("unused chat unexpectedly needs workflow history queries")
	}
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "original-execution", "original-input")
	if !s.MayHaveChatRunSources(conversation.ID) {
		t.Fatal("the first selection could be hidden from the chat timeline during admission")
	}
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	runID := unwrapWorkflow(response)["id"].(string)
	runs := &chatIsolationRunStore{WorkflowRunController: s.runs, allowExecutionLookup: true}
	restarted := workflowTestNew(Config{Store: s.store, Runs: runs, Consumers: s.consumers, Transactions: s.transactions, DataDirectory: s.dataDirectory})
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	bound, err := restarted.BindWorkflowTurn(ctx, conversation, "another-message", nil)
	if err != nil || bound.Reference != nil || bound.Capability == nil || runs.reads.Load() != 0 {
		t.Fatalf("restart lost exact-chat history or enumerated runs during sending: %+v, reads=%d, %v", bound, runs.reads.Load(), err)
	}
	historyOperations := map[string]bool{}
	for _, binding := range bound.Capability.Bindings {
		historyOperations[binding.Operation] = true
	}
	if !historyOperations["workflow.runs.get"] || !historyOperations["workflow.runs.repeat"] || historyOperations["workflow.runs.start"] {
		t.Fatalf("plain follow-up got incorrect workflow permissions: %v", historyOperations)
	}
	if err := restarted.StopChatWorkflowTurn(ctx, conversation.ID, actual.ID); err != nil {
		t.Fatal(err)
	}
	stopped, err := restarted.runs.GetRun(ctx, runID)
	if err != nil || stopped.Status != constants.WorkflowRunStatusCancelling && stopped.Status != constants.WorkflowRunStatusCancelled {
		t.Fatalf("restart did not restore the exact execution's stop target: %+v, %v", stopped, err)
	}
}

func TestChatWorkflowExactStopReadsDurableSourcesWhenIndexIsUnavailable(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	conversation := consumerFixtureChat(t, "existing-chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "original-execution", "original-input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	runID := unwrapWorkflow(response)["id"].(string)
	restarted := workflowTestNew(Config{Store: s.store, Runs: s.runs, Consumers: s.consumers, Transactions: s.transactions, DataDirectory: s.dataDirectory})
	if err := restarted.StopChatWorkflowTurn(ctx, conversation.ID, actual.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.runs.GetRun(ctx, runID)
	if err != nil || stored.Status != constants.WorkflowRunStatusCancelling {
		t.Fatalf("failed index restoration skipped cancellation of a durable run: %+v %v", stored, err)
	}
}

type chatIsolationRunStore struct {
	// WorkflowRunController 保留不相关读写的真实实现。
	controllers.WorkflowRunController
	// reads 记录是否进入发送或取消临界路径上的持久查询。
	reads atomic.Int32
	// allowExecutionLookup 允许取消存量运行时读取已知的精确回合。
	allowExecutionLookup bool
}

func (s *chatIsolationRunStore) ListChatRunSources(ctx context.Context, chatID string, executionIDs []string) ([]models.WorkflowRun, error) {
	if s.allowExecutionLookup && executionIDs != nil {
		return s.WorkflowRunController.ListChatRunSources(ctx, chatID, executionIDs)
	}
	s.reads.Add(1)
	return nil, status_error.DatabaseError
}

type chatIsolationThreadStore struct {
	// ChannelThreadController 保留与来源识别无关的接口。
	controllers.ChannelThreadController
	// reads 记录本来无需工作流的团队输入是否第二次读取归属。
	reads atomic.Int32
	// stopAt 是置于仓储中的生效停止意图。
	stopAt *int64
}

func (s *chatIsolationThreadStore) Get(_ context.Context, id string) (models.ChannelThread, error) {
	return models.ChannelThread{ID: id, CancelingAt: s.stopAt}, nil
}

func (s *chatIsolationThreadStore) FindByChatID(context.Context, string) (models.ChannelThread, bool, error) {
	s.reads.Add(1)
	return models.ChannelThread{}, false, status_error.DatabaseError
}

type chatIsolationUnavailableConsumers struct {
	// WorkflowConsumerController 保留已存在授权的真实读取行为。
	controllers.WorkflowConsumerController
}

func (chatIsolationUnavailableConsumers) CreateConsumer(context.Context, models.WorkflowConsumerLease) error {
	return status_error.DatabaseError
}

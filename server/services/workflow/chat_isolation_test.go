package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestChatWorkflowHistoryToolsRecoverAfterLookupFailure(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	conversation := consumerFixtureChat(t, "existing-chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "original-execution", "original-input")
	if _, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start"}); err != nil {
		t.Fatal(err)
	}
	lookup := &workflowHistoryLookup{WorkflowRunController: s.runs, unavailable: true}
	restarted := workflowTestNew(Config{Store: s.store, Runs: lookup, Consumers: s.consumers, Transactions: s.transactions, DataDirectory: s.dataDirectory, ChannelThreads: workflowHistoryThread{}})
	conversation.HiddenFromWakerHistory = true
	bound, err := restarted.BindWorkflowTurn(ctx, conversation, "followup", nil)
	if err != nil || bound.Capability != nil {
		t.Fatalf("ordinary followup could not proceed during source failure: %+v %v", bound, err)
	}
	lookup.unavailable = false
	bound, err = restarted.BindWorkflowTurn(ctx, conversation, "followup", nil)
	if err != nil || bound.Capability == nil {
		t.Fatalf("recovered source did not restore history tools: %+v %v", bound, err)
	}
	operations := map[string]bool{}
	for _, binding := range bound.Capability.Bindings {
		operations[binding.Operation] = true
	}
	if !operations["workflow.runs.list"] || !operations["workflow.runs.repeat"] || operations["workflow.runs.start"] {
		t.Fatalf("history tools changed the selected-workflow permission: %v", operations)
	}
}

func TestChatWorkflowOrdinaryStopDoesNotWaitForCommandLock(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s.state.Commands.Lock()
	done := make(chan error, 1)
	go func() { done <- s.StopChatWorkflowTurn(ctx, "ordinary-chat", "ordinary-execution") }()
	select {
	case err := <-done:
		s.state.Commands.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		s.state.Commands.Unlock()
		<-done
		t.Fatal("an unrelated workflow command delayed ordinary cancellation")
	}
}

func TestChatWorkflowAdmissionSharesTheChatDeletionBoundary(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	conversation := consumerFixtureChat(t, "deleting-chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "input")
	release, err := chatstorage.AcquireOperation(s.dataDirectory, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start"}
	_, err = s.handleConsumerTool(ctx, lease, actual, conversation, input)
	release()
	if !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("a late workflow start entered the chat deletion boundary: %v", err)
	}
	runs, err := s.runs.ListChatRunSources(ctx, conversation.ID, []string{actual.ID})
	if err != nil || len(runs) != 0 {
		t.Fatalf("rejected admission created a run: %+v %v", runs, err)
	}
}

func TestChatWorkflowExactStopUsesDurableSources(t *testing.T) {
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
		t.Fatalf("durable source did not stop its run: %+v %v", stored, err)
	}
	actual, lease = bindConsumerFixture(t, s, conversation, "", "later-execution", "later-input")
	if err := restarted.StopChatWorkflowTurn(ctx, conversation.ID, actual.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: runID, RequestID: "late-repeat"}); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("stopped consumer admitted a late run: %v", err)
	}
}

type workflowHistoryLookup struct {
	// WorkflowRunController 保留真实 SQLite 的其余领域行为。
	controllers.WorkflowRunController
	// unavailable 模拟来源查询暂不可用。
	unavailable bool
}

func (s *workflowHistoryLookup) HasChatRunSources(ctx context.Context, chatID string) (bool, error) {
	if s.unavailable {
		return false, status_error.DatabaseError
	}
	return s.WorkflowRunController.HasChatRunSources(ctx, chatID)
}

type workflowHistoryThread struct {
	// ChannelThreadController 保留本测试不调用的团队接口。
	controllers.ChannelThreadController
}

func (workflowHistoryThread) Get(_ context.Context, id string) (models.ChannelThread, error) {
	return models.ChannelThread{ID: id}, nil
}

func (workflowHistoryThread) FindByChatID(context.Context, string) (models.ChannelThread, bool, error) {
	return models.ChannelThread{ID: "existing-thread"}, true, nil
}

func TestMainSourceWorkflowRunsOnlyFrozenSelection(t *testing.T) {
	s, _ := workflowTestService(t)
	ctx := t.Context()
	def := savedTestWorkflow(t, s, humanSource)
	chat := consumerFixtureChat(t, "work-source")
	chat.HiddenFromWakerHistory = true
	selected := workinput.SelectWorkflow(def.ID)
	if _, err := s.BindWorkflowTurn(ctx, chat, "not-bound", selected); !errors.Is(err, status_error.WorkflowCapabilityUnsupported) {
		t.Fatalf("unbound hidden work accepted workflow: %v", err)
	}
	s.authorizeMainWork = func(_ context.Context, w models.Chat) error {
		if w.ID != chat.ID {
			return status_error.AgentOperationForbidden
		}
		return nil
	}
	actual, lease := bindConsumerFixture(t, s, chat, def.ID, "bound", "input")
	if lease.Selection.RevisionID != def.CurrentRevisionID {
		t.Fatalf("unfrozen selection: %+v", lease)
	}
	chat.ActiveExecutionID = lease.ID
	actual.Status = constants.ExecutionStatusRunning
	s.chats = &consumerChatBoundary{actual: actual, summary: chatservice.ChatSummary{Chat: chat}}
	s.executions = consumerExecutionBoundary{}
	scope := workflowtool.Scope{Kind: constants.WorkflowCapabilityScopeConsumer, Key: lease.ID}
	start := workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: def.ID, RevisionID: def.CurrentRevisionID, RequestID: "start"}
	value, err := s.handleTool(ctx, scope, start)
	if err != nil || value == nil {
		t.Fatalf("selected workflow did not start: %v %v", err, value)
	}
	s.authorizeMainWork = func(context.Context, models.Chat) error { return status_error.AgentOperationForbidden }
	if _, err := s.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryWorkflows}); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("revoked source retained a workflow capability: %v", err)
	}
}

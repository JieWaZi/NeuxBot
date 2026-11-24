package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentcap "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestLocalWorkflowToolSetSurvivesChangingSelection(t *testing.T) {
	s, _ := workflowTestService(t)
	chat := consumerFixtureChat(t, "local-chat")
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	unselected, _ := bindLocalWorkflowFixture(t, s, chat, nil)
	selected, call := bindLocalWorkflowFixture(t, s, chat, workinput.SelectWorkflow(definition.ID))
	operations := func(bound execution.WorkflowTurnBinding) []string {
		var names []string
		for _, binding := range bound.Capability.Bindings {
			names = append(names, binding.Operation)
		}
		return names
	}
	if !slices.Equal(operations(unselected), operations(selected)) {
		t.Fatalf("workflow catalog changed on selection: unselected %v, selected %v", operations(unselected), operations(selected))
	}
	if unselected.Reference != nil || selected.Reference == nil || selected.Reference.RevisionID != definition.CurrentRevisionID {
		t.Fatal("workflow turns must retain only the current frozen target")
	}
	if unselected.DeveloperInstruction == "" || unselected.DeveloperInstruction != selected.DeveloperInstruction {
		t.Fatal("the selected revision changed the stable workflow instructions")
	}
	if _, err := call("workflow.runs.repeat", "repeat", map[string]any{"runId": "prior-run"}); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("selected turn allowed repeat: %v", err)
	}
}

func TestCancelledExecutionCannotAdmitItsFirstInFlightLocalWorkflowRun(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "local-chat")
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	bound, _ := bindLocalWorkflowFixture(t, s, conversation, nil)
	registry := agentcap.New(nil)
	t.Cleanup(registry.Close)
	executionContext, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	grant, activate, finish, err := registry.Prepare(agentcap.GrantSpec{
		Lifecycle: executionContext, ExecutionID: "local-turn", Bindings: bound.Capability.Bindings,
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(finish)
	activate()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.store = blockedLocalStartRevisionStore{WorkflowController: s.store, entered: entered, release: release}
	body, _ := json.Marshal(map[string]any{"operation": "workflow.runs.start", "requestId": "first-start", "params": map[string]any{
		"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"text": "input"},
	}})
	request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", strings.NewReader(string(body)))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+grant.Token)
	request.Header.Set(constants.AgentCLIExecutionHeader, "local-turn")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.ServeHTTP(response, request)
	}()
	var commandContext context.Context
	select {
	case commandContext = <-entered:
	case <-done:
		t.Fatalf("first local workflow start did not enter revision preparation: %d %s", response.Code, response.Body)
	case <-time.After(3 * time.Second):
		t.Fatal("first local workflow start did not enter revision preparation")
	}
	cancelExecution()
	if commandContext.Err() != context.Canceled {
		t.Fatal("in-flight workflow input retained a live execution context")
	}
	if err := s.StopChatWorkflowTurn(ctx, conversation.ID, "local-turn"); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled workflow start did not finish")
	}
	var result agentcap.Response
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.OK {
		t.Fatalf("cancelled local workflow start returned success: %s", response.Body)
	}
	runs, err := s.runs.ListChatRunSources(ctx, conversation.ID, []string{"local-turn"})
	if err != nil || len(runs) != 0 {
		t.Fatalf("cancelled first start admitted a run: %+v %v", runs, err)
	}
}

// blockedLocalStartRevisionStore 在首次运行读取版本时停住，保留真实数据库接纳行为。
type blockedLocalStartRevisionStore struct {
	// WorkflowController 继续执行真实工作流仓储操作。
	controllers.WorkflowController
	// entered 提供已经进入首次运行准备的命令上下文。
	entered chan context.Context
	// release 允许在执行取消后继续原来的版本读取。
	release <-chan struct{}
}

// GetRevision 在测试释放后使用原命令上下文读取真实版本。
func (s blockedLocalStartRevisionStore) GetRevision(ctx context.Context, definitionID, revisionID string) (models.WorkflowRevision, error) {
	s.entered <- ctx
	<-s.release
	return s.WorkflowController.GetRevision(ctx, definitionID, revisionID)
}

func TestLocalWorkflowCancellationRequiresAnOwnedChatRun(t *testing.T) {
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "local-chat")
	_, call := bindLocalWorkflowFixture(t, s, conversation, nil)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	for _, tc := range []struct {
		// id 是测试传给命令的确切运行身份。
		id string
		// sourceChat 是持久的来源对话。
		sourceChat string
		// sourceExecution 是持久的发起回合。
		sourceExecution string
		// workbench 表示运行由用户在工作台单独启动。
		workbench bool
		// allowed 表示该运行可由本地命令取消。
		allowed bool
	}{
		{"foreign", "another-chat", "another-turn", false, false},
		{"manual", "", "", false, false},
		{"workbench", conversation.ID, "prior-turn", true, false},
		{"same-chat", conversation.ID, "prior-turn", false, true},
		{"current-turn", conversation.ID, "local-turn", false, true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			insertWorkflowChatHistory(t, s, models.WorkflowRun{ID: tc.id, DefinitionID: definition.ID, RevisionID: definition.CurrentRevisionID,
				SourceChatID: tc.sourceChat, SourceExecutionID: tc.sourceExecution, SourceWorkbench: tc.workbench, Status: constants.WorkflowRunStatusRunning})
			params := map[string]any{"runId": tc.id}
			if _, err := call("workflow.runs.get", "", params); err != nil {
				t.Fatalf("read by explicit Run ID failed: %v", err)
			}
			_, err := call("workflow.runs.cancel", "cancel-"+tc.id, params)
			stored, readErr := s.runs.GetRun(t.Context(), tc.id)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.allowed {
				if err != nil || stored.Status != constants.WorkflowRunStatusCancelling {
					t.Fatalf("owned Run was not cancelled: %s, %v", stored.Status, err)
				}
			} else if err == nil || stored.Status != constants.WorkflowRunStatusRunning {
				t.Fatalf("unowned Run changed: %s, %v", stored.Status, err)
			}
		})
	}
}

func TestLocalWorkflowRunListPagesBeyondRecentHistory(t *testing.T) {
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "local-chat")
	_, call := bindLocalWorkflowFixture(t, s, conversation, nil)
	removedAt := int64(200)
	for i := range 113 {
		id := fmt.Sprintf("run-%03d", i)
		run := models.WorkflowRun{ID: id, DefinitionID: fmt.Sprintf("workflow-%d", i/10), SourceChatID: conversation.ID, SourceExecutionID: id,
			CreatedAt: 1 + int64(i/2), Status: constants.WorkflowRunStatusSucceeded}
		if i >= 61 {
			run.HistoryDeletedAt = &removedAt
		}
		insertWorkflowChatHistory(t, s, run)
	}
	checkPage := func(params map[string]any, size, pageNum int, first, last, next string) {
		t.Helper()
		result, err := call("workflow.runs.list", "", params)
		if err != nil {
			t.Fatal(err)
		}
		page := result.Data.(map[string]any)
		items := page["items"].([]any)
		if len(items) != size || page["total"] != 61 || page["pageNum"] != pageNum || page["nextCursor"] != next ||
			items[0].(map[string]any)["runId"] != first || items[len(items)-1].(map[string]any)["runId"] != last {
			t.Fatalf("wrong page for %v: %v", params, page)
		}
	}
	checkPage(nil, 50, 1, "run-060", "run-011", "50")
	checkPage(map[string]any{"cursor": "50"}, 11, 1, "run-010", "run-000", "")
	checkPage(map[string]any{"pageNum": 2, "pageSize": 50}, 11, 2, "run-010", "run-000", "")
	if _, err := call("workflow.runs.list", "", map[string]any{"cursor": "50", "pageNum": 1}); !errors.Is(err, status_error.AgentInputInvalid) {
		t.Fatalf("ambiguous page request was accepted: %v", err)
	}
}

func insertWorkflowChatHistory(t *testing.T, s *Service, run models.WorkflowRun) {
	t.Helper()
	run.RootRunID, run.RootFiberID = run.ID, run.ID
	run.Context = map[string]any{}
	if err := s.runs.CreateRun(t.Context(), run, models.WorkflowFiber{ID: run.ID, RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkflowReferenceFreezesDefinitionAndRevision(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	source := workflowTestSource(t)
	definition := savedTestWorkflow(t, s, source)
	other := savedTestWorkflow(t, s, strings.Replace(source, "纯计算验收", "其他工作流", 1))
	bound, call := bindLocalWorkflowFixture(t, s, consumerFixtureChat(t, "local-chat"), workinput.SelectWorkflow(definition.ID))
	if bound.Reference == nil || bound.Reference.RevisionID != definition.CurrentRevisionID {
		t.Fatalf("selection did not freeze the runnable revision: %+v", bound.Reference)
	}
	newer, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "publish", ExpectedRevisionID: definition.CurrentRevisionID, SDK: constants.WorkflowSDKIdentifier, Source: strings.Replace(source, "纯计算验收", "新版工作流", 1)})
	if err != nil {
		t.Fatal(err)
	}
	description, err := call("workflow.definitions.describe", "", map[string]any{"definitionId": definition.ID})
	if err != nil || description.Data.(map[string]any)["revisionId"] != bound.Reference.RevisionID {
		t.Fatalf("describe did not return the send-time contract: %+v %v", description, err)
	}
	if _, err := call("workflow.definitions.describe", "", map[string]any{"definitionId": other.ID}); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("describe accepted a definition other than the selected workflow: %v", err)
	}
	if _, err := call("workflow.definitions.describe", "", map[string]any{"definitionId": definition.ID, "revisionId": newer.RevisionID}); !errors.Is(err, status_error.WorkflowRevisionConflict) {
		t.Fatalf("describe accepted a revision published outside the turn: %v", err)
	}
	if reads := s.chats.(*consumerChatBoundary).executionReads.Load(); reads != 0 {
		t.Fatalf("definition reads reloaded the authorized execution %d times", reads)
	}
	args := map[string]any{"text": "frozen"}
	if _, err := call("workflow.runs.start", "wrong-target", map[string]any{"definitionId": other.ID, "revisionId": other.CurrentRevisionID, "args": args}); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("start accepted a definition other than the selected workflow: %v", err)
	}
	if _, err := call("workflow.runs.start", "wrong-revision", map[string]any{"definitionId": definition.ID, "revisionId": newer.RevisionID, "args": args}); !errors.Is(err, status_error.WorkflowRevisionConflict) {
		t.Fatalf("start accepted a revision published outside the turn: %v", err)
	}
	started, err := call("workflow.runs.start", "frozen", map[string]any{"definitionId": definition.ID, "revisionId": bound.Reference.RevisionID, "args": args})
	if err != nil || started.Data.(map[string]any)["revisionId"] != bound.Reference.RevisionID {
		t.Fatalf("start did not use the send-time revision: %+v %v", started, err)
	}

}

func TestLocalWorkflowReferenceCanRunTheExplicitRevisionSavedByTheTurn(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	source := workflowTestSource(t)
	definition := savedTestWorkflow(t, s, source)
	bound, call := bindLocalWorkflowFixture(t, s, consumerFixtureChat(t, "local-chat"), workinput.SelectWorkflow(definition.ID))
	saved, err := call("workflow.source.save", "repair", map[string]any{"workflowId": definition.ID, "expectedRevisionId": bound.Reference.RevisionID, "source": strings.Replace(source, "纯计算验收", "伙伴修复保存", 1)})
	if err != nil {
		t.Fatal(err)
	}
	revisionID := saved.Data.(map[string]any)["revisionId"].(string)
	if _, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "publish", ExpectedRevisionID: revisionID, SDK: constants.WorkflowSDKIdentifier, Source: strings.Replace(source, "纯计算验收", "另行发布", 1)}); err != nil {
		t.Fatal(err)
	}
	description, err := call("workflow.definitions.describe", "", map[string]any{"definitionId": definition.ID, "revisionId": revisionID})
	if err != nil || description.Data.(map[string]any)["revisionId"] != revisionID {
		t.Fatalf("describe did not use the explicit revision saved by this turn: %+v %v", description, err)
	}
	for _, binding := range bound.Capability.Bindings {
		if binding.Operation != "workflow.source.save" {
			continue
		}
		recovered, err := binding.Receipt(ctx, "repair")
		if err != nil || !recovered.Replayed || recovered.Data.(map[string]any)["saved"] != true {
			t.Fatalf("save receipt could not be recovered: %+v %v", recovered, err)
		}
	}
	started, err := call("workflow.runs.start", "start-repair", map[string]any{"definitionId": definition.ID, "revisionId": revisionID, "args": map[string]any{"text": "repaired"}})
	if err != nil || started.Data.(map[string]any)["revisionId"] != revisionID {
		t.Fatalf("start did not use the explicit revision saved by this turn: %+v %v", started, err)
	}
}

func TestLocalWorkflowCanStartAfterRejectedRepeatOrScope(t *testing.T) {
	for _, rejected := range []string{"nonterminal-repeat", "invalid-scope"} {
		t.Run(rejected, func(t *testing.T) {
			ctx := t.Context()
			s, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, s, workflowTestSource(t))
			conversation := consumerFixtureChat(t, "local-chat")
			var sourceRunID string
			if rejected == "nonterminal-repeat" {
				actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "prior-execution", "prior-input")
				result, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "prior-run", Args: map[string]any{"text": "previous input"}})
				if err != nil {
					t.Fatal(err)
				}
				sourceRunID = unwrapWorkflow(result)["id"].(string)
			}
			_, call := bindLocalWorkflowFixture(t, s, conversation, nil)
			params := map[string]any{"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"text": "corrected"}}
			switch rejected {
			case "nonterminal-repeat":
				if _, err := call("workflow.runs.repeat", "bad-repeat", map[string]any{"runId": sourceRunID}); !errors.Is(err, status_error.WorkflowStateConflict) {
					t.Fatalf("nonterminal repeat returned %v", err)
				}
			case "invalid-scope":
				other := savedTestWorkflow(t, s, strings.Replace(workflowTestSource(t), "纯计算验收", "备用工作流", 1))
				boundary := s.chats.(*consumerChatBoundary)
				boundary.summary.Chat.WorkspaceDirectory = filepath.Join(conversation.WorkspaceDirectory, "missing")
				_, err := call("workflow.runs.start", "bad-scope", map[string]any{"definitionId": other.ID, "revisionId": other.CurrentRevisionID, "args": params["args"]})
				if !util.HasStatusCode(err, status_error.WorkflowResourceInvalid) {
					t.Fatalf("invalid scope returned %v", err)
				}
				boundary.summary.Chat.WorkspaceDirectory = conversation.WorkspaceDirectory
			}
			if _, err := s.consumers.GetConsumer(ctx, "local-turn"); !errors.Is(err, status_error.WorkflowNotFound) {
				t.Fatalf("rejected start left a durable lease: %v", err)
			}
			started, err := call("workflow.runs.start", "corrected", params)
			if err != nil || started.Data.(map[string]any)["definitionId"] != definition.ID {
				t.Fatalf("rejected operation poisoned the next valid request: %+v %v", started, err)
			}
		})
	}
}

func bindLocalWorkflowFixture(t *testing.T, s *Service, conversation models.Chat, selected *workinput.WorkflowReference) (execution.WorkflowTurnBinding, func(string, string, map[string]any) (agentcap.Result, error)) {
	t.Helper()
	ctx := t.Context()
	conversation.ActiveExecutionID = "local-turn"
	s.chats = &consumerChatBoundary{
		actual:  models.Execution{ID: "local-turn", ChatID: conversation.ID, UserMessageID: "input", Status: constants.ExecutionStatusRunning, PermissionLevel: constants.PermissionLevelDefault},
		summary: chatservice.ChatSummary{Chat: conversation},
	}
	s.executions = consumerExecutionBoundary{}
	bound, err := s.BindLocalWorkflowTurn(ctx, conversation, "local-turn", selected, "")
	if err != nil || bound.Capability == nil {
		t.Fatalf("local tools unavailable: %+v %v", bound, err)
	}
	call := func(operation, requestID string, params map[string]any) (agentcap.Result, error) {
		t.Helper()
		for _, binding := range bound.Capability.Bindings {
			if binding.Operation == operation {
				return binding.Call(ctx, agentcap.Request{Operation: operation, RequestID: requestID, Params: params})
			}
		}
		t.Fatalf("missing local tool %s", operation)
		return agentcap.Result{}, nil
	}
	return bound, call
}

func TestLocalChatCanRepairAndRunAWorkflowWithoutAnAuthoringSession(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	source := workflowTestSource(t)
	definition := savedTestWorkflow(t, s, source)
	conversation := consumerFixtureChat(t, "local-chat")
	conversation.ActiveExecutionID = "local-turn"
	s.chats = &consumerChatBoundary{
		actual:  models.Execution{ID: "local-turn", ChatID: conversation.ID, UserMessageID: "input", Status: constants.ExecutionStatusRunning, PermissionLevel: constants.PermissionLevelDefault},
		summary: chatservice.ChatSummary{Chat: conversation},
	}
	s.executions = consumerExecutionBoundary{}
	bound, err := s.BindLocalWorkflowTurn(ctx, conversation, "local-turn", nil, "")
	if err != nil || bound.Capability == nil {
		t.Fatalf("local tools unavailable: %+v %v", bound, err)
	}
	call := func(op, requestID string, params map[string]any) (agentcap.Result, error) {
		t.Helper()
		for _, binding := range bound.Capability.Bindings {
			if binding.Operation == op {
				return binding.Call(ctx, agentcap.Request{Operation: op, RequestID: requestID, Params: params})
			}
		}
		t.Fatalf("missing local tool %s", op)
		return agentcap.Result{}, nil
	}
	if _, err := call("workflow.source.get", "", map[string]any{"workflowId": definition.ID}); err != nil {
		t.Fatal(err)
	}
	if found, err := call("workflow.definitions.list", "", map[string]any{"search": "纯计算验收"}); err != nil ||
		len(found.Data.(map[string]any)["items"].([]map[string]any)) != 1 ||
		found.Data.(map[string]any)["items"].([]map[string]any)[0]["id"] != definition.ID {
		t.Fatalf("local workflow discovery failed: %+v %v", found, err)
	}
	params := map[string]any{"workflowId": definition.ID, "expectedRevisionId": definition.CurrentRevisionID, "source": strings.Replace(source, "纯计算验收", "已修复工作流", 1)}
	saved, err := call("workflow.source.save", "repair", params)
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := saved.Data.(map[string]any)["revisionId"].(string)
	if revision == "" || revision == definition.CurrentRevisionID {
		t.Fatalf("repair did not save a new revision: %+v", saved)
	}
	if _, err := call("workflow.source.save", "stale-save", params); !errors.Is(err, status_error.WorkflowRevisionConflict) {
		t.Fatalf("stale workflow revision was not rejected: %v", err)
	}
	result, err := call("workflow.runs.start", "run-repair", map[string]any{"definitionId": definition.ID, "revisionId": revision, "args": map[string]any{"text": "after repair"}})
	if err != nil || result.Data.(map[string]any)["revisionId"] != revision {
		t.Fatalf("repaired revision was not admitted from the same turn: %+v %v", result, err)
	}
	runID := result.Data.(map[string]any)["runId"].(string)
	boundary := s.chats.(*consumerChatBoundary)
	boundary.actual.ID, boundary.summary.Chat.ActiveExecutionID = "follow-up", "follow-up"
	bound, err = s.BindLocalWorkflowTurn(ctx, conversation, "follow-up", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := call("workflow.evidence.list", "", map[string]any{"runId": runID})
	if err != nil || evidence.Data.(map[string]any)["run"].(map[string]any)["definitionId"] != definition.ID {
		t.Fatalf("follow-up could not identify the workflow from the original run: %+v %v", evidence, err)
	}
	bound, err = s.BindLocalWorkflowTurn(ctx, conversation, "follow-up", workinput.SelectWorkflow(definition.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call("workflow.source.get", "", nil); err != nil {
		t.Fatalf("reference did not resolve the default workflow: %v", err)
	}
	params["workflowId"] = "different-workflow"
	params["expectedRevisionId"] = revision
	if _, err := call("workflow.source.save", "wrong-target", params); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("save accepted an ID different from the referenced target: %v", err)
	}
}

func TestChatWorkflowRepeatKeepsOriginalVersionAndInputs(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	chat := consumerFixtureChat(t, "chat")
	old, lease := bindConsumerFixture(t, s, chat, definition.ID, "first", "first-input")
	old.PermissionLevel = constants.PermissionLevelFullAccess
	first, err := s.handleConsumerTool(ctx, lease, old, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start-first", Args: map[string]any{"text": "NeuxBot"}})
	if err != nil {
		t.Fatal(err)
	}
	oldRun := runTicks(t, s, unwrapWorkflow(first)["id"].(string), 8)
	if oldRun.Status != constants.WorkflowRunStatusSucceeded || oldRun.SourceMessageID != "first-input" {
		t.Fatalf("actual run or source is missing: %+v", oldRun)
	}
	if _, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "newer", ExpectedRevisionID: definition.CurrentRevisionID, SDK: constants.WorkflowSDKIdentifier, Source: strings.Replace(workflowTestSource(t), "parts.join(' ')", "parts.join('-')", 1)}); err != nil {
		t.Fatal(err)
	}
	second, recent := bindConsumerFixture(t, s, chat, "", "second", "repeat-input")
	command := workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: oldRun.ID, RequestID: "repeat"}
	response, err := s.handleConsumerTool(ctx, recent, second, chat, command)
	if err != nil {
		t.Fatal(err)
	}
	newID := unwrapWorkflow(response)["id"].(string)
	replayed, err := s.handleConsumerTool(ctx, recent, second, chat, command)
	if err != nil || unwrapWorkflow(replayed)["id"] != newID {
		t.Fatalf("repeat lost the original receipt: %+v %v", replayed, err)
	}
	command.RequestID = "different-key"
	if _, err := s.handleConsumerTool(ctx, recent, second, chat, command); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatalf("same user input unexpectedly accepted another run: %v", err)
	}
	newRun := runTicks(t, s, newID, 8)
	if newRun.ID == oldRun.ID || newRun.SourceRunID != oldRun.ID || newRun.SourceMessageID != "repeat-input" || newRun.SourceExecutionID != second.ID || newRun.RevisionID != definition.CurrentRevisionID ||
		newRun.Defaults.PermissionLevel != constants.PermissionLevelDefault || newRun.Result.(map[string]any)["message"] != "NEUXBOT NEUXBOT!" {
		t.Fatalf("repeat drifted from original inputs or attached to wrong message: %+v", newRun)
	}
	foreign := consumerFixtureChat(t, "foreign")
	if _, err := s.handleConsumerTool(ctx, models.WorkflowConsumerLease{ChatID: foreign.ID}, models.Execution{}, foreign, command); !errors.Is(err, status_error.WorkflowNotFound) {
		t.Fatalf("cross-chat repeat disclosed a run: %v", err)
	}
}

func TestLocalWorkflowAuthoringRespectsBoundTargetWhileReadingOtherRunEvidence(t *testing.T) {
	s, _ := workflowTestService(t)
	source := workflowTestSource(t)
	selected := savedTestWorkflow(t, s, source)
	other := savedTestWorkflow(t, s, strings.Replace(source, "纯计算验收", "其他工作流", 1))
	conversation := consumerFixtureChat(t, "local-chat")
	_, call := bindLocalWorkflowFixture(t, s, conversation, workinput.SelectWorkflow(selected.ID))
	for _, op := range []string{"workflow.source.get", "workflow.authoring.prepare"} {
		if _, err := call(op, "", map[string]any{"workflowId": other.ID}); !errors.Is(err, status_error.WorkflowStateConflict) {
			t.Errorf("%s read another selected workflow: %v", op, err)
		}
	}
	run := models.WorkflowRun{ID: "other-run", DefinitionID: other.ID, RevisionID: other.CurrentRevisionID, Status: constants.WorkflowRunStatusSucceeded}
	insertWorkflowChatHistory(t, s, run)
	evidence, err := call("workflow.evidence.list", "", map[string]any{"runId": run.ID})
	if err != nil || evidence.Data.(map[string]any)["run"].(map[string]any)["definitionId"] != other.ID {
		t.Fatalf("explicit evidence read did not return the other run: %+v %v", evidence, err)
	}
}

func TestChatRunReturnsFilesFromEveryFrozenProjectRoot(t *testing.T) {
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "multi-root")
	primary := conversation.WorkspaceDirectory
	second := consumerFixtureChat(t, "second-root").WorkspaceDirectory
	conversation.AdditionalWorkingDirectories = []string{primary, second}
	source := `export const meta={sdk:'bot.workflow',title:'项目文件',description:'读取第二个项目目录',inputSchema:{type:'object',properties:{path:{type:'string',title:'路径',description:'已有文件路径。'}},required:['path']},outputSchema:{type:'string',format:'file'},phases:[{id:'work',title:'交付',detail:'交付项目文件',canExit:true}]};phase('work');return args.path;`
	definition := savedTestWorkflow(t, s, source)
	report := filepath.Join(second, "report.txt")
	if err := os.WriteFile(report, []byte("second project directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	actor, lease := bindConsumerFixture(t, s, conversation, definition.ID, "start", "input")
	response, err := s.handleConsumerTool(t.Context(), lease, actor, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "root", Args: map[string]any{"path": report}})
	if err != nil {
		t.Fatal(err)
	}
	run := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	assertOutput := func(run models.WorkflowRun) {
		t.Helper()
		outputs, err := s.OutputFiles(t.Context(), run)
		if run.ScopeBinding.WorkingDirectory != primary || run.Status != constants.WorkflowRunStatusSucceeded || err != nil || len(outputs) != 1 || !outputs[0].Available {
			t.Fatalf("run lost its CWD or second authorized root: status=%s failure=%+v outputs=%+v error=%v", run.Status, run.Failure, outputs, err)
		}
	}
	assertOutput(run)
	repeatActor, repeatLease := bindConsumerFixture(t, s, conversation, "", "following", "next-input")
	response, err = s.handleConsumerTool(t.Context(), repeatLease, repeatActor, conversation, workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: run.ID, RequestID: "repeat"})
	if err != nil {
		t.Fatal(err)
	}
	assertOutput(runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8))
}

func TestChatWorkflowRepeatKeepsLowerWorkerPermissionAndTightensReadOnly(t *testing.T) {
	s, _ := workflowTestService(t)
	s.worker = &controlledWorker{}
	chat := consumerFixtureChat(t, "chat")
	definition := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('review report',{id:'report',label:'报告',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime',permissionLevel:'default'}});`)
	first, lease := bindConsumerFixture(t, s, chat, definition.ID, "first", "original-input")
	first.PermissionLevel = constants.PermissionLevelFullAccess
	response, err := s.handleConsumerTool(t.Context(), lease, first, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start"})
	if err != nil {
		t.Fatal(err)
	}
	finish := func(id string) models.WorkflowRun {
		t.Helper()
		runTicks(t, s, id, 2)
		dispatchPrepared(t, s, id)
		return runTicks(t, s, id, 2)
	}
	original := finish(unwrapWorkflow(response)["id"].(string))
	if original.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("source run did not succeed: %+v", original)
	}
	current, lease := bindConsumerFixture(t, s, chat, "", "second", "second-input")
	current.WorkspaceReadOnly = true
	repeated, err := s.repeatChatRun(t.Context(), lease, current, chat, original, "repeat", false)
	if err != nil {
		t.Fatal(err)
	}
	configuration := repeated.WorkerConfigurations["report"]
	if repeated.Defaults.PermissionLevel != constants.PermissionLevelDefault || !repeated.Defaults.WorkspaceReadOnly ||
		configuration.PermissionLevel != constants.PermissionLevelDefault || !configuration.WorkspaceReadOnly {
		t.Fatalf("repeat widened the current caller or changed the lower worker permission: %+v", repeated)
	}
	if final := finish(repeated.ID); final.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("the narrower configuration failed to run: %+v", final)
	}
}

func TestChatWorkflowRepeatAfterSourceRemovalDoesNotStartAnotherRun(t *testing.T) {
	for _, removed := range []string{"history", "definition"} {
		t.Run(removed, func(t *testing.T) {
			ctx := t.Context()
			s, store := workflowTestService(t)
			def := savedTestWorkflow(t, s, workflowTestSource(t))
			chat := consumerFixtureChat(t, "chat")
			actor, lease := bindConsumerFixture(t, s, chat, def.ID, "tenth", "input-10")
			reply, err := s.handleConsumerTool(ctx, lease, actor, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: def.ID, RevisionID: def.CurrentRevisionID, RequestID: "ten", Args: map[string]any{"text": "original"}})
			if err != nil {
				t.Fatal(err)
			}
			source := runTicks(t, s, unwrapWorkflow(reply)["id"].(string), 8)
			if removed == "history" {
				err = s.runs.DeleteRunHistory(ctx, def.ID, source.ID, *source.CompletedAt)
			} else {
				err = store.DeleteDefinition(ctx, def.ID, source.RevisionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			next, history := bindConsumerFixture(t, s, chat, "", "eleventh", "input-11")
			_, err = s.handleConsumerTool(ctx, history, next, chat, workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: source.ID, RequestID: "eleven"})
			if !errors.Is(err, status_error.WorkflowNotFound) {
				t.Fatalf("removed %s returned %v", removed, err)
			}
			runs, err := s.runs.ListChatRunSources(ctx, chat.ID, []string{next.ID})
			if err != nil || len(runs) != 0 {
				t.Fatalf("removal created an execution: %v %v", runs, err)
			}
		})
	}
}

func TestStopChatWorkflowTurnPreservesTerminalAndRejectsLateAdmission(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	chat := consumerFixtureChat(t, "chat")
	first, firstLease := bindConsumerFixture(t, s, chat, definition.ID, "first", "message-first")
	second, secondLease := bindConsumerFixture(t, s, chat, definition.ID, "second", "message-second")
	input := workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "test"}}
	if err := s.StopChatWorkflowTurn(ctx, chat.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleConsumerTool(ctx, firstLease, first, chat, input); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("stale validation admitted a run after stop: %v", err)
	}
	response, err := s.handleConsumerTool(ctx, secondLease, second, chat, input)
	if err != nil {
		t.Fatal(err)
	}
	secondID := unwrapWorkflow(response)["id"].(string)
	if err := s.StopChatWorkflowTurn(ctx, chat.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	run, err := s.runs.GetRun(ctx, secondID)
	if err != nil || run.Status != constants.WorkflowRunStatusQueued {
		t.Fatalf("old stop affected a different submission: %+v %v", run, err)
	}
	run = runTicks(t, s, secondID, 8)
	if run.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("test run was not successful: %+v", run)
	}
	if err := s.StopChatWorkflowTurn(ctx, chat.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.runs.GetRun(ctx, secondID)
	if err != nil || stored.Status != run.Status || stored.Version != run.Version {
		t.Fatalf("stop revised a committed terminal result: %+v %v", stored, err)
	}
}

func TestChatWorkflowRepeatUsesTheRetryPlanForCancelledRun(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	chat := consumerFixtureChat(t, "chat")
	first, firstLease := bindConsumerFixture(t, s, chat, definition.ID, "first", "message-first")
	originalResponse, err := s.handleConsumerTool(ctx, firstLease, first, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start-first", Args: map[string]any{"text": "Frozen"}})
	if err != nil {
		t.Fatal(err)
	}
	oldID := unwrapWorkflow(originalResponse)["id"].(string)
	if err := s.CancelRun(ctx, oldID); err != nil {
		t.Fatal(err)
	}
	old := runTicks(t, s, oldID, 8)
	if old.Status != constants.WorkflowRunStatusCancelled {
		t.Fatalf("original run did not stop: %+v", old)
	}
	second, nextLease := bindConsumerFixture(t, s, chat, "", "second", "message-second")
	newResponse, err := s.handleConsumerTool(ctx, nextLease, second, chat, workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: oldID, RequestID: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	newRun := runTicks(t, s, unwrapWorkflow(newResponse)["id"].(string), 8)
	if newRun.Context["retrySourceRunId"] != oldID || newRun.SourceRunID != oldID || newRun.SourceMessageID != "message-second" || newRun.Result.(map[string]any)["message"] != "FROZEN FROZEN!" {
		t.Fatalf("cancelled source did not go through its own frozen retry plan: %+v", newRun)
	}
}

func TestChatWorkflowRepeatRejectsModifiedFrozenFile(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, `export const meta={sdk:'bot.workflow',title:'报告',description:'按已冻结的输入出报告',inputSchema:{type:'object',properties:{report:{type:'string',format:'file'}},required:['report']},phases:[{id:'review',title:'复核',detail:'使用原文件',canExit:true}]}; phase('review'); return {accepted:true};`)
	chat := consumerFixtureChat(t, "chat")
	originalPath := filepath.Join(chat.WorkspaceDirectory, "report.txt")
	if err := os.WriteFile(originalPath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, firstLease := bindConsumerFixture(t, s, chat, definition.ID, "first", "first-input")
	started, err := s.handleConsumerTool(ctx, firstLease, first, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"report": originalPath}})
	if err != nil {
		t.Fatal(err)
	}
	source := runTicks(t, s, unwrapWorkflow(started)["id"].(string), 8)
	if source.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("original run did not complete: %+v", source)
	}
	if err := os.WriteFile(originalPath, []byte("edited original"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, secondLease := bindConsumerFixture(t, s, chat, "", "second", "second-input")
	command := workflowtool.Input{Action: constants.WorkflowActionRunRepeat, RunID: source.ID, RequestID: "repeat"}
	response, err := s.handleConsumerTool(ctx, secondLease, second, chat, command)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.runs.GetRun(ctx, unwrapWorkflow(response)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(repeated.Args.(map[string]any)["report"].(string))
	if err != nil || string(bytes) != "original" {
		t.Fatalf("repeat read live input in place of the snapshot: %q %v", bytes, err)
	}
	if err := os.WriteFile(source.Args.(map[string]any)["report"].(string), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, thirdLease := bindConsumerFixture(t, s, chat, "", "third", "third-input")
	command.RequestID = "repeat-tampered"
	if _, err := s.handleConsumerTool(ctx, thirdLease, third, chat, command); !util.HasStatusCode(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("tampered frozen input was admitted: %v", err)
	}
	rows, err := s.runs.ListChatRunSources(ctx, chat.ID, []string{third.ID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("tampered input created a run: %+v %v", rows, err)
	}
}

func consumerFixtureChat(t *testing.T, id string) models.Chat {
	t.Helper()
	directory, err := workflowfiles.EnsureScopeDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return models.Chat{ID: id, WakerID: "partner", WorkspaceDirectory: directory, LatestSequence: 1}
}

func bindConsumerFixture(t *testing.T, s *Service, chat models.Chat, definitionID, executionID, inputMessageID string) (models.Execution, models.WorkflowConsumerLease) {
	t.Helper()
	bound, err := s.BindWorkflowTurn(t.Context(), chat, executionID, workinput.SelectWorkflow(definitionID))
	if err != nil || bound.Capability == nil {
		t.Fatalf("grant not prepared: %+v %v", bound, err)
	}
	lease, err := s.consumers.GetConsumer(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	return models.Execution{ID: executionID, ChatID: chat.ID, UserMessageID: inputMessageID, PermissionLevel: constants.PermissionLevelDefault}, lease
}

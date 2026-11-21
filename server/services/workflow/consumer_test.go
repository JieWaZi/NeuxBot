package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// consumerExecutionBoundary 只替代外部 Runtime 配置验证，运行仍经真实 SQLite 和 Compiler。
type consumerExecutionBoundary struct {
	// Service 保留与本测试无关的完整 Core 接口，未使用方法不得调用。
	execution.Service
}

func (consumerExecutionBoundary) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return nil
}

// consumerChatBoundary 提供 Runtime 所属真实回合的可控权威查询边界。
type consumerChatBoundary struct {
	// Module 保留与能力校验无关的 Chat 接口，未使用方法不得调用。
	chat.Module
	// actual 是当前权威执行配置和生命周期事实。
	actual models.Execution
	// summary 保存当前回合和获准工作范围。
	summary chat.ChatSummary
	// executionReads 记录能力调用读取真实执行的次数。
	executionReads atomic.Int32
}

func (c *consumerChatBoundary) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	c.executionReads.Add(1)
	if c.actual.ChatID != chatID || c.actual.ID != executionID {
		return models.Execution{}, status_error.ExecutionNotFound
	}
	return c.actual, nil
}
func (c *consumerChatBoundary) GetChatSummary(_ context.Context, chatID string) (chat.ChatSummary, error) {
	if c.summary.Chat.ID != chatID {
		return chat.ChatSummary{}, status_error.ChatNotFound
	}
	return c.summary, nil
}

func TestConsumerDescribesAdmissionRevisionAfterHeadChanges(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "selection", "user-message")
	if _, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "publish-next", ExpectedRevisionID: definition.CurrentRevisionID, SDK: constants.WorkflowSDKIdentifier, Source: strings.Replace(workflowTestSource(t), "纯计算验收", "新版工作流", 1)}); err != nil {
		t.Fatal(err)
	}
	value, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := unwrapWorkflow(value)["items"].([]map[string]any)
	if len(items) != 1 || items[0]["revisionId"] != definition.CurrentRevisionID {
		t.Fatalf("contract drifted from the admission revision %s: %+v", definition.CurrentRevisionID, items)
	}
}

func TestConsumerLifetimeFollowsItsActiveExecution(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "long-running-chat")
	lease := models.WorkflowConsumerLease{
		ID: "long-running-execution", ChatID: conversation.ID,
		Selection: &workinput.WorkflowReference{ID: definition.ID, RevisionID: definition.CurrentRevisionID},
		CreatedAt: time.Now().Add(-25 * time.Hour).UnixMilli(),
	}
	if err := s.consumers.CreateConsumer(ctx, lease); err != nil {
		t.Fatal(err)
	}
	conversation.ActiveExecutionID = lease.ID
	queries := &consumerChatBoundary{
		actual:  models.Execution{ID: lease.ID, ChatID: conversation.ID, UserMessageID: "input", Status: constants.ExecutionStatusRunning, PermissionLevel: constants.PermissionLevelDefault},
		summary: chat.ChatSummary{Chat: conversation},
	}
	s.chats = queries
	s.executions = consumerExecutionBoundary{}
	scope := workflowtool.Scope{Key: lease.ID, Kind: constants.WorkflowCapabilityScopeConsumer}
	query := workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryWorkflows}
	if _, err := s.handleTool(ctx, scope, query); err != nil {
		t.Fatalf("active execution lost its selected workflow after a day: %v", err)
	}
	start := workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "long-running-start", Args: map[string]any{"text": "input"}}
	if _, err := s.handleTool(ctx, scope, start); err != nil {
		t.Fatalf("active execution could not admit its selected workflow: %v", err)
	}
	now := time.Now().UnixMilli()
	queries.actual.CompletedAt = &now
	queries.actual.Status = constants.ExecutionStatusCompleted
	if _, err := s.handleTool(ctx, scope, query); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("completed execution retained workflow access: %v", err)
	}
	start.RequestID = "after-completion"
	if _, err := s.handleTool(ctx, scope, start); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("completed execution admitted another workflow: %v", err)
	}
	runs, err := s.runs.ListChatRunSources(ctx, conversation.ID, []string{lease.ID})
	if err != nil || len(runs) != 1 {
		t.Fatalf("execution lifetime produced unexpected runs: %+v %v", runs, err)
	}
}

func TestConsumerCLIScopesReplayActualDefaultsAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	other, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "create-other", Title: "另一个流程"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: other.ID, RequestID: "save", Source: strings.Replace(workflowTestSource(t), "纯计算验收", "另一个流程", 1), SDK: constants.WorkflowSDKIdentifier})
	if err != nil {
		t.Fatal(err)
	}
	other.CurrentRevisionID = receipt.RevisionID
	s.executions = consumerExecutionBoundary{}
	conversation := consumerFixtureChat(t, "chat")
	_, lease := bindConsumerFixture(t, s, conversation, definition.ID, "consumer-execution", "user-message")
	again, err := s.BindWorkflowTurn(ctx, conversation, lease.ID, workinput.SelectWorkflow(definition.ID))
	if err != nil || again.Reference == nil || again.Reference.RevisionID != lease.Selection.RevisionID {
		t.Fatalf("capability replay changed selection: %+v %v", again, err)
	}
	if _, err := s.BindWorkflowTurn(ctx, conversation, lease.ID, workinput.SelectWorkflow(other.ID)); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatalf("replay expanded selection: %v", err)
	}
	queries := &consumerChatBoundary{
		actual: models.Execution{ID: lease.ID, ChatID: lease.ChatID, UserMessageID: "user-message", Status: constants.ExecutionStatusRunning, RuntimeID: "actual-runtime", AccountID: "actual-account", Model: "actual-model", Reasoning: "actual-reasoning", PermissionLevel: constants.PermissionLevelDefault, WorkspaceReadOnly: true},
	}
	s.chats = queries
	approvedDirectory := t.TempDir()
	approvedDirectory, err = workflowfiles.EnsureScopeDirectory(approvedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	queries.summary = chat.ChatSummary{Chat: models.Chat{ID: lease.ChatID, ActiveExecutionID: lease.ID, AdditionalWorkingDirectories: []string{approvedDirectory}}}
	registry := capability.New(nil)
	grant, activate, revoke, err := registry.Prepare(capability.GrantSpec{ExecutionID: lease.ID, Bindings: s.consumerAgentBindings(lease), Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer revoke()
	call := func(input map[string]any, failed bool) map[string]any {
		t.Helper()
		params := map[string]any{}
		for key, value := range input {
			if key != "action" && key != "type" && key != "requestId" {
				if key == "workflowId" {
					key = "definitionId"
				}
				params[key] = value
			}
		}
		id, _ := input["action"].(string)
		switch id {
		case "query":
			switch input["type"] {
			case constants.WorkflowQueryWorkflows:
				id = "workflow.definitions.list"
			case constants.WorkflowQuerySDK:
				id = "workflow.sdk.get"
			default:
				id = "workflow.results.schema"
			}
		case "run.start":
			id = "workflow.runs.start"
			params["revisionId"] = definition.CurrentRevisionID
			if params["args"] == nil {
				params["args"] = map[string]any{}
			}
		case "run.get":
			id = "workflow.runs.get"
		case "run.cancel":
			id = "workflow.runs.cancel"
		default:
			id = "workflow." + id
		}
		requestID, _ := input["requestId"].(string)
		body, _ := json.Marshal(capability.Request{Operation: id, RequestID: requestID, Params: params})
		request, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1"+constants.AgentCLIPath+"/commands", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+grant.Token)
		if id == "workflow.runs.start" {
			request.Header.Set(constants.AgentCLIExecutionHeader, lease.ID)
		}
		request.RemoteAddr = "127.0.0.1:12345"
		recorder := httptest.NewRecorder()
		registry.ServeHTTP(recorder, request)
		response := recorder.Result()
		defer response.Body.Close()
		var value map[string]any
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		if value["ok"] != !failed {
			t.Fatalf("CLI success mismatch: %+v failed=%t", value, failed)
		}
		return value
	}
	beforeList := queries.executionReads.Load()
	list := call(map[string]any{"action": constants.WorkflowActionQuery, "type": constants.WorkflowQueryWorkflows}, false)
	if reads := queries.executionReads.Load() - beforeList; reads != 1 {
		t.Fatalf("listing workflows loaded the same execution %d times", reads)
	}
	call(map[string]any{"action": constants.WorkflowActionQuery, "type": constants.WorkflowQuerySDK}, true)
	call(map[string]any{"action": constants.WorkflowActionQuery, "type": constants.WorkflowQueryResultSchema}, true)
	call(map[string]any{"action": constants.WorkflowActionSourceGet}, true)
	items := list["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != definition.ID {
		t.Fatalf("directory leaked unselected definition: %+v", items)
	}
	start := map[string]any{"action": "run.start", "workflowId": definition.ID, "requestId": "run", "args": map[string]any{"text": "NEUXBOT"}}
	first := call(start, false)["data"].(map[string]any)
	second := call(start, false)["data"].(map[string]any)
	runID, _ := first["runId"].(string)
	if runID == "" || runID != second["runId"] {
		t.Fatal("duplicate consumer start created a second run")
	}
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil || run.Defaults.RuntimeID != "actual-runtime" || run.Defaults.AccountID != "actual-account" || run.Defaults.Model != "actual-model" || !run.Defaults.WorkspaceReadOnly || run.ScopeBinding.WorkingDirectory != approvedDirectory || run.Context["triggerSource"] != "agent" {
		t.Fatalf("model escaped actual execution configuration: %+v %v", run, err)
	}
	runTicks(t, s, runID, 12)
	observed := call(map[string]any{"action": "run.get", "runId": runID}, false)["data"].(map[string]any)["run"].(map[string]any)
	if observed["id"] != runID || observed["triggerSource"] != "agent" {
		t.Fatalf("public tool lost run identity/origin: %+v", observed)
	}
	for _, field := range []string{"flowContext", "rootFiberId", "workspace"} {
		if _, found := observed[field]; found {
			t.Errorf("public tool leaks %s", field)
		}
	}
	call(map[string]any{"action": "run.start", "workflowId": other.ID, "requestId": "other"}, true)
	call(map[string]any{"action": "run.start", "workflowId": definition.ID, "requestId": "escalate", "permissionLevel": "full_access"}, true)
	manual, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "manual", Args: map[string]any{"text": "manual"}})
	if err != nil {
		t.Fatal(err)
	}
	call(map[string]any{"action": "run.get", "runId": manual.ID}, true)
	call(map[string]any{"action": "run.cancel", "runId": manual.ID, "requestId": "cancel"}, true)
	call(map[string]any{"action": "run.cancel", "runId": runID, "requestId": "cancel"}, false)
	now := time.Now().UnixMilli()
	queries.actual.CompletedAt = &now
	queries.actual.Status = constants.ExecutionStatusCompleted
	call(map[string]any{"action": "run.get", "runId": runID}, true)
	call(map[string]any{"action": constants.WorkflowActionQuery, "type": constants.WorkflowQueryWorkflows}, true)
}

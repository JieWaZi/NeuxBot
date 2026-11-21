package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// workerDispatchExecution 捕获真实适配器交给 Execution 的请求，替代外部 Runtime 接纳。
type workerDispatchExecution struct {
	// Service 保留本用例不会调用的其他执行能力。
	execution.Service
	// intent 是适配器实际提交的中立工作意图。
	intent execution.WorkIntent
	// status 可模拟幂等重放返回已经完成的首次执行。
	status constants.ExecutionStatus
	// validationError 模拟原工作接纳后执行环境发生变化。
	validationError error
}

// deletedWorkerExecutions 记录删除事务提交后发出的精确执行取消。
type deletedWorkerExecutions struct {
	// Service 保留本用例不使用的原生执行端口。
	execution.Service
	// chatID 是最后一次取消的实际 Worker 会话。
	chatID string
	// executionID 是最后一次取消的原执行身份。
	executionID string
	// wholeChatStops 记录误用当前会话停止入口的次数。
	wholeChatStops int
}

func (e *deletedWorkerExecutions) CancelExecution(chatID, executionID string) {
	e.chatID, e.executionID = chatID, executionID
}

func (e *deletedWorkerExecutions) CancelExecutionAndWait(_ context.Context, chatID, executionID string) error {
	e.chatID, e.executionID = chatID, executionID
	return nil
}

func (e *deletedWorkerExecutions) CancelAndWait(context.Context, string) error {
	e.wholeChatStops++
	return nil
}

// commitWorkerAdmission 模拟首次接纳已 durable、派发尚未返回，不建立 ChatExecutionLocation。
func commitWorkerAdmission(t *testing.T, s *Service, effect models.WorkflowEffect) string {
	t.Helper()
	chatID := requestidempotency.Identity{ID: effect.ID}.ResourceID("chat", "fixture")
	authority, err := conversation.NewStore(s.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	if _, err := authority.Commit(t.Context(), agent.ConversationBatch{ChatID: chatID, Key: effect.ID, Changes: []agent.ConversationChange{
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated, Data: json.RawMessage(`{"wakerId":"fixture"}`)}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: effect.ExecutionID, Data: json.RawMessage(`{"runtimeId":"fixture"}`)}},
	}}); err != nil {
		t.Fatal(err)
	}
	return chatID
}

// stoppingWorker 在真实 outbox 边界模拟丢失收据、查询故障和一次停止失败。
type stoppingWorker struct {
	// controlledWorker 提供冻结配置及派发、停止次数记录。
	controlledWorker
	// cancelError 是原生停止端口返回的错误。
	cancelError error
	// inspectError 模拟首次执行查询结果不确定或丢失。
	inspectError error
	// cancelDispatch 模拟真实派发退出时收到执行所有者取消。
	cancelDispatch context.CancelFunc
}

func (w *stoppingWorker) Dispatch(_ context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	w.prompts = append(w.prompts, input.Effect.ID)
	if w.cancelDispatch != nil {
		w.cancelDispatch()
	}
	return WorkerReceipt{}, status_error.WorkflowEffectFailed
}

func (w *stoppingWorker) Inspect(_ context.Context, effect models.WorkflowEffect) (WorkerReceipt, error) {
	w.inspections++
	return WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusRunning}, w.inspectError
}

func (w *stoppingWorker) Cancel(context.Context, models.WorkflowEffect) error {
	w.cancellations++
	return w.cancelError
}

func TestWorkerStopsOnceAcrossAdmissionAndCancellation(t *testing.T) {
	for _, mode := range []string{"prepared-after-restart", "stop-failed", "execution-context", "dispatch-unknown", "running-chat-missing"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			s, _ := workflowTestService(t)
			worker := &stoppingWorker{cancelError: status_error.WorkflowEffectFailed}
			s.worker = worker
			definition := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`)
			run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: mode, Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("worker was not prepared: %+v %v", state, err)
			}
			effect := state.Effects[0]
			dispatchContext := ctx
			switch mode {
			case "prepared-after-restart", "stop-failed":
				commitWorkerAdmission(t, s, effect)
				if mode == "prepared-after-restart" {
					worker.cancelError = nil
				}
				if err := s.CancelRun(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
				if current := runTicks(t, s, run.ID, 1); current.Status != constants.WorkflowRunStatusCancelling {
					t.Fatalf("prepared admission bypassed its actual stop: %+v", current)
				}
			case "execution-context":
				var cancel context.CancelFunc
				dispatchContext, cancel = context.WithCancel(ctx)
				defer cancel()
				worker.cancelDispatch = cancel
				s.runner = &runnerLifecycle{ctx: ctx}
			case "dispatch-unknown":
				worker.inspectError = status_error.WorkflowEffectFailed
			case "running-chat-missing":
				worker.inspectError = status_error.ChatNotFound
				if err := s.state.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
					current.Status = constants.WorkflowStepStatusRunning
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if s.state.EffectActive(effect.ID) {
				t.Fatal("fixture unexpectedly retained a process-local dispatch")
			}
			s.dispatchEffect(dispatchContext, state.Run, effect)
			if worker.cancellations != 1 {
				t.Fatalf("one stop triggered compensating stops: calls=%d", worker.cancellations)
			}
			if mode == "prepared-after-restart" || mode == "stop-failed" {
				if len(worker.prompts) != 0 {
					t.Fatal("stopping a prepared admission started another execution")
				}
			}
			expectedStep, expectedRun := constants.WorkflowStepStatusUnknown, constants.WorkflowRunStatusFailed
			if mode == "prepared-after-restart" {
				expectedStep, expectedRun = constants.WorkflowStepStatusCancelled, constants.WorkflowRunStatusCancelled
			}
			state, err = s.runs.LoadRunState(ctx, run.ID)
			if err != nil || state.Effects[0].Status != expectedStep {
				t.Fatalf("one stop did not record its outcome: %+v %v", state, err)
			}
			if completed := runTicks(t, s, run.ID, 2); completed.Status != expectedRun || worker.cancellations != 1 {
				t.Fatalf("stop outcome did not finish directly: %+v calls=%d", completed, worker.cancellations)
			}
		})
	}
}

func TestWorkerCancellationKeepsLaterChatExecutionRunning(t *testing.T) {
	s, _ := workflowTestService(t)
	effect := models.WorkflowEffect{ID: "attempt", ExecutionID: "original", Boundary: workflowdsl.EffectBoundary{Primitive: "worker", Arguments: []workflowdsl.Value{
		workflowdsl.JSONValue("read"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}}),
	}}}
	effect.ChatID = commitWorkerAdmission(t, s, effect)
	authority, err := conversation.NewStore(s.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if _, err := authority.Commit(t.Context(), agent.ConversationBatch{ChatID: effect.ChatID, Key: "continue", ExpectedVersion: 2, Changes: []agent.ConversationChange{
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: effect.ExecutionID, Data: json.RawMessage(`{"status":"completed"}`)}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "continued", Data: json.RawMessage(`{"runtimeId":"fixture"}`)}},
	}}); err != nil {
		t.Fatal(err)
	}
	if current, err := authority.ReadLatestExecutionID(t.Context(), effect.ChatID); err != nil || current != "continued" {
		t.Fatalf("worker chat did not continue: current=%s error=%v", current, err)
	}
	port := &deletedWorkerExecutions{}
	if err := (CoreWorkerAdapter{Executions: port}).Cancel(t.Context(), effect); err != nil {
		t.Fatal(err)
	}
	if port.wholeChatStops != 0 || port.chatID != effect.ChatID || port.executionID != effect.ExecutionID {
		t.Fatalf("workflow cancellation targeted the later chat execution: %+v", port)
	}
}

func TestDeletedWorkerChatFailsTaskAndRejectsLateSuccess(t *testing.T) {
	for _, status := range []constants.WorkflowStepStatus{constants.WorkflowStepStatusPrepared, constants.WorkflowStepStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			ctx := t.Context()
			s, _ := workflowTestService(t)
			s.worker = &retryWorker{}
			executions := &deletedWorkerExecutions{}
			s.executions = executions
			definition := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`)
			run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "deleted-worker", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("worker was not prepared: %+v %v", state, err)
			}
			effect := state.Effects[0]
			chatID := commitWorkerAdmission(t, s, effect)
			if effect.ChatID != chatID {
				t.Fatalf("prepared worker has no durable chat ownership: %+v", effect)
			}
			if err := s.state.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
				current.Status = status
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			job, cancel, admitted := s.state.BeginEffect(ctx, effect.ID, false, 1)
			if !admitted {
				t.Fatal("worker job was not admitted")
			}
			defer cancel()
			defer s.state.EndEffect(effect.ID)
			if err := s.transactions.Transaction(ctx, func(tx context.Context) error {
				if err := s.FailChatRuns(tx, []string{chatID}); err != nil {
					return err
				}
				if job.Err() != nil || executions.chatID != "" {
					t.Error("deletion cancelled native work before committing its failed task")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if job.Err() == nil || executions.chatID != chatID || executions.executionID != effect.ExecutionID {
				t.Fatalf("deletion did not cancel its exact work after commit: job=%v chat=%s execution=%s", job.Err(), executions.chatID, executions.executionID)
			}
			failed, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || failed.Run.Status != constants.WorkflowRunStatusFailed || failed.Effects[0].Status != constants.WorkflowStepStatusFailed || failed.Instances[0].Status != constants.WorkflowStepStatusFailed {
				t.Fatalf("deleting a worker did not directly fail its task: %+v %v", failed, err)
			}
			s.finishEffect(ctx, effect, WorkerReceipt{ChatID: chatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: "late result"}, nil)
			after, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || after.Run.Status != constants.WorkflowRunStatusFailed || after.Run.Version != failed.Run.Version || after.Effects[0].Status != constants.WorkflowStepStatusFailed || after.Effects[0].Result != nil {
				t.Fatalf("late success revived the deleted task: %+v %v", after, err)
			}
		})
	}
}

func (e *workerDispatchExecution) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return e.validationError
}

func (e *workerDispatchExecution) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	e.intent = intent
	status := e.status
	if status == "" {
		status = constants.ExecutionStatusRunning
	}
	return execution.AcceptedExecution{Chat: models.Chat{ID: "worker-chat"}, Execution: models.Execution{ID: intent.ExecutionID, Status: status}}, nil
}

func TestWorkerConfigurationInheritsOnlyWithinTheSameSelection(t *testing.T) {
	fast := true
	defaults := models.WorkflowExecutionDefaults{
		RuntimeID: "codex", AccountID: "work", Model: "original-model", Reasoning: "high",
		Fast: &fast, PermissionLevel: constants.PermissionLevelAuto, WorkspaceReadOnly: true,
	}
	for _, test := range []struct {
		// name 标识节点覆盖的层级。
		name string
		// resolve 是当前节点的字段，缺省时继承兼容的运行配置。
		resolve map[string]any
		// runtime、account、model、reasoning、fast 是期望生效的选择。
		runtime   string
		account   string
		model     string
		reasoning string
		fast      *bool
	}{
		{name: "inherit", runtime: "codex", account: "work", model: "original-model", reasoning: "high", fast: &fast},
		{name: "runtime", resolve: map[string]any{"runtimeId": "claude"}, runtime: "claude"},
		{name: "account", resolve: map[string]any{"accountId": "system"}, runtime: "codex", account: "system"},
		{name: "model", resolve: map[string]any{"model": "other-model"}, runtime: "codex", account: "work", model: "other-model"},
		{name: "default-model", resolve: map[string]any{"model": ""}, runtime: "codex", account: "work"},
		{name: "default-reasoning", resolve: map[string]any{"reasoning": ""}, runtime: "codex", account: "work", model: "original-model", fast: &fast},
		{name: "explicit", resolve: map[string]any{"runtimeId": "claude", "accountId": "other", "model": "target-model", "reasoning": "low"},
			runtime: "claude", account: "other", model: "target-model", reasoning: "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := workerConfiguration(map[string]any{"resolve": test.resolve}, defaults)
			want := models.WorkflowExecutionDefaults{
				RuntimeID: test.runtime, AccountID: test.account, Model: test.model, Reasoning: test.reasoning,
				Fast: test.fast, PermissionLevel: defaults.PermissionLevel, WorkspaceReadOnly: defaults.WorkspaceReadOnly,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("节点配置 = %+v，期望 %+v", got, want)
			}
		})
	}
}

func TestCoreWorkerDispatchAlignsStructuredPromptWithResultBindings(t *testing.T) {
	for _, runtimeID := range []string{"codex", "unsupported"} {
		t.Run(runtimeID, func(t *testing.T) {
			port := &workerDispatchExecution{}
			adapter := CoreWorkerAdapter{Executions: port}
			options := map[string]any{"resolve": map[string]any{"wakerId": "fixture"}, "schema": map[string]any{"type": "object", "required": []any{"verified"}, "properties": map[string]any{"verified": map[string]any{"type": "boolean"}}}}
			effect := models.WorkflowEffect{ID: "attempt", ExecutionID: "worker-execution", AuthToken: "attempt-token", Boundary: workflowdsl.EffectBoundary{NodeID: "review", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("Verify the original evidence."), workflowdsl.JSONValue(options)}}}
			owner := &Service{}
			_, err := adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: workerTestRun(t, "review", runtimeID), ResultBindings: owner.AgentBindings(workflowtool.Scope{Key: effect.ID, Token: effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult})})
			if err != nil {
				t.Fatal(err)
			}
			intent := port.intent
			operations := []string{}
			for _, capability := range intent.Options.ToolCapabilities {
				for _, binding := range capability.Bindings {
					operations = append(operations, binding.Operation)
				}
			}
			if !reflect.DeepEqual(operations, []string{"workflow.results.schema", "workflow.results.submit"}) {
				t.Fatalf("structured worker lost its actual attempt result bindings: %+v", operations)
			}
			if intent.Input.Content != "Verify the original evidence." {
				t.Fatalf("host result instructions entered the visible business message: %s", intent.Input.Content)
			}
			resultInstructions := []string{}
			for _, instruction := range intent.Input.ContextInstructions {
				if instruction.Name == "WORKFLOW_RESULTS.md" {
					resultInstructions = append(resultInstructions, instruction.Content)
				}
			}
			if len(resultInstructions) != 1 {
				t.Fatalf("worker needs exactly one model-only result contract: %+v", intent.Input.ContextInstructions)
			}
			prompt := resultInstructions[0]
			for _, required := range []string{"Worker Results", "host-provided bot skill", "workflow.results.schema", "workflow.results.submit", "stable requestId", "When bot_capabilities MCP is injected", "workflow_results_schema", "workflow_results_submit", "Otherwise follow the host-provided CLI transport", "Do not switch transports to bypass an error", "without an invented wrapper or extra JSON encoding", "Final chat text is not a result submission", "An accepted candidate cannot be replaced", "finish this execution so the host can confirm node success separately"} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("model-only context lost result contract %q: %s", required, prompt)
				}
			}
			const schemaJSON = `{"properties":{"verified":{"type":"boolean"}},"required":["verified"],"type":"object"}`
			if !strings.Contains(prompt, schemaJSON) || strings.Contains(prompt, "Return exactly one JSON value") {
				t.Fatalf("worker lost its complete result schema or received competing final-text instructions: %s", prompt)
			}
			delete(options, "schema")
			effect.Boundary.Arguments[1] = workflowdsl.JSONValue(options)
			_, err = adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: workerTestRun(t, "review", runtimeID)})
			if err != nil || port.intent.Input.Content != "Verify the original evidence." || len(port.intent.Options.ToolCapabilities) != 0 {
				t.Fatalf("plain worker result contract changed: %+v %v", port.intent, err)
			}
			for _, instruction := range port.intent.Input.ContextInstructions {
				if instruction.Name == "WORKFLOW_RESULTS.md" {
					t.Fatal("void worker received a structured result contract")
				}
			}
		})
	}
}

func TestCoreWorkerDispatchPreservesFrozenFast(t *testing.T) {
	falseValue, trueValue := false, true
	for _, fast := range []*bool{nil, &falseValue, &trueValue} {
		port := &workerDispatchExecution{}
		run := workerTestRun(t, "work", "codex")
		run.WorkerConfigurations["work"] = models.WorkflowExecutionDefaults{RuntimeID: "codex", Fast: fast, PermissionLevel: constants.PermissionLevelDefault}
		effect := models.WorkflowEffect{ID: "fast-attempt", ExecutionID: "fast-execution", Boundary: workflowdsl.EffectBoundary{NodeID: "work", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("执行已接纳配置"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}})}}}
		if _, err := (CoreWorkerAdapter{Executions: port}).Dispatch(t.Context(), WorkerDispatch{Run: run, Effect: effect}); err != nil {
			t.Fatal(err)
		}
		if !port.intent.Options.ConfigurationResolved || !reflect.DeepEqual(port.intent.Options.Fast, fast) {
			t.Fatalf("worker changed admitted Fast: want=%v actual=%+v", fast, port.intent.Options)
		}
	}
}

func TestCoreWorkerDispatchRejectsUnenforceableConnectorPolicy(t *testing.T) {
	port := &workerDispatchExecution{}
	options := map[string]any{
		"resolve":    map[string]any{"wakerId": "fixture"},
		"toolPolicy": map[string]any{"connectors": map[string]any{"mode": "allowlist", "connectorIds": []any{}}},
	}
	effect := models.WorkflowEffect{ID: "attempt", ExecutionID: "first", Boundary: workflowdsl.EffectBoundary{
		NodeID: "read", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("read"), workflowdsl.JSONValue(options)},
	}}
	_, err := (CoreWorkerAdapter{Executions: port}).Dispatch(t.Context(), WorkerDispatch{Run: workerTestRun(t, "read", "codex"), Effect: effect})
	if err == nil || port.intent.ClientRequestID != "" {
		t.Fatalf("dispatch would start a worker without its requested restriction: %+v %v", port.intent, err)
	}
}

func workerTestRun(t *testing.T, nodeID, runtimeID string) models.WorkflowRun {
	t.Helper()
	run := models.WorkflowRun{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", DefinitionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{nodeID: {RuntimeID: runtimeID}}}
	workspace, err := workflowfiles.PrepareWorkspace(t.TempDir(), run.DefinitionID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(workspace)
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	run.Context = map[string]any{"workspace": value}
	return run
}

func TestCoreWorkerDispatchAddsInternalWorkspaceWithoutReplacingBusinessScope(t *testing.T) {
	port := &workerDispatchExecution{}
	adapter := CoreWorkerAdapter{Executions: port}
	effect := models.WorkflowEffect{ID: "workspace-attempt", ExecutionID: "workspace-execution", Boundary: workflowdsl.EffectBoundary{NodeID: "write", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("保存本次成果"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}})}}}
	run := workerTestRun(t, "write", "codex")
	primary, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run.ScopeBinding.WorkingDirectory = primary
	run.AdditionalWorkingDirectories = []string{secondary}
	workspace := run.Context["workspace"].(map[string]any)
	if _, err := adapter.Dispatch(context.Background(), WorkerDispatch{Run: run, Effect: effect}); err != nil {
		t.Fatal(err)
	}
	if port.intent.Chat.Create.WorkingDirectory != primary || port.intent.Input.Content != "保存本次成果" {
		t.Fatalf("workspace instructions replaced the frozen business scope or message: %+v", port.intent)
	}
	if !reflect.DeepEqual(port.intent.Chat.Create.AdditionalWorkingDirectories, []string{workspace["rootDirectory"].(string), secondary}) {
		t.Fatalf("worker scope omitted its host-frozen internal root: %+v", port.intent.Chat.Create)
	}
	if len(port.intent.Input.ContextInstructions) == 0 {
		t.Fatal("worker did not receive its frozen internal workspace")
	}
	content := port.intent.Input.ContextInstructions[0].Content
	for _, path := range workspace {
		if !strings.Contains(content, path.(string)) {
			t.Fatalf("worker cannot locate its internal directory %q: %s", path, content)
		}
	}
}

func TestCoreWorkerDispatchRejectsReboundWorkspaceRoot(t *testing.T) {
	port := &workerDispatchExecution{}
	run := workerTestRun(t, "work", "codex")
	root := run.Context["workspace"].(map[string]any)["rootDirectory"].(string)
	if err := os.Rename(root, root+"-original"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	effect := models.WorkflowEffect{ID: "rebound-workspace", ExecutionID: "execution", Boundary: workflowdsl.EffectBoundary{NodeID: "work", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("处理工作"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}})}}}
	_, err := (CoreWorkerAdapter{Executions: port}).Dispatch(context.Background(), WorkerDispatch{Run: run, Effect: effect})
	var failure interface{ Key() string }
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowResourceInvalid.Key() {
		t.Fatalf("worker accepted a replaced workspace root: %v", err)
	}
	if port.intent.Chat.Create != nil {
		t.Fatal("rebound workspace was dispatched with outside authorization")
	}
	for _, name := range []string{"scripts", "state", "cache", "tmp", "runs"} {
		if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
			t.Fatalf("outside directory changed through a replaced workspace: %s %v", name, err)
		}
	}
}

func TestCoreWorkerDispatchRejectsMissingFrozenWorkspace(t *testing.T) {
	port := &workerDispatchExecution{}
	effect := models.WorkflowEffect{ID: "missing-workspace", ExecutionID: "execution", Boundary: workflowdsl.EffectBoundary{NodeID: "work", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("处理工作"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}})}}}
	run := models.WorkflowRun{ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: "/approved/business"}, WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{"work": {RuntimeID: "codex"}}}
	_, err := (CoreWorkerAdapter{Executions: port}).Dispatch(context.Background(), WorkerDispatch{Run: run, Effect: effect})
	var failure interface{ Key() string }
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
		t.Fatalf("worker accepted a run without its frozen internal authorization: %v", err)
	}
	if port.intent.Chat.Create != nil {
		t.Fatal("invalid workspace was dispatched to execution")
	}
}

// finalMessageChat 只替换适配器使用的权威查询，避免模拟其他 Chat 行为。
type finalMessageChat struct {
	// Module 保留不属于本用例的 Chat 接口。
	chat.Module
	// executions 记录适配器查询的准确执行身份。
	executions []string
	// initial 是测试显式提供的首次执行，空值沿用已完成执行。
	initial *models.Execution
	// outcome 是指定执行的原生终止与失败事实。
	outcome chat.ExecutionOutcome
}

func (c *finalMessageChat) ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error) {
	initial := c.initial
	if initial == nil {
		initial = &models.Execution{ID: "first", Status: constants.ExecutionStatusCompleted}
	}
	return chat.InitialExecutionState{CreateRequestID: "attempt", InitialExecution: initial}, nil
}
func (c *finalMessageChat) ReadExecutionOutcome(_ context.Context, chatID, executionID string) (chat.ExecutionOutcome, error) {
	if chatID != "worker-chat" || executionID != "first" {
		return chat.ExecutionOutcome{}, status_error.ExecutionNotFound
	}
	return c.outcome, nil
}
func (c *finalMessageChat) ReadExecutionMessages(_ context.Context, _ string, executionID string) ([]messageview.Message, error) {
	c.executions = append(c.executions, executionID)
	return []messageview.Message{
		{ID: "process", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartProcessCommentary, Text: "过程"}}},
		{ID: "final", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "最终正文"}}},
		{ID: "continued", ExecutionID: "later", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "续聊文本"}}},
	}, nil
}
func TestCoreWorkerAdapterKeepsFinalMessageAcrossHistoryAndContinuedChat(t *testing.T) {
	port := &finalMessageChat{outcome: chat.ExecutionOutcome{TerminationReason: constants.RuntimeTerminationReasonNormal}}
	adapter := CoreWorkerAdapter{Chats: port}
	receipt, err := adapter.Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.FinalPresent || receipt.FinalMessageID != "final" || receipt.Text != "最终正文" || !reflect.DeepEqual(port.executions, []string{"first"}) {
		t.Fatalf("wrong final message: %+v executions=%v", receipt, port.executions)
	}
}

func TestCoreWorkerAdapterRejectsUnconfirmedCompletedOutput(t *testing.T) {
	for _, test := range []struct {
		// name 是原生终止或交付故障场景。
		name string
		// reason 是权威终止原因，空值模拟缺少终止证据。
		reason constants.RuntimeTerminationReason
		// failure 保留与终止原因独立的交付故障。
		failure constants.RuntimeFailureCode
	}{
		{name: "truncated", reason: constants.RuntimeTerminationReasonTruncated},
		{name: "turn-limit", reason: constants.RuntimeTerminationReasonTurnLimit},
		{name: "refusal", reason: constants.RuntimeTerminationReasonRefusal},
		{name: "unknown", reason: constants.RuntimeTerminationReasonUnknown},
		{name: "missing-termination"},
		{name: "delivery-failed", reason: constants.RuntimeTerminationReasonNormal, failure: constants.RuntimeFailureCodeEventDelivery},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &finalMessageChat{outcome: chat.ExecutionOutcome{TerminationReason: test.reason, FailureCode: test.failure}}
			receipt, err := (CoreWorkerAdapter{Chats: port}).Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Status != constants.ExecutionStatusCompleted || receipt.FinalPresent {
				t.Fatalf("unconfirmed completion became a usable worker result: %+v", receipt)
			}
			if receipt.OutputFailure == nil {
				t.Fatal("missing durable completion evidence")
			}
			failure, _ := receipt.OutputFailure.Data.(map[string]any)
			reason := string(test.reason)
			if reason == "" {
				reason = "unknown"
			}
			if failure["code"] != "RESULT_UNCONFIRMED" || failure["terminationReason"] != reason || failure["failureCode"] != string(test.failure) {
				t.Fatalf("completion evidence changed: %+v", failure)
			}
		})
	}
}

func TestUnconfirmedWorkerOutputCannotUseCandidateRepairOrRetry(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "candidate"}[structured], func(t *testing.T) {
			s, _ := workflowTestService(t)
			s.worker = &retryWorker{}
			schema := ""
			if structured {
				schema = ",schema:{type:'object'}"
			}
			definition := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('read',{id:'read',label:'读取',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},retry:{maxAttempts:2,safety:'pure'}`+schema+`});`)
			ctx := context.Background()
			run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "unconfirmed", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture", WorkspaceReadOnly: true}})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("missing worker: %+v %v", state, err)
			}
			effect := state.Effects[0]
			if structured {
				err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
					candidate := workflowdsl.JSONValue(map[string]any{"verified": true})
					current.Candidate = &candidate
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			s.repairOutput = func(context.Context, models.WorkflowExecutionDefaults, map[string]any, string) (string, error) {
				t.Fatal("unconfirmed output invoked format repair")
				return "{}", nil
			}
			failure := workflowdsl.JSONValue(map[string]any{"code": constants.WorkflowResultUnconfirmedCode, "terminationReason": "truncated"})
			receipt := WorkerReceipt{ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, Text: "{}", FinalPresent: true, OutputFailure: &failure}
			receipt, err = s.repairWorkerOutput(ctx, run, effect, receipt)
			if err != nil {
				t.Fatal(err)
			}
			s.finishEffect(ctx, effect, receipt, nil)
			runTicks(t, s, run.ID, 3)
			state, err = s.runs.LoadRunState(ctx, run.ID)
			if err != nil || state.Run.Status != constants.WorkflowRunStatusFailed || len(state.Effects) != 1 || state.Effects[0].Status != constants.WorkflowStepStatusFailed || state.Effects[0].ExecutionStatus != constants.ExecutionStatusCompleted || state.Effects[0].OutputRepairs != 0 {
				t.Fatalf("unconfirmed outcome advanced or retried: %+v %v", state, err)
			}
			if state.Effects[0].Failure == nil || structured && state.Effects[0].Candidate == nil {
				t.Fatal("original evidence was discarded")
			}
		})
	}
}

func TestCoreWorkerDispatchReplayedCompletionVerifiesOutcome(t *testing.T) {
	port := &workerDispatchExecution{status: constants.ExecutionStatusCompleted, validationError: status_error.RuntimeUnavailable}
	queries := &finalMessageChat{outcome: chat.ExecutionOutcome{TerminationReason: constants.RuntimeTerminationReasonTruncated}}
	effect := models.WorkflowEffect{ID: "attempt", ExecutionID: "first", Boundary: workflowdsl.EffectBoundary{NodeID: "read", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("read"), workflowdsl.JSONValue(map[string]any{"resolve": map[string]any{"wakerId": "fixture"}})}}}
	receipt, err := (CoreWorkerAdapter{Executions: port, Chats: queries}).Dispatch(context.Background(), WorkerDispatch{Run: workerTestRun(t, "read", "codex"), Effect: effect})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != constants.ExecutionStatusCompleted || receipt.OutputFailure == nil || receipt.FinalPresent {
		t.Fatalf("completed replay bypassed outcome verification: %+v", receipt)
	}
}

func TestCoreWorkerFailureKeepsApplicationPresentation(t *testing.T) {
	port := &finalMessageChat{initial: &models.Execution{ID: "first", Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeWorkspaceScopeUnsupported", ErrorParams: map[string]interface{}{"MaxDirectories": 2}, ErrorMessageSource: constants.ErrorMessageSourceLocalized}}
	receipt, err := (CoreWorkerAdapter{Chats: port}).Inspect(t.Context(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ErrorCode != "RuntimeWorkspaceScopeUnsupported" || receipt.ErrorParams["MaxDirectories"] != 2 || receipt.ErrorMessageSource != "localized" {
		t.Fatalf("worker failure = %+v", receipt)
	}
	port.initial.ErrorCode = "RuntimeQuotaExhausted"
	port.initial.ErrorMessage = "Native quota exhausted"
	port.initial.ErrorMessageSource = constants.ErrorMessageSourcePublic
	receipt, err = (CoreWorkerAdapter{Chats: port}).Inspect(t.Context(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil || receipt.ErrorMessage != "Native quota exhausted" || receipt.ErrorMessageSource != "public" {
		t.Fatalf("worker public reason = %+v error=%v", receipt, err)
	}
}

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
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
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
}

func (*workerDispatchExecution) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return nil
}

func (e *workerDispatchExecution) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	e.intent = intent
	return execution.AcceptedExecution{Chat: models.Chat{ID: "worker-chat"}, Execution: models.Execution{ID: intent.ExecutionID, Status: constants.ExecutionStatusRunning}}, nil
}

func TestCoreWorkerDispatchAlignsStructuredPromptWithResultBindings(t *testing.T) {
	for _, runtimeID := range []string{"codex", "unsupported"} {
		t.Run(runtimeID, func(t *testing.T) {
			port := &workerDispatchExecution{}
			adapter := CoreWorkerAdapter{Executions: port}
			options := map[string]any{"resolve": map[string]any{"wakerId": "fixture"}, "schema": map[string]any{"type": "object", "required": []any{"verified"}, "properties": map[string]any{"verified": map[string]any{"type": "boolean"}}}}
			effect := models.WorkflowEffect{ID: "attempt", ExecutionID: "worker-execution", AuthToken: "attempt-token", Boundary: workflowdsl.EffectBoundary{NodeID: "review", Arguments: []workflowdsl.Value{workflowdsl.JSONValue("Verify the original evidence."), workflowdsl.JSONValue(options)}}}
			owner := &Service{}
			_, err := adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: workerTestRun(t, "review", runtimeID), ResultBindings: owner.AgentBindings(effect.ID, effect.AuthToken, constants.WorkflowCapabilityScopeResult)})
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
			prompt := intent.Input.Content
			for _, required := range []string{"Worker Results", "host-provided ally skill", "workflow.results.schema", "workflow.results.submit", "requestId", "When ally_capabilities MCP is injected", "workflow_results_submit", "Final chat text is not a result submission"} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("structured dispatch does not direct the worker through its result capability (%s): %s", required, prompt)
				}
			}
			if !strings.HasPrefix(prompt, "Verify the original evidence.") || strings.Contains(prompt, "Return exactly one JSON value") {
				t.Fatalf("structured worker still receives competing final-text instructions: %s", prompt)
			}
			delete(options, "schema")
			effect.Boundary.Arguments[1] = workflowdsl.JSONValue(options)
			_, err = adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: workerTestRun(t, "review", runtimeID)})
			if err != nil || port.intent.Input.Content != "Verify the original evidence." || len(port.intent.Options.ToolCapabilities) != 0 {
				t.Fatalf("plain worker result contract changed: %+v %v", port.intent, err)
			}
		})
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
	run.ScopeBinding.WorkingDirectory = "/approved/business"
	workspace := run.Context["workspace"].(map[string]any)
	if _, err := adapter.Dispatch(context.Background(), WorkerDispatch{Run: run, Effect: effect}); err != nil {
		t.Fatal(err)
	}
	if port.intent.Chat.Create.WorkingDirectory != "/approved/business" || port.intent.Input.Content != "保存本次成果" {
		t.Fatalf("workspace instructions replaced the frozen business scope or message: %+v", port.intent)
	}
	if !reflect.DeepEqual(port.intent.Chat.Create.AdditionalWorkingDirectories, []string{workspace["rootDirectory"].(string)}) {
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
}

func (c *finalMessageChat) ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error) {
	return chat.InitialExecutionState{CreateRequestID: "attempt", InitialExecution: &models.Execution{ID: "first", Status: constants.ExecutionStatusCompleted}}, nil
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
	port := &finalMessageChat{}
	adapter := CoreWorkerAdapter{Chats: port}
	receipt, err := adapter.Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.FinalPresent || receipt.FinalMessageID != "final" || receipt.Text != "最终正文" || !reflect.DeepEqual(port.executions, []string{"first"}) {
		t.Fatalf("wrong final message: %+v executions=%v", receipt, port.executions)
	}
}

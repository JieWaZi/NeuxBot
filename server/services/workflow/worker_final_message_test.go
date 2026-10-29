package workflow

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
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
			_, err := adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: models.WorkflowRun{WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{"review": {RuntimeID: runtimeID}}}, ResultBindings: owner.AgentBindings(effect.ID, effect.AuthToken, constants.WorkflowCapabilityScopeResult)})
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
			_, err = adapter.Dispatch(context.Background(), WorkerDispatch{Effect: effect, Run: models.WorkflowRun{WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{"review": {RuntimeID: runtimeID}}}})
			if err != nil || port.intent.Input.Content != "Verify the original evidence." || len(port.intent.Options.ToolCapabilities) != 0 {
				t.Fatalf("plain worker result contract changed: %+v %v", port.intent, err)
			}
		})
	}
}

// finalMessageChat 只替换适配器使用的权威查询，避免模拟其他 Chat 行为。
type finalMessageChat struct {
	// Module 保留不属于本用例的 Chat 接口。
	chat.Module
	// cursors 记录适配器实际读取的历史窗口。
	cursors []int64
}

func (c *finalMessageChat) ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error) {
	return chat.InitialExecutionState{CreateRequestID: "attempt", InitialExecution: &models.Execution{ID: "first", Status: constants.ExecutionStatusCompleted}}, nil
}
func (c *finalMessageChat) ReadMessageHistory(_ context.Context, _ string, before int64, _ int) (chat.MessageHistoryPage, error) {
	c.cursors = append(c.cursors, before)
	if before == 0 {
		return chat.MessageHistoryPage{Resources: []messageview.Message{{ID: "continued", ExecutionID: "later", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "续聊文本"}}}}, HasMore: true, NextBeforeSequence: 99}, nil
	}
	return chat.MessageHistoryPage{Resources: []messageview.Message{
		{ID: "process", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartProcessCommentary, Text: "过程"}}},
		{ID: "final", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "最终正文"}}},
	}}, nil
}
func TestCoreWorkerAdapterKeepsFinalMessageAcrossHistoryAndContinuedChat(t *testing.T) {
	port := &finalMessageChat{}
	adapter := CoreWorkerAdapter{Chats: port}
	receipt, err := adapter.Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.FinalPresent || receipt.FinalMessageID != "final" || receipt.Text != "最终正文" || !reflect.DeepEqual(port.cursors, []int64{0, 99}) {
		t.Fatalf("wrong final message: %+v cursors=%v", receipt, port.cursors)
	}
}

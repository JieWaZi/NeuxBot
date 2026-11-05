package workflows

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// domainWorkflowWorker drives actual Workflow domain results without starting a native execution.
type domainWorkflowWorker struct {
	// CoreWorkerAdapter validates admission and builds the actual WorkIntent.
	workflow.CoreWorkerAdapter
	// model supplies one test result for that exact WorkIntent.
	model workflowFixtureModel
}

func (w domainWorkflowWorker) Dispatch(ctx context.Context, input workflow.WorkerDispatch) (workflow.WorkerReceipt, error) {
	boundary := &creationInputBoundary{Service: w.Executions}
	adapter := w.CoreWorkerAdapter
	adapter.Executions = boundary
	receipt, err := adapter.Dispatch(ctx, input)
	if err != nil {
		return receipt, err
	}
	intent := boundary.intent
	request := fixtureInvocation{
		businessDirectory: intent.Chat.Create.WorkingDirectory,
		prompt:            intent.Input.Content,
		AccountID:         intent.Options.AccountID, Model: intent.Options.Model, Reasoning: intent.Options.Reasoning,
		Permission: intent.Options.PermissionLevel,
	}
	request.callResult = func(ctx context.Context, operation string, params map[string]any) (map[string]any, error) {
		for _, binding := range input.ResultBindings {
			if binding.Operation != operation {
				continue
			}
			result, err := binding.Call(ctx, capability.Request{Operation: operation, RequestID: "result", Params: params})
			data, _ := result.Data.(map[string]any)
			return data, err
		}
		return nil, status_error.AgentOperationForbidden
	}
	final, err := w.model.Run(ctx, request)
	receipt.ExecutionID = input.Effect.ExecutionID
	receipt.ChatID = "fixture-" + input.Effect.ID
	receipt.Status = constants.ExecutionStatusCompleted
	if final != nil {
		receipt.Text = strings.TrimSpace(*final)
		receipt.FinalPresent = true
		receipt.FinalMessageID = receipt.ExecutionID
	}
	if err != nil {
		receipt.Status = constants.ExecutionStatusFailed
		receipt.ErrorCode = status_error.WorkflowEffectFailed.Key()
	}
	return receipt, nil
}

func (domainWorkflowWorker) Inspect(context.Context, models.WorkflowEffect) (workflow.WorkerReceipt, error) {
	return workflow.WorkerReceipt{}, status_error.ExecutionNotFound
}

func (domainWorkflowWorker) Cancel(context.Context, models.WorkflowEffect) error { return nil }

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// repairWorkerOutput 在业务执行成功后使用独立文本通道修正格式，预算先落盘再调用。
func (s *Service) repairWorkerOutput(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect, receipt WorkerReceipt) (WorkerReceipt, error) {
	if receipt.Status != constants.ExecutionStatusCompleted || receipt.OutputFailure != nil {
		return receipt, nil
	}
	schema, _ := effect.Boundary.Arguments[1].Data.(map[string]any)["schema"].(map[string]any)
	if len(schema) == 0 {
		return receipt, nil
	}
	for {
		current, err := s.runs.GetEffect(ctx, effect.ID)
		if err != nil {
			return receipt, err
		}
		if current.Candidate != nil {
			return receipt, nil
		}
		var data any
		if json.Unmarshal([]byte(receipt.Text), &data) == nil && workflowdsl.ValidateJSON(schema, data) == nil {
			return receipt, nil
		}
		if current.OutputRepairs >= 2 {
			return receipt, status_error.WorkflowSchemaInvalid.WithField("reason", "output_repair_exhausted")
		}
		if s.repairOutput == nil {
			return receipt, status_error.WorkflowCapabilityUnsupported.WithField("capability", "format_only_output_repair")
		}
		attempt := 0
		err = s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
			if current.OutputRepairs >= 2 || state.Run.Status == constants.WorkflowRunStatusCancelling {
				return status_error.WorkflowStateConflict
			}
			current.OutputRepairs++
			attempt = current.OutputRepairs
			_, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: effect.RunID, EventID: util.New(), Type: "workflow.output_repair.started", EntityID: effect.ID, Payload: map[string]any{"attempt": attempt}})
			return err
		})
		if err != nil {
			return receipt, err
		}
		defaults := run.WorkerConfigurations[effect.Boundary.NodeID]
		repaired, repairErr := s.repairOutput(ctx, defaults, schema, receipt.Text)
		_ = s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
			payload := map[string]any{"attempt": attempt, "success": repairErr == nil}
			if repairErr != nil {
				payload["error"] = fmt.Sprint(repairErr)
			}
			_, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: effect.RunID, EventID: util.New(), Type: "workflow.output_repair.completed", EntityID: effect.ID, Payload: payload})
			return err
		})
		if repairErr != nil {
			return receipt, repairErr
		}
		if len(repaired) > constants.WorkflowOutputLimitBytes {
			return receipt, status_error.WorkflowBudgetExceeded
		}
		receipt.Text = repaired
		receipt.FinalPresent = true
	}
}

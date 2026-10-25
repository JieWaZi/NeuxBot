package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 跳过可选伙伴步骤
type SkipWorkflowWorker struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 实例标识
	InstanceID string `in:"path" name:"instanceId" validate:"required,max=32"`
	// 跳过要求
	Body SkipWorkerCommand `in:"body"`
}

// Path 返回实际 Worker 实例的跳过入口。
func (*SkipWorkflowWorker) Path() string { return "/:id/runs/:runId/instances/:instanceId/skip" }

// Output 提交用户明确的停止与跳过意图。
func (o *SkipWorkflowWorker) Output(ctx *gin.Context) (interface{}, error) {
	if err := requireOwnedRun(ctx, o.ID, o.RunID); err != nil {
		return nil, ginx.WithStack(err)
	}
	o.Body.RunID, o.Body.InstanceID = o.RunID, o.InstanceID
	if err := skipWorker(ctx, dependencies(), o.Body); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 步骤跳过请求
type SkipWorkerCommand struct {
	// 运行标识
	RunID string `json:"-"`
	// 实例标识
	InstanceID string `json:"-"`
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=128"`
	// 尝试代际
	AttemptGeneration int64 `json:"attemptGeneration" validate:"min=1"`
}

// skipWorker 先提交跳过意图，再由 Runner 等待真实 Runtime 停止后返回 null。
func skipWorker(ctx context.Context, d *workflowDependencies, command SkipWorkerCommand) error {
	if command.RequestID == "" || len(command.RequestID) > 128 {
		return status_error.BadRequest
	}
	digest := workflowstate.Digest([]any{command, command.RunID, command.InstanceID})
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		if receipt, found, err := d.Store.FindReceipt(ctx, command.InstanceID, command.RequestID); err != nil {
			return err
		} else if found {
			if receipt.Kind != "skip" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			return nil
		}
		state, err := d.State.OwnedRun(ctx, command.RunID)
		if err != nil {
			return err
		}
		if workflowstate.TerminalRun(state.Run.Status) || state.Run.Status == constants.WorkflowRunStatusCancelling {
			return status_error.WorkflowStateConflict
		}
		for index := range state.Effects {
			effect := &state.Effects[index]
			if effect.InstanceID != command.InstanceID {
				continue
			}
			allowed, _ := workflowstate.BoundaryOptions(effect.Boundary)["skippable"].(bool)
			if effect.Boundary.Primitive != "worker" || !allowed || workflowstate.StepTerminal(effect.Status) || effect.AttemptGeneration != command.AttemptGeneration {
				return status_error.WorkflowStateConflict
			}
			effect.SkipRequested = true
			if effect.Status == constants.WorkflowStepStatusPrepared && !effect.SlotReserved && !d.State.EffectActive(effect.ID) {
				value := workflowdsl.JSONValue(nil)
				effect.Status, effect.Result, effect.Failure = constants.WorkflowStepStatusSkipped, &value, nil
			} else {
				effect.Failure = workflowstate.FailureValue("SKIP_REQUESTED")
			}
			if _, err := d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: state.Run.ID, EventID: util.New(), Type: constants.WorkflowWorkerSkipRequestedEvent, EntityID: command.InstanceID, Payload: map[string]any{"instanceId": command.InstanceID, "attemptGeneration": command.AttemptGeneration, "source": "user"}}); err != nil {
				return err
			}
			if err := d.State.CommitRun(ctx, state); err != nil {
				return err
			}
			return d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: command.InstanceID, RequestID: command.RequestID, Kind: "skip", PayloadDigest: digest, Result: map[string]any{"requested": true}})
		}
		return status_error.WorkflowNotFound
	})
}

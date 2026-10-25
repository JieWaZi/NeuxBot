package workflows

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 回答操作许可
type AnswerWorkflowOperation struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 许可标识
	OperationID string `in:"path" name:"operationId" validate:"required,max=32"`
	// 请求体
	Body AnswerOperationCommand `in:"body"`
}

func (*AnswerWorkflowOperation) Path() string {
	return "/:id/runs/:runId/operations/:operationId/answer"
}
func (o *AnswerWorkflowOperation) Output(ctx *gin.Context) (interface{}, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	o.Body.RunID = o.RunID
	o.Body.OperationID = o.OperationID
	if err = answerOperation(ctx, dependencies(), o.Body); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 操作许可回答
type AnswerOperationCommand struct {
	// 运行标识
	RunID string `json:"-"`
	// 许可标识
	OperationID string `json:"-"`
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 操作摘要
	DescriptorDigest string `json:"descriptorDigest" validate:"required,len=64"`
	// 明确批准
	Allowed bool `json:"allowed"`
}

// answerOperation 以 first-answer-wins 和幂等收据提交真实操作许可。
func answerOperation(ctx context.Context, d *workflowDependencies, command AnswerOperationCommand) error {
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		digest := workflowstate.Digest([]any{command, command.RunID, command.OperationID})
		scope := "operation:" + command.OperationID
		if receipt, found, err := d.Store.FindReceipt(ctx, scope, command.RequestID); err != nil {
			return err
		} else if found {
			if receipt.PayloadDigest != digest {
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
		for index := range state.Operations {
			operation := &state.Operations[index]
			if operation.ID != command.OperationID {
				continue
			}
			if operation.Status != constants.WorkflowStepStatusWaiting || operation.DescriptorDigest != command.DescriptorDigest {
				return status_error.WorkflowStateConflict
			}
			now := time.Now().UnixMilli()
			operation.Status = constants.WorkflowStepStatusAnswered
			operation.Allowed = command.Allowed
			operation.AnsweredAt = &now
			revision, err := d.Store.GetRevision(ctx, state.Run.DefinitionID, state.Run.RevisionID)
			if err != nil {
				return err
			}
			for effectIndex := range state.Effects {
				effect := &state.Effects[effectIndex]
				if effect.ID == operation.EffectID {
					effect.DeadlineAt = min(state.Run.DeadlineAt, now+workflowstate.EffectTimeout(revision.Manifest, effect.Boundary))
					if !command.Allowed {
						effect.Status = constants.WorkflowStepStatusFailed
						effect.Failure = workflowstate.FailureValue("OPERATION_DENIED")
					}
				}
			}
			if err = d.State.CommitRun(ctx, state); err != nil {
				return err
			}
			return d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: scope, RequestID: command.RequestID, PayloadDigest: digest, Kind: "operation_answer", Result: map[string]any{"answered": true}})
		}
		return status_error.WorkflowNotFound
	})
}

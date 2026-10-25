package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重跑失败流程
type RetryWorkflowRun struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 重跑要求
	Body RetryRunCommand `in:"body"`
}

// Path 返回独立重跑的业务入口。
func (*RetryWorkflowRun) Path() string { return "/:id/runs/:runId/retry" }

// Output 保留旧运行并返回真实的新运行。
func (o *RetryWorkflowRun) Output(ctx *gin.Context) (interface{}, error) {
	if err := requireOwnedRun(ctx, o.ID, o.RunID); err != nil {
		return nil, ginx.WithStack(err)
	}
	o.Body.RunID = o.RunID
	run, err := retryRun(ctx, dependencies(), o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return run, nil
}

// 流程重跑请求
type RetryRunCommand struct {
	// 来源运行
	RunID string `json:"-"`
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=128"`
	// 运行版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=1"`
}

// retryRun 只允许已确定结果的失败重跑，结果不确定时须通过优化分析并调整流程。
func retryRun(ctx context.Context, d *workflowDependencies, command RetryRunCommand) (models.WorkflowRun, error) {
	run, err := d.Runs.GetRun(ctx, command.RunID)
	if err != nil {
		return run, err
	}
	if command.RequestID == "" || len(command.RequestID) > 128 {
		return run, status_error.BadRequest
	}
	if run.Version != command.ExpectedVersion || run.Status != constants.WorkflowRunStatusFailed && run.Status != constants.WorkflowRunStatusCancelled {
		return models.WorkflowRun{}, status_error.WorkflowStateConflict
	}
	state, err := d.Runs.LoadRunState(ctx, run.ID)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	for _, effect := range state.Effects {
		if effect.Status == constants.WorkflowStepStatusUnknown {
			return models.WorkflowRun{}, status_error.WorkflowStateConflict.WithField("reason", "optimize_uncertain_run")
		}
	}
	requestID := requestidempotency.Identity{ID: command.RequestID}.ResourceID("workflow-retry", run.ID)
	return d.RetryStart(ctx, run, requestID)
}

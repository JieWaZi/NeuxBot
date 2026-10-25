package workflows

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除执行记录
type DeleteWorkflowRun struct {
	// MethodDelete 声明历史删除使用 HTTP DELETE。
	ginx.MethodDelete
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
}

// Path 返回执行历史删除的管理路径。
func (*DeleteWorkflowRun) Path() string { return "/:id/runs/:runId" }

// Output 移除已结束执行的历史入口。
func (o *DeleteWorkflowRun) Output(ctx *gin.Context) (interface{}, error) {
	if err := deleteRunHistory(ctx, dependencies(), o.ID, o.RunID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// deleteRunHistory 与运行命令共享事务和锁，不影响幂等回执、子运行与已登记交付。
func deleteRunHistory(ctx context.Context, d *workflowDependencies, definitionID, runID string) error {
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		run, err := d.Runs.GetRun(ctx, runID)
		if err != nil {
			return err
		}
		if run.DefinitionID != definitionID {
			return status_error.WorkflowNotFound
		}
		if !workflowstate.TerminalRun(run.Status) {
			return status_error.WorkflowStateConflict.WithField("reason", "run_not_terminal")
		}
		if run.HistoryDeletedAt != nil {
			return nil
		}
		if err := d.Runs.DeleteRunHistory(ctx, definitionID, runID, time.Now().UnixMilli()); err != nil {
			return err
		}
		_, err = d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definitionID, EventID: util.New(), Type: constants.WorkflowRunHistoryDeletedEvent, EntityID: runID, Payload: map[string]any{"runId": runID}})
		return err
	})
}

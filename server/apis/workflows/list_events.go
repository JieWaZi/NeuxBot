package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 事件历史
type ListWorkflowEvents struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"query" name:"runId" validate:"max=32"`
	// 恢复游标
	After int64 `in:"query" name:"after" validate:"min=0"`
	// 仅查询日志
	LogsOnly bool `in:"query" name:"logsOnly"`
}

// 事件列表
type WorkflowEventListResponse struct {
	// 持久事件
	Resources []models.WorkflowEvent `json:"resources"`
}

func (*ListWorkflowEvents) Path() string { return "/:id/events/history" }
func (o *ListWorkflowEvents) Output(ctx *gin.Context) (interface{}, error) {
	stream, err := workflowEventStream(ctx, o.ID, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	list := global.WorkflowControllerManager.ListEvents
	if o.LogsOnly {
		list = global.WorkflowControllerManager.ListLogEvents
	}
	events, err := list(ctx, stream, o.After, constants.WorkflowEventHistoryPageSize)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return WorkflowEventListResponse{Resources: events}, nil
}
func workflowEventStream(ctx *gin.Context, id, runID string) (string, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ctx, id); err != nil {
		return "", err
	}
	if runID == "" {
		return id, nil
	}
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, runID)
	if err != nil {
		return "", err
	}
	if run.DefinitionID != id {
		return "", status_error.WorkflowNotFound
	}
	return runID, nil
}

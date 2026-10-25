package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出运行
type ListWorkflowRuns struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 时间游标
	Before int64 `in:"query" name:"before" validate:"min=0"`
	// 身份游标
	BeforeID string `in:"query" name:"beforeId" validate:"max=32"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 运行列表
type WorkflowRunListResponse struct {
	// 运行列表
	Items []models.WorkflowRun `json:"items"`
	// 时间游标
	NextBefore int64 `json:"nextBefore"`
	// 身份游标
	NextBeforeID string `json:"nextBeforeId"`
}

func (*ListWorkflowRuns) Path() string { return "/:id/runs" }
func (o *ListWorkflowRuns) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	items, err := global.WorkflowRunControllerManager.ListRuns(ctx, o.ID, o.Before, o.BeforeID, o.Limit)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result := WorkflowRunListResponse{Items: items}
	limit := o.Limit
	if limit == 0 {
		limit = 50
	}
	if len(items) == limit {
		last := items[len(items)-1]
		result.NextBefore = last.CreatedAt
		result.NextBeforeID = last.ID
	}
	return result, nil
}

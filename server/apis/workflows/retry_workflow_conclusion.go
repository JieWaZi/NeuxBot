package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 补交整图结论
type RetryWorkflowConclusion struct {
	// 请求方法
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
}

// Path 返回原运行的结论补交入口。
func (*RetryWorkflowConclusion) Path() string { return "/:id/runs/:runId/conclusion/retry" }

// Output 通过原运行与原收件地址补交，在详情中更新实际收据。
func (o *RetryWorkflowConclusion) Output(ctx *gin.Context) (interface{}, error) {
	if err := requireOwnedRun(ctx, o.ID, o.RunID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.WorkflowService.RetryConclusion(ctx, o.RunID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

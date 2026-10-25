package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 启动工作流
type StartWorkflowRun struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body workflow.StartRunCommand `in:"body"`
}

func (*StartWorkflowRun) Path() string { return "/:id/runs" }
func (o *StartWorkflowRun) Output(ctx *gin.Context) (interface{}, error) {
	o.Body.DefinitionID = o.ID
	run, err := global.WorkflowService.StartRun(ctx, o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 前端需要接纳后的真实运行身份打开独立运行 Scene。
	return run, nil
}

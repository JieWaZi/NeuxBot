package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 工作流触发配置
type GetWorkflowTriggers struct {
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
}

// Path 返回工作流触发配置查询路径。
func (*GetWorkflowTriggers) Path() string { return "/:id/triggers" }

// Output 读取定义归属及整份自动运行配置。
func (o *GetWorkflowTriggers) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	values, _, err := global.AutomaticTaskControllerManager.List(ctx, controllers.AutomaticTaskFilter{WorkflowID: o.ID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return WorkflowTriggersView{Tasks: values, Fingerprint: triggersFingerprint(values)}, nil
}

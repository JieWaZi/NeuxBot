package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 保存工作流源码
type SaveWorkflowSource struct {
	// 通过嵌入契约声明该接口使用 HTTP PUT 方法。
	ginx.MethodPut
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body workflow.SaveSourceCommand `in:"body"`
}

func (*SaveWorkflowSource) Path() string { return "/:id/source" }
func (o *SaveWorkflowSource) Output(ctx *gin.Context) (interface{}, error) {
	o.Body.DefinitionID = o.ID
	receipt, diagnostics, err := global.WorkflowService.SaveSource(ctx, o.Body)
	if err != nil {
		if len(diagnostics) > 0 {
			return nil, ginx.WithStack(status_error.WorkflowSourceInvalid.WithParams(map[string]interface{}{"diagnostics": diagnostics}))
		}
		return nil, ginx.WithStack(err)
	} /* 创作与前端用此回执确认生效版本。 */
	return receipt, nil
}

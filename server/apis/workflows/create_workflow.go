package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建工作流
type CreateWorkflow struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 请求体
	Body CreateDefinitionCommand `in:"body"`
}

func (*CreateWorkflow) Path() string { return "" }
func (o *CreateWorkflow) Output(ctx *gin.Context) (interface{}, error) {
	definition, err := createDefinition(ctx, dependencies(), o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	} /* 创建后前端需要稳定身份进入空 Scene。 */
	return definition, nil
}

// 创建工作流请求
type CreateDefinitionCommand struct {
	// 请求标识
	RequestID string `json:"requestId"`
	// 草稿标题
	Title string `json:"-"`
	// 创作伙伴
	CreationWakerID string `json:"-"`
	// 执行范围
	ScopeBinding models.WorkflowScopeBinding `json:"-"`
}

func createDefinition(ctx context.Context, d *workflowDependencies, command CreateDefinitionCommand) (models.WorkflowDefinition, error) {
	return d.CreateDefinition(ctx, workflow.CreateDefinitionCommand{
		RequestID: command.RequestID, Title: command.Title,
		CreationWakerID: command.CreationWakerID, ScopeBinding: command.ScopeBinding,
	})
}

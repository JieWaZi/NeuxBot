package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取工作流
type GetWorkflow struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 版本标识
	RevisionID string `in:"query" name:"revisionId" validate:"max=32"`
}

// 工作流详情
type WorkflowDetailResponse struct {
	// 工作流定义
	Definition models.WorkflowDefinition `json:"definition"`
	// 创作会话
	CreationSessions []models.WorkflowCreationSession `json:"creationSessions"`
	// 修改批注
	Annotations []models.WorkflowAnnotation `json:"annotations"`
	// 选中版本
	Revision *models.WorkflowRevision `json:"revision"`
}

func (*GetWorkflow) Path() string { return "/:id" }
func (o *GetWorkflow) Output(ctx *gin.Context) (interface{}, error) {
	definition, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	creationSessions, err := global.WorkflowCreationControllerManager.ListCreationSessions(ctx, o.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	annotations, err := global.WorkflowCreationControllerManager.ListAnnotations(ctx, o.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := WorkflowDetailResponse{Definition: definition, CreationSessions: creationSessions, Annotations: annotations}
	id := o.RevisionID
	if id == "" {
		id = definition.CurrentRevisionID
	}
	if id != "" {
		revision, err := global.WorkflowControllerManager.GetRevision(ctx, o.ID, id)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		response.Revision = &revision
	}
	return response, nil
}

package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
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
	Revision *WorkflowRevisionResponse `json:"revision"`
}

// 工作流版本详情
type WorkflowRevisionResponse struct {
	// 版本标识
	ID string `json:"id"`
	// 工作流标识
	DefinitionID string `json:"definitionId"`
	// 版本序号
	Ordinal int64 `json:"ordinal"`
	// 源码摘要
	SourceDigest string `json:"sourceDigest"`
	// 画布摘要
	ManifestDigest string `json:"manifestDigest"`
	// 契约版本
	SDKVersion string `json:"sdkVersion"`
	// 编译器版本
	CompilerVersion string `json:"compilerVersion"`
	// 创作会话
	CreationSessionID string `json:"creationSessionId"`
	// 恢复来源
	RestoredFromRevisionID string `json:"restoredFromRevisionId"`
	// 保存时间
	CreatedAt int64 `json:"createdAt"`
	// 画布投影
	Manifest workflowdsl.CanvasManifest `json:"manifest"`
}

// publicRevision 仅输出源码派生的封闭画布，不泄露 Runner 内部结构和脚本字节。
func publicRevision(revision models.WorkflowRevision) *WorkflowRevisionResponse {
	return &WorkflowRevisionResponse{ID: revision.ID, DefinitionID: revision.DefinitionID, Ordinal: revision.Ordinal, SourceDigest: revision.SourceDigest, ManifestDigest: workflowdsl.CanvasDigest(workflowdsl.ExportCanvas(revision.Manifest)), SDKVersion: revision.SDKVersion, CompilerVersion: revision.CompilerVersion, CreationSessionID: revision.CreationSessionID, RestoredFromRevisionID: revision.RestoredFromRevisionID, CreatedAt: revision.CreatedAt, Manifest: workflowdsl.ExportCanvas(revision.Manifest)}
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
		response.Revision = publicRevision(revision)
	}
	return response, nil
}

package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取工作流源码
type GetWorkflowSource struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 版本标识
	RevisionID string `in:"query" name:"revisionId" validate:"max=32"`
}

// 工作流源码
type WorkflowSourceResponse struct {
	// 版本标识
	RevisionID string `json:"revisionId"`
	// 原始源码
	Source string `json:"source"`
	// 源码摘要
	SourceDigest string `json:"sourceDigest"`
	// 契约版本
	SDKVersion string `json:"sdkVersion"`
	// 操作实现
	ActionDetails map[string]workflowdsl.ActionSourceDetail `json:"actionDetails,omitempty"`
}

func (*GetWorkflowSource) Path() string { return "/:id/source" }
func (o *GetWorkflowSource) Output(ctx *gin.Context) (interface{}, error) {
	id := o.RevisionID
	if id == "" {
		definition, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		id = definition.CurrentRevisionID
	}
	if id == "" {
		return WorkflowSourceResponse{}, nil
	}
	revision, err := global.WorkflowControllerManager.GetRevision(ctx, o.ID, id)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	details, err := workflowdsl.InspectActionSources(revision.Source, revision.Manifest)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return WorkflowSourceResponse{RevisionID: revision.ID, Source: revision.Source, SourceDigest: revision.SourceDigest, SDKVersion: revision.SDKVersion, ActionDetails: details}, nil
}

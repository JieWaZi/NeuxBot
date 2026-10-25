package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出工作流版本
type ListWorkflowRevisions struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 版本游标
	Before int64 `in:"query" name:"before" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 工作流版本摘要
type WorkflowRevisionSummary struct {
	// 版本标识
	ID string `json:"id"`
	// 版本序号
	Ordinal int64 `json:"ordinal"`
	// 源码摘要
	SourceDigest string `json:"sourceDigest"`
	// 创作会话
	CreationSessionID string `json:"creationSessionId"`
	// 恢复来源
	RestoredFromRevisionID string `json:"restoredFromRevisionId"`
	// 保存时间
	CreatedAt int64 `json:"createdAt"`
}

// 工作流版本列表
type WorkflowRevisionListResponse struct {
	// 版本列表
	Items []WorkflowRevisionSummary `json:"items"`
	// 版本游标
	NextBefore int64 `json:"nextBefore"`
}

func (*ListWorkflowRevisions) Path() string { return "/:id/revisions" }
func (o *ListWorkflowRevisions) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	values, err := global.WorkflowControllerManager.ListRevisions(ctx, o.ID, o.Before, o.Limit)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := WorkflowRevisionListResponse{Items: []WorkflowRevisionSummary{}}
	for _, value := range values {
		response.Items = append(response.Items, WorkflowRevisionSummary{ID: value.ID, Ordinal: value.Ordinal, SourceDigest: value.SourceDigest, CreationSessionID: value.CreationSessionID, RestoredFromRevisionID: value.RestoredFromRevisionID, CreatedAt: value.CreatedAt})
	}
	limit := o.Limit
	if limit == 0 {
		limit = 50
	}
	if len(values) == limit {
		response.NextBefore = values[len(values)-1].Ordinal
	}
	return response, nil
}

package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出工作流
type ListWorkflows struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 搜索文字
	Search string `in:"query" name:"search" validate:"max=200"`
	// 分类名称
	Category string `in:"query" name:"category" validate:"max=120"`
	// 仅未分类
	Uncategorized bool `in:"query" name:"uncategorized"`
	// 时间游标
	Before int64 `in:"query" name:"before" validate:"min=0"`
	// 身份游标
	BeforeID string `in:"query" name:"beforeId" validate:"max=32"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 工作流列表
type WorkflowListResponse struct {
	// 工作流列表
	Items []models.WorkflowDefinition `json:"items"`
	// 时间游标
	NextBefore int64 `json:"nextBefore"`
	// 身份游标
	NextBeforeID string `json:"nextBeforeId"`
}

func (*ListWorkflows) Path() string { return "" }
func (o *ListWorkflows) Output(ctx *gin.Context) (interface{}, error) {
	var category *string
	if o.Uncategorized {
		empty := ""
		category = &empty
	} else if o.Category != "" {
		category = &o.Category
	}
	items, err := global.WorkflowControllerManager.ListDefinitions(ctx, o.Search, o.Before, o.BeforeID, o.Limit, category)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := WorkflowListResponse{Items: items}
	limit := o.Limit
	if limit == 0 {
		limit = 50
	}
	if len(items) == limit {
		last := items[len(items)-1]
		response.NextBefore = last.UpdatedAt
		response.NextBeforeID = last.ID
	}
	return response, nil
}

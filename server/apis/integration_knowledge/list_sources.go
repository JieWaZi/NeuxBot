package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 资料列表响应
type SourceListResponse struct {
	// 成功状态
	Success bool `json:"success"`
	// 资料列表
	Data []PublishedSource `json:"data"`
	// 实际总数
	Total int64 `json:"total"`
	// 当前页码
	Page int `json:"page"`
	// 每页数量
	PageSize int `json:"page_size"`
}

// 列出集成资料
type PublishedListSources struct {
	ginx.MethodGet
	// 知识库标识
	ID string `in:"path" name:"id" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0,max=10000000"`
	// 返回数量
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// 当前页码
	Page int `in:"query" name:"page"`
	// 每页数量
	PageSize int `in:"query" name:"page_size"`
	// 检索内容
	Keyword string `in:"query" name:"keyword"`
	// 原件状态
	State string `in:"query" name:"state"`
	// 解析状态
	ParseStatus string `in:"query" name:"parse_status"`
	// 排序方向
	SortOrder string `in:"query" name:"sort_order"`
}

// Path 定位资料列表路径。
func (*PublishedListSources) Path() string { return "/knowledge-bases/:id/knowledge" }

// Output 返回经过过滤的资料列表及实际计数。
func (o *PublishedListSources) Output(ctx *gin.Context) (interface{}, error) {
	page, size, err := publishedPagination(o.Page, o.PageSize)
	if err != nil {
		return nil, err
	}
	offset := (page - 1) * size
	if ctx.Request.URL.Query().Has("offset") {
		offset = o.Offset
		if o.Limit > 0 {
			size = o.Limit
		}
		page = offset/size + 1
	}
	state := o.State
	if state == "" {
		state = o.ParseStatus
	}
	if state == "completed" {
		state = "processed"
	}
	sort := "newest"
	if o.SortOrder == "asc" {
		sort = "oldest"
	}
	rows, total, err := dependencies(ctx).engine.ListSourcesQuery(ctx.Request.Context(), o.ID, core.SourceQuery{Query: o.Keyword, State: state, Sort: sort, Offset: offset, Limit: size})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	data := []PublishedSource{}
	for _, row := range rows {
		data = append(data, sourceDTO(row))
	}
	return SourceListResponse{Success: true, Data: data, Total: total, Page: page, PageSize: size}, nil
}

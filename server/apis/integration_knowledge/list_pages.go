package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki 页面列表
type PageListResponse struct {
	// 页面列表
	Pages []*core.Page `json:"pages"`
	// 实际总数
	Total int64 `json:"total"`
	// 当前页码
	Page int `json:"page"`
	// 每页数量
	PageSize int `json:"page_size"`
	// 总页数
	TotalPages int64 `json:"total_pages"`
}

// 列出集成 Wiki
type PublishedListPages struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0,max=10000000"`
	// 返回数量
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// 当前页码
	Page int `in:"query" name:"page"`
	// 每页数量
	PageSize int `in:"query" name:"page_size"`
	// 检索内容
	Query string `in:"query" name:"query"`
	// 页面状态
	Status string `in:"query" name:"status"`
	// 页面类型
	PageType string `in:"query" name:"page_type"`
	// 主题标识
	FolderID *string `in:"query" name:"folder_id"`
}

// Path 定位 Wiki 单数知识库路径。
func (*PublishedListPages) Path() string { return "/knowledgebase/:kb_id/wiki/pages" }

// Output 保留 page/page_size 包装，不将 offset 当作 page。
func (o *PublishedListPages) Output(ctx *gin.Context) (interface{}, error) {
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
	rows, total, err := dependencies(ctx).engine.ListPages(ctx.Request.Context(), core.PageQuery{LibraryID: o.LibraryID, Query: o.Query, PageType: o.PageType, Status: o.Status, ThemeID: o.FolderID, Offset: offset, Limit: size})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if rows == nil {
		rows = []*core.Page{}
	}
	return PageListResponse{Pages: rows, Total: total, Page: page, PageSize: size, TotalPages: (total + int64(size) - 1) / int64(size)}, nil
}

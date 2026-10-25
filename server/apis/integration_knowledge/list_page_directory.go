package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 轻量页面目录响应
type PageDirectoryResponse struct {
	// 目录条目
	Items []*core.PageDirectoryEntry `json:"items"`
	// 实际总数
	Total int64 `json:"total"`
	// 起始位置
	Offset int `json:"offset"`
	// 每批条数
	Limit int `json:"limit"`
}

// 列出集成轻量页面目录
type PublishedListPageDirectory struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每批条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=1000"`
}

func (*PublishedListPageDirectory) Path() string { return "/knowledgebase/:kb_id/wiki/page-directory" }
func (o *PublishedListPageDirectory) Output(ctx *gin.Context) (interface{}, error) {
	limit := o.Limit
	if limit == 0 {
		limit = core.PageDirectoryMaxLimit
	}
	rows, total, err := dependencies(ctx).engine.ListPageDirectory(ctx.Request.Context(), o.LibraryID, o.Offset, limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return PageDirectoryResponse{Items: rows, Total: total, Offset: o.Offset, Limit: limit}, nil
}

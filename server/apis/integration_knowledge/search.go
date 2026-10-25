package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki 搜索响应
type SearchResponse struct {
	// 匹配页面
	Pages []*core.Page `json:"pages"`
}

// 检索集成 Wiki
type PublishedSearch struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 检索内容
	Query string `in:"query" name:"q" validate:"required,max=2000"`
	// 返回数量
	Limit int `in:"query" name:"limit"`
}

// Path 定位 Wiki 只读搜索路径。
func (*PublishedSearch) Path() string { return "/knowledgebase/:kb_id/wiki/search" }

// Output 使用当前核心检索，不额外建立搜索索引或调用模型。
func (o *PublishedSearch) Output(ctx *gin.Context) (interface{}, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Query == "" || o.Limit < 1 || o.Limit > 100 {
		return nil, status_error.KnowledgeInvalid
	}
	pages, _, err := dependencies(ctx).engine.ListPages(ctx.Request.Context(), core.PageQuery{LibraryID: o.LibraryID, Query: o.Query, Limit: o.Limit})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if pages == nil {
		pages = []*core.Page{}
	}
	return SearchResponse{Pages: pages}, nil
}

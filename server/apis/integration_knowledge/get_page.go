package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取集成 Wiki
type PublishedGetPage struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 分层句柄
	Slug string `in:"path" name:"slug" validate:"required"`
}

// Path 保留分层 slug 的实际通配路径。
func (*PublishedGetPage) Path() string { return "/knowledgebase/:kb_id/wiki/pages/*slug" }

// Output 返回页面对象。
func (o *PublishedGetPage) Output(ctx *gin.Context) (interface{}, error) {
	slug, err := pathSlug(o.Slug)
	if err != nil {
		return nil, err
	}
	page, err := dependencies(ctx).engine.GetPageBySlug(ctx.Request.Context(), o.LibraryID, slug)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return page, nil
}

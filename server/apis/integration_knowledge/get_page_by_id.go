package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 按标识读取集成 Wiki
type PublishedGetPageByID struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
}

// Path 通过稳定页面标识定位页面，句柄读取另有独立入口。
func (*PublishedGetPageByID) Path() string { return "/knowledgebase/:kb_id/wiki/page-by-id/:pageId" }

// Output 仍以路径中的实际知识库限定页面归属。
func (o *PublishedGetPageByID) Output(ctx *gin.Context) (interface{}, error) {
	page, err := dependencies(ctx).engine.GetPage(ctx.Request.Context(), o.LibraryID, o.PageID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return page, nil
}

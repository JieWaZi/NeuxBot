package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 按地址读取知识页面
type GetPageBySlug struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 语义地址
	Slug string `in:"query" name:"slug" validate:"required"`
}

func (o *GetPageBySlug) Path() string { return "/libraries/:libraryId/page-by-slug" }
func (o *GetPageBySlug) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	page, err := global.KnowledgeEngine.GetPageBySlug(ctx, o.LibraryID, o.Slug)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return pageDTO(page), nil
}

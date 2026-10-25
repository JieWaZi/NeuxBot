package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识页面
type GetPage struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
}

func (o *GetPage) Path() string { return "/libraries/:libraryId/pages/:pageId" }
func (o *GetPage) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.GetPage(ctx, o.LibraryID, o.PageID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return pageDTO(v), nil
}

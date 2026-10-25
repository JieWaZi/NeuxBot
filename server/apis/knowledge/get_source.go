package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取原件版本
type GetSource struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源版本
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

func (o *GetSource) Path() string { return "/libraries/:libraryId/sources/:sourceId" }
func (o *GetSource) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	s, err := global.KnowledgeEngine.GetSource(ctx, o.LibraryID, o.SourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items, err := sourceActivityDTOs(ctx, o.LibraryID, []*core.SourceVersion{s})
	if err != nil {
		return nil, err
	}
	return items[0], nil
}

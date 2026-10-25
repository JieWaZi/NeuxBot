package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// PreviewSourcePurge 读取真实来源清理事实，不修改原件。
type PreviewSourcePurge struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源身份
	SourceID string `in:"query" name:"sourceId" validate:"required"`
}

func (o *PreviewSourcePurge) Path() string { return "/libraries/:libraryId/source-purges/preview" }
func (o *PreviewSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.PreviewSourcePurge(ctx, core.PurgePreviewInput{LibraryID: o.LibraryID, SourceID: o.SourceID})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}

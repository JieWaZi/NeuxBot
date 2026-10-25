package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetSourcePurge 读取真实来源清理事实，不修改原件。
type GetSourcePurge struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 清理标识
	PurgeID string `in:"path" name:"purgeId" validate:"required"`
}

func (o *GetSourcePurge) Path() string { return "/libraries/:libraryId/source-purges/:purgeId" }
func (o *GetSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.GetSourcePurge(ctx, o.LibraryID, o.PurgeID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}

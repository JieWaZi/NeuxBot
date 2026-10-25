package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识库执行偏好
type GetLibraryExecutionDefaults struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *GetLibraryExecutionDefaults) Path() string {
	return "/libraries/:libraryId/execution-defaults"
}
func (o *GetLibraryExecutionDefaults) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	if _, err := global.KnowledgeEngine.GetLibrary(ctx, o.LibraryID); err != nil {
		return nil, bridge.MapError(err)
	}
	return global.KnowledgeControllerManager.LibraryExecutionDefaults(ctx, o.LibraryID)
}

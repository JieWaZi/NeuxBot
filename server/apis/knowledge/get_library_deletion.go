package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识库删除状态
type GetLibraryDeletion struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

// Path 返回知识库删除操作的路由。
func (o *GetLibraryDeletion) Path() string { return "/libraries/:libraryId/deletion" }

// Output 读取持久化清理状态，不把逻辑删除回执视为磁盘清理完成。
func (o *GetLibraryDeletion) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.GetLibraryDeletion(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}

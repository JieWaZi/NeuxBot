package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端知识库删除状态
type GetRemoteLibraryDeletion struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

// Path 返回知识库删除操作的路由。
func (o *GetRemoteLibraryDeletion) Path() string {
	return "/:connectionId/libraries/:libraryId/deletion"
}

// Output 读取持久化清理状态，不把逻辑删除回执视为磁盘清理完成。
func (o *GetRemoteLibraryDeletion) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := c.custom.GetLibraryDeletion(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return v, nil
}

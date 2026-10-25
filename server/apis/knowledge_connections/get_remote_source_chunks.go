package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端文档分块
type GetRemoteSourceChunks struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源版本
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

func (o *GetRemoteSourceChunks) Path() string {
	return "/:connectionId/libraries/:libraryId/sources/:sourceId/chunks"
}

// Output 仅返回远端已保存的来源分块，未解析来源保留真实空数组。
func (o *GetRemoteSourceChunks) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	rows, err := c.custom.ListSourceChunks(ctx, o.SourceID)
	if err != nil {
		return nil, remoteError(err)
	}
	return rows, nil
}

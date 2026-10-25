package knowledge_connections

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端原件
type GetRemoteSource struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 原件标识
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

func (o *GetRemoteSource) Path() string {
	return "/:connectionId/libraries/:libraryId/sources/:sourceId"
}
func (o *GetRemoteSource) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom != nil {
		v, e := c.custom.GetSource(ctx, o.SourceID)
		if e != nil {
			return nil, remoteError(e)
		}
		return customSource(v), nil
	}
	v, e := c.official.GetSource(ctx, o.LibraryID, o.SourceID)
	if e != nil {
		return nil, remoteError(e)
	}
	return officialSource(*v), nil
}

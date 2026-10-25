package knowledge_connections

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端知识库
type GetRemoteLibrary struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *GetRemoteLibrary) Path() string { return "/:connectionId/libraries/:libraryId" }
func (o *GetRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom != nil {
		v, e := c.custom.GetLibrary(ctx)
		if e != nil {
			return nil, remoteError(e)
		}
		return libraryWithPageTotal(ctx, c, customLibrary(c, v))
	}
	v, e := c.official.GetLibrary(ctx, o.LibraryID)
	if e != nil {
		return nil, remoteError(e)
	}
	return libraryWithPageTotal(ctx, c, officialLibrary(c, *v))
}

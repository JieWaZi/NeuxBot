package knowledge_connections

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 按地址读取远端页面
type GetRemotePageBySlug struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 语义地址
	Slug string `in:"query" name:"slug"`
}

func (o *GetRemotePageBySlug) Path() string {
	return "/:connectionId/libraries/:libraryId/page-by-slug"
}
func (o *GetRemotePageBySlug) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom != nil {
		v, e := c.custom.GetPageBySlug(ctx, o.Slug)
		if e != nil {
			return nil, remoteError(e)
		}
		return customPage(v), nil
	}
	v, e := c.official.GetPage(ctx, o.LibraryID, o.Slug)
	if e != nil {
		return nil, remoteError(e)
	}
	return officialPage(*v), nil
}

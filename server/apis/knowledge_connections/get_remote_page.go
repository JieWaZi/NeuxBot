package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取远端页面
type GetRemotePage struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 语义地址
	Slug string `in:"query" name:"slug"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
}

func (o *GetRemotePage) Path() string { return "/:connectionId/libraries/:libraryId/pages/:pageId" }
func (o *GetRemotePage) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom != nil {
		v, e := c.custom.GetPage(ctx, o.PageID)
		if e != nil {
			return nil, remoteError(e)
		}
		return customPage(v), nil
	}
	if o.Slug == "" {
		return nil, status_error.KnowledgeInvalid
	}
	v, e := c.official.GetPage(ctx, o.LibraryID, o.Slug)
	if e != nil {
		return nil, remoteError(e)
	}
	if v.ID != o.PageID {
		return nil, status_error.KnowledgeNotFound
	}
	return officialPage(*v), nil
}

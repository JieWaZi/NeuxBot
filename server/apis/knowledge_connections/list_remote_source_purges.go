package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListRemoteSourcePurges 读取远端权威清理清单，不创建本地清理任务。
type ListRemoteSourcePurges struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListRemoteSourcePurges) Path() string {
	return "/:connectionId/libraries/:libraryId/source-purges"
}
func (o *ListRemoteSourcePurges) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	limit := o.Limit
	if limit == 0 {
		limit = 50
	}
	v, err := c.custom.ListSourcePurges(ctx, o.Offset, limit)
	if err != nil {
		return nil, remoteError(err)
	}
	return core.SourcePurgeList{Items: v.Items, Total: v.Total}, nil
}
func init() { Router.Register(&ListRemoteSourcePurges{}) }

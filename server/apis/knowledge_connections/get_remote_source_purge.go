package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetRemoteSourcePurge 使用完整远端身份代理原件清理；不在本机执行删除。
type GetRemoteSourcePurge struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 清理标识
	PurgeID string `in:"path" name:"purgeId" validate:"required"`
}

func (o *GetRemoteSourcePurge) Path() string {
	return "/:connectionId/libraries/:libraryId/source-purges/:purgeId"
}
func (o *GetRemoteSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := c.custom.GetSourcePurge(ctx, o.PurgeID)
	if err != nil {
		return nil, remoteError(err)
	}
	return v, nil
}
func init() { Router.Register(&GetRemoteSourcePurge{}) }

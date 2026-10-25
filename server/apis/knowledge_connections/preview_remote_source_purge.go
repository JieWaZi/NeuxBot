package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// PreviewRemoteSourcePurge 使用完整远端身份代理原件清理；不在本机执行删除。
type PreviewRemoteSourcePurge struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源身份
	SourceID string `in:"query" name:"sourceId" validate:"required"`
}

func (o *PreviewRemoteSourcePurge) Path() string {
	return "/:connectionId/libraries/:libraryId/source-purges/preview"
}
func (o *PreviewRemoteSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := c.custom.PreviewSourcePurge(ctx, core.PurgePreviewInput{LibraryID: o.LibraryID, SourceID: o.SourceID})
	if err != nil {
		return nil, remoteError(err)
	}
	return v, nil
}
func init() { Router.Register(&PreviewRemoteSourcePurge{}) }

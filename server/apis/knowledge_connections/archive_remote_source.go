package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 归档远端原件
type ArchiveRemoteSource struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 原件标识
	SourceID string `in:"path" name:"sourceId" validate:"required"`
	// 请求体
	Body knowledge.ArchiveSourceInput `in:"body"`
}

func (o *ArchiveRemoteSource) Path() string {
	return "/:connectionId/libraries/:libraryId/sources/:sourceId/archive"
}
func (o *ArchiveRemoteSource) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "source.archive", o.Body, func() (*core.Receipt, error) {
		return c.custom.ArchiveSource(ctx, core.ArchiveSourceInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceVersionID: o.SourceID, ExpectedVersion: o.Body.ExpectedVersion, Archived: o.Body.Archived})
	})
}

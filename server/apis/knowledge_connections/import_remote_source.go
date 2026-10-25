package knowledge_connections

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 保存远端原件
type ImportRemoteSource struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body knowledge.ImportSourceInput `in:"body"`
}

func (o *ImportRemoteSource) Path() string { return "/:connectionId/libraries/:libraryId/sources" }
func (o *ImportRemoteSource) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	raw, e := base64.StdEncoding.DecodeString(o.Body.Bytes)
	if e != nil || len(raw) > 32<<20 {
		return nil, status_error.KnowledgeInvalid
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "source.import", o.Body, func() (*core.Receipt, error) {
		return c.custom.ImportSource(ctx, core.ImportSourceInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceID: o.Body.SourceID, ExpectedVersion: o.Body.ExpectedVersion, Filename: o.Body.Filename, MIME: o.Body.MIME, Bytes: raw})
	})
}

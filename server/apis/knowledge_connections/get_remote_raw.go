package knowledge_connections

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 下载远端原件
type GetRemoteRaw struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 原件标识
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

func (o *GetRemoteRaw) Path() string {
	return "/:connectionId/libraries/:libraryId/sources/:sourceId/raw"
}
func (o *GetRemoteRaw) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	var v *remote.Download
	var e error
	if c.custom != nil {
		v, e = c.custom.DownloadSource(ctx, o.SourceID)
	} else {
		v, e = c.official.DownloadSourceInLibrary(ctx, o.LibraryID, o.SourceID)
	}
	if e != nil {
		return nil, remoteError(e)
	}
	if len(v.Bytes) > 32<<20 {
		return nil, status_error.KnowledgeRemoteIncompatible
	}
	return knowledge.RawSource{Filename: v.Filename, MIME: v.MIME, Bytes: base64.StdEncoding.EncodeToString(v.Bytes)}, nil
}

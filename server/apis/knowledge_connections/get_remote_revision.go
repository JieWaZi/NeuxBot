package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 获取远端历史正文
type GetRemoteRevision struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 语义地址
	Slug string `in:"query" name:"slug"`
	// 历史版本
	Version int `in:"path" name:"version" validate:"min=1"`
}

func (o *GetRemoteRevision) Path() string {
	return "/:connectionId/libraries/:libraryId/pages/:pageId/revisions/:version"
}
func (o *GetRemoteRevision) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom != nil {
		v, e := c.custom.GetRevision(ctx, o.PageID, o.Version)
		if e != nil {
			return nil, remoteError(e)
		}
		return customRevision(v), nil
	}
	if o.Slug == "" {
		return nil, status_error.KnowledgeInvalid
	}
	v, e := c.official.ListRevisions(ctx, o.LibraryID, o.Slug, remote.RevisionOptions{ContentVersion: o.Version})
	if e != nil {
		return nil, remoteError(e)
	}
	if len(v.Revisions) != 1 || v.Revisions[0].PageID != o.PageID {
		return nil, status_error.KnowledgeRemoteIncompatible
	}
	x := v.Revisions[0]
	at, _ := time.Parse(time.RFC3339Nano, x.EditedAt)
	return knowledge.Revision{ID: x.ID, PageID: x.PageID, Version: x.Version, Title: x.Title, Summary: x.Summary, Content: x.Content, EditSource: x.EditSource, EditedAt: at.UnixMilli()}, nil
}

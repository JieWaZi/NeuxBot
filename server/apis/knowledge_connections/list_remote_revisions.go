package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 获取远端历史列表
type ListRemoteRevisions struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 语义地址
	Slug string `in:"query" name:"slug"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListRemoteRevisions) Path() string {
	return "/:connectionId/libraries/:libraryId/pages/:pageId/revisions"
}
func (o *ListRemoteRevisions) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	out := RemoteRevisionList{Items: []RemoteRevision{}}
	if c.custom != nil {
		v, e := c.custom.ListRevisions(ctx, o.PageID, offset, limit)
		if e != nil {
			return nil, remoteError(e)
		}
		out.Total = v.Total
		for _, x := range v.Items {
			out.Items = append(out.Items, RemoteRevision{Revision: customRevision(x), ContentLoaded: true})
		}
		return out, nil
	}
	if o.Slug == "" {
		return nil, status_error.KnowledgeInvalid
	}
	v, e := c.official.ListRevisions(ctx, o.LibraryID, o.Slug, remote.RevisionOptions{Offset: offset, Limit: limit})
	if e != nil {
		return nil, remoteError(e)
	}
	out.Total = v.Total
	out.CurrentVersion = v.CurrentVersion
	for _, x := range v.Revisions {
		if x.PageID != o.PageID {
			return nil, status_error.KnowledgeRemoteIncompatible
		}
		at, _ := time.Parse(time.RFC3339Nano, x.EditedAt)
		out.Items = append(out.Items, RemoteRevision{Revision: knowledge.Revision{ID: x.ID, PageID: x.PageID, Version: x.Version, Title: x.Title, Summary: x.Summary, EditSource: x.EditSource, EditedAt: at.UnixMilli()}, ContentLoaded: false})
	}
	return out, nil
}

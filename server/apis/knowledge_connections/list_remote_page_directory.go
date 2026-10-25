package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 获取远端轻量页面目录
type ListRemotePageDirectory struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每批条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=1000"`
}

func (*ListRemotePageDirectory) Path() string {
	return "/:connectionId/libraries/:libraryId/page-directory"
}
func (o *ListRemotePageDirectory) Output(ctx *gin.Context) (result interface{}, err error) {
	requestCtx := ctx.Request.Context()
	// 请求取消先于网络或存储错误归类，不能显示成远端不可达。
	defer func() {
		if requestCtx.Err() != nil {
			result = nil
			err = status_error.KnowledgeCancelled
		}
	}()
	if requestCtx.Err() != nil {
		return nil, status_error.KnowledgeCancelled
	}
	observedAt := time.Now().UnixMilli()
	c, err := client(requestCtx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	limit := o.Limit
	if limit == 0 {
		limit = core.PageDirectoryMaxLimit
	}
	out := knowledge.PageDirectoryList{Items: []knowledge.PageDirectoryEntry{}}
	if c.custom != nil {
		result, e := c.custom.ListPageDirectory(requestCtx, o.Offset, limit)
		if e != nil {
			return nil, remoteError(e)
		}
		out.Total = result.Total
		for _, row := range result.Items {
			out.Items = append(out.Items, knowledge.PageDirectoryDTO(row))
		}
	} else {
		// 官方仅提供完整页面分页；保留协议上限，向界面返回窄目录而不传播正文。
		limit = min(limit, 100)
		if o.Offset%limit != 0 {
			return nil, status_error.KnowledgeInvalid
		}
		result, e := c.official.ListPages(requestCtx, o.LibraryID, remote.PageOptions{Page: o.Offset/limit + 1, PageSize: limit})
		if e != nil {
			return nil, remoteError(e)
		}
		out.Total = result.Total
		for _, row := range result.Pages {
			out.Items = append(out.Items, knowledge.PageDirectoryEntry{ID: row.ID, Slug: row.Slug, Title: row.Title, PageType: row.PageType, Summary: row.Summary, ThemeID: row.FolderID, CategoryPath: row.CategoryPath, SortOrder: row.SortOrder, Version: row.Version})
		}
	}
	if err := (&ListRemotePages{LibraryID: o.LibraryID}).recordPageTotal(requestCtx, c, out.Total, observedAt); err != nil {
		return nil, err
	}
	return out, nil
}

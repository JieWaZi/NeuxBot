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

// 获取远端页面列表
type ListRemotePages struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// 关键词
	Query string `in:"query" name:"query"`
	// 页面类型
	PageType string `in:"query" name:"pageType"`
	// 主题标识
	ThemeID *string `in:"query" name:"themeId"`
}

func (o *ListRemotePages) Path() string { return "/:connectionId/libraries/:libraryId/pages" }
func (o *ListRemotePages) Output(ctx *gin.Context) (interface{}, error) {
	observedAt := time.Now().UnixMilli()
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	items := []knowledge.Page{}
	if c.custom != nil {
		v, e := c.custom.ListPages(ctx, core.PageQuery{LibraryID: o.LibraryID, Offset: offset, Limit: limit, Query: o.Query, PageType: o.PageType, ThemeID: o.ThemeID})
		if e != nil {
			return nil, remoteError(e)
		}
		for _, p := range v.Items {
			items = append(items, customPage(p))
		}
		if err := o.recordPageTotal(ctx, c, v.Total, observedAt); err != nil {
			return nil, err
		}
		return knowledge.PageList{Items: items, Total: v.Total}, nil
	}
	if offset%limit != 0 {
		return nil, status_error.KnowledgeInvalid
	}
	v, e := c.official.ListPages(ctx, o.LibraryID, remote.PageOptions{Page: offset/limit + 1, PageSize: limit, Query: o.Query, PageType: o.PageType, FolderID: o.ThemeID})
	if e != nil {
		return nil, remoteError(e)
	}
	for _, p := range v.Pages {
		items = append(items, officialPage(p))
	}
	if err := o.recordPageTotal(ctx, c, v.Total, observedAt); err != nil {
		return nil, err
	}
	return knowledge.PageList{Items: items, Total: v.Total}, nil
}

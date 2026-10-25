package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 保存远端页面
type WriteRemotePage struct {
	ginx.MethodPut
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 请求体
	Body RemoteWritePageInput `in:"body"`
}

func (o *WriteRemotePage) Path() string { return "/:connectionId/libraries/:libraryId/pages/:pageId" }
func (o *WriteRemotePage) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "page.write", o.Body, func() (*core.Receipt, error) {
		var p core.Page
		if o.Body.ExpectedVersion > 0 {
			v, e := c.custom.GetPage(ctx, o.PageID)
			if e != nil {
				return nil, e
			}
			p = *v
		}
		// 不用新读取的远端时间替换调用者的并发屏障。
		p.UpdatedAt = time.UnixMilli(o.Body.Page.UpdatedAt)
		p.ID = o.PageID
		p.KnowledgeBaseID = o.LibraryID
		p.Slug = o.Body.Page.Slug
		p.Title = o.Body.Page.Title
		p.PageType = o.Body.Page.PageType
		p.Status = o.Body.Page.Status
		p.Content = o.Body.Page.Content
		p.Summary = o.Body.Page.Summary
		p.Aliases = o.Body.Page.Aliases
		p.FolderID = o.Body.Page.ThemeID
		p.SortOrder = o.Body.Page.SortOrder
		p.ManualPlacement = true
		return c.custom.WritePage(ctx, core.WritePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, StructureVersion: o.Body.StructureVersion, Page: p})
	})
}

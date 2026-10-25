package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建远端页面
type CreateRemotePage struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body knowledge.CreatePageInput `in:"body"`
}

func (o *CreateRemotePage) Path() string { return "/:connectionId/libraries/:libraryId/pages" }
func (o *CreateRemotePage) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	p := o.Body.Page
	return remoteInvoke(ctx, c, o.Body.RequestID, "page.create", o.Body, func() (*core.Receipt, error) {
		return c.custom.WritePage(ctx, core.WritePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, Page: core.Page{KnowledgeBaseID: o.LibraryID, Slug: p.Slug, Title: p.Title, PageType: p.PageType, Status: p.Status, Content: p.Content, Summary: p.Summary, Aliases: p.Aliases, FolderID: p.ThemeID, SortOrder: p.SortOrder, ManualPlacement: true}})
	})
}

package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建知识页面
type CreatePage struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body CreatePageInput `in:"body"`
}

// 创建页面请求
type CreatePageInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 页面内容
	Page Page `json:"page"`
}

func (o *CreatePage) Path() string { return "/libraries/:libraryId/pages" }
func (o *CreatePage) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	p := o.Body.Page
	receipt, err := executeWrite(ctx, o.Body.RequestID, "page.create", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.WritePage(ctx, core.WritePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, Page: core.Page{KnowledgeBaseID: o.LibraryID, Slug: p.Slug, Title: p.Title, PageType: p.PageType, Status: p.Status, Content: p.Content, Summary: p.Summary, Aliases: p.Aliases, FolderID: p.ThemeID, SortOrder: p.SortOrder, ManualPlacement: true}})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(receipt), nil
}

package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 保存知识页面
type WritePage struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 请求体
	Body WritePageInput `in:"body"`
}

// 保存页面请求
type WritePageInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int `json:"expectedVersion" validate:"min=1"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 页面内容
	Page Page `json:"page"`
}

func (o *WritePage) Path() string { return "/libraries/:libraryId/pages/:pageId" }
func (o *WritePage) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	// 人工编辑从当前权威页复制证据，不允许Renderer伪造引文或保护片段。
	var page core.Page
	if o.Body.ExpectedVersion > 0 {
		v, err := global.KnowledgeEngine.GetPage(ctx, o.LibraryID, o.PageID)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		page = *v
	}
	// 证据来自权威当前页，读取时间必须来自调用者的原快照。
	page.UpdatedAt = time.UnixMilli(o.Body.Page.UpdatedAt)
	page.ID = o.PageID
	page.KnowledgeBaseID = o.LibraryID
	page.Slug = o.Body.Page.Slug
	page.Title = o.Body.Page.Title
	page.PageType = o.Body.Page.PageType
	page.Status = o.Body.Page.Status
	page.Content = o.Body.Page.Content
	page.Summary = o.Body.Page.Summary
	page.Aliases = o.Body.Page.Aliases
	page.FolderID = o.Body.Page.ThemeID
	page.SortOrder = o.Body.Page.SortOrder
	page.ManualPlacement = true
	r, err := executeWrite(ctx, o.Body.RequestID, "page.write", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.WritePage(ctx, core.WritePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, StructureVersion: o.Body.StructureVersion, Page: page})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}

package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建集成 Wiki 页面
type PublishedCreatePage struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 页面内容
	Body PublishedCreatePageInput `in:"body"`
}

// 公开页面创建内容
type PublishedCreatePageInput struct {
	// 分层句柄
	Slug string `json:"slug" validate:"required,max=500"`
	// 页面标题
	Title string `json:"title" validate:"required,max=500"`
	// 页面正文
	Content string `json:"content" validate:"max=1000000"`
	// 页面摘要
	Summary string `json:"summary" validate:"max=10000"`
	// 页面类型
	PageType string `json:"page_type" validate:"required"`
	// 页面别名
	Aliases []string `json:"aliases"`
	// 目录标识
	FolderID string `json:"folder_id"`
	// 页面排序
	SortOrder int `json:"sort_order"`
	// 已读目录版本
	StructureVersion int64 `json:"structure_version" validate:"min=1"`
}

func (*PublishedCreatePage) Path() string { return "/knowledgebase/:kb_id/wiki/pages" }
func (o *PublishedCreatePage) Output(ctx *gin.Context) (interface{}, error) {
	slug, err := pathSlug(o.Body.Slug)
	if err != nil {
		return nil, err
	}
	original, request, err := scopedRequest(ctx, o.LibraryID, "")
	if err != nil {
		return nil, err
	}
	d := dependencies(ctx)
	lib, err := d.engine.GetLibrary(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	release, err := d.auth.AcquireGuard(ctx.Request.Context(), auth)
	if err != nil {
		return nil, err
	}
	defer release()
	activity := core.WithActivityActor(ctx.Request.Context(), core.ActivityActor{UserID: auth.Identity.KeyID, Trigger: "api"})
	receipt, err := performWrite(ctx, activity, request, "page.create", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return d.engine.WritePage(activity, core.WritePageInput{RequestID: request, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, ExpectedPolicyVersion: lib.PolicyVersion, Page: core.Page{KnowledgeBaseID: o.LibraryID, Slug: slug, Title: o.Body.Title, Content: o.Body.Content, Summary: o.Body.Summary, PageType: o.Body.PageType, Aliases: o.Body.Aliases, FolderID: o.Body.FolderID, SortOrder: o.Body.SortOrder, ManualPlacement: true}})
	})
	if err != nil {
		return nil, err
	}
	page, err := d.engine.GetPage(ctx.Request.Context(), o.LibraryID, receipt.ResourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return PublishedPageWriteResponse{Page: *page, Receipt: publicReceipt(receipt, original)}, nil
}

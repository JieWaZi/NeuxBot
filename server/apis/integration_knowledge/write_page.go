package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki 条件编辑请求
type PublishedPageInput struct {
	// 已读版本
	Version int `json:"version" validate:"min=1"`
	// 目录版本
	StructureVersion int64 `json:"structure_version,omitempty"`
	// 页面标题
	Title *string `json:"title,omitempty"`
	// 页面正文
	Content *string `json:"content,omitempty"`
	// 页面摘要
	Summary *string `json:"summary,omitempty"`
	// 页面类型
	PageType *string `json:"page_type,omitempty"`
	// 页面别名
	Aliases *[]string `json:"aliases,omitempty"`
	// 目录标识
	FolderID *string `json:"folder_id,omitempty"`
	// 目录排序
	SortOrder *int `json:"sort_order,omitempty"`
}

// Wiki 编辑响应
type PublishedPageWriteResponse struct {
	// 页面内容
	core.Page
	// 修改回执
	Receipt *core.Receipt `json:"receipt"`
}

// 编辑集成 Wiki
type PublishedWritePage struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 分层句柄
	Slug string `in:"path" name:"slug" validate:"required"`
	// 请求体
	Body PublishedPageInput `in:"body"`
}

// Path 定位按句柄条件编辑页面的入口。
func (*PublishedWritePage) Path() string { return "/knowledgebase/:kb_id/wiki/pages/*slug" }

// Output 将同一用户已读版本送入原子写入，不用服务器重读版本替换它。
func (o *PublishedWritePage) Output(ctx *gin.Context) (interface{}, error) {
	if o.Body.Version < 1 {
		return nil, status_error.KnowledgeInvalid
	}
	slug, err := pathSlug(o.Slug)
	if err != nil {
		return nil, err
	}
	originalID, requestID, err := scopedRequest(ctx, o.LibraryID, "")
	if err != nil {
		return nil, err
	}
	d := dependencies(ctx)
	current, err := d.engine.GetPageBySlug(ctx.Request.Context(), o.LibraryID, slug)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	page := *current
	if o.Body.Title != nil {
		page.Title = *o.Body.Title
	}
	if o.Body.Content != nil {
		page.Content = *o.Body.Content
	}
	if o.Body.Summary != nil {
		page.Summary = *o.Body.Summary
	}
	if o.Body.PageType != nil {
		page.PageType = *o.Body.PageType
	}
	if o.Body.Aliases != nil {
		page.Aliases = append([]string(nil), (*o.Body.Aliases)...)
	}
	if o.Body.FolderID != nil {
		page.FolderID, page.ManualPlacement = *o.Body.FolderID, true
	}
	if o.Body.SortOrder != nil {
		page.SortOrder, page.ManualPlacement = *o.Body.SortOrder, true
	}
	lib, err := d.engine.GetLibrary(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	structureVersion := o.Body.StructureVersion
	if structureVersion == 0 {
		structureVersion = lib.StructureVersion
	}
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	release, err := d.auth.AcquireGuard(ctx.Request.Context(), auth)
	if err != nil {
		return nil, err
	}
	defer release()
	activity := core.WithActivityActor(ctx.Request.Context(), core.ActivityActor{UserID: auth.Identity.KeyID, Trigger: "api"})
	receipt, err := performWrite(ctx, activity, requestID, "page.write", o.LibraryID, struct {
		// Slug 绑定实际编辑目标。
		Slug string
		// Body 固定用户编辑输入。
		Body PublishedPageInput
	}{slug, o.Body}, func() (*core.Receipt, error) {
		return d.engine.WritePage(activity, core.WritePageInput{RequestID: requestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.Version, ExpectedPolicyVersion: lib.PolicyVersion, StructureVersion: structureVersion, Page: page})
	})
	if err != nil {
		return nil, err
	}
	updated, err := d.engine.GetPage(ctx.Request.Context(), o.LibraryID, current.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return PublishedPageWriteResponse{Page: *updated, Receipt: publicReceipt(receipt, originalID)}, nil
}

package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 目录条件修改
type PublishedThemeInput struct {
	// 目录版本
	StructureVersion int64 `json:"structure_version" validate:"min=1"`
	// 目录名称
	Name string `json:"name" validate:"max=200"`
	// 父目录
	ParentID string `json:"parent_id"`
	// 目录排序
	SortOrder int `json:"sort_order"`
}

// 目录修改结果
type PublishedThemeResponse struct {
	// 目录内容
	core.Theme
	// 原请求回执
	Receipt *core.Receipt `json:"receipt"`
}

// 创建集成目录
type PublishedCreateTheme struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 目录内容
	Body PublishedThemeInput `in:"body"`
}

func (*PublishedCreateTheme) Path() string { return "/knowledgebase/:kb_id/wiki/folders" }
func (o *PublishedCreateTheme) Output(ctx *gin.Context) (interface{}, error) {
	return writeTheme(ctx, o.LibraryID, "", o.Body, false)
}

// 修改集成目录
type PublishedWriteTheme struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 目录标识
	ThemeID string `in:"path" name:"folder_id" validate:"required"`
	// 目录内容
	Body PublishedThemeInput `in:"body"`
}

func (*PublishedWriteTheme) Path() string { return "/knowledgebase/:kb_id/wiki/folders/:folder_id" }
func (o *PublishedWriteTheme) Output(ctx *gin.Context) (interface{}, error) {
	return writeTheme(ctx, o.LibraryID, o.ThemeID, o.Body, false)
}

// 删除空集成目录
type PublishedDeleteTheme struct {
	ginx.MethodDelete
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 目录标识
	ThemeID string `in:"path" name:"folder_id" validate:"required"`
	// 目录版本
	StructureVersion int64 `in:"query" name:"structure_version" validate:"min=1"`
}

func (*PublishedDeleteTheme) Path() string { return "/knowledgebase/:kb_id/wiki/folders/:folder_id" }
func (o *PublishedDeleteTheme) Output(ctx *gin.Context) (interface{}, error) {
	return writeTheme(ctx, o.LibraryID, o.ThemeID, PublishedThemeInput{StructureVersion: o.StructureVersion}, true)
}
func writeTheme(ctx *gin.Context, lib, id string, in PublishedThemeInput, remove bool) (interface{}, error) {
	if in.StructureVersion < 1 {
		return nil, status_error.KnowledgeInvalid
	}
	original, request, err := scopedRequest(ctx, lib, "")
	if err != nil {
		return nil, err
	}

	input := core.WriteThemeInput{RequestID: request, LibraryID: lib, StructureVersion: in.StructureVersion, Delete: remove, Theme: core.Theme{ID: id, KnowledgeBaseID: lib, ParentID: in.ParentID, Name: in.Name, SortOrder: in.SortOrder, Manual: true}}
	d := dependencies(ctx)
	library, err := d.engine.GetLibrary(ctx.Request.Context(), lib)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	input.ExpectedPolicyVersion = library.PolicyVersion
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	logical := input
	logical.ExpectedPolicyVersion = 0
	release, err := d.auth.AcquireGuard(ctx.Request.Context(), auth)
	if err != nil {
		return nil, err
	}
	defer release()
	activity := core.WithActivityActor(ctx.Request.Context(), core.ActivityActor{UserID: auth.Identity.KeyID, Trigger: "api"})
	receipt, err := performWrite(ctx, activity, request, "theme.write", lib, logical, func() (*core.Receipt, error) { return d.engine.WriteTheme(activity, input) })
	if err != nil {
		return nil, err
	}
	if remove {
		return publicReceipt(receipt, original), nil
	}
	themes, err := d.engine.ListThemes(ctx.Request.Context(), lib)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	for _, theme := range themes {
		if theme.ID == receipt.ResourceID {
			return PublishedThemeResponse{Theme: *theme, Receipt: publicReceipt(receipt, original)}, nil
		}
	}
	return nil, status_error.KnowledgeNotFound
}

package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 保存知识主题
type WriteTheme struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 主题标识
	ThemeID string `in:"path" name:"themeId" validate:"required"`
	// 请求体
	Body WriteThemeInput `in:"body"`
}

// 保存主题请求
type WriteThemeInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 主题内容
	Theme Theme `json:"theme"`
	// 删除主题
	Delete bool `json:"delete"`
}

func (o *WriteTheme) Path() string { return "/libraries/:libraryId/themes/:themeId" }
func (o *WriteTheme) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "theme.write", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.WriteTheme(ctx, core.WriteThemeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, Delete: o.Body.Delete, Theme: core.Theme{ID: o.ThemeID, KnowledgeBaseID: o.LibraryID, ParentID: o.Body.Theme.ParentID, Name: o.Body.Theme.Name, SortOrder: o.Body.Theme.SortOrder, Manual: true}})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}

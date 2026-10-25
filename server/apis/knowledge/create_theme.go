package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建知识主题
type CreateTheme struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body CreateThemeInput `in:"body"`
}

// 创建主题请求
type CreateThemeInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 主题内容
	Theme Theme `json:"theme"`
}

func (o *CreateTheme) Path() string { return "/libraries/:libraryId/themes" }
func (o *CreateTheme) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	t := o.Body.Theme
	receipt, err := executeWrite(ctx, o.Body.RequestID, "theme.create", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.WriteTheme(ctx, core.WriteThemeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, Theme: core.Theme{KnowledgeBaseID: o.LibraryID, ParentID: t.ParentID, Name: t.Name, SortOrder: t.SortOrder, Manual: true}})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(receipt), nil
}

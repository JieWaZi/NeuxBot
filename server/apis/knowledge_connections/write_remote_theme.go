package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 修改远端主题
type WriteRemoteTheme struct {
	ginx.MethodPut
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 主题标识
	ThemeID string `in:"path" name:"themeId" validate:"required"`
	// 请求体
	Body knowledge.WriteThemeInput `in:"body"`
}

func (o *WriteRemoteTheme) Path() string {
	return "/:connectionId/libraries/:libraryId/themes/:themeId"
}
func (o *WriteRemoteTheme) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	t := o.Body.Theme
	return remoteInvoke(ctx, c, o.Body.RequestID, "theme.write", o.Body, func() (*core.Receipt, error) {
		return c.custom.WriteTheme(ctx, core.WriteThemeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, StructureVersion: o.Body.StructureVersion, Delete: o.Body.Delete, Theme: core.Theme{ID: o.ThemeID, KnowledgeBaseID: o.LibraryID, Name: t.Name, ParentID: t.ParentID, SortOrder: t.SortOrder, Manual: true}})
	})
}

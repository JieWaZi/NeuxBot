package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 兼容 Wiki 目录
type PublishedTheme struct {
	// 目录内容
	core.Theme
	// 页面数量
	PageCount int64 `json:"page_count"`
	// 存在子目录
	HasChildren bool `json:"has_children"`
}

// 目录列表响应
type ThemeListResponse struct {
	// 父目录标识
	ParentID string `json:"parent_id"`
	// 子目录列表
	Folders []PublishedTheme `json:"folders"`
}

// 列出集成 Wiki 目录
type PublishedListThemes struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 父目录标识
	ParentID string `in:"query" name:"parent_id"`
	// 读取完整目录
	All bool `in:"query" name:"all"`
}

// Path 定位 Wiki 目录读取入口。
func (*PublishedListThemes) Path() string { return "/knowledgebase/:kb_id/wiki/folders" }

// Output 返回真实父子关系，普通调用只枚举指定父目录。
func (o *PublishedListThemes) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	themes, err := d.engine.ListThemes(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	counts, err := d.engine.CountPagesByTheme(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	parents := map[string]bool{}
	for _, theme := range themes {
		parents[theme.ParentID] = true
	}
	result := ThemeListResponse{ParentID: o.ParentID, Folders: []PublishedTheme{}}
	for _, theme := range themes {
		if !o.All && theme.ParentID != o.ParentID {
			continue
		}
		result.Folders = append(result.Folders, PublishedTheme{Theme: *theme, PageCount: counts[theme.ID], HasChildren: parents[theme.ID]})
	}
	return result, nil
}

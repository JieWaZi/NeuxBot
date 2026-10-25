package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 页面目录条目
type PageDirectoryEntry struct {
	// 页面标识
	ID string `json:"id"`
	// 语义地址
	Slug string `json:"slug"`
	// 页面标题
	Title string `json:"title"`
	// 页面类型
	PageType string `json:"pageType"`
	// 页面简介
	Summary string `json:"summary"`
	// 主题标识
	ThemeID string `json:"themeId"`
	// 目录路径
	CategoryPath []string `json:"categoryPath"`
	// 排序
	SortOrder int `json:"sortOrder"`
	// 当前版本
	Version int `json:"version"`
}

// 页面目录响应
type PageDirectoryList struct {
	// 目录条目
	Items []PageDirectoryEntry `json:"items"`
	// 总数
	Total int64 `json:"total"`
}

// 获取轻量页面目录
type ListPageDirectory struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每批条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=1000"`
}

func (*ListPageDirectory) Path() string { return "/libraries/:libraryId/page-directory" }
func (o *ListPageDirectory) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	requestCtx := ctx.Request.Context()
	rows, total, err := global.KnowledgeEngine.ListPageDirectory(requestCtx, o.LibraryID, o.Offset, o.Limit)
	if requestCtx.Err() != nil {
		return nil, status_error.KnowledgeCancelled
	}
	if err != nil {
		return nil, bridge.MapError(err)
	}
	out := PageDirectoryList{Items: []PageDirectoryEntry{}, Total: total}
	for _, row := range rows {
		out.Items = append(out.Items, PageDirectoryDTO(row))
	}
	return out, nil
}

// PageDirectoryDTO 仅映射目录展示字段，不能用作页面详情或编辑快照。
func PageDirectoryDTO(v *core.PageDirectoryEntry) PageDirectoryEntry {
	return PageDirectoryEntry{ID: v.ID, Slug: v.Slug, Title: v.Title, PageType: v.PageType, Summary: v.Summary, ThemeID: v.FolderID, CategoryPath: v.CategoryPath, SortOrder: v.SortOrder, Version: v.Version}
}

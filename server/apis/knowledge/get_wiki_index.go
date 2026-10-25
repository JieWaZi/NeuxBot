package knowledge

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki索引条目
type WikiIndexEntry struct {
	// 页面地址
	Slug string `json:"slug"`
	// 页面标题
	Title string `json:"title"`
	// 页面简介
	Summary string `json:"summary"`
	// 父页地址
	ParentSlug string `json:"parentSlug,omitempty"`
	// 分类路径
	CategoryPath []string `json:"categoryPath,omitempty"`
	// 完整路径
	WikiPath string `json:"wikiPath,omitempty"`
	// 分类层级
	Depth int `json:"depth,omitempty"`
	// 同级顺序
	SortOrder int `json:"sortOrder,omitempty"`
}

// Wiki索引分组
type WikiIndexGroup struct {
	// 页面类型
	Type string `json:"type"`
	// 分组总数
	Total int64 `json:"total"`
	// 页面条目
	Items []WikiIndexEntry `json:"items"`
	// 后页游标
	NextCursor string `json:"nextCursor,omitempty"`
}

// Wiki结构化索引
type WikiIndexView struct {
	// 索引导言
	Intro string `json:"intro"`
	// 索引版本
	Version int `json:"version"`
	// 分组目录
	Groups []WikiIndexGroup `json:"groups"`
}

// 获取Wiki索引
type GetWikiIndex struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面类型
	PageTypes string `in:"query" name:"pageTypes"`
	// 每组条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=200"`
	// 分页游标
	Cursor string `in:"query" name:"cursor"`
}

func (o *GetWikiIndex) Path() string { return "/libraries/:libraryId/wiki/index" }
func (o *GetWikiIndex) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	selected := []string{}
	for _, kind := range strings.Split(o.PageTypes, ",") {
		if kind = strings.TrimSpace(kind); kind != "" {
			selected = append(selected, kind)
		}
	}
	view, err := global.KnowledgeEngine.GetIndexView(ctx, o.LibraryID, selected, o.Limit, o.Cursor)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	result := WikiIndexView{Intro: view.Intro, Version: view.Version, Groups: make([]WikiIndexGroup, 0, len(view.Groups))}
	for _, g := range view.Groups {
		group := WikiIndexGroup{Type: g.Type, Total: g.Total, NextCursor: g.NextCursor, Items: make([]WikiIndexEntry, 0, len(g.Items))}
		for _, item := range g.Items {
			group.Items = append(group.Items, WikiIndexEntry{Slug: item.Slug, Title: item.Title, Summary: item.Summary, ParentSlug: item.ParentSlug, CategoryPath: []string(item.CategoryPath), WikiPath: item.WikiPath, Depth: item.Depth, SortOrder: item.SortOrder})
		}
		result.Groups = append(result.Groups, group)
	}
	return result, nil
}

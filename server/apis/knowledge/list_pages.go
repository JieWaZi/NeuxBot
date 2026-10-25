package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 页面列表
type PageList struct {
	// 页面
	Items []Page `json:"items"`
	// 总数
	Total int64 `json:"total"`
}

// 获取页面列表
type ListPages struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// 关键词
	Query string `in:"query" name:"query"`
	// 页面类型
	PageType string `in:"query" name:"pageType"`
	// 主题标识
	ThemeID *string `in:"query" name:"themeId"`
}

func (o *ListPages) Path() string { return "/libraries/:libraryId/pages" }
func (o *ListPages) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	offset, limit := pagination(Pagination{Offset: o.Offset, Limit: o.Limit})
	rows, total, err := global.KnowledgeEngine.ListPages(ctx, core.PageQuery{LibraryID: o.LibraryID, Query: o.Query, PageType: o.PageType, ThemeID: o.ThemeID, Offset: offset, Limit: limit})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := []Page{}
	for _, v := range rows {
		items = append(items, pageDTO(v))
	}
	return PageList{Items: items, Total: total}, nil
}

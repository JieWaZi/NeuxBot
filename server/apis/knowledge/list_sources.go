package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 原件列表
type SourceList struct {
	// 原件
	Items []Source `json:"items"`
	// 总数
	Total int64 `json:"total"`
}

// 获取原件列表
type ListSources struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 文件名关键词
	Query string `in:"query" name:"query" validate:"max=500"`
	// 资料状态
	State string `in:"query" name:"state" validate:"omitempty,oneof=all pending queued processing processed unchanged failed cancelled"`
	// 上传时间排序
	Sort string `in:"query" name:"sort" validate:"omitempty,oneof=newest oldest"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListSources) Path() string { return "/libraries/:libraryId/sources" }
func (o *ListSources) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	offset, limit := pagination(Pagination{Offset: o.Offset, Limit: o.Limit})
	rows, total, err := global.KnowledgeEngine.ListSourcesQuery(ctx, o.LibraryID, core.SourceQuery{Offset: offset, Limit: limit, Query: o.Query, State: o.State, Sort: o.Sort})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := make([]Source, 0, len(rows))
	for _, row := range rows {
		items = append(items, sourceDTO(row))
	}
	return SourceList{Items: items, Total: total}, nil
}

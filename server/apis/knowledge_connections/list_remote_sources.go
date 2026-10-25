package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端原件列表
type ListRemoteSources struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
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

func (o *ListRemoteSources) Path() string { return "/:connectionId/libraries/:libraryId/sources" }
func (o *ListRemoteSources) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	items := []knowledge.Source{}
	if c.custom != nil {
		v, e := c.custom.ListSources(ctx, offset, limit, core.SourceQuery{Query: o.Query, State: o.State, Sort: o.Sort})
		if e != nil {
			return nil, remoteError(e)
		}
		for _, x := range v.Items {
			items = append(items, customSource(x))
		}
		return knowledge.SourceList{Items: items, Total: v.Total}, nil
	}
	if offset%limit != 0 {
		return nil, status_error.KnowledgeInvalid
	}
	v, e := c.official.ListSources(ctx, o.LibraryID, offset/limit+1, limit, core.SourceQuery{Query: o.Query, State: o.State, Sort: o.Sort})
	if e != nil {
		return nil, remoteError(e)
	}
	for _, x := range v.Data {
		items = append(items, officialSource(x))
	}
	return knowledge.SourceList{Items: items, Total: v.Total}, nil
}

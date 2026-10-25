package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端知识库列表
type ListRemoteLibraries struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListRemoteLibraries) Path() string { return "/:connectionId/libraries" }
func (o *ListRemoteLibraries) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, "")
	if err != nil {
		return nil, err
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	items := []knowledge.Library{}
	if c.connection.Backend == "integration" {
		rows, e := mountedLibraries(ctx, c)
		if e != nil {
			return nil, e
		}
		start := min(offset, len(rows))
		end := min(start+limit, len(rows))
		return withLibrarySummaries(ctx, c, rows[start:end], int64(len(rows)))
	}
	if c.custom != nil {
		v, e := c.custom.ListLibraries(ctx, offset, limit)
		if e != nil {
			return nil, remoteError(e)
		}
		for _, x := range v.Items {
			items = append(items, customLibrary(c, x))
		}
		return withLibrarySummaries(ctx, c, items, v.Total)
	}
	rows, e := c.official.ListLibraries(ctx)
	if e != nil {
		return nil, remoteError(e)
	}
	start := min(offset, len(rows))
	end := min(start+limit, len(rows))
	for _, x := range rows[start:end] {
		items = append(items, officialLibrary(c, x))
	}
	return withLibrarySummaries(ctx, c, items, int64(len(rows)))
}

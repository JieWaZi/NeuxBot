package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端关系图
type GetRemoteGraph struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 图形模式
	Mode string `in:"query" name:"mode"`
	// 中心地址
	Center string `in:"query" name:"center"`
	// 层数
	Depth int `in:"query" name:"depth" validate:"min=0,max=3"`
	// 节点上限
	Limit int `in:"query" name:"limit" validate:"min=0,max=200"`
}

func (o *GetRemoteGraph) Path() string { return "/:connectionId/libraries/:libraryId/graph" }
func (o *GetRemoteGraph) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.official == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, e := c.official.Graph(ctx, o.LibraryID, remote.GraphOptions{Mode: o.Mode, Center: o.Center, Depth: o.Depth, Limit: o.Limit})
	if e != nil {
		return nil, remoteError(e)
	}
	return RemoteGraph{Nodes: v.Nodes, Edges: v.Edges, Meta: v.Meta}, nil
}

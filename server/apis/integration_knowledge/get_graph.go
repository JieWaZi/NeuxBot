package integration_knowledge

import (
	"strings"

	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取集成 Wiki 图谱
type PublishedGetGraph struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 图谱模式
	Mode string `in:"query" name:"mode"`
	// 中心句柄
	Center string `in:"query" name:"center"`
	// 展开深度
	Depth int `in:"query" name:"depth"`
	// 返回节点数
	Limit int `in:"query" name:"limit"`
	// 页面类型
	Types string `in:"query" name:"types"`
}

// Path 保留已核对的图谱路径。
func (*PublishedGetGraph) Path() string { return "/knowledgebase/:kb_id/wiki/graph" }

// Output 返回图谱子集及实际截断信息。
func (o *PublishedGetGraph) Output(ctx *gin.Context) (interface{}, error) {
	if o.Limit == 0 {
		o.Limit = 300
	}
	if o.Depth == 0 {
		o.Depth = 1
	}
	var types []string
	if o.Types != "" {
		types = strings.Split(o.Types, ",")
	}
	graph, err := dependencies(ctx).engine.Graph(ctx.Request.Context(), core.GraphRequest{KnowledgeBaseID: o.LibraryID, Mode: o.Mode, Center: o.Center, Depth: o.Depth, Limit: o.Limit, Types: types})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return graph, nil
}

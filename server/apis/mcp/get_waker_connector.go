package mcp

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取连接器
type GetWakerConnector struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
}

// Path 返回伙伴连接器详情接口路径。
func (g *GetWakerConnector) Path() string {
	return "/:wakerId/connectors/:connectorId"
}

// Output 返回连接器详情。
func (g *GetWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return getConnector(ctx, g)
}

// getConnector 复用相同资源用例，空伙伴范围表示全局库。
func getConnector(ctx *gin.Context, g *GetWakerConnector) (ConnectorResponse, error) {
	item, err := visibleConnector(
		ctx,
		strings.TrimSpace(g.WakerID),
		strings.TrimSpace(g.ConnectorID),
	)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	result, err := connectorResponse(ctx, item)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return result, nil
}

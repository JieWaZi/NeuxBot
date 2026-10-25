package mcp

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 连接器列表
type ListWakerConnectors struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
}

// Path 返回伙伴连接器列表接口路径。
func (l *ListWakerConnectors) Path() string { return "/:wakerId/connectors" }

// Output 返回连接器列表。
func (l *ListWakerConnectors) Output(ctx *gin.Context) (interface{}, error) {
	wakerID := strings.TrimSpace(l.WakerID)
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	items, err := global.MCPConnectorControllerManager.ListForWaker(
		ctx,
		wakerID,
		strings.TrimSpace(l.Query),
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result, err := connectorResponses(ctx, items)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

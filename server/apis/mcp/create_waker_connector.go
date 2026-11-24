package mcp

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建连接器
type CreateWakerConnector struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body ConnectorInput `in:"body"`
}

// Path 返回新建伙伴连接器接口路径。
func (c *CreateWakerConnector) Path() string { return "/:wakerId/connectors" }

// Output 新建当前伙伴自有连接器并安装给自身。
func (c *CreateWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return createConnector(ctx, c)
}

// createConnector 复用相同资源用例，空伙伴范围表示全局库。
func createConnector(ctx *gin.Context, c *CreateWakerConnector) (ConnectorResponse, error) {
	created, err := connectorService().Create(ctx, strings.TrimSpace(c.WakerID), connectorDefinition(c.Body))
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return connectorResponse(ctx, created)
}

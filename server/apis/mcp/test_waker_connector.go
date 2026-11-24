package mcp

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 测试连接器
type TestWakerConnector struct {
	// MethodPost 声明以 POST 发起一次真实连接测试。
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
}

// Path 返回伙伴连接器测试接口路径。
func (t *TestWakerConnector) Path() string {
	return "/:wakerId/connectors/:connectorId/test"
}

// Output 执行协议级连接测试并返回最新详情。
func (t *TestWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return testConnector(ctx, t)
}

// testConnector 复用相同资源用例，空伙伴范围表示全局库。
func testConnector(ctx *gin.Context, t *TestWakerConnector) (ConnectorResponse, error) {
	tested, err := connectorService().Test(ctx, strings.TrimSpace(t.WakerID), strings.TrimSpace(t.ConnectorID))
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return connectorResponse(ctx, tested)
}

// connectionTestResult 是公开 MCP 的协议诊断结果。
type connectionTestResult = mcpclient.Result

var runMCPConnectionTest = mcpclient.Test

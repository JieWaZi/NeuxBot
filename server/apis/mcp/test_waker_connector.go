package mcp

import (
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
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
	wakerID := strings.TrimSpace(t.WakerID)
	connectorID := strings.TrimSpace(t.ConnectorID)
	item, err := visibleConnector(
		ctx,
		wakerID,
		connectorID,
	)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	connector := item.Connector
	if connector.Scope != constants.MCPConnectorScopeBuiltIn &&
		trustDigest(connector) != connector.TrustedConfigDigest {
		return ConnectorResponse{}, ginx.WithStack(status_error.MCPConnectorTrustRequired)
	}
	testResult := runMCPConnectionTest(ctx, connector)
	connector.TestStatus = testResult.Status
	connector.TestedAt = time.Now().UnixMilli()
	connector.TestDurationMS = testResult.DurationMS
	connector.TestError = bounded(testResult.Error, constants.MCPConnectorMaximumSummaryLength)
	if testResult.Status == constants.MCPTestStatusSucceeded {
		connector.TestTools = boundedTools(testResult.Tools)
		connector.TestToolCount = len(testResult.Tools)
	}
	if err := global.MCPConnectorControllerManager.UpdateTestSnapshot(
		ctx,
		connector,
		connector.UpdatedAt,
	); err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, connectorID)
	updated, err := visibleConnector(ctx, wakerID, connectorID)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	result, err := connectorResponse(ctx, updated)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return result, nil
}

// connectionTestResult 是公开 MCP 的协议诊断结果。
type connectionTestResult = mcpclient.Result

var runMCPConnectionTest = mcpclient.Test

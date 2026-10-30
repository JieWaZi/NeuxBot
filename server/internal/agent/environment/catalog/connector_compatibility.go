package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	"slices"
)

// BaseRuntimeIDs 返回连接器兼容矩阵使用的基础 Runtime 列表，顺序稳定便于 UI 展示。
func BaseRuntimeIDs() []string {
	return []string{
		runtimeconfig.RuntimeIDOpenCode,
		runtimeconfig.RuntimeIDGemini,
		runtimeconfig.RuntimeIDGrok,
		runtimeconfig.RuntimeIDWorkBuddy,
		runtimeconfig.RuntimeIDQoder,
		runtimeconfig.RuntimeIDQoderCN,
		runtimeconfig.RuntimeIDCodex,
		runtimeconfig.RuntimeIDClaude,
		runtimeconfig.RuntimeIDKimi,
		runtimeconfig.RuntimeIDCursor,
		runtimeconfig.RuntimeIDPi,
	}
}

// ConnectorCompatibility 返回一个基础 Runtime 是否支持指定 MCP 传输方式。
func ConnectorCompatibility(
	baseRuntimeID string,
	transport runtimeconfig.ConnectorTransport,
) (bool, string) {
	descriptor, exists := native.Lookup(baseRuntimeID)
	nativeTransport := c.MCPTransport(transport)
	if transport == runtimeconfig.ConnectorTransportStreamableHTTP {
		nativeTransport = c.MCPHTTP
	}
	if exists && slices.Contains(descriptor.MCPTransports, nativeTransport) {
		return true, ""
	}
	return false, "transport_not_supported"
}

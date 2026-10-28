package acp

import (
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// capabilityServers 只转换受宿主租约保护的公开 MCP 配置，远程传输必须由握手声明。
func capabilityServers(contributions contract.Contributions, resources []contract.ResourceBinding, capabilities sdk.McpCapabilities) ([]sdk.McpServer, error) {
	bindings := make(map[string]*contract.MCPConnection)
	for _, binding := range resources {
		if binding.MCP != nil {
			bindings[binding.ID] = binding.MCP
		}
	}
	servers := make([]sdk.McpServer, 0, len(contributions.MCP))
	for _, resource := range contributions.MCP {
		connection := bindings[resource.ResourceID]
		if connection == nil {
			if resource.Optional {
				continue
			}
			return nil, fault(contract.FaultPreparation, "mcp", "ACP required MCP resource is missing", nil)
		}
		if strings.TrimSpace(resource.Name) == "" {
			return nil, fault(contract.FaultInvalidSpec, "mcp", "ACP MCP service name is missing", nil)
		}
		switch connection.Transport {
		case contract.MCPStdio:
			if strings.TrimSpace(connection.Command) == "" {
				return nil, fault(contract.FaultInvalidSpec, "mcp", "ACP MCP command is missing", nil)
			}
			environment := make([]sdk.EnvVariable, 0, len(connection.Environment))
			for _, name := range sortedKeys(connection.Environment) {
				environment = append(environment, sdk.EnvVariable{Name: name, Value: connection.Environment[name]})
			}
			servers = append(servers, sdk.McpServer{Stdio: &sdk.McpServerStdio{Name: resource.Name, Command: connection.Command, Args: append([]string{}, connection.Arguments...), Env: environment}})
		case contract.MCPHTTP, contract.MCPSSE:
			if connection.Transport == contract.MCPHTTP && !capabilities.Http || connection.Transport == contract.MCPSSE && !capabilities.Sse {
				return nil, fault(contract.FaultUnsupported, "mcp", "ACP requested MCP transport is not advertised", nil)
			}
			if strings.TrimSpace(connection.URL) == "" {
				return nil, fault(contract.FaultInvalidSpec, "mcp", "ACP MCP URL is missing", nil)
			}
			headers := make([]sdk.HttpHeader, 0, len(connection.Headers))
			for _, name := range sortedKeys(connection.Headers) {
				headers = append(headers, sdk.HttpHeader{Name: name, Value: connection.Headers[name]})
			}
			if connection.Transport == contract.MCPHTTP {
				servers = append(servers, sdk.McpServer{Http: &sdk.McpServerHttpInline{Name: resource.Name, Url: connection.URL, Headers: headers}})
			} else {
				servers = append(servers, sdk.McpServer{Sse: &sdk.McpServerSseInline{Name: resource.Name, Url: connection.URL, Headers: headers}})
			}
		default:
			return nil, fault(contract.FaultUnsupported, "mcp", "ACP MCP transport is unsupported", nil)
		}
	}
	return servers, nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

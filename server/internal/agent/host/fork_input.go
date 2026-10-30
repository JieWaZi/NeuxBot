package runtimehost

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentcontract "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	runtimecatalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
	"sort"
	"strings"
)

type mcpPreparation struct {
	// compatible 是经过校验且可以注入当前 Runtime 的连接器。
	compatible []runtimecontract.Connector
}

// PrepareForkMCPServers 校验产品连接器并生成无领域身份的原生分叉资源。
func PrepareForkMCPServers(connectors []runtimecontract.Connector) ([]acp.McpServer, error) {
	prepared, err := prepareMCPConnectorSet(connectors, func(runtimeconfig.ConnectorTransport) bool { return true })
	if err != nil {
		return nil, err
	}
	return acpMCPServers(prepared.compatible)
}

// prepareMCPConnectorSet 建立 Runtime 无关的 MCP 连接器事实。
func prepareMCPConnectorSet(
	connectors []runtimecontract.Connector,
	supported func(runtimeconfig.ConnectorTransport) bool,
) (mcpPreparation, error) {
	compatible := make([]runtimecontract.Connector, 0, len(connectors))
	seenIDs := make(map[string]struct{}, len(connectors))
	seenRuntimeKeys := make(map[string]struct{}, len(connectors))
	for _, connector := range connectors {
		if strings.TrimSpace(connector.ID) == "" ||
			strings.TrimSpace(connector.Name) == "" ||
			runtimecontract.ValidateConnectorConfiguration(connector) != nil {
			return mcpPreparation{}, status_error.BadRequest
		}
		if _, exists := seenIDs[connector.ID]; exists {
			return mcpPreparation{}, status_error.BadRequest
		}
		seenIDs[connector.ID] = struct{}{}
		if _, exists := seenRuntimeKeys[connector.RuntimeKey]; exists {
			return mcpPreparation{}, status_error.BadRequest
		}
		seenRuntimeKeys[connector.RuntimeKey] = struct{}{}
		if !supported(connector.Transport) {
			continue
		}
		if err := runtimecontract.ValidateConnectorConfiguration(connector); err != nil {
			return mcpPreparation{}, err
		}
		compatible = append(compatible, connector)
	}

	return mcpPreparation{
		compatible: compatible,
	}, nil
}

// acpMCPServers 转换经过兼容性校验的 Connector。
func acpMCPServers(
	connectors []runtimecontract.Connector,
) ([]acp.McpServer, error) {
	servers := make([]acp.McpServer, 0, len(connectors))
	for _, connector := range connectors {
		switch connector.Transport {
		case runtimeconfig.ConnectorTransportStdio:
			environmentKeys := make([]string, 0, len(connector.Environment))
			for key := range connector.Environment {
				environmentKeys = append(environmentKeys, key)
			}
			sort.Strings(environmentKeys)
			environment := make([]acp.EnvVariable, 0, len(environmentKeys))
			for _, key := range environmentKeys {
				environment = append(environment, acp.EnvVariable{
					Name:  key,
					Value: connector.Environment[key],
				})
			}
			servers = append(servers, acp.McpServer{
				Stdio: &acp.McpServerStdio{
					Name:    connector.RuntimeKey,
					Command: connector.Command,
					Args:    append([]string{}, connector.Arguments...),
					Env:     environment,
				},
			})
		case runtimeconfig.ConnectorTransportStreamableHTTP:
			servers = append(servers, acp.McpServer{
				Http: &acp.McpServerHttpInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		case runtimeconfig.ConnectorTransportSSE:
			servers = append(servers, acp.McpServer{
				Sse: &acp.McpServerSseInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		default:
			return nil, status_error.SkillUnavailable
		}
	}
	return servers, nil
}

// acpHTTPHeaders 把 Connector header 转为顺序稳定的 ACP 列表。
func acpHTTPHeaders(values map[string]string) []acp.HttpHeader {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	headers := make([]acp.HttpHeader, 0, len(keys))
	for _, key := range keys {
		headers = append(headers, acp.HttpHeader{Name: key, Value: values[key]})
	}
	return headers
}

// NewForkHandler 将产品分叉请求转换为环境层的窄协议事实。
func NewForkHandler(catalog *runtimecatalog.Catalog) func(context.Context, runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error) {
	return func(ctx context.Context, request runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error) {
		if err := runtimecontract.ValidateExecutionPolicy(request.Plan.Policy.Execution); err != nil {
			return runtimecontract.ForkSessionResult{}, err
		}
		workspace, err := runtimecontract.Validate(request.Plan.Workspace)
		if err != nil {
			return runtimecontract.ForkSessionResult{}, err
		}
		connectors := make([]runtimecontract.Connector, 0, len(request.Plan.Connectors))
		for _, declaration := range request.Plan.Connectors {
			connectors = append(connectors, runtimecontract.Connector{ID: declaration.ID, Name: declaration.Name, RuntimeKey: declaration.RuntimeKey,
				Transport: declaration.Transport, Command: declaration.Command, Arguments: declaration.Arguments, URL: declaration.URL, TimeoutMS: declaration.TimeoutMS})
		}
		servers, err := PrepareForkMCPServers(connectors)
		if err != nil {
			return runtimecontract.ForkSessionResult{}, err
		}
		result, err := catalog.ForkSession(ctx, runtimecatalog.ForkRequest{
			Target: request.Plan.Target.LaunchTarget(), Detection: request.Plan.Detection,
			Workspace:        agentcontract.WorkspaceScope{Root: workspace.Path, Directories: workspace.AdditionalWritableDirectories},
			ManagedWorkspace: workspace.Managed, PermissionLevel: request.Plan.Policy.Execution.PermissionLevel, MCPServers: servers,
			SourceSessionID: request.SourceSessionID, Position: request.Position, SourceWorkspace: request.SourceWorkspace,
			ReceiptDirectory: request.ReceiptDirectory, ReconcileOnly: request.ReconcileOnly,
		})
		if err != nil {
			return runtimecontract.ForkSessionResult{}, err
		}
		return runtimecontract.ForkSessionResult{SessionID: result.SessionID, Mode: result.Mode}, nil
	}

}

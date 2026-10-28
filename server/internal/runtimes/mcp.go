package runtimes

import (
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// BaseRuntimeIDs 返回连接器兼容矩阵使用的基础 Runtime 列表，顺序稳定便于 UI 展示。
func BaseRuntimeIDs() []string {
	return []string{
		RuntimeIDOpenCode,
		RuntimeIDGemini,
		RuntimeIDGrok,
		RuntimeIDWorkBuddy,
		RuntimeIDQoder,
		RuntimeIDQoderCN,
		RuntimeIDCodex,
		RuntimeIDClaude,
		RuntimeIDKimi,
		RuntimeIDCursor,
		RuntimeIDPi,
	}
}

type mcpPreparation struct {
	// compatible 是经过校验且可以注入当前 Runtime 的连接器。
	compatible []Connector
}

// ConnectorCompatibility 返回一个基础 Runtime 是否支持指定 MCP 传输方式。
func ConnectorCompatibility(
	baseRuntimeID string,
	transport ConnectorTransport,
) (bool, string) {
	binding, exists := defaultACPRuntimeBindings()[baseRuntimeID]
	if exists && binding.supportsMCPTransport != nil && binding.supportsMCPTransport(transport) {
		return true, ""
	}
	return false, "transport_not_supported"
}

// DetectedConnectorTransportSupported 只依据同一份探测快照判断 MCP 传输是否可用于新执行。
func DetectedConnectorTransportSupported(
	detection DetectionResult,
	transport ConnectorTransport,
) bool {
	switch transport {
	case ConnectorTransportStdio:
		return detection.State == DetectionStateReady
	case ConnectorTransportStreamableHTTP:
		return detection.Capabilities.Has(CapabilityMCPStreamableHTTP)
	case ConnectorTransportSSE:
		return detection.Capabilities.Has(CapabilityMCPSSE)
	default:
		return false
	}
}

// prepareACPMCPConnectors 只执行 ACP Client 需要的通用校验、兼容过滤和选择投影。
func prepareACPMCPConnectors(
	capabilities acp.McpCapabilities,
	connectors []Connector,
) (mcpPreparation, error) {
	return prepareMCPConnectorSet(connectors, func(transport ConnectorTransport) bool {
		switch transport {
		case ConnectorTransportStdio:
			return true
		case ConnectorTransportStreamableHTTP:
			return capabilities.Http
		case ConnectorTransportSSE:
			return capabilities.Sse
		default:
			return false
		}
	})
}

// prepareMCPConnectorSet 建立 Runtime 无关的 MCP 连接器事实。
func prepareMCPConnectorSet(
	connectors []Connector,
	supported func(ConnectorTransport) bool,
) (mcpPreparation, error) {
	compatible := make([]Connector, 0, len(connectors))
	seenIDs := make(map[string]struct{}, len(connectors))
	seenRuntimeKeys := make(map[string]struct{}, len(connectors))
	for _, connector := range connectors {
		if strings.TrimSpace(connector.ID) == "" ||
			strings.TrimSpace(connector.Name) == "" ||
			!validRuntimeConnectorKey(connector.RuntimeKey) {
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
		if err := validateConnector(connector); err != nil {
			return mcpPreparation{}, err
		}
		compatible = append(compatible, connector)
	}

	return mcpPreparation{
		compatible: compatible,
	}, nil
}

func validateConnector(connector Connector) error {
	switch connector.Transport {
	case ConnectorTransportStdio:
		if strings.TrimSpace(connector.Command) == "" ||
			strings.TrimSpace(connector.URL) != "" ||
			len(connector.Headers) > 0 {
			return status_error.BadRequest
		}
	case ConnectorTransportStreamableHTTP, ConnectorTransportSSE:
		if strings.TrimSpace(connector.URL) == "" ||
			strings.TrimSpace(connector.Command) != "" ||
			len(connector.Arguments) > 0 ||
			len(connector.Environment) > 0 {
			return status_error.BadRequest
		}
	default:
		return status_error.BadRequest
	}
	return nil
}

// ValidateConnectorConfiguration 在入口接纳前复用 Runtime 的名称与传输契约，不启动连接器。
func ValidateConnectorConfiguration(connector Connector) error {
	if strings.TrimSpace(connector.ID) == "" || strings.TrimSpace(connector.Name) == "" || !validRuntimeConnectorKey(connector.RuntimeKey) {
		return status_error.BadRequest
	}
	return validateConnector(connector)
}

func validRuntimeConnectorKey(value string) bool {
	if len(value) == 0 || len(value) > 64 || !strings.HasPrefix(value, "ally_") {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return character != '_' && character != '-' &&
			(character < '0' || character > '9') &&
			(character < 'a' || character > 'z')
	}) == -1
}

// acpMCPServers 转换经过兼容性校验的 Connector。
func acpMCPServers(
	connectors []Connector,
) ([]acp.McpServer, error) {
	servers := make([]acp.McpServer, 0, len(connectors))
	for _, connector := range connectors {
		switch connector.Transport {
		case ConnectorTransportStdio:
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
		case ConnectorTransportStreamableHTTP:
			servers = append(servers, acp.McpServer{
				Http: &acp.McpServerHttpInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		case ConnectorTransportSSE:
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

package runtimes

import (
	"fmt"
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
	// events 是连接器因 Runtime 不兼容而未注入时的用户可见说明。
	events []Event
	// selected 是已兼容注入且被用户在当前消息中明确引用的连接器。
	selected []Connector
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
	selected := make([]Connector, 0, len(connectors))
	events := []Event{}
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
			event, err := newEvent(EventTypeCommentaryDelta, map[string]any{
				"text": fmt.Sprintf(
					"连接器“%s”未注入：当前 Runtime 不支持 %s 传输。\n",
					connector.Name,
					connector.Transport,
				),
			})
			if err != nil {
				return mcpPreparation{}, err
			}
			events = append(events, event)
			continue
		}
		if err := validateConnector(connector); err != nil {
			return mcpPreparation{}, err
		}
		compatible = append(compatible, connector)
		if connector.Selected {
			selected = append(selected, connector)
		}
	}

	return mcpPreparation{
		compatible: compatible,
		events:     events,
		selected:   selected,
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

// applySelectedConnectorHint 把当前回合显式引用的连接器表达为不含凭据的使用提示。
func applySelectedConnectorHint(message string, connectors []Connector) string {
	if len(connectors) == 0 {
		return message
	}
	var builder strings.Builder
	builder.WriteString(message)
	if message != "" {
		builder.WriteString("\n\n")
	}
	builder.WriteString(
		"Use these explicitly referenced MCP connectors for this request when relevant:\n",
	)
	for _, connector := range connectors {
		fmt.Fprintf(
			&builder,
			"- %s (%s)\n",
			connector.RuntimeKey,
			connector.Name,
		)
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

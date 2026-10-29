package contract

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"strings"
)

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

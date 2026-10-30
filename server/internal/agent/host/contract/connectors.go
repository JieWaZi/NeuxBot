package contract

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"strings"
)

// DetectedConnectorTransportSupported 只依据同一份探测快照判断 MCP 传输是否可用于新执行。
func DetectedConnectorTransportSupported(
	detection runtimeconfig.DetectionResult,
	transport runtimeconfig.ConnectorTransport,
) bool {
	switch transport {
	case runtimeconfig.ConnectorTransportStdio:
		return detection.State == runtimeconfig.DetectionStateReady
	case runtimeconfig.ConnectorTransportStreamableHTTP:
		return detection.Capabilities.Has(runtimeconfig.CapabilityMCPStreamableHTTP)
	case runtimeconfig.ConnectorTransportSSE:
		return detection.Capabilities.Has(runtimeconfig.CapabilityMCPSSE)
	default:
		return false
	}
}

func validateConnector(connector Connector) error {
	switch connector.Transport {
	case runtimeconfig.ConnectorTransportStdio:
		if strings.TrimSpace(connector.Command) == "" ||
			strings.TrimSpace(connector.URL) != "" ||
			len(connector.Headers) > 0 {
			return status_error.BadRequest
		}
	case runtimeconfig.ConnectorTransportStreamableHTTP, runtimeconfig.ConnectorTransportSSE:
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

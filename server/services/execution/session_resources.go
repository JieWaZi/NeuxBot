package execution

import (
	"encoding/json"
	"slices"
	"strings"

	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// sessionResourceKey 用保存的配置事实区分资源，不把明文凭据或测试结果带入会话声明。
func sessionResourceKey(pluginKey, workspaceConfiguration string, connectors []models.MCPConnector) string {
	connectors = slices.Clone(connectors)
	slices.SortFunc(connectors, func(a, b models.MCPConnector) int { return strings.Compare(a.ID, b.ID) })
	identities := make([]any, 0, len(connectors))
	for _, connector := range connectors {
		nativeName := connector.RuntimeKey
		if nativeName == "" {
			nativeName = connector.Name
		}
		environment, headers := slices.Clone(connector.Environment), slices.Clone(connector.Headers)
		for _, definitions := range [][]models.MCPSecretDefinition{environment, headers} {
			slices.SortFunc(definitions, func(a, b models.MCPSecretDefinition) int { return strings.Compare(a.Name, b.Name) })
		}
		// Value 的 JSON 契约明确排除明文；只纳入持久定义与密文，忽略刷新和探测状态。
		identities = append(identities, []any{connector.ID, nativeName, connector.Transport, connector.Command, connector.Arguments, connector.URL, connector.TimeoutMS, environment, headers})
	}
	body, _ := json.Marshal([]any{pluginKey, workspaceConfiguration, identities})
	return agentIdentity(string(body))
}

// runtimeConnectorDeclarations 使用实际原生名称声明配置，不把管理展示名写入会话 metadata。
func runtimeConnectorDeclarations(connectors []models.MCPConnector) []agentapi.ConnectorDeclaration {
	result := make([]agentapi.ConnectorDeclaration, 0, len(connectors))
	for _, connector := range connectors {
		nativeName := connector.RuntimeKey
		if nativeName == "" {
			nativeName = connector.Name
		}
		result = append(result, agentapi.ConnectorDeclaration{
			ID: connector.ID, Name: nativeName, RuntimeKey: connector.RuntimeKey,
			Transport: agentapi.ConnectorTransport(connector.Transport), Command: connector.Command,
			Arguments: slices.Clone(connector.Arguments), URL: connector.URL, TimeoutMS: connector.TimeoutMS,
		})
	}
	return result
}

// runtimeConnectorResources 只在冷获取时展开实际连接秘密，本轮显式选择由实际输入保存。
func runtimeConnectorResources(connectors []models.MCPConnector) []agentapi.Connector {
	result := make([]agentapi.Connector, 0, len(connectors))
	for _, connector := range connectors {
		result = append(result, agentapi.Connector{
			ID: connector.ID, Name: connector.Name, RuntimeKey: connector.RuntimeKey,
			Transport: agentapi.ConnectorTransport(connector.Transport), Command: connector.Command,
			Arguments: slices.Clone(connector.Arguments), URL: connector.URL, TimeoutMS: connector.TimeoutMS,
			Environment: connectorSecretValues(connector.Environment), Headers: connectorSecretValues(connector.Headers),
		})
	}
	return result
}

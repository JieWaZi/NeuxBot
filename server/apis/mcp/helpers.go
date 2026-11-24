package mcp

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mcpconnector"
)

func connectorService() *mcpconnector.Service {
	return mcpconnector.New(mcpconnector.Config{
		Connectors:   global.MCPConnectorControllerManager,
		Wakers:       global.WakerControllerManager,
		Transactions: global.TransactionControllerManager,
		Categories:   global.ResourceCategoryControllerManager,
		Events:       global.EventHub,
		Probe:        runMCPConnectionTest,
	})
}

func connectorDefinition(input ConnectorInput) mcpconnector.Definition {
	environment := make([]mcpconnector.SecretInput, 0, len(input.Environment))
	for _, entry := range input.Environment {
		environment = append(environment, mcpconnector.SecretInput(entry))
	}
	headers := make([]mcpconnector.SecretInput, 0, len(input.Headers))
	for _, entry := range input.Headers {
		headers = append(headers, mcpconnector.SecretInput(entry))
	}
	return mcpconnector.Definition{
		Categories: input.Categories, Name: input.Name, Description: input.Description, Transport: input.Transport,
		Command: input.Command, Arguments: input.Arguments, Environment: environment, URL: input.URL,
		Headers: headers, TimeoutMS: input.TimeoutMS, TrustConfirmed: input.TrustConfirmed,
	}
}

func visibleConnector(ctx context.Context, wakerID, connectorID string) (models.MCPConnectorWithState, error) {
	return connectorService().Visible(ctx, wakerID, connectorID)
}

func connectorResponses(
	ctx context.Context,
	items []models.MCPConnectorWithState,
) ([]ConnectorResponse, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Connector.ID)
	}
	counts, err := global.MCPConnectorControllerManager.InstalledCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]ConnectorResponse, 0, len(items))
	for _, item := range items {
		result = append(result, newConnectorResponse(item, counts[item.Connector.ID]))
	}
	return result, nil
}

func connectorResponse(ctx context.Context, item models.MCPConnectorWithState) (ConnectorResponse, error) {
	result, err := connectorResponses(ctx, []models.MCPConnectorWithState{item})
	if err != nil {
		return ConnectorResponse{}, err
	}
	return result[0], nil
}

func newConnectorResponse(
	item models.MCPConnectorWithState,
	affectedWakerCount int64,
) ConnectorResponse {
	connector := item.Connector
	activationPolicy := constants.MCPActivationPolicyPerExecution
	enabled := item.Enabled

	return ConnectorResponse{
		Connector: ConnectorResource{
			ID:               connector.ID,
			Categories:       append([]string{}, connector.Categories...),
			OwnerWakerID:     connector.OwnerWakerID,
			Name:             connector.Name,
			Description:      connector.Description,
			Scope:            connector.Scope,
			ActivationPolicy: activationPolicy,
			Transport:        connector.Transport,
			Command:          connector.Command,
			Arguments:        append([]string{}, connector.Arguments...),
			Environment:      publicSecretDefinitions(connector.Environment),
			URL:              connector.URL,
			Headers:          publicSecretDefinitions(connector.Headers),
			TimeoutMS:        connector.TimeoutMS,
			TrustedAt:        connector.TrustedAt,
			TestStatus:       connector.TestStatus,
			TestedAt:         connector.TestedAt,
			TestDurationMS:   connector.TestDurationMS,
			TestToolCount:    connector.TestToolCount,
			TestError:        connector.TestError,
			TestTools:        append([]models.MCPToolSummary{}, connector.TestTools...),
			CreatedAt:        connector.CreatedAt,
			UpdatedAt:        connector.UpdatedAt,
		},
		Enabled:            enabled,
		AffectedWakerCount: affectedWakerCount,
		Compatibility:      ConnectorCompatibility(connector.Transport),
	}
}

func publicSecretDefinitions(
	definitions []models.MCPSecretDefinition,
) []SecretDefinition {
	result := make([]SecretDefinition, 0, len(definitions))
	for _, definition := range definitions {
		item := SecretDefinition{
			ID:         definition.ID,
			Name:       definition.Name,
			Configured: definition.Configured,
		}
		result = append(result, item)
	}
	return result
}

// ConnectorCompatibility 按既有智能体传输规则生成公开结论，供独立和插件连接器共同展示。
func ConnectorCompatibility(
	transport constants.MCPTransport,
) []RuntimeCompatibility {
	runtimeIDs := agentapi.BaseRuntimeIDs()
	result := make([]RuntimeCompatibility, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		supported, reason := agentapi.ConnectorCompatibility(
			runtimeID,
			agentapi.ConnectorTransport(transport),
		)
		result = append(result, RuntimeCompatibility{
			RuntimeID: runtimeID,
			Supported: supported,
			Reason:    reason,
		})
	}
	return result
}

func notifyResourceChanged(ctx context.Context, id string) {
	mcpconnector.PublishChange(ctx, global.EventHub, id)
}

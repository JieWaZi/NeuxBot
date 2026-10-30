package mcp

import (
	catalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/mozillazg/go-pinyin"
)

func visibleConnector(
	ctx context.Context,
	wakerID string,
	connectorID string,
) (models.MCPConnectorWithState, error) {
	if wakerID == "" {
		connector, err := global.MCPConnectorControllerManager.Get(ctx, connectorID)
		if err != nil {
			return models.MCPConnectorWithState{}, err
		}
		if connector.OwnerWakerID != "" {
			return models.MCPConnectorWithState{}, status_error.MCPConnectorNotFound
		}
		return models.MCPConnectorWithState{Connector: connector}, nil
	}
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return models.MCPConnectorWithState{}, err
	}
	items, err := global.MCPConnectorControllerManager.ListForWaker(ctx, wakerID, "")
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	for _, item := range items {
		if item.Connector.ID == connectorID {
			return item, nil
		}
	}
	return models.MCPConnectorWithState{}, status_error.MCPConnectorNotFound.WithParams(
		map[string]interface{}{"ConnectorID": connectorID},
	)
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

func prepareConnector(
	base models.MCPConnector,
	input ConnectorInput,
	existing *models.MCPConnector,
) (models.MCPConnector, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Command = strings.TrimSpace(input.Command)
	input.URL = strings.TrimSpace(input.URL)
	if input.TimeoutMS == 0 {
		input.TimeoutMS = constants.MCPConnectorDefaultTimeoutMS
	}
	if input.Name == "" || len(input.Name) > 120 ||
		input.TimeoutMS < constants.MCPConnectorMinimumTimeoutMS ||
		input.TimeoutMS > constants.MCPConnectorMaximumTimeoutMS {
		return base, status_error.BadRequest
	}
	if err := validateTransportInput(input); err != nil {
		return base, err
	}

	environment, err := prepareSecretDefinitions(
		input.Environment,
		existingDefinitions(existing, true),
		false,
	)
	if err != nil {
		return base, err
	}
	headers, err := prepareSecretDefinitions(
		input.Headers,
		existingDefinitions(existing, false),
		true,
	)
	if err != nil {
		return base, err
	}

	base.Name = input.Name
	base.Description = input.Description
	base.Transport = input.Transport
	base.Command = input.Command
	base.Arguments = append([]string{}, input.Arguments...)
	base.Environment = environment
	base.URL = input.URL
	base.Headers = headers
	base.TimeoutMS = input.TimeoutMS
	return base, nil
}

func prepareSecretDefinitions(
	inputs []SecretInput,
	existing []models.MCPSecretDefinition,
	isHeader bool,
) ([]models.MCPSecretDefinition, error) {
	definitions := make([]models.MCPSecretDefinition, 0, len(inputs))
	existingByID := make(map[string]models.MCPSecretDefinition, len(existing))
	existingByName := make(map[string]models.MCPSecretDefinition, len(existing))
	for _, definition := range existing {
		existingByID[definition.ID] = definition
		existingByName[normalizedSecretName(definition.Name, isHeader)] = definition
	}
	usedIDs := make(map[string]struct{}, len(inputs))
	usedNames := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" || input.Clear && input.Value != "" {
			return nil, status_error.BadRequest
		}
		nameKey := normalizedSecretName(name, isHeader)
		if _, exists := usedNames[nameKey]; exists {
			return nil, status_error.BadRequest
		}
		usedNames[nameKey] = struct{}{}

		previous, hasPrevious := existingByID[strings.TrimSpace(input.ID)]
		if !hasPrevious && input.ID == "" {
			previous, hasPrevious = existingByName[nameKey]
		}
		entryID := util.New()
		configured := false
		value := ""
		if hasPrevious {
			entryID = previous.ID
			configured = previous.Configured
			value = previous.Value
		}
		if input.Clear {
			configured = false
			value = ""
		} else if input.Value != "" {
			configured = true
			value = input.Value
		}
		if _, exists := usedIDs[entryID]; exists {
			return nil, status_error.BadRequest
		}
		usedIDs[entryID] = struct{}{}
		definitions = append(definitions, models.MCPSecretDefinition{
			ID:         entryID,
			Name:       name,
			Configured: configured,
			Value:      value,
		})
	}
	return definitions, nil
}

func validateTransportInput(input ConnectorInput) error {
	switch input.Transport {
	case constants.MCPTransportStdio:
		if input.Command == "" || input.URL != "" || len(input.Headers) > 0 {
			return status_error.BadRequest
		}
	case constants.MCPTransportStreamableHTTP, constants.MCPTransportSSE:
		if input.URL == "" || input.Command != "" ||
			len(input.Arguments) > 0 || len(input.Environment) > 0 {
			return status_error.BadRequest
		}
		parsed, err := url.ParseRequestURI(input.URL)
		if err != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return status_error.BadRequest
		}
	default:
		return status_error.BadRequest
	}
	return nil
}

func builtInDefinitionChanged(
	existing models.MCPConnector,
	prepared models.MCPConnector,
) bool {
	return existing.Name != prepared.Name ||
		existing.Description != prepared.Description ||
		existing.Transport != prepared.Transport ||
		existing.Command != prepared.Command ||
		!slices.Equal(existing.Arguments, prepared.Arguments) ||
		existing.URL != prepared.URL ||
		existing.TimeoutMS != prepared.TimeoutMS ||
		!secretDefinitionNamesEqual(existing.Environment, prepared.Environment) ||
		!secretDefinitionNamesEqual(existing.Headers, prepared.Headers)
}

func connectorConfigurationChanged(
	existing models.MCPConnector,
	prepared models.MCPConnector,
) bool {
	return existing.Transport != prepared.Transport ||
		existing.Command != prepared.Command ||
		!slices.Equal(existing.Arguments, prepared.Arguments) ||
		existing.URL != prepared.URL ||
		existing.TimeoutMS != prepared.TimeoutMS ||
		!reflect.DeepEqual(existing.Environment, prepared.Environment) ||
		!reflect.DeepEqual(existing.Headers, prepared.Headers)
}

func trustDigest(connector models.MCPConnector) string {
	type trustConfiguration struct {
		// Transport 是连接器使用的传输方式。
		Transport constants.MCPTransport `json:"transport"`
		// Command 是 STDIO 连接器可执行命令。
		Command string `json:"command"`
		// Arguments 是 STDIO 命令参数。
		Arguments []string `json:"arguments"`
		// EnvironmentNames 是 STDIO 环境变量名。
		EnvironmentNames []string `json:"environmentNames"`
		// URL 是远程 MCP 服务地址。
		URL string `json:"url"`
		// HeaderNames 是远程请求头名。
		HeaderNames []string `json:"headerNames"`
	}
	configuration := trustConfiguration{
		Transport:        connector.Transport,
		Command:          connector.Command,
		Arguments:        append([]string{}, connector.Arguments...),
		EnvironmentNames: secretDefinitionNames(connector.Environment),
		URL:              connector.URL,
		HeaderNames:      secretDefinitionNames(connector.Headers),
	}
	encoded, _ := json.Marshal(configuration)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func clearTestSnapshot(connector *models.MCPConnector) {
	connector.TestStatus = constants.MCPTestStatusUntested
	connector.TestedAt = 0
	connector.TestDurationMS = 0
	connector.TestToolCount = 0
	connector.TestError = ""
	connector.TestTools = []models.MCPToolSummary{}
}

// ConnectorCompatibility 按既有智能体传输规则生成公开结论，供独立和插件连接器共同展示。
func ConnectorCompatibility(
	transport constants.MCPTransport,
) []RuntimeCompatibility {
	runtimeIDs := catalog.BaseRuntimeIDs()
	result := make([]RuntimeCompatibility, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		supported, reason := catalog.ConnectorCompatibility(
			runtimeID,
			runtimeconfig.ConnectorTransport(transport),
		)
		result = append(result, RuntimeCompatibility{
			RuntimeID: runtimeID,
			Supported: supported,
			Reason:    reason,
		})
	}
	return result
}

func existingDefinitions(
	connector *models.MCPConnector,
	environment bool,
) []models.MCPSecretDefinition {
	if connector == nil {
		return []models.MCPSecretDefinition{}
	}
	if environment {
		return connector.Environment
	}
	return connector.Headers
}

func secretDefinitionNames(definitions []models.MCPSecretDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func secretDefinitionNamesEqual(
	left []models.MCPSecretDefinition,
	right []models.MCPSecretDefinition,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID || left[index].Name != right[index].Name {
			return false
		}
	}
	return true
}

func normalizedSecretName(name string, isHeader bool) string {
	if isHeader {
		return strings.ToLower(http.CanonicalHeaderKey(name))
	}
	return name
}

func bounded(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func boundedTools(tools []models.MCPToolSummary) []models.MCPToolSummary {
	limit := min(len(tools), constants.MCPConnectorMaximumToolCount)
	result := make([]models.MCPToolSummary, 0, limit)
	for _, tool := range tools[:limit] {
		name := bounded(tool.Name, 200)
		if name == "" {
			continue
		}
		result = append(result, models.MCPToolSummary{
			Name:        name,
			Description: bounded(tool.Description, constants.MCPConnectorMaximumSummaryLength),
		})
	}
	return result
}

func newPublicConnector(input ConnectorInput) (models.MCPConnector, error) {
	now := time.Now().UnixMilli()
	connector, err := prepareConnector(models.MCPConnector{
		ID:          util.New(),
		Scope:       constants.MCPConnectorScopePublic,
		TestStatus:  constants.MCPTestStatusUntested,
		Arguments:   []string{},
		Environment: []models.MCPSecretDefinition{},
		Headers:     []models.MCPSecretDefinition{},
		TestTools:   []models.MCPToolSummary{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}, input, nil)
	if err != nil {
		return connector, err
	}
	connector.RuntimeKey = newRuntimeKey(connector.Name, connector.ID)
	if !input.TrustConfirmed {
		return connector, status_error.MCPConnectorTrustRequired
	}
	connector.TrustedConfigDigest = trustDigest(connector)
	connector.TrustedAt = now
	return connector, nil
}

func newRuntimeKey(name string, connectorID string) string {
	segments := runtimeKeySegments(name)
	base := strings.Join(segments, "_")
	digest := sha256.Sum256([]byte(connectorID))
	suffix := hex.EncodeToString(digest[:])[:constants.MCPConnectorRuntimeKeySuffixLength]
	maximumBaseLength := constants.MCPConnectorMaximumRuntimeKeyLength - len("ally__") - len(suffix)
	if len(base) > maximumBaseLength {
		base = strings.Trim(base[:maximumBaseLength], "_")
	}
	if base == "" {
		base = "mcp"
	}
	return "ally_" + base + "_" + suffix
}

func runtimeKeySegments(name string) []string {
	arguments := pinyin.NewArgs()
	arguments.Style = pinyin.Normal
	segments := []string{}
	var ascii strings.Builder
	flushASCII := func() {
		if ascii.Len() == 0 {
			return
		}
		segments = append(segments, ascii.String())
		ascii.Reset()
	}
	for _, character := range strings.TrimSpace(name) {
		if character >= 'A' && character <= 'Z' {
			ascii.WriteRune(character + ('a' - 'A'))
			continue
		}
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' {
			ascii.WriteRune(character)
			continue
		}
		flushASCII()
		if !unicode.Is(unicode.Han, character) {
			continue
		}
		converted := pinyin.SinglePinyin(character, arguments)
		if len(converted) > 0 && converted[0] != "" {
			segments = append(segments, converted[0])
		}
	}
	flushASCII()
	return segments
}

// notifyResourceChanged 仅在对应资源写入生效后登记缓存失效。
func notifyResourceChanged(ctx context.Context, id string) {
	if global.EventHub == nil {
		return
	}
	resource := events.Resource{Kind: "connector", ID: id}
	if id == "" {
		resource = events.Resource{Kind: "global"}
	}
	events.AfterCommit(ctx, func() { _ = events.Emit(global.EventHub, "resources", "resources.changed", resource, resource) })
}

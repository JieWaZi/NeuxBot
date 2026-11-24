package mcpconnector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/resourcecategory"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/mozillazg/go-pinyin"
)

// SecretInput 描述用户界面提交的一个凭据定义。
type SecretInput struct {
	// ID 是原有定义的标识。
	ID string
	// Name 是环境变量名或请求头名。
	Name string
	// Value 是用户提交的新值。
	Value string
	// Clear 表示用户明确清除已保存的值。
	Clear bool
}

// Definition 是连接器的可编辑配置，供界面与当前执行命令复用。
type Definition struct {
	// Categories 是资源库中的分类。
	Categories []string
	// Name 是界面显示名称。
	Name string
	// Description 是连接器用途。
	Description string
	// Transport 是连接器的传输方式。
	Transport constants.MCPTransport
	// Command 是 STDIO 子进程命令。
	Command string
	// Arguments 是 STDIO 子进程参数。
	Arguments []string
	// Environment 是 STDIO 敏感配置定义。
	Environment []SecretInput
	// URL 是远程服务地址。
	URL string
	// Headers 是 HTTP 敏感请求头定义。
	Headers []SecretInput
	// TimeoutMS 是测试及启动超时。
	TimeoutMS int64
	// TrustConfirmed 表示可信界面上的配置获用户确认。
	TrustConfirmed bool
}

// Trusted 验证配置仍等于最近获用户信任的状态。
func Trusted(connector models.MCPConnector) bool {
	return connector.Scope == constants.MCPConnectorScopeBuiltIn || trustDigest(connector) == connector.TrustedConfigDigest
}

func prepareConnector(
	base models.MCPConnector,
	input Definition,
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

	categories, err := resourcecategory.Normalize(input.Categories)
	if err != nil {
		return base, err
	}
	base.Categories = categories
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

func validateTransportInput(input Definition) error {
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

func newPublicConnector(input Definition) (models.MCPConnector, error) {
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
	return connector, nil
}

func newRuntimeKey(name string, connectorID string) string {
	segments := runtimeKeySegments(name)
	base := strings.Join(segments, "_")
	digest := sha256.Sum256([]byte(connectorID))
	suffix := hex.EncodeToString(digest[:])[:constants.MCPConnectorRuntimeKeySuffixLength]
	maximumBaseLength := constants.MCPConnectorMaximumRuntimeKeyLength - len("bot__") - len(suffix)
	if len(base) > maximumBaseLength {
		base = strings.Trim(base[:maximumBaseLength], "_")
	}
	if base == "" {
		base = "mcp"
	}
	return "bot_" + base + "_" + suffix
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

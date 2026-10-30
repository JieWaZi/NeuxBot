package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpCatalogOptions 返回 ACP 探测得到的模型与推理目录副本，不再注入 Default 占位项。
func acpCatalogOptions(catalog acpOptionCatalog) ([]runtimeconfig.ModelOption, []runtimeconfig.ReasoningOption) {
	return cloneModels(catalog.Models), cloneReasoningOptions(catalog.ReasoningOptions)
}

// acpInitializeCatalog 消费 ACP initialize 与通用 Runtime 元数据，并补充 Binding 声明的 Skill 能力。
func acpInitializeCatalog(
	binding acpRuntimeBinding,
	response acp.InitializeResponse,
) acpOptionCatalog {
	capabilities := response.AgentCapabilities
	result := runtimeconfig.CapabilitySet{
		runtimeconfig.CapabilityPartialMessages: true,
	}
	if capabilities.LoadSession || capabilities.SessionCapabilities.Resume != nil {
		result[runtimeconfig.CapabilityResumeSession] = true
	}
	if capabilities.SessionCapabilities.AdditionalDirectories != nil {
		result[runtimeconfig.CapabilityAddDirectory] = true
	}
	if capabilities.PromptCapabilities.Image {
		result[runtimeconfig.CapabilityImagePath] = true
	}
	if binding.descriptor.SkillDirectory {
		result[runtimeconfig.CapabilitySkillDirectory] = true
	}
	if capabilities.McpCapabilities.Http {
		result[runtimeconfig.CapabilityMCPStreamableHTTP] = true
	}
	if capabilities.McpCapabilities.Sse {
		result[runtimeconfig.CapabilityMCPSSE] = true
	}
	if acpSteeringSupported(response.Meta) {
		result[runtimeconfig.CapabilityStreamingUserMessages] = true
	}
	version := ""
	if response.AgentInfo != nil {
		version = acpmeta.RuntimeVersion(response.AgentInfo.Meta)
		if version == "" && binding.descriptor.NativeProcess && !binding.descriptor.NativeVersionMetadataOnly {
			version = response.AgentInfo.Version
		}
	}
	return acpOptionCatalog{
		AgentConnected: true,
		Version:        version,
		Capabilities:   result,
	}
}

// acpSteeringSupported 读取 upstream Adapter 通过 ACP 扩展元数据声明的 steering 能力。
func acpSteeringSupported(meta map[string]any) bool {
	steering, ok := meta[constants.RuntimeACPMetaKeySteering].(map[string]any)
	if !ok {
		return false
	}
	supported, _ := steering[constants.RuntimeACPMetaKeySupported].(bool)
	return supported
}

// detectSessionOptions 优先读取模型专属目录；非原生适配器通过切换模型取得配置。
func (agent *catalogDecoder) detectSessionOptions(
	ctx context.Context,
	sessionID acp.SessionId,
	initialOptions []acp.SessionConfigOption,
	modes []acp.SessionMode,
) (acpOptionCatalog, error) {
	modelConfig, ok := acpSelectConfig(initialOptions, agent.binding.descriptor.ModelConfigID, acp.SessionConfigOptionCategoryModel)
	if !ok {
		if agent.binding.descriptor.NativeProcess {
			return acpOptionCatalog{PermissionLevels: agent.binding.permissionLevels(initialOptions, modes)}, nil
		}
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}
	modelOptions := acpSelectValues(modelConfig.Options)
	if len(modelOptions) == 0 {
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}

	catalog := acpOptionCatalog{
		Models:           make([]runtimeconfig.ModelOption, 0, len(modelOptions)),
		PermissionLevels: agent.binding.permissionLevels(initialOptions, modes),
		ReasoningOptions: acpReasoningOptions(
			initialOptions,
			agent.binding.descriptor.ReasoningConfigID,
		),
		DefaultModel:     strings.TrimSpace(string(modelConfig.CurrentValue)),
		DefaultReasoning: detectedDefaultReasoning(initialOptions, agent.binding.descriptor.ReasoningConfigID, modelConfig),
	}
	seen := make(map[string]struct{}, len(modelOptions))
	var detectionErr error
	for _, option := range modelOptions {
		modelID := strings.TrimSpace(string(option.Value))
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}

		configOptions := initialOptions
		if values, ok := agent.models[modelID]; ok {
			configOptions = values
		}

		name := strings.TrimSpace(option.Name)
		if name == "" {
			name = modelID
		}
		if agent.binding.modelDisplayName != nil {
			name = agent.binding.modelDisplayName(option, name)
		}
		reasoning := acpReasoningOptions(configOptions, agent.binding.descriptor.ReasoningConfigID)
		if modelOptions, known := acpmeta.ModelConfigOptions(option.Meta); known {
			reasoning = acpReasoningOptions(modelOptions, agent.binding.descriptor.ReasoningConfigID)
		} else if _, known := agent.models[modelID]; !known && option.Value != modelConfig.CurrentValue {
			// 未提供模型专属目录时，不把当前模型的强度套用于其他模型。
			reasoning = nil
		}
		catalog.Models = append(catalog.Models, runtimeconfig.ModelOption{
			ID:               modelID,
			Name:             name,
			ReasoningOptions: reasoning,
			PermissionLevels: agent.binding.permissionLevels(configOptions, modes),
		})
	}
	if agent.binding.catalogOptions != nil {
		catalog = agent.binding.catalogOptions(catalog)
	}
	if len(catalog.Models) == 0 {
		return catalog, status_error.RuntimeUnavailable
	}
	return catalog, detectionErr
}

// detectedDefaultReasoning 读取探测 Session 当前模型生效的推理强度。
func detectedDefaultReasoning(
	options []acp.SessionConfigOption,
	reasoningConfigID acp.SessionConfigId,
	modelConfig *acp.SessionConfigOptionSelect,
) string {
	if config, ok := acpSelectConfig(options, reasoningConfigID, acp.SessionConfigOptionCategoryThoughtLevel); ok {
		if value := strings.TrimSpace(string(config.CurrentValue)); value != "" {
			return value
		}
	}
	for _, option := range acpSelectValues(modelConfig.Options) {
		if option.Value != modelConfig.CurrentValue {
			continue
		}
		if configs, known := acpmeta.ModelConfigOptions(option.Meta); known {
			if config, ok := acpSelectConfig(configs, reasoningConfigID, acp.SessionConfigOptionCategoryThoughtLevel); ok {
				return strings.TrimSpace(string(config.CurrentValue))
			}
		}
		break
	}
	return ""
}

func hasAnyMode(available map[string]struct{}, ids ...string) bool {
	for _, id := range ids {
		if _, exists := available[id]; exists {
			return true
		}
	}
	return false
}

// acpReasoningOptions 读取当前 Adapter 推理配置中的平铺选项。
func acpReasoningOptions(
	options []acp.SessionConfigOption,
	configID acp.SessionConfigId,
) []runtimeconfig.ReasoningOption {
	config, ok := acpSelectConfig(options, configID, acp.SessionConfigOptionCategoryThoughtLevel)
	if !ok {
		return nil
	}
	values := acpSelectValues(config.Options)
	result := make([]runtimeconfig.ReasoningOption, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, option := range values {
		id := strings.TrimSpace(string(option.Value))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(option.Name)
		if name == "" {
			name = id
		}
		result = append(result, runtimeconfig.ReasoningOption{ID: id, Name: name})
	}
	return result
}

// acpSelectConfig 按 Adapter 声明的稳定配置标识查找 select 配置。
func acpSelectConfig(
	options []acp.SessionConfigOption,
	id acp.SessionConfigId,
	category acp.SessionConfigOptionCategory,
) (*acp.SessionConfigOptionSelect, bool) {
	for _, option := range options {
		if option.Select != nil && option.Select.Category != nil && *option.Select.Category == category {
			return option.Select, true
		}
	}
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id && option.Select.Category == nil {
			return option.Select, true
		}
	}
	return nil, false
}

// acpSelectValues 把 ACP 平铺或分组的 select 值转换为同一有序列表。
func acpSelectValues(
	options acp.SessionConfigSelectOptions,
) []acp.SessionConfigSelectOption {
	if options.Ungrouped != nil {
		return append([]acp.SessionConfigSelectOption(nil), (*options.Ungrouped)...)
	}
	if options.Grouped == nil {
		return nil
	}
	var result []acp.SessionConfigSelectOption
	for _, group := range *options.Grouped {
		result = append(result, group.Options...)
	}
	return result
}

// acpConfigCurrentValue 读取 Agent 确认的配置值，避免将本地选择误当作生效结果。
func acpConfigCurrentValue(options []acp.SessionConfigOption, id acp.SessionConfigId) acp.SessionConfigValueId {
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id {
			return option.Select.CurrentValue
		}
	}
	return ""
}

// catalogDecoder只解释已读取目录，不拥有协议连接。
type catalogDecoder struct {
	// binding 是产品展示和权限映射规则。
	binding acpRuntimeBinding
	// models 是管理连接已确认的模型专属原始配置。
	models map[string][]acp.SessionConfigOption
}

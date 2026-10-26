package runtimes

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpCatalogOptions 返回 ACP 探测得到的模型与推理目录副本，不再注入 Default 占位项。
func acpCatalogOptions(catalog acpOptionCatalog) ([]ModelOption, []ReasoningOption) {
	return cloneModels(catalog.Models), cloneReasoningOptions(catalog.ReasoningOptions)
}

// detectOptions 创建不执行 Prompt 的临时 ACP Session，并读取全部模型对应的配置。
func (agent *embeddedACPAgent) detectOptions(
	ctx context.Context,
	workingDirectory string,
	selectedModel ...string,
) (catalog acpOptionCatalog, detectionErr error) {
	catalog = acpInitializeCatalog(agent.binding, agent.initializeResponse)
	response, err := agent.connection.NewSession(ctx, acp.NewSessionRequest{
		Cwd:                   workingDirectory,
		AdditionalDirectories: []string{},
		McpServers:            []acp.McpServer{},
	})
	if err != nil {
		return catalog, err
	}
	if response.SessionId == "" {
		return catalog, status_error.RuntimeUnavailable
	}

	defer func() {
		closeContext, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), acpDeliveryDrainTimeout)
		defer cancelClose()
		if _, closeErr := agent.connection.CloseSession(closeContext, acp.CloseSessionRequest{SessionId: response.SessionId}); closeErr != nil {
			delete(catalog.Capabilities, CapabilityIsolatedTextGeneration)
			logx.Errorf("close ACP option detection session failed: session_id=%s error=%v", response.SessionId, closeErr)
			if detectionErr == nil {
				detectionErr = status_error.RuntimeUnavailable
			}
		}
	}()
	catalog.SessionReady = true
	if response.Modes != nil {
		catalog.Modes = append([]acp.SessionMode(nil), response.Modes.AvailableModes...)
	}

	// 定向探测只在即将关闭的独占会话切换模型，不修改其他会话或真实默认配置。
	if len(selectedModel) > 0 && selectedModel[0] != "" {
		updated, selectErr := agent.connection.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: response.SessionId, ConfigId: agent.binding.modelConfigID, Value: acp.SessionConfigValueId(selectedModel[0])}})
		if selectErr != nil {
			return catalog, selectErr
		}
		if acpConfigCurrentValue(updated.ConfigOptions, agent.binding.modelConfigID) != acp.SessionConfigValueId(selectedModel[0]) {
			return catalog, status_error.RuntimeProtocolIncompatible
		}
		response.ConfigOptions = updated.ConfigOptions
	}
	options, detectionErr := agent.detectSessionOptions(
		ctx,
		response.SessionId,
		response.ConfigOptions,
		catalog.Modes,
	)
	if detectionErr == nil && agent.binding.probeModelOptions {
		options, detectionErr = agent.detectMissingModelOptions(ctx, response.SessionId, response.ConfigOptions, catalog.Modes, options)
	}
	catalog.Models = options.Models
	catalog.ReasoningOptions = options.ReasoningOptions
	catalog.DefaultModel = options.DefaultModel
	catalog.DefaultReasoning = options.DefaultReasoning
	catalog.PermissionLevels = options.PermissionLevels
	// 能力来自本次临时会话的实际配置；不能从 ready、厂商名称或旧缓存推断。
	// 模式配置失败只限制隔离生成，不改变普通聊天的模型目录和就绪事实。
	if detectionErr == nil {
		mode, modeErr := agent.binding.modeID(ExecutionPolicy{PermissionLevel: PermissionLevelDefault, WorkspaceReadOnly: true}, response.Modes)
		if modeErr == nil && mode != "" {
			_, modeErr = agent.connection.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: response.SessionId, ModeId: mode})
		}
		if modeErr == nil {
			catalog.Capabilities[CapabilityIsolatedTextGeneration] = true
		} else {
			switch acpFailureCode(modeErr) {
			case FailureCodeProtocolIncompatible, FailureCodePolicyUnsupported, FailureCodeInvalidRequest:
				catalog.Capabilities[CapabilityIsolatedTextGeneration] = false
			}
		}
	}
	return catalog, detectionErr
}

// detectMissingModelOptions 为没有专属元数据的可见模型补读实际配置，整个过程不发送 Prompt。
func (agent *embeddedACPAgent) detectMissingModelOptions(
	ctx context.Context,
	sessionID acp.SessionId,
	initialOptions []acp.SessionConfigOption,
	modes []acp.SessionMode,
	catalog acpOptionCatalog,
) (acpOptionCatalog, error) {
	modelConfig, ok := acpSelectConfig(initialOptions, agent.binding.modelConfigID)
	if !ok {
		return catalog, nil
	}
	known := map[string]bool{string(modelConfig.CurrentValue): true}
	for _, option := range acpSelectValues(modelConfig.Options) {
		if _, provided := acpmeta.ModelConfigOptions(option.Meta); provided {
			known[string(option.Value)] = true
		}
	}
	for index, model := range catalog.Models {
		if known[model.ID] {
			continue
		}
		updated, err := agent.connection.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: sessionID, ConfigId: agent.binding.modelConfigID, Value: acp.SessionConfigValueId(model.ID),
		}})
		if err != nil {
			return catalog, err
		}
		if acpConfigCurrentValue(updated.ConfigOptions, agent.binding.modelConfigID) != acp.SessionConfigValueId(model.ID) {
			return catalog, status_error.RuntimeProtocolIncompatible
		}
		selected, err := agent.detectSessionOptions(ctx, sessionID, updated.ConfigOptions, modes)
		if err != nil {
			return catalog, err
		}
		found := false
		for _, candidate := range selected.Models {
			if candidate.ID == model.ID {
				catalog.Models[index] = candidate
				found = true
				break
			}
		}
		if !found {
			return catalog, status_error.RuntimeProtocolIncompatible
		}
	}
	return catalog, nil
}

// acpInitializeCatalog 消费 ACP initialize 与通用 Runtime 元数据，并补充 Binding 声明的 Skill 能力。
func acpInitializeCatalog(
	binding acpRuntimeBinding,
	response acp.InitializeResponse,
) acpOptionCatalog {
	capabilities := response.AgentCapabilities
	result := CapabilitySet{
		CapabilityPartialMessages: true,
	}
	if capabilities.LoadSession || capabilities.SessionCapabilities.Resume != nil {
		result[CapabilityResumeSession] = true
	}
	if capabilities.SessionCapabilities.AdditionalDirectories != nil {
		result[CapabilityAddDirectory] = true
	}
	if capabilities.PromptCapabilities.Image {
		result[CapabilityImagePath] = true
	}
	if binding.skillDirectory {
		result[CapabilitySkillDirectory] = true
	}
	if capabilities.McpCapabilities.Http {
		result[CapabilityMCPStreamableHTTP] = true
	}
	if capabilities.McpCapabilities.Sse {
		result[CapabilityMCPSSE] = true
	}
	if acpSteeringSupported(response.Meta) {
		result[CapabilityStreamingUserMessages] = true
	}
	version := ""
	if response.AgentInfo != nil {
		version = acpmeta.RuntimeVersion(response.AgentInfo.Meta)
		if version == "" && binding.nativeProcess && !binding.nativeVersionMetadataOnly {
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
func (agent *embeddedACPAgent) detectSessionOptions(
	ctx context.Context,
	sessionID acp.SessionId,
	initialOptions []acp.SessionConfigOption,
	modes []acp.SessionMode,
) (acpOptionCatalog, error) {
	modelConfig, ok := acpSelectConfig(initialOptions, agent.binding.modelConfigID)
	if !ok {
		if agent.binding.nativeProcess {
			return acpOptionCatalog{PermissionLevels: agent.binding.permissionLevels(initialOptions, modes)}, nil
		}
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}
	modelOptions := acpSelectValues(modelConfig.Options)
	if len(modelOptions) == 0 {
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}

	catalog := acpOptionCatalog{
		Models:           make([]ModelOption, 0, len(modelOptions)),
		PermissionLevels: agent.binding.permissionLevels(initialOptions, modes),
		ReasoningOptions: acpReasoningOptions(
			initialOptions,
			agent.binding.reasoningConfigID,
		),
		DefaultModel:     strings.TrimSpace(string(modelConfig.CurrentValue)),
		DefaultReasoning: detectedDefaultReasoning(initialOptions, agent.binding.reasoningConfigID, modelConfig),
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
		if option.Value != modelConfig.CurrentValue && !agent.binding.nativeProcess {
			updated, err := agent.connection.SetSessionConfigOption(
				ctx,
				acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
					SessionId: sessionID,
					ConfigId:  agent.binding.modelConfigID,
					Value:     option.Value,
				}},
			)
			if err != nil {
				logx.Errorf("select ACP model during option detection failed: session_id=%s model_id=%s error=%v", sessionID, modelID, err)
				detectionErr = status_error.RuntimeUnavailable
				continue
			}
			configOptions = updated.ConfigOptions
		}

		name := strings.TrimSpace(option.Name)
		if name == "" {
			name = modelID
		}
		if agent.binding.modelDisplayName != nil {
			name = agent.binding.modelDisplayName(option, name)
		}
		reasoning := acpReasoningOptions(configOptions, agent.binding.reasoningConfigID)
		if modelOptions, known := acpmeta.ModelConfigOptions(option.Meta); known {
			reasoning = acpReasoningOptions(modelOptions, agent.binding.reasoningConfigID)
		} else if agent.binding.nativeProcess && option.Value != modelConfig.CurrentValue {
			// 未提供模型专属目录时，不把当前模型的强度套用于其他模型。
			reasoning = nil
		}
		catalog.Models = append(catalog.Models, ModelOption{
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
	if config, ok := acpSelectConfig(options, reasoningConfigID); ok {
		if value := strings.TrimSpace(string(config.CurrentValue)); value != "" {
			return value
		}
	}
	for _, option := range acpSelectValues(modelConfig.Options) {
		if option.Value != modelConfig.CurrentValue {
			continue
		}
		if configs, known := acpmeta.ModelConfigOptions(option.Meta); known {
			if config, ok := acpSelectConfig(configs, reasoningConfigID); ok {
				return strings.TrimSpace(string(config.CurrentValue))
			}
		}
		break
	}
	return ""
}

// acpPermissionLevels 把 Agent 实际声明的 Mode 选项归一为三个产品等级。
func acpPermissionLevels(
	options []acp.SessionConfigOption,
	modes []acp.SessionMode,
) []PermissionLevel {
	available := make(map[string]struct{})
	modeConfigFound := false
	for _, option := range options {
		if option.Select == nil || option.Select.Category == nil ||
			*option.Select.Category != acp.SessionConfigOptionCategoryMode {
			continue
		}
		modeConfigFound = true
		for _, value := range acpSelectValues(option.Select.Options) {
			available[string(value.Value)] = struct{}{}
		}
	}
	if !modeConfigFound {
		for _, mode := range modes {
			available[string(mode.Id)] = struct{}{}
		}
	}
	levels := make([]PermissionLevel, 0, 3)
	if hasAnyMode(available, "read-only", "default") {
		levels = append(levels, PermissionLevelDefault)
	}
	if hasAnyMode(available, "agent", "auto") {
		levels = append(levels, PermissionLevelAuto)
	}
	if hasAnyMode(available, "agent-full-access", "bypassPermissions") {
		levels = append(levels, PermissionLevelFullAccess)
	}
	return levels
}

func hasAnyMode(available map[string]struct{}, ids ...string) bool {
	for _, id := range ids {
		if _, exists := available[id]; exists {
			return true
		}
	}
	return false
}

// rememberSessionModes 在持有 sessionMutex 时记录会话声明，供权限配置使用。
func (agent *embeddedACPAgent) rememberSessionModes(id acp.SessionId, modes *acp.SessionModeState) {
	if agent.sessionModes == nil {
		agent.sessionModes = make(map[acp.SessionId]*acp.SessionModeState)
	}
	agent.sessionModes[id] = modes
}

// acpReasoningOptions 读取当前 Adapter 推理配置中的平铺选项。
func acpReasoningOptions(
	options []acp.SessionConfigOption,
	configID acp.SessionConfigId,
) []ReasoningOption {
	config, ok := acpSelectConfig(options, configID)
	if !ok {
		return nil
	}
	values := acpSelectValues(config.Options)
	result := make([]ReasoningOption, 0, len(values))
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
		result = append(result, ReasoningOption{ID: id, Name: name})
	}
	return result
}

// acpSelectConfig 按 Adapter 声明的稳定配置标识查找 select 配置。
func acpSelectConfig(
	options []acp.SessionConfigOption,
	id acp.SessionConfigId,
) (*acp.SessionConfigOptionSelect, bool) {
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id {
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

package runtimes

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"path/filepath"
	"sort"
	"strings"
)

type runtimeDefinition struct {
	// id 是当前 Runtime 或 Profile 的开放字符串标识。
	id string
	// baseRuntimeID 是提供协议语义的内置 Runtime 标识。
	baseRuntimeID string
	// name 是面向调用方展示的名称。
	name string
	// profile 表示该 Definition 是否由 Local Profile 生成。
	profile bool
	// binaryCandidates 是按优先级尝试的可执行文件候选。
	binaryCandidates []string
	// userBinaryCandidates 是相对用户主目录的官方原生程序路径，不包含仅分发命令的桌面入口。
	userBinaryCandidates []string
	// prefixArgs 是所有探测和调用共享的前置参数。
	prefixArgs []string
	// environment 是 Definition 和 Profile 合并后的环境覆盖。
	environment map[string]string
	// models 是检测失败时仍可使用的模型提示。
	models []ModelOption
	// defaultModel 是未显式选择模型时的模型标识。
	defaultModel string
	// reasoningOptions 是该 Runtime 明确支持的推理强度选项。
	reasoningOptions []ReasoningOption
	// instructionFileName 是工作区根目录使用的指令文件名。
	instructionFileName string
	// skillDirectoryName 是工作区根目录使用的技能目录名。
	skillDirectoryName string
	// configHomeEnvironmentVariable 是配置主目录对应的受控环境变量名。
	configHomeEnvironmentVariable string
}

func newRegistry(profiles []ProfileSpec) (map[string]runtimeDefinition, error) {
	definitions := map[string]runtimeDefinition{
		RuntimeIDOpenCode:  builtinNativeDefinition(RuntimeIDOpenCode, "OpenCode", []string{"opencode"}, ".opencode", "OPENCODE_CONFIG_DIR"),
		RuntimeIDGemini:    builtinGeminiDefinition(),
		RuntimeIDGrok:      builtinNativeDefinition(RuntimeIDGrok, "Grok Build", []string{"grok"}, ".grok", "GROK_HOME"),
		RuntimeIDWorkBuddy: builtinWorkBuddyDefinition(),
		RuntimeIDQoderCN:   builtinQoderDefinition(true),
		RuntimeIDQoder:     builtinQoderDefinition(false),
		RuntimeIDCodex:     builtinCodexDefinition(),
		RuntimeIDClaude:    builtinClaudeDefinition(),
		RuntimeIDKimi:      builtinNativeDefinition(RuntimeIDKimi, "Kimi", []string{"kimi"}, ".agents", ""),
		RuntimeIDCursor:    builtinNativeDefinition(RuntimeIDCursor, "Cursor", []string{"cursor-agent", "agent"}, ".cursor", ""),
		RuntimeIDPi:        builtinNativeDefinition(RuntimeIDPi, "Pi", []string{"pi"}, ".pi", "PI_CODING_AGENT_DIR"),
	}
	for _, profile := range profiles {
		resolved, err := resolveProfile(profile, definitions)
		if err != nil {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		if _, exists := definitions[resolved.id]; exists {
			return nil, status_error.RuntimeUnavailable
		}
		definitions[resolved.id] = resolved
	}
	return definitions, nil
}

func resolveProfile(
	profile ProfileSpec,
	definitions map[string]runtimeDefinition,
) (runtimeDefinition, error) {
	id := strings.TrimSpace(profile.ID)
	if id == "" {
		return runtimeDefinition{}, status_error.BadRequest
	}
	base, exists := definitions[profile.BaseRuntimeID]
	if !exists || base.profile {
		return runtimeDefinition{}, status_error.RuntimeUnavailable
	}
	resolved := cloneDefinition(base)
	resolved.id = id
	resolved.baseRuntimeID = base.baseRuntimeID
	resolved.profile = true
	if name := strings.TrimSpace(profile.Name); name != "" {
		resolved.name = name
	}
	if profile.BinaryCandidates != nil {
		resolved.binaryCandidates = cloneStrings(profile.BinaryCandidates)
		resolved.userBinaryCandidates = nil
	}
	if profile.PrefixArgs != nil {
		resolved.prefixArgs = cloneStrings(profile.PrefixArgs)
	}
	for key, value := range profile.Environment {
		resolved.environment[key] = value
	}
	if profile.Models != nil {
		resolved.models = cloneModels(profile.Models)
	}
	if profile.DefaultModel != "" {
		resolved.defaultModel = profile.DefaultModel
	}
	if err := validateRuntimeDefinition(resolved); err != nil {
		return runtimeDefinition{}, status_error.RuntimeConfigurationInvalid
	}
	return resolved, nil
}

// definitionWithLaunchOverride 将持久化启动覆盖合并到不可变 Runtime Definition 副本。
func definitionWithLaunchOverride(
	definition runtimeDefinition,
	override LaunchOverride,
) (runtimeDefinition, error) {
	resolved := cloneDefinition(definition)
	if binary := strings.TrimSpace(override.Binary); binary != "" {
		resolved.binaryCandidates = []string{binary}
		resolved.userBinaryCandidates = nil
	}
	if override.PrefixArgs != nil {
		resolved.prefixArgs = cloneStrings(override.PrefixArgs)
	}
	for key, value := range override.Environment {
		resolved.environment[key] = value
	}
	if err := validateRuntimeDefinition(resolved); err != nil {
		return runtimeDefinition{}, status_error.RuntimeConfigurationInvalid
	}
	return resolved, nil
}

func validateRuntimeDefinition(definition runtimeDefinition) error {
	if len(definition.binaryCandidates) == 0 {
		return status_error.BadRequest
	}
	for _, candidate := range definition.binaryCandidates {
		if strings.TrimSpace(candidate) == "" {
			return status_error.RuntimeUnavailable
		}
		if strings.ContainsRune(candidate, '\x00') {
			return status_error.RuntimeUnavailable
		}
	}
	if err := validateArguments("prefix", definition.prefixArgs); err != nil {
		return err
	}
	for key, value := range definition.environment {
		if err := validateEnvironmentEntry(key, value); err != nil {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	if definition.configHomeEnvironmentVariable != "" {
		if err := validateEnvironmentEntry(definition.configHomeEnvironmentVariable, ""); err != nil {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	modelIDs := make(map[string]struct{}, len(definition.models))
	for _, model := range definition.models {
		if strings.TrimSpace(model.ID) == "" {
			return status_error.BadRequest
		}
		if _, exists := modelIDs[model.ID]; exists {
			return status_error.BadRequest
		}
		modelIDs[model.ID] = struct{}{}
	}
	if definition.defaultModel != "" && len(modelIDs) > 0 {
		if _, exists := modelIDs[definition.defaultModel]; !exists {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	reasoningIDs := make(map[string]struct{}, len(definition.reasoningOptions))
	for _, option := range definition.reasoningOptions {
		if strings.TrimSpace(option.ID) == "" {
			return status_error.BadRequest
		}
		if _, exists := reasoningIDs[option.ID]; exists {
			return status_error.BadRequest
		}
		reasoningIDs[option.ID] = struct{}{}
	}
	if err := validateRuntimeFiles(definition); err != nil {
		return err
	}
	return nil
}

func validateRuntimeFiles(definition runtimeDefinition) error {
	if definition.instructionFileName == "" {
		return status_error.BadRequest
	}
	if filepath.Base(definition.instructionFileName) != definition.instructionFileName ||
		definition.instructionFileName == "." || definition.instructionFileName == ".." ||
		strings.ContainsAny(definition.instructionFileName, "/\\\x00\r\n") {
		return status_error.BadRequest
	}
	if definition.skillDirectoryName == "" {
		return status_error.SkillUnavailable
	}
	if filepath.Base(definition.skillDirectoryName) != definition.skillDirectoryName ||
		definition.skillDirectoryName == "." || definition.skillDirectoryName == ".." ||
		strings.ContainsAny(definition.skillDirectoryName, "/\\\x00\r\n") {
		return status_error.SkillUnavailable
	}
	return nil
}

func validateArguments(name string, arguments []string) error {
	for _, argument := range arguments {
		if strings.ContainsRune(argument, '\x00') {
			return status_error.BadRequest
		}
	}
	return nil
}

func cloneDefinition(source runtimeDefinition) runtimeDefinition {
	cloned := source
	cloned.binaryCandidates = cloneStrings(source.binaryCandidates)
	cloned.userBinaryCandidates = cloneStrings(source.userBinaryCandidates)
	cloned.prefixArgs = cloneStrings(source.prefixArgs)
	cloned.environment = cloneStringMap(source.environment)
	cloned.models = cloneModels(source.models)
	cloned.reasoningOptions = cloneReasoningOptions(source.reasoningOptions)
	return cloned
}

func definitionSummaries(
	definitions map[string]runtimeDefinition,
) []RuntimeSummary {
	summaries := make([]RuntimeSummary, 0, len(definitions))
	for _, definition := range definitions {
		summaries = append(summaries, RuntimeSummary{
			ID:                            definition.id,
			BaseRuntimeID:                 definition.baseRuntimeID,
			Name:                          definition.name,
			Profile:                       definition.profile,
			BinaryCandidates:              cloneStrings(definition.binaryCandidates),
			PrefixArgs:                    cloneStrings(definition.prefixArgs),
			Models:                        cloneModels(definition.models),
			DefaultModel:                  definition.defaultModel,
			ReasoningOptions:              cloneReasoningOptions(definition.reasoningOptions),
			InstructionFileName:           definition.instructionFileName,
			SkillDirectoryName:            definition.skillDirectoryName,
			ConfigHomeEnvironmentVariable: definition.configHomeEnvironmentVariable,
		})
	}
	sort.Slice(summaries, func(left, right int) bool {
		return summaries[left].ID < summaries[right].ID
	})
	return summaries
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneModels(values []ModelOption) []ModelOption {
	cloned := make([]ModelOption, len(values))
	for index, model := range values {
		cloned[index] = model
		cloned[index].ReasoningOptions = cloneReasoningOptions(model.ReasoningOptions)
		cloned[index].PermissionLevels = append([]PermissionLevel(nil), model.PermissionLevels...)
	}
	return cloned
}

func cloneReasoningOptions(values []ReasoningOption) []ReasoningOption {
	return append([]ReasoningOption(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

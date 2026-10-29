package contract

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

const (
	// DiagnosticCodeACPOptionsUnavailable 表示 ACP 模型目录未能完整刷新。
	DiagnosticCodeACPOptionsUnavailable = "ACP_OPTIONS_UNAVAILABLE"
	// DiagnosticCodeACPOptionsFallback 表示本次探测沿用了上一次成功的模型目录。
	DiagnosticCodeACPOptionsFallback = "ACP_OPTIONS_FALLBACK"
	// DiagnosticCodePermissionDefaultDowngraded 表示存盘默认权限已按当前探测能力安全降级。
	DiagnosticCodePermissionDefaultDowngraded = "PERMISSION_DEFAULT_DOWNGRADED"
)

// PreservePreviousOptionsOnFailure 在 ACP 模型目录刷新失败时保留上次成功目录。
func PreservePreviousOptionsOnFailure(
	previous DetectionResult,
	current DetectionResult,
) DetectionResult {
	previousCatalogAvailable := previous.Capabilities[CapabilityDynamicModels] ||
		hasDiagnostic(previous.Diagnostics, DiagnosticCodeACPOptionsFallback)
	if !hasDiagnostic(current.Diagnostics, DiagnosticCodeACPOptionsUnavailable) ||
		!previousCatalogAvailable || len(previous.Models) == 0 {
		return current
	}
	current.Models = cloneModels(previous.Models)
	current.ReasoningOptions = cloneReasoningOptions(previous.ReasoningOptions)
	current.DetectedDefaultModel = previous.DetectedDefaultModel
	current.DetectedDefaultReasoning = previous.DetectedDefaultReasoning
	current.Diagnostics = append(current.Diagnostics, Diagnostic{
		Code:    DiagnosticCodeACPOptionsFallback,
		Level:   DiagnosticLevelWarning,
		Message: "using the previous model catalog after ACP option refresh failed",
	})
	return current
}

// AvailableModels 合并 Runtime 探测目录与用户维护的自定义模型标识。
func AvailableModels(
	detection DetectionResult,
	customModelIDs []string,
) []ModelOption {
	models := cloneModels(detection.Models)
	seen := make(map[string]struct{}, len(models)+len(customModelIDs))
	for _, model := range models {
		seen[model.ID] = struct{}{}
	}
	for _, rawID := range customModelIDs {
		modelID := strings.TrimSpace(rawID)
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		permissionLevels := []PermissionLevel(nil)
		if len(models) > 0 {
			permissionLevels = append(permissionLevels, models[0].PermissionLevels...)
		}
		models = append(models, ModelOption{
			ID:               modelID,
			Name:             modelID,
			PermissionLevels: permissionLevels,
		})
	}
	return models
}

// NormalizeDetectedDefaults 按探测目录与自定义模型保留有效默认值，并把失效值回落到探测快照。
func NormalizeDetectedDefaults(
	detection DetectionResult,
	customModelIDs []string,
	modelID string,
	reasoningID string,
) (string, string) {
	models := AvailableModels(detection, customModelIDs)
	fallbackModel := strings.TrimSpace(detection.DetectedDefaultModel)
	if fallbackModel == "" && len(models) > 0 {
		fallbackModel = models[0].ID
	}

	modelIndex := -1
	for index, model := range models {
		if model.ID == modelID {
			modelIndex = index
			break
		}
	}
	if modelIndex < 0 {
		modelID = fallbackModel
		for index, model := range models {
			if model.ID == modelID {
				modelIndex = index
				break
			}
		}
	}
	if modelIndex >= 0 {
		modelID = models[modelIndex].ID
	} else {
		modelID = ""
	}

	reasoningOptions := detection.ReasoningOptions
	if modelIndex >= 0 && len(models[modelIndex].ReasoningOptions) > 0 {
		reasoningOptions = models[modelIndex].ReasoningOptions
	}
	for _, option := range reasoningOptions {
		if option.ID == reasoningID {
			return modelID, reasoningID
		}
	}
	fallbackReasoning := strings.TrimSpace(detection.DetectedDefaultReasoning)
	if fallbackReasoning != "" {
		for _, option := range reasoningOptions {
			if option.ID == fallbackReasoning {
				return modelID, fallbackReasoning
			}
		}
	}
	if len(reasoningOptions) == 0 {
		return modelID, ""
	}
	return modelID, reasoningOptions[0].ID
}

// ModelPermissionLevels 返回指定模型当前探测到的产品权限等级；模型缺失或未声明时为空。
func ModelPermissionLevels(
	detection DetectionResult,
	customModelIDs []string,
	modelID string,
) []PermissionLevel {
	models := AvailableModels(detection, customModelIDs)
	selectedID := strings.TrimSpace(modelID)
	if selectedID == "" {
		selectedID = strings.TrimSpace(detection.DetectedDefaultModel)
	}
	if selectedID == "" && len(models) > 0 {
		selectedID = models[0].ID
	}
	for _, model := range models {
		if model.ID != selectedID {
			continue
		}
		return append([]PermissionLevel(nil), model.PermissionLevels...)
	}
	return nil
}

// PermissionLevelSupported 判断产品权限是否出现在探测声明的列表中。
func PermissionLevelSupported(
	supported []PermissionLevel,
	level PermissionLevel,
) bool {
	if !constants.SelectablePermissionLevel(level) {
		return false
	}
	for _, candidate := range supported {
		if candidate == level {
			return true
		}
	}
	return false
}

// SafePermissionLevel 在探测列表内保留或安全降级默认权限；列表为空时保持原值且不算已降级。
func SafePermissionLevel(
	preferred PermissionLevel,
	supported []PermissionLevel,
) (PermissionLevel, bool) {
	if PermissionLevelSupported(supported, preferred) {
		return preferred, false
	}
	if len(supported) == 0 {
		return preferred, false
	}
	for _, candidate := range []PermissionLevel{
		PermissionLevelAuto,
		PermissionLevelDefault,
	} {
		if PermissionLevelSupported(supported, candidate) {
			return candidate, true
		}
	}
	return supported[0], true
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
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

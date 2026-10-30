package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"maps"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpAdapterConfig 只保存本次管理连接已冻结的宿主启动事实。
type acpAdapterConfig struct {
	// RefreshCredentials 指向共享账号凭据权威。
	RefreshCredentials runtimeconfig.CredentialRefresh
	// UseNativeAuth 明确本次使用系统账号原生认证文件。
	UseNativeAuth bool
	// BaseRuntimeID 是公开原生工厂的基础身份。
	BaseRuntimeID string
	// RuntimeID 是基础智能体或本地配置身份。
	RuntimeID string
	// DetectionID 关联本次能力探测。
	DetectionID string
	// Executable 是已经解析的原生程序。
	Executable string
	// PrefixArgs 是 Profile 的受控前置参数。
	PrefixArgs []string
	// NativePluginDirectories 是固定的原生插件目录。
	NativePluginDirectories []string
	// CodexPluginConfiguration 是本次进程的原生点路径覆盖。
	CodexPluginConfiguration map[string]string
	// ClaudeSettingsPath 是本次进程的受管设置文件。
	ClaudeSettingsPath string
	// Environment 是本次进程的完整环境。
	Environment []string
	// WorkingDirectory 是管理会话的工作目录。
	WorkingDirectory string
	// StateRootDirectory 是宿主拥有的持久原生状态根。
	StateRootDirectory string
	// PermissionMode 是产品已经授权的原生权限语义。
	PermissionMode string
}

// acpRuntimeBinding 仅保留技术声明副本与产品模型解释，不拥有原生工厂。
type acpRuntimeBinding struct {
	// descriptor 来自独立运行时的唯一技术注册表。
	descriptor native.Descriptor
	// modelDisplayName 根据目录事实补充产品展示名称。
	modelDisplayName func(acp.SessionConfigSelectOption, string) string
	// catalogOptions 过滤产品可选的模型与推理目录。
	catalogOptions func(acpOptionCatalog) acpOptionCatalog
}

// defaultACPRuntimeBindings 从技术注册表投影产品目录规则。
func defaultACPRuntimeBindings() map[string]acpRuntimeBinding {
	bindings := make(map[string]acpRuntimeBinding)
	for _, descriptor := range native.Descriptors() {
		binding := acpRuntimeBinding{descriptor: descriptor}
		if descriptor.ID == runtimeconfig.RuntimeIDWorkBuddy {
			binding.modelDisplayName, binding.catalogOptions = workBuddyModelDisplayName, workBuddyCatalogOptions
		}
		bindings[descriptor.ID] = binding
	}
	return bindings
}

// acpAdapterConfigForManagement 只冻结账号、工作范围和产品已经授权的权限。
func acpAdapterConfigForManagement(prepared preparedRuntime, target runtimeconfig.RuntimeTarget, workspace c.WorkspaceScope, managed bool, permission constants.PermissionLevel, binding acpRuntimeBinding) acpAdapterConfig {
	config := acpAdapterConfig{
		RefreshCredentials: target.RefreshCredentials, UseNativeAuth: !target.Isolated,
		BaseRuntimeID: binding.descriptor.ID, RuntimeID: target.RuntimeID, Executable: prepared.detection.ResolvedBinary,
		PrefixArgs: cloneStrings(prepared.definition.prefixArgs), Environment: prepared.environment.list(), WorkingDirectory: workspace.Root,
		PermissionMode: string(permission), NativePluginDirectories: cloneStrings(target.NativePluginDirectories),
		CodexPluginConfiguration: maps.Clone(target.CodexPluginConfiguration), ClaudeSettingsPath: target.ClaudeSettingsPath,
	}
	if managed {
		config.StateRootDirectory = filepath.Dir(workspace.Root)
	}
	return config
}

// acpAvailableModeIDs 合并标准模式目录与 mode 类配置中的原生标识。
func acpAvailableModeIDs(options []acp.SessionConfigOption, modes []acp.SessionMode) map[string]struct{} {
	available := make(map[string]struct{})
	for _, mode := range modes {
		available[string(mode.Id)] = struct{}{}
	}
	for _, option := range options {
		if option.Select == nil || option.Select.Category == nil || *option.Select.Category != acp.SessionConfigOptionCategoryMode {
			continue
		}
		for _, value := range acpSelectValues(option.Select.Options) {
			available[string(value.Value)] = struct{}{}
		}
	}
	return available
}

// permissionLevels 把技术模式语义映射为产品权限，不另存厂商原生标识。
func (binding acpRuntimeBinding) permissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []runtimeconfig.PermissionLevel {
	ids := binding.descriptor.Modes
	all := []runtimeconfig.PermissionLevel{runtimeconfig.PermissionLevelDefault, runtimeconfig.PermissionLevelAuto, runtimeconfig.PermissionLevelFullAccess}
	if binding.descriptor.ManagedPermissions || len(ids.Default)+len(ids.Automatic)+len(ids.Unrestricted) == 0 {
		return all
	}
	available := acpAvailableModeIDs(options, modes)
	if ids.SingleModeFallback != "" && len(modes) == 1 && string(modes[0].Id) == ids.SingleModeFallback && !hasAnyMode(available, append(append(cloneStrings(ids.Automatic), ids.Unrestricted...), ids.ReadOnly...)...) {
		return all
	}
	result := []runtimeconfig.PermissionLevel{}
	for index, candidates := range [][]string{ids.Default, ids.Automatic, ids.Unrestricted} {
		if hasAnyMode(available, candidates...) {
			result = append(result, all[index])
		}
	}
	return result
}

// workBuddyCatalogOptions 只开放具体模型及其思考档位，不开放自动路由入口与思考开关。
func workBuddyCatalogOptions(catalog acpOptionCatalog) acpOptionCatalog {
	models := make([]runtimeconfig.ModelOption, 0, len(catalog.Models))
	currentModelVisible := false
	for _, model := range catalog.Models {
		switch model.ID {
		case constants.RuntimeWorkBuddyFastModelID, constants.RuntimeWorkBuddyBalancedModelID, constants.RuntimeWorkBuddyDeepModelID:
			continue
		}
		model.ReasoningOptions = workBuddyReasoningOptions(model.ReasoningOptions)
		models = append(models, model)
		currentModelVisible = currentModelVisible || model.ID == catalog.DefaultModel
	}
	catalog.Models = models
	// 强度仅绑定到已探测的模型，避免无具体档位的模型借用全局目录。
	catalog.ReasoningOptions = nil
	if !currentModelVisible {
		// 原生默认模型被隐藏时，不能把它的思考目录套到另一个具体模型上。
		catalog.DefaultModel, catalog.DefaultReasoning = "", ""
		catalog.ReasoningOptions = nil
	}
	return catalog
}

// workBuddyReasoningOptions 保留上游具体强度的标识、名称和顺序，不将思考开关作为强度。
func workBuddyReasoningOptions(options []runtimeconfig.ReasoningOption) []runtimeconfig.ReasoningOption {
	if options == nil {
		return nil
	}
	result := make([]runtimeconfig.ReasoningOption, 0, len(options))
	for _, option := range options {
		if option.ID != constants.RuntimeWorkBuddyThinkingEnabled && option.ID != constants.RuntimeWorkBuddyThinkingDisabled {
			result = append(result, option)
		}
	}
	return result
}

// workBuddyModelDisplayName 仅将 ACP 明确声明零积分的模型标为免费，不推测限时优惠。
func workBuddyModelDisplayName(option acp.SessionConfigSelectOption, name string) string {
	if option.Description == nil || strings.TrimSpace(*option.Description) != constants.RuntimeWorkBuddyZeroCreditDescription ||
		strings.HasSuffix(name, constants.RuntimeModelFreeNameSuffix) {
		return name
	}
	return name + constants.RuntimeModelFreeNameSuffix
}

package runtimes

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"

	acpworkbuddy "github.com/JieWaZi/acp-go/pkg/workbuddy"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// workBuddyACPRuntimeBinding 准备原生管理工厂并解释模型和权限目录。
func workBuddyACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDWorkBuddy
	binding.displayName = "WorkBuddy"
	binding.nativeVersionMetadataOnly = true
	binding.modelDisplayName = workBuddyModelDisplayName
	binding.catalogOptions = workBuddyCatalogOptions
	binding.probeModelOptions = true
	binding.supportsMCPTransport = allMCPTransports
	binding.permissionLevels = func(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
		available := acpAvailableModeIDs(options, modes)
		levels := []PermissionLevel{}
		for _, entry := range []struct {
			// mode 是上游明确声明的权限模式。
			mode string
			// level 是对应的产品权限。
			level PermissionLevel
		}{{"default", PermissionLevelDefault}, {"auto", PermissionLevelAuto}, {"bypassPermissions", PermissionLevelFullAccess}} {
			if hasAnyMode(available, entry.mode) {
				levels = append(levels, entry.level)
			}
		}
		return levels
	}
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpworkbuddy.NewAgent(ctx, acpworkbuddy.Config{
			Logger: acpAdapterLogger(ctx), WorkBuddyPath: config.Executable,
			PrefixArgs: cloneStrings(config.PrefixArgs), Environment: cloneStrings(config.Environment),
			WorkingDirectory: config.WorkingDirectory, PermissionMode: config.PermissionMode,
		})
	}
	return binding
}

// workBuddyCatalogOptions 只开放具体模型及其思考档位，不开放自动路由入口与思考开关。
func workBuddyCatalogOptions(catalog acpOptionCatalog) acpOptionCatalog {
	models := make([]ModelOption, 0, len(catalog.Models))
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
func workBuddyReasoningOptions(options []ReasoningOption) []ReasoningOption {
	if options == nil {
		return nil
	}
	result := make([]ReasoningOption, 0, len(options))
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

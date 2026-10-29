package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// kimiACPRuntimeBinding 组合 Kimi 自身的旧版兼容与产品策略。
func kimiACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDKimi
	binding.displayName = "Kimi"
	binding.permissionLevels = kimiACPPermissionLevels

	return binding
}

// kimiACPPermissionLevels 映射 TypeScript 模式，并兼容 Python Kimi 的单一 default 模式。
func kimiACPPermissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []runtimecontract.PermissionLevel {
	available := acpAvailableModeIDs(options, modes)
	if isKimiLegacyDefaultMode(modes) && !hasAnyMode(available, "auto", "yolo", "plan") {
		return []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}
	}
	levels := []runtimecontract.PermissionLevel{}
	for _, entry := range []struct {
		// mode 是 Kimi 声明的原生权限模式。
		mode string
		// level 是对应的 NeuxBot 产品权限。
		level runtimecontract.PermissionLevel
	}{
		{"default", runtimecontract.PermissionLevelDefault},
		{"auto", runtimecontract.PermissionLevelAuto},
		{"yolo", runtimecontract.PermissionLevelFullAccess},
	} {
		if hasAnyMode(available, entry.mode) {
			levels = append(levels, entry.level)
		}
	}
	return levels
}

// isKimiLegacyDefaultMode 识别只能沿用 CLI 本地权限配置的旧版 Kimi。
func isKimiLegacyDefaultMode(modes []acp.SessionMode) bool {
	return len(modes) == 1 && modes[0].Id == "default"
}

package runtimes

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acpkimi "github.com/JieWaZi/acp-go/pkg/kimi"
	acp "github.com/coder/acp-go-sdk"
)

// kimiACPRuntimeBinding 组合 Kimi 自身的旧版兼容与产品策略。
func kimiACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDKimi
	binding.displayName = "Kimi"
	binding.selectedSkillPrompt = func(prompt string, selections []skillSelection) string {
		return selectedSkillCommandPrompt("/skill:", prompt, selections)
	}
	binding.permissionLevels = kimiACPPermissionLevels
	binding.modeID = kimiACPMode
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpkimi.NewAgent(ctx, acpkimi.Config{
			Logger:           acpAdapterLogger(ctx),
			KimiPath:         config.Executable,
			PrefixArgs:       cloneStrings(config.PrefixArgs),
			Environment:      append([]string(nil), config.Environment...),
			WorkingDirectory: config.WorkingDirectory,
			PermissionMode:   config.PermissionMode,
		})
	}
	return binding
}

// kimiACPPermissionLevels 映射 TypeScript 模式，并兼容 Python Kimi 的单一 default 模式。
func kimiACPPermissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
	available := acpAvailableModeIDs(options, modes)
	if isKimiLegacyDefaultMode(modes) && !hasAnyMode(available, "auto", "yolo", "plan") {
		return []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	}
	levels := []PermissionLevel{}
	for _, entry := range []struct {
		// mode 是 Kimi 声明的原生权限模式。
		mode string
		// level 是对应的 NeuxBot 产品权限。
		level PermissionLevel
	}{
		{"default", PermissionLevelDefault},
		{"auto", PermissionLevelAuto},
		{"yolo", PermissionLevelFullAccess},
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

// kimiACPMode 兼容 Python Kimi 仅提供 default 的旧模式目录。
func kimiACPMode(policy ExecutionPolicy, modes *acp.SessionModeState) (acp.SessionModeId, error) {
	if modes != nil && isKimiLegacyDefaultMode(modes.AvailableModes) {
		if policy.WorkspaceReadOnly {
			return "", status_error.RuntimeProtocolIncompatible
		}
		return "default", nil
	}
	if policy.WorkspaceReadOnly {
		return "plan", nil
	}
	if policy.PermissionLevel == PermissionLevelFullAccess {
		return "yolo", nil
	}
	if policy.PermissionLevel == PermissionLevelAuto {
		return "auto", nil
	}
	return "default", nil
}

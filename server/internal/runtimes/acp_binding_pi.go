package runtimes

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acppi "github.com/JieWaZi/acp-go/pkg/pi"
	acp "github.com/coder/acp-go-sdk"
)

// piACPRuntimeBinding 组合 Pi 自身的思考目录与产品策略。
func piACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDPi
	binding.displayName = "Pi"
	binding.permissionLevels = func([]acp.SessionConfigOption, []acp.SessionMode) []PermissionLevel {
		return []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	}
	binding.supportsMCPTransport = allMCPTransports
	binding.selectedSkillPrompt = func(prompt string, selections []skillSelection) string {
		return selectedSkillCommandPrompt("/skill:", prompt, selections)
	}
	binding.modeID = func(policy ExecutionPolicy, _ *acp.SessionModeState) (acp.SessionModeId, error) {
		if policy.WorkspaceReadOnly {
			return "", status_error.RuntimeProtocolIncompatible
		}
		return "", nil
	}
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		modulePath := ""
		for _, entry := range config.Environment {
			key, value, ok := strings.Cut(entry, "=")
			if ok && normalizeEnvironmentKey(key) == normalizeEnvironmentKey("PI_MCP_MODULE_PATH") {
				modulePath = value
			}
		}
		return acppi.NewAgent(ctx, acppi.Config{
			Logger:           acpAdapterLogger(ctx),
			PiPath:           config.Executable,
			PrefixArgs:       cloneStrings(config.PrefixArgs),
			Environment:      append([]string(nil), config.Environment...),
			WorkingDirectory: config.WorkingDirectory,
			PermissionMode:   config.PermissionMode,
			MCPModulePath:    modulePath,
		})
	}
	return binding
}

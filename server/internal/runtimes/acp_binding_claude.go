package runtimes

import (
	"context"
	"strings"

	acpclaude "github.com/JieWaZi/acp-go/pkg/claude"
	acp "github.com/coder/acp-go-sdk"
)

const (
	// claudeACPModelConfigID 是 acp-go Claude Adapter 的模型配置标识。
	claudeACPModelConfigID acp.SessionConfigId = "model"
	// claudeACPReasoningConfigID 是 acp-go Claude Adapter 的推理强度配置标识。
	claudeACPReasoningConfigID acp.SessionConfigId = "effort"
)

// claudeACPRuntimeBinding 组合 Claude 自身的 ACP 能力与产品策略。
func claudeACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDClaude
	binding.displayName = "Claude"
	binding.executableLogKey = "claude_binary"
	binding.modelConfigID = claudeACPModelConfigID
	binding.reasoningConfigID = claudeACPReasoningConfigID
	binding.supportsMCPTransport = allMCPTransports
	binding.elicitationSessionPaths = append(binding.elicitationSessionPaths, []string{"claudeCode", "sessionId"})
	binding.modeID = func(policy ExecutionPolicy, _ *acp.SessionModeState) (acp.SessionModeId, error) {
		if policy.WorkspaceReadOnly || policy.PermissionLevel == PermissionLevelDefault {
			return "default", nil
		}
		if policy.PermissionLevel == PermissionLevelFullAccess {
			return "bypassPermissions", nil
		}
		return "auto", nil
	}
	binding.toolIdentity = claudeACPToolIdentity
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		prefixArgs := cloneStrings(config.PrefixArgs)
		if config.ClaudeSettingsPath != "" {
			prefixArgs = append(prefixArgs, "--settings", config.ClaudeSettingsPath)
		}
		for _, directory := range config.NativePluginDirectories {
			prefixArgs = append(prefixArgs, "--plugin-dir", directory)
		}
		return acpclaude.NewAgent(ctx, acpclaude.Config{
			Logger:      acpAdapterLogger(ctx),
			ClaudePath:  config.Executable,
			PrefixArgs:  prefixArgs,
			Environment: append([]string(nil), config.Environment...),
		})
	}
	return binding
}

// claudeACPToolIdentity 读取 acp-go 按 upstream 保留的 Claude 工具身份。
func claudeACPToolIdentity(payload map[string]any) (string, string) {
	meta, _ := payload["_meta"].(map[string]any)
	claudeCode, _ := meta["claudeCode"].(map[string]any)
	skill, _ := claudeCode["skill"].(string)
	title, _ := payload["title"].(string)
	if skill != "" && strings.HasPrefix(strings.ToLower(title), "load skill") {
		return "skill", ""
	}
	toolName, _ := claudeCode["toolName"].(string)
	return toolName, ""
}

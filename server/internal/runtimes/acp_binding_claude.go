package runtimes

import (
	"context"

	acpclaude "github.com/JieWaZi/acp-go/pkg/claude"
	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	// claudeACPModelConfigID 是 acp-go Claude Adapter 的模型配置标识。
	claudeACPModelConfigID acp.SessionConfigId = "model"
	// claudeACPReasoningConfigID 是 acp-go Claude Adapter 的推理强度配置标识。
	claudeACPReasoningConfigID acp.SessionConfigId = "effort"
)

// claudeACPRuntimeBinding 组合 Claude 的原生工厂与能力目录。
func claudeACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDClaude
	binding.displayName = "Claude"
	binding.executableLogKey = "claude_binary"
	binding.modelConfigID = claudeACPModelConfigID
	binding.reasoningConfigID = claudeACPReasoningConfigID
	binding.supportsMCPTransport = allMCPTransports
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

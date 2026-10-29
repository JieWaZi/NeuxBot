package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

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
	binding.baseRuntimeID = runtimecontract.RuntimeIDClaude
	binding.displayName = "Claude"
	binding.executableLogKey = "claude_binary"
	binding.modelConfigID = claudeACPModelConfigID
	binding.reasoningConfigID = claudeACPReasoningConfigID
	binding.supportsMCPTransport = allMCPTransports

	return binding
}

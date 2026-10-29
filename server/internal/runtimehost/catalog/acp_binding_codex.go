package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	// codexACPModelConfigID 是 acp-go Codex Adapter 的模型配置标识。
	codexACPModelConfigID acp.SessionConfigId = "model"
	// codexACPReasoningConfigID 是 acp-go Codex Adapter 的推理强度配置标识。
	codexACPReasoningConfigID acp.SessionConfigId = "reasoning_effort"
)

// codexACPRuntimeBinding 组合 Codex 自身的 ACP 能力与产品策略。
func codexACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDCodex
	binding.displayName = "Codex"
	binding.executableLogKey = "codex_binary"
	binding.modelConfigID = codexACPModelConfigID
	binding.reasoningConfigID = codexACPReasoningConfigID

	return binding
}

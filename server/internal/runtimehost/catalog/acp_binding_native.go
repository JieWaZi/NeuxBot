package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"github.com/JieWaZi/NeuxBot/server/constants"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// managedNativeBinding 声明受管原生工厂与产品权限目录。
func managedNativeBinding(id, name string) acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.nativeVersionMetadataOnly = true
	binding.baseRuntimeID = id
	binding.displayName = name
	binding.supportsMCPTransport = allMCPTransports
	binding.permissionLevels = func([]acp.SessionConfigOption, []acp.SessionMode) []runtimecontract.PermissionLevel {
		return []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}
	}
	return binding
}

// openCodeACPRuntimeBinding 准备 OpenCode 原生管理工厂。
func openCodeACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(runtimecontract.RuntimeIDOpenCode, "OpenCode")

	return binding
}

// geminiACPRuntimeBinding 使用账号受管根保存 owned-child 的稳定身份和原生历史。
func geminiACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(runtimecontract.RuntimeIDGemini, "Gemini CLI")
	binding.stateDirectory = func(prepared preparedRuntime, _ runtimecontract.Workspace) string {
		return prepared.environment.get("GEMINI_CLI_HOME")
	}

	return binding
}

// grokACPRuntimeBinding 由原生会话 metadata 安装 ask、auto 或 yolo 权限。
func grokACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(runtimecontract.RuntimeIDGrok, "Grok Build")

	return binding
}

// qoderACPRuntimeBinding 直连官方 stdio ACP，权限与模型仅使用当前会话声明的目录。
// 官方契约：https://docs.qoder.com/cli/acp；当前模式目录同时核对 @qoder-ai/qodercli 1.1.78。
func qoderACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDQoder
	binding.displayName = "Qoder"
	binding.supportsMCPTransport = allMCPTransports
	binding.permissionLevels = func(options []acp.SessionConfigOption, modes []acp.SessionMode) []runtimecontract.PermissionLevel {
		available := acpAvailableModeIDs(options, modes)
		levels := []runtimecontract.PermissionLevel{}
		if hasAnyMode(available, constants.RuntimeQoderDefaultModeID) {
			levels = append(levels, runtimecontract.PermissionLevelDefault)
		}
		if hasAnyMode(available, constants.RuntimeQoderAutoModeID) {
			levels = append(levels, runtimecontract.PermissionLevelAuto)
		}
		if hasAnyMode(available, constants.RuntimeQoderBypassModeID, constants.RuntimeQoderYoloModeID) {
			levels = append(levels, runtimecontract.PermissionLevelFullAccess)
		}
		return levels
	}

	return binding
}

// qoderCNACPRuntimeBinding 与国际版共用官方 ACP 语义，进程身份和配置仍各自隔离。
func qoderCNACPRuntimeBinding() acpRuntimeBinding {
	binding := qoderACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDQoderCN
	binding.displayName = "Qoder CN"
	return binding
}

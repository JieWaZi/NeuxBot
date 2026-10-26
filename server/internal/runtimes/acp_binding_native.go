package runtimes

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	acpgemini "github.com/JieWaZi/acp-go/pkg/gemini"
	acpgrok "github.com/JieWaZi/acp-go/pkg/grok"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	acpopencode "github.com/JieWaZi/acp-go/pkg/opencode"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// managedNativeBinding 由适配器冻结权限，每次调用重建进程并通过原生历史恢复。
func managedNativeBinding(id, name string) acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.nativeVersionMetadataOnly = true
	binding.baseRuntimeID = id
	binding.displayName = name
	binding.supportsMCPTransport = allMCPTransports
	binding.permissionLevels = func([]acp.SessionConfigOption, []acp.SessionMode) []PermissionLevel {
		return []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	}
	binding.modeID = func(ExecutionPolicy, *acp.SessionModeState) (acp.SessionModeId, error) {
		return "", nil
	}
	binding.validateSelection = cursorValidateSelection
	return binding
}

// openCodeACPRuntimeBinding 只在受管账号边界转换真实 provider/model ID。
func openCodeACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(RuntimeIDOpenCode, "OpenCode")
	binding.resolveSelections = func(_ *embeddedACPAgent, _ acp.SessionId, request RunRequest) RunRequest {
		if request.Target.Isolated && request.Target.AccountID != "" {
			request.Policy.Model = accountconfig.NativeModelID(RuntimeIDOpenCode, request.Policy.Model)
		}
		return request
	}
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpopencode.NewAgent(ctx, acpopencode.Config{Logger: acpAdapterLogger(ctx), OpenCodePath: config.Executable, PrefixArgs: cloneStrings(config.PrefixArgs), Environment: cloneStrings(config.Environment), WorkingDirectory: config.WorkingDirectory, PermissionMode: config.PermissionMode})
	}
	return binding
}

// geminiACPRuntimeBinding 使用账号受管根保存 owned-child 的稳定身份和原生历史。
func geminiACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(RuntimeIDGemini, "Gemini CLI")
	binding.stateDirectory = func(request acpRunRequest) string { return request.Prepared.environment.get("GEMINI_CLI_HOME") }
	binding.modeID = func(policy ExecutionPolicy, _ *acp.SessionModeState) (acp.SessionModeId, error) {
		if policy.WorkspaceReadOnly {
			return "default", nil
		}
		if policy.PermissionLevel == PermissionLevelFullAccess {
			return "yolo", nil
		}
		return "default", nil
	}
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpgemini.NewAgent(ctx, acpgemini.Config{Logger: acpAdapterLogger(ctx), GeminiPath: config.Executable, PrefixArgs: cloneStrings(config.PrefixArgs), Environment: cloneStrings(config.Environment), WorkingDirectory: config.WorkingDirectory, StateDirectory: config.StateDirectory, PermissionMode: config.PermissionMode})
	}
	return binding
}

// grokACPRuntimeBinding 由原生会话 metadata 安装 ask、auto 或 yolo 权限。
func grokACPRuntimeBinding() acpRuntimeBinding {
	binding := managedNativeBinding(RuntimeIDGrok, "Grok Build")
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpgrok.NewAgent(ctx, acpgrok.Config{Logger: acpAdapterLogger(ctx), GrokPath: config.Executable, PrefixArgs: cloneStrings(config.PrefixArgs), Environment: cloneStrings(config.Environment), WorkingDirectory: config.WorkingDirectory, PermissionMode: config.PermissionMode})
	}
	return binding
}

// qoderACPRuntimeBinding 直连官方 stdio ACP，权限与模型仅使用当前会话声明的目录。
// 官方契约：https://docs.qoder.com/cli/acp；当前模式目录同时核对 @qoder-ai/qodercli 1.1.78。
func qoderACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDQoder
	binding.displayName = "Qoder"
	binding.supportsMCPTransport = allMCPTransports
	binding.validateSelection = cursorValidateSelection
	binding.permissionLevels = func(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
		available := acpAvailableModeIDs(options, modes)
		levels := []PermissionLevel{}
		if hasAnyMode(available, constants.RuntimeQoderDefaultModeID) {
			levels = append(levels, PermissionLevelDefault)
		}
		if hasAnyMode(available, constants.RuntimeQoderAutoModeID) {
			levels = append(levels, PermissionLevelAuto)
		}
		if hasAnyMode(available, constants.RuntimeQoderBypassModeID, constants.RuntimeQoderYoloModeID) {
			levels = append(levels, PermissionLevelFullAccess)
		}
		return levels
	}
	binding.modeID = func(policy ExecutionPolicy, modes *acp.SessionModeState) (acp.SessionModeId, error) {
		// 只读不能借用权限菜单中的其他模式；缺少目录时不猜测执行权限。
		if policy.WorkspaceReadOnly || modes == nil {
			return "", status_error.RuntimeProtocolIncompatible
		}
		candidates := []string{constants.RuntimeQoderDefaultModeID}
		if policy.PermissionLevel == PermissionLevelFullAccess {
			candidates = []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}
		} else if policy.PermissionLevel == PermissionLevelAuto {
			candidates = []string{constants.RuntimeQoderAutoModeID}
		}
		for _, candidate := range candidates {
			for _, mode := range modes.AvailableModes {
				if string(mode.Id) == candidate {
					return mode.Id, nil
				}
			}
		}
		return "", status_error.RuntimeProtocolIncompatible
	}
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return nativeacp.NewAgent(ctx, nativeacp.Config{
			Command: config.Executable, Args: append(cloneStrings(config.PrefixArgs), "--acp"),
			VersionArgs: append(cloneStrings(config.PrefixArgs), "--version"), RuntimeName: config.RuntimeID,
			Environment: cloneStrings(config.Environment), WorkingDirectory: config.WorkingDirectory,
			Logger: acpAdapterLogger(ctx), PromptFIFO: true,
		})
	}
	return binding
}

// qoderCNACPRuntimeBinding 与国际版共用官方 ACP 语义，进程身份和配置仍各自隔离。
func qoderCNACPRuntimeBinding() acpRuntimeBinding {
	binding := qoderACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDQoderCN
	binding.displayName = "Qoder CN"
	return binding
}

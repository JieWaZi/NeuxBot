package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"maps"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpAdapterConfig 保存所有 acp-go Runtime Adapter 共用的启动事实。
type acpAdapterConfig struct {
	// resources 由外部管理所有者持有刷新入口，原生退出前不释放。
	resources *managementOwner
	// RefreshCredentials 将原生刷新路由到共享凭据权威。
	RefreshCredentials runtimecontract.CredentialRefresh
	// BaseRuntimeID 是公开原生工厂的基础身份。
	BaseRuntimeID string
	// RuntimeID 是本次连接所属的内置智能体或本地配置标识。
	RuntimeID string
	// DetectionID 关联一次独立能力探测；普通执行为空。
	DetectionID string
	// Executable 是探测阶段已经解析并验证的 Runtime 可执行文件。
	Executable string
	// PrefixArgs 是本地 Profile 配置的受控前置参数。
	PrefixArgs []string
	// NativePluginDirectories 是本轮已经固定的原生包目录。
	NativePluginDirectories []string
	// CodexPluginConfiguration 是自有插件仅在本次进程生效的原生点路径覆盖。
	CodexPluginConfiguration map[string]string
	// ClaudeSettingsPath 是只对本次进程生效的受管模型设置文件。
	ClaudeSettingsPath string
	// Environment 是 Adapter 与其 Session 进程使用的完整环境快照。
	Environment []string
	// WorkingDirectory 是原生 ACP 进程的隔离启动目录。
	WorkingDirectory string
	// StateDirectory 是 Cursor 等 Adapter 跨进程恢复使用的持久状态目录。
	StateDirectory string
	// PermissionMode 是本次原生进程冻结的执行权限。
	PermissionMode string
}

// acpAgentFactory 从统一启动事实创建一个标准 ACP Agent。
type acpAgentFactory func(context.Context, acpAdapterConfig) (acp.Agent, error)

// acpRuntimeBinding 保存探测目录与原生管理工厂的厂商差异。
type acpRuntimeBinding struct {
	// baseRuntimeID 是内置智能体标识。
	baseRuntimeID string
	// displayName 是诊断展示名称。
	displayName string
	// executableLogKey 是结构化日志的程序字段名。
	executableLogKey string
	// modelConfigID 是模型目录的配置标识。
	modelConfigID acp.SessionConfigId
	// modelDisplayName 根据已声明的目录元数据补充展示名称。
	modelDisplayName func(acp.SessionConfigSelectOption, string) string
	// catalogOptions 约束产品公开的模型和推理目录。
	catalogOptions func(acpOptionCatalog) acpOptionCatalog
	// probeModelOptions 表示需要在临时会话中补探具体模型配置。
	probeModelOptions bool
	// reasoningConfigID 是推理强度目录的配置标识。
	reasoningConfigID acp.SessionConfigId
	// permissionLevels 把实际模式目录映射为产品权限。
	permissionLevels func([]acp.SessionConfigOption, []acp.SessionMode) []runtimecontract.PermissionLevel
	// supportsMCPTransport 声明工厂支持的 MCP 传输方式。
	supportsMCPTransport func(runtimecontract.ConnectorTransport) bool
	// stateDirectory 返回原生管理连接需要的持久状态位置。
	stateDirectory func(preparedRuntime, runtimecontract.Workspace) string
	// skillDirectory 表示适配器读取标准技能目录。
	skillDirectory bool
	// newAgent 从已准备的事实构造原生适配器。
	newAgent acpAgentFactory
	// nativeVersionMetadataOnly 防止将适配器构建号当作 CLI 版本。
	nativeVersionMetadataOnly bool
	// nativeProcess 表示协议由外部原生进程承载。
	nativeProcess bool
}

// defaultACPRuntimeBindings 返回所有内置 Runtime 的固定薄 Binding。
func defaultACPRuntimeBindings() map[string]acpRuntimeBinding {
	return map[string]acpRuntimeBinding{
		runtimecontract.RuntimeIDOpenCode:  openCodeACPRuntimeBinding(),
		runtimecontract.RuntimeIDGemini:    geminiACPRuntimeBinding(),
		runtimecontract.RuntimeIDGrok:      grokACPRuntimeBinding(),
		runtimecontract.RuntimeIDWorkBuddy: workBuddyACPRuntimeBinding(),
		runtimecontract.RuntimeIDQoder:     qoderACPRuntimeBinding(),
		runtimecontract.RuntimeIDQoderCN:   qoderCNACPRuntimeBinding(),
		runtimecontract.RuntimeIDCodex:     codexACPRuntimeBinding(),
		runtimecontract.RuntimeIDClaude:    claudeACPRuntimeBinding(),
		runtimecontract.RuntimeIDKimi:      kimiACPRuntimeBinding(),
		runtimecontract.RuntimeIDCursor:    cursorACPRuntimeBinding(),
		runtimecontract.RuntimeIDPi:        piACPRuntimeBinding(),
	}
}

// standardACPRuntimeBinding 声明通用配置目录与兼容传输。
func standardACPRuntimeBinding() acpRuntimeBinding {
	return acpRuntimeBinding{
		executableLogKey:  "agent_binary",
		newAgent:          newNativeAgent,
		modelConfigID:     "model",
		reasoningConfigID: "reasoning",
		permissionLevels:  acpPermissionLevels,
		supportsMCPTransport: func(transport runtimecontract.ConnectorTransport) bool {
			return transport == runtimecontract.ConnectorTransportStdio || transport == runtimecontract.ConnectorTransportStreamableHTTP
		},
		skillDirectory: true,
	}
}

// acpAdapterConfigForManagement 投影账号、工作区与原生工厂所需的启动事实。
func acpAdapterConfigForManagement(prepared preparedRuntime, request runtimecontract.RunRequest, binding acpRuntimeBinding) acpAdapterConfig {
	config := acpAdapterConfig{
		RefreshCredentials:       request.Target.RefreshCredentials,
		BaseRuntimeID:            binding.baseRuntimeID,
		RuntimeID:                request.Target.RuntimeID,
		Executable:               prepared.detection.ResolvedBinary,
		PrefixArgs:               cloneStrings(prepared.definition.prefixArgs),
		Environment:              prepared.environment.list(),
		WorkingDirectory:         request.Workspace.Path,
		PermissionMode:           string(request.Policy.Execution.PermissionLevel),
		NativePluginDirectories:  cloneStrings(request.Target.NativePluginDirectories),
		CodexPluginConfiguration: maps.Clone(request.Target.CodexPluginConfiguration),
		ClaudeSettingsPath:       request.Target.ClaudeSettingsPath,
	}
	if binding.stateDirectory != nil {
		config.StateDirectory = binding.stateDirectory(prepared, request.Workspace)
	}
	return config
}

// allMCPTransports 接受 NeuxBot 当前支持的三种 MCP 传输。
func allMCPTransports(transport runtimecontract.ConnectorTransport) bool {
	return transport == runtimecontract.ConnectorTransportStdio ||
		transport == runtimecontract.ConnectorTransportStreamableHTTP ||
		transport == runtimecontract.ConnectorTransportSSE
}

// acpAvailableModeIDs 合并标准模式目录与 mode 类配置中的原生标识。
func acpAvailableModeIDs(options []acp.SessionConfigOption, modes []acp.SessionMode) map[string]struct{} {
	available := make(map[string]struct{})
	for _, mode := range modes {
		available[string(mode.Id)] = struct{}{}
	}
	for _, option := range options {
		if option.Select == nil || option.Select.Category == nil ||
			*option.Select.Category != acp.SessionConfigOptionCategoryMode {
			continue
		}
		for _, value := range acpSelectValues(option.Select.Options) {
			available[string(value.Value)] = struct{}{}
		}
	}
	return available
}

// nativeACPRuntimeBinding 声明由外部 CLI 承载协议的管理工厂。
func nativeACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.nativeProcess = true
	return binding
}

package runtimes

import (
	"context"
	"encoding/json"
	"maps"
	"strconv"
	"strings"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpAdapterConfig 保存所有 acp-go Runtime Adapter 共用的启动事实。
type acpAdapterConfig struct {
	// RefreshCredentials 将原生刷新路由到共享凭据权威。
	RefreshCredentials CredentialRefresh
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
	// UserInput 表示本次拥有宿主问答交付通道，独立结果收集调用不安装该桥。
	UserInput bool
}

// acpAgentFactory 从统一启动事实创建一个标准 ACP Agent。
type acpAgentFactory func(context.Context, acpAdapterConfig) (acp.Agent, error)

// acpRuntimeBinding 只保存不同 Runtime 在 ACP 标准边界之外的最小差异。
type acpRuntimeBinding struct {
	// baseRuntimeID 是绑定对应的内置 Runtime 标识。
	baseRuntimeID string
	// displayName 用于不含秘密值的诊断与错误消息。
	displayName string
	// executableLogKey 是结构化日志中的可执行文件字段名。
	executableLogKey string
	// modelConfigID 是 Adapter 暴露的模型配置标识。
	modelConfigID acp.SessionConfigId
	// modelDisplayName 根据厂商明确返回的目录信息补充模型展示名称，不改写选择标识。
	modelDisplayName func(acp.SessionConfigSelectOption, string) string
	// catalogOptions 约束厂商在产品内开放的模型与推理目录，不改变底层协议回执。
	catalogOptions func(acpOptionCatalog) acpOptionCatalog
	// probeModelOptions 在独占临时会话中补探未提供专属配置的可见模型。
	probeModelOptions bool
	// reasoningConfigID 是 Adapter 暴露的推理强度配置标识。
	reasoningConfigID acp.SessionConfigId
	// permissionLevels 把当前 Adapter 的模式目录映射为统一产品权限。
	permissionLevels func([]acp.SessionConfigOption, []acp.SessionMode) []PermissionLevel
	// supportsMCPTransport 声明当前 Adapter 可以接收的 MCP 传输类型。
	supportsMCPTransport func(ConnectorTransport) bool
	// elicitationSessionPaths 声明结构化交互元数据中的会话标识路径。
	elicitationSessionPaths [][]string
	// modeID 根据执行策略和上游实际目录选择会话模式。
	modeID func(ExecutionPolicy, *acp.SessionModeState) (acp.SessionModeId, error)
	// resolveSelections 补全依赖上游动态目录的默认模型与推理配置。
	resolveSelections func(*embeddedACPAgent, acp.SessionId, RunRequest) RunRequest
	// skipSelection 判断某项已经生效、无需重复写入。
	skipSelection func(*embeddedACPAgent, acp.SessionId, acp.SessionConfigId, string) bool
	// validateSelection 校验配置回执与本次选择仍然一致。
	validateSelection func(*embeddedACPAgent, acp.SessionId, RunRequest, acp.SessionConfigId, string, []acp.SessionConfigOption) error
	// supportsPersistent 判断上游能力是否足以保留热会话。
	supportsPersistent func(acp.AgentCapabilities) bool
	// retainPromptFailure 判断指定失败是否仍应保留可恢复会话。
	retainPromptFailure func(FailureCode) bool
	// failureCode 把 Adapter 专属错误证据归一为稳定失败码。
	failureCode func(error) FailureCode
	// agentScope 返回影响外部 Agent 隔离边界的稳定摘要原文。
	agentScope func(acpRunRequest) string
	// stateDirectory 返回 Adapter 跨进程恢复使用的持久状态目录。
	stateDirectory func(acpRunRequest) string
	// selectedSkillPrompt 使用 Runtime 原生命令语法表达显式 Skill 选择。
	selectedSkillPrompt func(string, []skillSelection) string
	// messagePhase 读取 Adapter 保留的可选消息阶段元数据。
	messagePhase func(map[string]any) string
	// toolIdentity 读取 Adapter 保留的可选工具身份元数据。
	toolIdentity func(map[string]any) (string, string)
	// skillDirectory 表示 Adapter 会从标准 Runtime Skill 目录发现技能。
	skillDirectory bool
	// newAgent 使用对应 acp-go 包创建标准 ACP Agent。
	newAgent acpAgentFactory
	// nativeVersionMetadataOnly 禁止用适配器构建号充当真实 CLI 版本。
	nativeVersionMetadataOnly bool
	// nativeProcess 表示外部 ACP 进程不能跨普通调用共享。
	nativeProcess bool
	// retainedProcess 表示外部进程由可恢复聊天独占并跨轮次保留。
	retainedProcess bool
}

// defaultACPRuntimeBindings 返回所有内置 Runtime 的固定薄 Binding。
func defaultACPRuntimeBindings() map[string]acpRuntimeBinding {
	return map[string]acpRuntimeBinding{
		RuntimeIDOpenCode:  openCodeACPRuntimeBinding(),
		RuntimeIDGemini:    geminiACPRuntimeBinding(),
		RuntimeIDGrok:      grokACPRuntimeBinding(),
		RuntimeIDWorkBuddy: workBuddyACPRuntimeBinding(),
		RuntimeIDQoder:     qoderACPRuntimeBinding(),
		RuntimeIDQoderCN:   qoderCNACPRuntimeBinding(),
		RuntimeIDCodex:     codexACPRuntimeBinding(),
		RuntimeIDClaude:    claudeACPRuntimeBinding(),
		RuntimeIDKimi:      kimiACPRuntimeBinding(),
		RuntimeIDCursor:    cursorACPRuntimeBinding(),
		RuntimeIDPi:        piACPRuntimeBinding(),
	}
}

// standardACPRuntimeBinding 提供不含任何厂商身份的安全默认行为。
func standardACPRuntimeBinding() acpRuntimeBinding {
	return acpRuntimeBinding{
		executableLogKey:  "agent_binary",
		modelConfigID:     "model",
		reasoningConfigID: "reasoning",
		permissionLevels:  acpPermissionLevels,
		supportsMCPTransport: func(transport ConnectorTransport) bool {
			return transport == ConnectorTransportStdio || transport == ConnectorTransportStreamableHTTP
		},
		elicitationSessionPaths: [][]string{{"sessionId"}, {"session", "sessionId"}},
		modeID:                  func(ExecutionPolicy, *acp.SessionModeState) (acp.SessionModeId, error) { return "", nil },
		resolveSelections:       func(_ *embeddedACPAgent, _ acp.SessionId, request RunRequest) RunRequest { return request },
		skipSelection:           func(*embeddedACPAgent, acp.SessionId, acp.SessionConfigId, string) bool { return false },
		validateSelection: func(*embeddedACPAgent, acp.SessionId, RunRequest, acp.SessionConfigId, string, []acp.SessionConfigOption) error {
			return nil
		},
		supportsPersistent: func(capabilities acp.AgentCapabilities) bool {
			return capabilities.SessionCapabilities.Resume != nil && capabilities.SessionCapabilities.Close != nil
		},
		retainPromptFailure: func(FailureCode) bool { return false },
		failureCode:         acpFailureCode,
		agentScope:          func(acpRunRequest) string { return "" },
		stateDirectory:      func(acpRunRequest) string { return "" },
		selectedSkillPrompt: func(prompt string, selections []skillSelection) string {
			return selectedSkillCommandPrompt("/", prompt, selections)
		},
		messagePhase:   func(map[string]any) string { return "" },
		toolIdentity:   func(map[string]any) (string, string) { return "", "" },
		skillDirectory: true,
	}
}

// acpAdapterConfigForRequest 把 NeuxBot 已准备完成的启动事实投影为通用 Adapter 配置。
func acpAdapterConfigForRequest(
	request acpRunRequest,
	binding acpRuntimeBinding,
) acpAdapterConfig {
	config := acpAdapterConfig{
		RefreshCredentials: request.Request.Target.RefreshCredentials,
		RuntimeID:          request.Request.Target.RuntimeID,
		Executable:         request.Prepared.detection.ResolvedBinary,
		PrefixArgs:         cloneStrings(request.Prepared.definition.prefixArgs),
		Environment:        request.Prepared.environment.list(),
		WorkingDirectory:   request.Request.Workspace.Path,
		PermissionMode:     string(request.Request.Policy.Execution.PermissionLevel),
		UserInput:          request.Text == nil,
	}
	config.NativePluginDirectories = cloneStrings(request.Request.Target.NativePluginDirectories)
	config.CodexPluginConfiguration = maps.Clone(request.Request.Target.CodexPluginConfiguration)
	config.ClaudeSettingsPath = request.Request.Target.ClaudeSettingsPath
	if binding.stateDirectory != nil {
		config.StateDirectory = binding.stateDirectory(request)
	}

	return config
}

// acpAgentKey 使不同 Runtime、二进制版本和受控配置使用独立 ACP Agent。
func acpAgentKey(request acpRunRequest) string {
	binding := defaultACPRuntimeBindings()[request.Prepared.definition.baseRuntimeID]
	return acpAgentKeyForBinding(request, binding)
}

// acpAgentKeyForBinding 使用当前 Backend 实际绑定计算 Agent 隔离键。
func acpAgentKeyForBinding(request acpRunRequest, binding acpRuntimeBinding) string {
	prefixArgs := cloneStrings(request.Prepared.definition.prefixArgs)
	for _, directory := range request.Request.Target.NativePluginDirectories {
		prefixArgs = append(prefixArgs, directory)
	}
	pluginConfiguration, _ := json.Marshal(request.Request.Target.CodexPluginConfiguration)
	workspace := ""
	if binding.agentScope != nil {
		workspace = binding.agentScope(request)
	}
	if request.Text != nil {
		workspace += "\x00text\x00" + request.InvocationID
	}
	return request.Request.Target.RuntimeID + "\x00" +
		request.Prepared.detection.ResolvedBinary + "\x00" +
		request.Prepared.detection.Version + "\x00" +
		strconv.Itoa(len(prefixArgs)) + "\x00" +
		strings.Join(prefixArgs, "\x00") + "\x00" + string(pluginConfiguration) + "\x00" + request.Request.Target.NativePluginScope + "\x00" + request.Request.Target.ClaudeSettingsPath + "\x00" +
		request.Prepared.environment.fingerprint + "\x00" + workspace
}

// allMCPTransports 接受 NeuxBot 当前支持的三种 MCP 传输。
func allMCPTransports(transport ConnectorTransport) bool {
	return transport == ConnectorTransportStdio ||
		transport == ConnectorTransportStreamableHTTP ||
		transport == ConnectorTransportSSE
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

// nativeACPRuntimeBinding 提供普通外部 CLI 共用的进程隔离策略。
func nativeACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.nativeProcess = true
	binding.agentScope = func(request acpRunRequest) string {
		return request.Request.Workspace.Path + "\x00" + request.InvocationID
	}
	return binding
}

// sessionACPAgentScope 将可热复用的连接绑定到唯一原生会话，新建前临时使用调用身份。
func sessionACPAgentScope(request acpRunRequest) string {
	if request.Request.Session.Mode == SessionModeResume {
		return "session:" + request.Request.Session.ID
	}
	return "invocation:" + request.InvocationID
}

// selectedSkillCommandPrompt 按 Adapter 原生命令前缀构造有序 Skill 选择。
func selectedSkillCommandPrompt(prefix string, prompt string, selections []skillSelection) string {
	if len(selections) == 0 {
		return prompt
	}
	var builder strings.Builder
	for _, selection := range selections {
		builder.WriteString(prefix)
		builder.WriteString(selection.name)
		builder.WriteString("\n")
	}
	if prompt != "" {
		builder.WriteString("\n")
		builder.WriteString(prompt)
	}
	return builder.String()
}

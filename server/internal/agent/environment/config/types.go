// Package config 保存运行环境的中立配置事实，不拥有业务会话或资源。
package config

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
)

const (
	// RuntimeIDCodex 是内置 Codex CLI 运行时标识。
	RuntimeIDCodex = constants.RuntimeIDCodex
	// RuntimeIDClaude 是内置 Claude Code 运行时标识。
	RuntimeIDClaude = constants.RuntimeIDClaude
	// RuntimeIDKimi 是 Kimi CLI 运行时标识。
	RuntimeIDKimi = constants.RuntimeIDKimi
	// RuntimeIDCursor 是 Cursor CLI 运行时标识。
	RuntimeIDCursor = constants.RuntimeIDCursor
	// RuntimeIDPi 是 Pi 运行时标识。
	RuntimeIDPi = constants.RuntimeIDPi
	// RuntimeIDOpenCode 是 OpenCode 运行时标识。
	RuntimeIDOpenCode = constants.RuntimeIDOpenCode
	// RuntimeIDGemini 是 Gemini CLI 运行时标识。
	RuntimeIDGemini = constants.RuntimeIDGemini
	// RuntimeIDGrok 是 Grok Build 运行时标识。
	RuntimeIDGrok = constants.RuntimeIDGrok
	// RuntimeIDWorkBuddy 是 WorkBuddy 标准 ACP 运行时标识。
	RuntimeIDWorkBuddy = constants.RuntimeIDWorkBuddy
	// RuntimeIDQoder 是 Qoder CLI 标准 ACP 运行时标识。
	RuntimeIDQoder = constants.RuntimeIDQoder
	// RuntimeIDQoderCN 是 Qoder 中国版 CLI 标准 ACP 运行时标识。
	RuntimeIDQoderCN = constants.RuntimeIDQoderCN
)

// CredentialRefresh 把凭据更新交给宿主权威，适配器只在原生认证请求时调用。
type CredentialRefresh = codexoauth.RefreshFunc

// DetectRequest 保存有界探测 Runtime 所需的显式输入。
type DetectRequest struct {
	// ReleaseResources 在原生使用者退出后回收本次账号与目录，失败保留所有者并允许重试。
	ReleaseResources func(context.Context) error `json:"-"`
	// UseNativeAuth 明确本次探测使用系统原生认证文件，受管账号始终为 false。
	UseNativeAuth bool `json:"-"`
	// RefreshCredentials 将原生能力探测的刷新交给账号凭据权威。
	RefreshCredentials CredentialRefresh `json:"-"`
	// WorkingDirectory 是调用方创建并拥有的隔离探测目录；空值由 SDK 创建。
	WorkingDirectory string
	// Model 是本次必须实际选择并读取能力的原生模型 ID。
	Model string
	// RuntimeID 指定内置 Runtime 或 Local Profile 的开放字符串 ID。
	RuntimeID string
	// BaseEnvironment 是调用方显式提供的基础进程环境。
	BaseEnvironment []string
	// HomeDirectory 用于补全 GUI 进程可能缺失的常见可执行文件目录。
	HomeDirectory string
	// LaunchOverride 保存调用方允许覆盖的命令、前置参数和环境层。
	LaunchOverride LaunchOverride
}

// LaunchOverride 保存运行时环境条目允许用户编辑的有限启动覆盖。
type LaunchOverride struct {
	// Binary 是单个可执行文件名或绝对路径，空值表示沿用 Definition 候选。
	Binary string
	// PrefixArgs 是放在 Runtime 子命令之前的参数；nil 表示沿用 Definition 参数。
	PrefixArgs []string
	// Environment 是受控的 Runtime 环境覆盖，例如 CODEX_HOME。
	Environment map[string]string
}

// ModelOption 描述 Runtime 可以选择的一个模型。
type ModelOption struct {
	// ID 是通过 ACP Session 配置传递的模型标识。
	ID string
	// Name 是面向调用方展示的模型名称。
	Name string
	// ReasoningOptions 是该模型通过 Runtime 协议声明支持的推理强度。
	ReasoningOptions []ReasoningOption
	// PermissionLevels 是该模型当前可以准确表达的产品权限等级。
	PermissionLevels []PermissionLevel
}

// ReasoningOption 描述 Runtime 可以选择的一个推理强度。
type ReasoningOption struct {
	// ID 是通过 ACP Session 配置传递的推理强度标识。
	ID string
	// Name 是面向调用方展示的推理强度名称。
	Name string
}

// ProfileSpec 描述对内置 Runtime Definition 的有限覆盖。
type ProfileSpec struct {
	// ID 是 Profile 自己的开放字符串 Runtime ID。
	ID string
	// BaseRuntimeID 指定被继承的内置 Runtime ID。
	BaseRuntimeID string
	// Name 覆盖 Runtime 的展示名称，空值表示继承。
	Name string
	// BinaryCandidates 覆盖按优先级尝试的可执行文件候选，nil 表示继承。
	BinaryCandidates []string
	// PrefixArgs 覆盖每次调用都放在子命令之前的参数，nil 表示继承。
	PrefixArgs []string
	// Environment 合并到 Base Definition 环境层，同名键覆盖 Base。
	Environment map[string]string
	// Models 覆盖模型提示列表，nil 表示继承。
	Models []ModelOption
	// DefaultModel 覆盖默认模型标识，空值表示继承。
	DefaultModel string
}

// RuntimeSummary 是调用方可安全展示的 Runtime Definition 摘要。
type RuntimeSummary struct {
	// ID 是 Runtime 或 Local Profile 的开放字符串标识。
	ID string
	// BaseRuntimeID 是 Profile 继承的内置 Runtime ID，内置项等于自身 ID。
	BaseRuntimeID string
	// Name 是 Runtime 的展示名称。
	Name string
	// Profile 表示该项是否由调用方提供的 Local Profile 生成。
	Profile bool
	// BinaryCandidates 是按优先级尝试的可执行文件候选。
	BinaryCandidates []string
	// PrefixArgs 是每次调用都放在 Runtime 子命令之前的参数。
	PrefixArgs []string
	// Models 是当前 Definition 提供的模型提示。
	Models []ModelOption
	// DefaultModel 是未显式选择模型时使用的模型标识。
	DefaultModel string
	// ReasoningOptions 是该 Runtime 明确支持的推理强度选项。
	ReasoningOptions []ReasoningOption
	// InstructionFileName 是该 Runtime 在工作区根目录使用的指令文件名。
	InstructionFileName string
	// SkillDirectoryName 是该 Runtime 在工作区根目录使用的技能目录名。
	SkillDirectoryName string
	// ConfigHomeEnvironmentVariable 是配置主目录对应的受控环境变量名。
	ConfigHomeEnvironmentVariable string
}

// DetectionState 是共享 Runtime 探测状态枚举的引用。
type DetectionState = constants.RuntimeDetectionState

const (
	// DetectionStateUnavailable 表示未找到或不能调用 Runtime 可执行文件。
	DetectionStateUnavailable = constants.RuntimeDetectionStateUnavailable
	// DetectionStateIncompatible 表示可执行文件存在但缺少必要协议能力。
	DetectionStateIncompatible = constants.RuntimeDetectionStateIncompatible
	// DetectionStateReady 表示 Runtime 必要能力满足且可以准备调用。
	DetectionStateReady = constants.RuntimeDetectionStateReady
)

// AuthStatus 是共享 Runtime 鉴权状态枚举的引用。
type AuthStatus = constants.RuntimeAuthStatus

const (
	// AuthStatusUnknown 表示 SDK 无法可靠判断鉴权状态。
	AuthStatusUnknown = constants.RuntimeAuthStatusUnknown
	// AuthStatusOK 表示鉴权探测没有发现缺失凭据。
	AuthStatusOK = constants.RuntimeAuthStatusOK
	// AuthStatusMissing 表示 ACP Agent 明确报告尚未登录或缺少凭据。
	AuthStatusMissing = constants.RuntimeAuthStatusMissing
)

// Capability 是共享 Runtime 能力枚举的引用。
type Capability = constants.RuntimeCapability

const (
	// CapabilityStructuredOutput 表示 Runtime 支持要求的结构化输出格式。
	CapabilityStructuredOutput = constants.RuntimeCapabilityStructuredOutput
	// CapabilityResumeSession 表示 Runtime 支持恢复原生 Session。
	CapabilityResumeSession = constants.RuntimeCapabilityResumeSession
	// CapabilityDynamicModels 表示本次探测取得了动态模型列表。
	CapabilityDynamicModels = constants.RuntimeCapabilityDynamicModels
	// CapabilityStreamingUserMessages 表示活跃调用可以继续接收用户消息。
	CapabilityStreamingUserMessages = constants.RuntimeCapabilityStreamingUserMessages
	// CapabilityAddDirectory 表示 Runtime 支持声明附加工作目录。
	CapabilityAddDirectory = constants.RuntimeCapabilityAddDirectory
	// CapabilityPartialMessages 表示 Runtime 支持更细粒度的流式消息。
	CapabilityPartialMessages = constants.RuntimeCapabilityPartialMessages
	// CapabilityImagePath 表示 Runtime 支持通过本地路径原生提交图片。
	CapabilityImagePath = constants.RuntimeCapabilityImagePath
	// CapabilitySkillDirectory 表示 Runtime 支持从调用方指定目录原生加载技能。
	CapabilitySkillDirectory = constants.RuntimeCapabilitySkillDirectory
	// CapabilityMCPStreamableHTTP 表示 Runtime 支持 Streamable HTTP MCP。
	CapabilityMCPStreamableHTTP = constants.RuntimeCapabilityMCPStreamableHTTP
	// CapabilityMCPSSE 表示 Runtime 支持旧版 SSE MCP。
	CapabilityMCPSSE = constants.RuntimeCapabilityMCPSSE
)

// CapabilitySet 保存一次探测确认可用的 Runtime 能力。
type CapabilitySet map[Capability]bool

// Has 返回指定能力是否在当前快照中可用。
func (set CapabilitySet) Has(capability Capability) bool {
	return set[capability]
}

// DiagnosticLevel 是共享 Runtime 诊断级别枚举的引用。
type DiagnosticLevel = constants.RuntimeDiagnosticLevel

const (
	// DiagnosticLevelInfo 表示不影响调用的补充信息。
	DiagnosticLevelInfo = constants.RuntimeDiagnosticLevelInfo
	// DiagnosticLevelWarning 表示已降级但仍可能继续调用。
	DiagnosticLevelWarning = constants.RuntimeDiagnosticLevelWarning
	// DiagnosticLevelError 表示当前 Runtime 不能安全启动。
	DiagnosticLevelError = constants.RuntimeDiagnosticLevelError
)

// Diagnostic 是不包含秘密值的稳定 Runtime 诊断。
type Diagnostic struct {
	// Code 是调用方可用于本地化和聚合的稳定诊断码。
	Code string
	// Level 表示诊断对当前操作的影响。
	Level DiagnosticLevel
	// Message 是面向开发和日志的简明技术说明。
	Message string
}

// DetectionResult 是一次无副作用 Runtime 探测的完整结论。
type DetectionResult struct {
	// Runtime 是本次探测使用的有效 Definition 摘要。
	Runtime RuntimeSummary
	// State 表示 Runtime 是否可用及协议是否兼容。
	State DetectionState
	// ResolvedBinary 是实际接受探测并将用于 Run 的可执行文件路径。
	ResolvedBinary string
	// Installations 保存当前发现及此前选择过的 CLI 安装实例。
	Installations []CLIInstallation
	// Version 是 ACP Agent 包装的 Runtime CLI 版本，未知时为空。
	Version string
	// Auth 是独立于安装状态的鉴权探测结论。
	Auth AuthStatus
	// Models 是动态探测结果或 Definition 提供的模型提示。
	Models []ModelOption
	// ReasoningOptions 是当前默认模型通过 Runtime 协议声明的推理强度。
	ReasoningOptions []ReasoningOption
	// DetectedDefaultModel 是探测时 ACP Session 当前生效的模型标识。
	DetectedDefaultModel string
	// DetectedDefaultReasoning 是探测时 ACP Session 当前生效的推理强度标识。
	DetectedDefaultReasoning string
	// Capabilities 是本次 Help 和协议探测确认的能力快照。
	Capabilities CapabilitySet
	// Diagnostics 保存探测失败、未知和能力降级信息。
	Diagnostics []Diagnostic
}

// CLIInstallation 是以可执行文件绝对路径标识的一个 CLI 安装实例。
type CLIInstallation struct {
	// Path 是该安装实例的可执行文件绝对路径。
	Path string
	// Version 是该路径报告的 CLI 版本，探测失败时为空或保留上次结果。
	Version string
	// Available 表示本次刷新时该路径仍可执行。
	Available bool
}

// PermissionLevel 是 NeuxBot 跨 Runtime 使用的三档产品权限。
type PermissionLevel = constants.PermissionLevel

const (
	// PermissionLevelDefault 表示敏感操作需要用户逐次确认。
	PermissionLevelDefault = constants.PermissionLevelDefault
	// PermissionLevelAuto 表示 Runtime 可以自动执行风险可接受的操作。
	PermissionLevelAuto = constants.PermissionLevelAuto
	// PermissionLevelFullAccess 表示用户明确允许 Runtime 使用最大可用权限。
	PermissionLevelFullAccess = constants.PermissionLevelFullAccess
)

// RuntimeTarget 保存一次调用选择的 Runtime 及受控本地启动覆盖。
type RuntimeTarget struct {
	// RefreshCredentials 只协调原生令牌刷新，不限制会话或模型请求并发。
	RefreshCredentials CredentialRefresh `json:"-"`
	// AccountID 是本次执行固定使用的受管账号，空值表示沿用现有 CLI 配置。
	AccountID string
	// Environment 是账号准备生成的受控环境变量，不进入事件或日志。
	Environment map[string]string
	// Isolated 表示启动时移除宿主认证变量。
	Isolated bool
	// CloseAfterRun 要求返回终态前关闭持有账号凭据的进程。
	CloseAfterRun bool

	// RuntimeID 是用户本次选择的 Runtime 或本地 Profile 标识。
	RuntimeID string
	// Binary 是环境条目配置的可执行文件；为空时使用 Runtime 定义默认值。
	Binary string
	// PrefixArgs 是本地 Profile 配置的受控前置参数。
	PrefixArgs []string
	// NativePluginDirectories 是本轮已经核验且独占交付的Claude原生包目录。
	NativePluginDirectories []string
	// CodexPluginConfiguration 是自有插件仅在本次进程生效的原生点路径覆盖。
	CodexPluginConfiguration map[string]string
	// NativePluginScope 是本轮外部原生插件事实摘要，变化时不得复用旧进程。
	NativePluginScope string
	// ClaudeSettingsPath 是只对本次进程生效的受管模型设置文件。
	ClaudeSettingsPath string
	// ConfigHome 是 Runtime 使用的受控配置目录。
	ConfigHome string
}

// ConnectorTransport 是共享 Runtime MCP 传输枚举的引用。
type ConnectorTransport = constants.RuntimeConnectorTransport

const (
	// ConnectorTransportStdio 表示本地标准输入输出传输。
	ConnectorTransportStdio = constants.RuntimeConnectorTransportStdio
	// ConnectorTransportStreamableHTTP 表示 Streamable HTTP 传输。
	ConnectorTransportStreamableHTTP = constants.RuntimeConnectorTransportStreamableHTTP
	// ConnectorTransportSSE 表示旧版 HTTP SSE 传输。
	ConnectorTransportSSE = constants.RuntimeConnectorTransportSSE
)

// LaunchFacts 是已有探测与账号准备共同确认的原生启动事实，不创建协议连接。
type LaunchFacts struct {
	// BaseRuntimeID 决定公开适配器的协议类型。
	BaseRuntimeID string
	// Executable 是有效探测已经解析的真实程序路径。
	Executable string
	// PrefixArgs 是 Profile 与启动覆盖合并后的前置参数。
	PrefixArgs []string
	// Environment 是账号隔离、Profile 覆盖与工具路径补充后的完整环境。
	Environment []string
}

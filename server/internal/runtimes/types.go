// Package runtimes 提供不依赖业务持久化的 ACP Agent 运行时 SDK。
package runtimes

import (
	"context"
	"encoding/json"
	"time"

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

const (
	// EventTypeInvocationStarted 表示 Runtime 已完整接收本次首条用户输入。
	EventTypeInvocationStarted = constants.RuntimeEventTypeInvocationStarted
	// EventTypeStatus 表示 Runtime 的非终态协议状态。
	EventTypeStatus = constants.RuntimeEventTypeStatus
	// EventTypeTextDelta 表示面向用户的增量文本。
	EventTypeTextDelta = constants.RuntimeEventTypeTextDelta
	// EventTypeCommentaryDelta 表示工作过程中的用户可见说明增量。
	EventTypeCommentaryDelta = constants.RuntimeEventTypeCommentaryDelta
	// EventTypeThinkingStart 表示一次可见思考阶段开始。
	EventTypeThinkingStart = constants.RuntimeEventTypeThinkingStart
	// EventTypeThinkingDelta 表示可见思考内容增量。
	EventTypeThinkingDelta = constants.RuntimeEventTypeThinkingDelta
	// EventTypeToolUse 表示 Runtime 请求执行一个工具。
	EventTypeToolUse = constants.RuntimeEventTypeToolUse
	// EventTypeToolInputSnapshot 表示同一工具调用的累计参数状态，不表示再次执行。
	EventTypeToolInputSnapshot = constants.RuntimeEventTypeToolInputSnapshot
	// EventTypeToolUpdate 表示同一工具调用的非终态执行进度、正文或差异更新，不表示再次请求执行。
	EventTypeToolUpdate = constants.RuntimeEventTypeToolUpdate
	// EventTypeToolResult 表示一个工具调用已经返回。
	EventTypeToolResult = constants.RuntimeEventTypeToolResult
	// EventTypeUsage 表示 Runtime 报告统一的 Turn Token 与 Session 上下文用量。
	EventTypeUsage = constants.RuntimeEventTypeUsage
	// EventTypeSession 表示 Runtime 创建或捕获了原生 Session。
	EventTypeSession = constants.RuntimeEventTypeSession
	// EventTypeError 表示 Runtime 协议报告错误。
	EventTypeError = constants.RuntimeEventTypeError
	// EventTypeDiagnostic 表示 SDK 产生了不含秘密值的诊断。
	EventTypeDiagnostic = constants.RuntimeEventTypeDiagnostic
	// EventTypeRaw 表示 SDK 保留了尚未识别的原始输出。
	EventTypeRaw = constants.RuntimeEventTypeRaw
)

// Config 保存构造 Runtime SDK 所需的显式配置。
type Config struct {
	// Profiles 保存由调用方提供且仅存在于当前 SDK 实例中的本地 Profile。
	Profiles []ProfileSpec
	// Defaults 保存 Invocation 阶段限制和事件队列默认值。
	Defaults Defaults
}

// Defaults 保存 SDK 构造后不可变的 ACP Client 资源保护限制。
type Defaults struct {
	// Inactivity 是 ACP Prompt 没有 Session Update 时允许等待的最长时间。
	Inactivity time.Duration
	// EventDelivery 是 Event Sink 单次交付超时。
	EventDelivery time.Duration
}

// CredentialRefresh 把凭据更新交给宿主权威，适配器只在原生认证请求时调用。
type CredentialRefresh = codexoauth.RefreshFunc

// DetectRequest 保存有界探测 Runtime 所需的显式输入。
type DetectRequest struct {
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

// DetectionState 是共享 Runtime 探测状态枚举的兼容别名。
type DetectionState = constants.RuntimeDetectionState

const (
	// DetectionStateUnavailable 表示未找到或不能调用 Runtime 可执行文件。
	DetectionStateUnavailable = constants.RuntimeDetectionStateUnavailable
	// DetectionStateIncompatible 表示可执行文件存在但缺少必要协议能力。
	DetectionStateIncompatible = constants.RuntimeDetectionStateIncompatible
	// DetectionStateReady 表示 Runtime 必要能力满足且可以准备调用。
	DetectionStateReady = constants.RuntimeDetectionStateReady
)

// AuthStatus 是共享 Runtime 鉴权状态枚举的兼容别名。
type AuthStatus = constants.RuntimeAuthStatus

const (
	// AuthStatusUnknown 表示 SDK 无法可靠判断鉴权状态。
	AuthStatusUnknown = constants.RuntimeAuthStatusUnknown
	// AuthStatusOK 表示鉴权探测没有发现缺失凭据。
	AuthStatusOK = constants.RuntimeAuthStatusOK
	// AuthStatusMissing 表示 ACP Agent 明确报告尚未登录或缺少凭据。
	AuthStatusMissing = constants.RuntimeAuthStatusMissing
)

// Capability 是共享 Runtime 能力枚举的兼容别名。
type Capability = constants.RuntimeCapability

const (
	// CapabilityIsolatedTextGeneration 表示探测会话已确认隔离文本生成策略可配置。
	CapabilityIsolatedTextGeneration = constants.RuntimeCapabilityIsolatedTextGeneration
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

// DiagnosticLevel 是共享 Runtime 诊断级别枚举的兼容别名。
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

// EventType 是共享 Runtime 事件类型枚举的兼容别名。
type EventType = constants.RuntimeEventType

// Event 是交给 Host Event Sink 的完整统一事件信封。
type Event struct {
	// Type 是稳定的统一事件类型。
	Type EventType
	// Data 是该事件类型对应的完整 JSON 载荷。
	Data json.RawMessage
}

// InvocationStartedData 保存首条输入交付成功后可用于建立调用身份的最小事实。
type InvocationStartedData struct {
	// InvocationID 是 Runtime 为本次调用生成的稳定标识。
	InvocationID string `json:"invocationId"`
}

// EventSink 是 Runtime 统一事件的串行交付 seam。
type EventSink interface {
	// Accept 接收一个完整事件，Context 只限制本次交付。
	Accept(ctx context.Context, event Event) error
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

// ApprovalOption 保存 ACP Agent 提供的一项原始审批选择。
type ApprovalOption struct {
	// ID 是必须原样回传 ACP Agent 的选项标识。
	ID string
	// Name 是 ACP Agent 提供的用户可见名称。
	Name string
	// Kind 是 ACP Agent 提供的选项类别；未知值原样保留并回传。
	Kind string
}

// ApprovalLocation 保存 ACP 声明的一处安全文件位置。
type ApprovalLocation struct {
	// Path 是 ACP 提供的文件路径。
	Path string
	// Line 是 ACP 提供的可选无符号行号；为空表示未提供。
	Line *int
}

// ApprovalRequest 是 Runtime 交给 Host 的安全工具审批请求。
type ApprovalRequest struct {
	// InvocationID 是发起请求的 Runtime 调用身份，仅用于宿主内部事实归属。
	InvocationID string `json:"-"`
	// ToolCallID 是 ACP Session 内对应工具调用的稳定标识。
	ToolCallID string
	// Title 是 ACP Agent 提供的安全操作标题。
	Title string
	// Kind 是 ACP Agent 提供的工具类别。
	Kind string
	// Summary 是 ACP Agent 提供或由标准字段形成的安全摘要。
	Summary string
	// Command 是从 ACP 标准命令字段提取并限制长度的安全展示摘要。
	Command string
	// Locations 是 ACP Agent 提供的安全文件位置。
	Locations []ApprovalLocation
	// Options 是 ACP Agent 允许用户选择的全部有效选项。
	Options []ApprovalOption
}

// ApprovalDecision 保存 Host 对一次工具审批请求的处理结果。
type ApprovalDecision struct {
	// OptionID 是用户选择并需要原样回传 ACP Agent 的选项标识。
	OptionID string
	// Cancelled 表示执行取消或交互不可用，没有选中任何选项。
	Cancelled bool
}

// ApprovalHandler 是 Runtime 请求用户工具授权的双向端口。
type ApprovalHandler interface {
	// Request 阻塞到用户选择、执行取消或交互失效。
	Request(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error)
}

// ElicitationRequest 保存 Host 需要展示的一次结构化用户输入。
type ElicitationRequest struct {
	// InvocationID 是发起请求的 Runtime 调用身份，仅用于宿主内部事实归属。
	InvocationID string `json:"-"`
	// Mode 是 form 或 url。
	Mode string
	// Message 是 Agent 提供的用户可见说明。
	Message string
	// RequestedSchema 是 form 模式下的 JSON Schema 对象。
	RequestedSchema map[string]any
	// URL 是 url 模式下需要用户访问的地址。
	URL string
}

// ElicitationDecision 保存 Host 对一次结构化输入请求的处理结果。
type ElicitationDecision struct {
	// Accepted 表示用户提交了表单或确认 URL 交互。
	Accepted bool
	// Declined 表示用户明确拒绝本次输入请求。
	Declined bool
	// Cancelled 表示执行取消或交互不可用，没有形成有效输入。
	Cancelled bool
	// Content 是用户提交的表单内容。
	Content map[string]any
}

// ElicitationHandler 是 Runtime 请求结构化用户输入的双向端口。
type ElicitationHandler interface {
	// Request 阻塞到用户提交、拒绝、执行取消或交互失效。
	Request(ctx context.Context, request ElicitationRequest) (ElicitationDecision, error)
}

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

// Instruction 保存 Runtime 本次调用需要理解的一份结构化指令。
type Instruction struct {
	// Name 是指令来源的稳定展示名称。
	Name string
	// Content 是本次调用读取到的完整指令内容。
	Content string
}

// HistoryMessage 保存新建原生 Session 时用于播种的一条权威消息。
type HistoryMessage struct {
	// Role 是消息在对话中的稳定角色。
	Role string
	// Content 是消息已经完成展示投影的正文。
	Content string
	// CodeReferences 是该历史用户消息发送时固定的内容引用。
	CodeReferences []CodeReference
}

// Attachment 保存本次消息交给 Runtime 的一个受管附件引用。
type Attachment struct {
	// Name 是向 Agent 展示的附件名称。
	Name string
	// Path 是相对于工作区根目录的安全路径。
	Path string
	// MediaType 是后端识别的附件媒体类型。
	MediaType string
	// RuntimePath 是支持原生文件输入的 Runtime 使用的绝对路径。
	RuntimePath string
}

// CodeReference 保存本次消息固定的整文件引用或代码片段快照。
type CodeReference struct {
	// Path 是整文件的工作区相对路径或代码片段发送时使用的路径。
	Path string
	// StartLine 是代码片段的一基起始行，整文件引用为零。
	StartLine int
	// EndLine 是代码片段的一基结束行，整文件引用为零。
	EndLine int
	// Content 是代码片段发送时的文本快照，整文件引用为空。
	Content string
}

// Message 保存一次 Runtime 调用当前需要处理的用户输入。
type Message struct {
	// Text 是用户提交的正文。
	Text string
	// Attachments 是按用户选择顺序交付的附件。
	Attachments []Attachment
	// CodeReferences 是按用户选择顺序交付的内容引用。
	CodeReferences []CodeReference
}

// Skill 保存 Runtime 本次调用可以按需读取的一项技能。
type Skill struct {
	// Name 是技能向 Agent 展示的名称。
	Name string
	// Path 是 NeuxBot 当前可用技能目录的绝对路径。
	Path string
	// Selected 表示用户在当前消息中明确选择了该技能。
	Selected bool
}

// Connector 保存 Runtime 本次调用可以使用的一项 MCP 连接。
type Connector struct {
	// ID 是 MCP 连接器的稳定标识。
	ID string
	// Name 是 MCP 连接器向 Agent 展示的名称。
	Name string
	// RuntimeKey 是 MCP 连接器写入 Runtime 原生配置的稳定名称。
	RuntimeKey string
	// Selected 表示用户在当前消息中明确选择了该连接器。
	Selected bool
	// Transport 是 MCP 连接器使用的传输方式。
	Transport ConnectorTransport
	// Command 是 STDIO 连接器启动的可执行命令。
	Command string
	// Arguments 是 STDIO 命令的有序参数。
	Arguments []string
	// Environment 是 STDIO 子进程需要的环境变量。
	Environment map[string]string
	// URL 是 Streamable HTTP 或 SSE 服务地址。
	URL string
	// Headers 是远程 MCP 请求需要的请求头。
	Headers map[string]string
	// TimeoutMS 是连接测试超时；Runtime 原生支持时也作为 MCP 启动超时注入。
	TimeoutMS int64
}

// ConnectorTransport 是共享 Runtime MCP 传输枚举的兼容别名。
type ConnectorTransport = constants.RuntimeConnectorTransport

const (
	// ConnectorTransportStdio 表示本地标准输入输出传输。
	ConnectorTransportStdio = constants.RuntimeConnectorTransportStdio
	// ConnectorTransportStreamableHTTP 表示 Streamable HTTP 传输。
	ConnectorTransportStreamableHTTP = constants.RuntimeConnectorTransportStreamableHTTP
	// ConnectorTransportSSE 表示旧版 HTTP SSE 传输。
	ConnectorTransportSSE = constants.RuntimeConnectorTransportSSE
)

// RunPolicy 保存一次调用的模型选择和产品权限对应的执行范围。
type RunPolicy struct {
	// Model 是本次调用选择的模型标识。
	Model string
	// Reasoning 是本次调用选择的推理强度。
	Reasoning string
	// Execution 是调用方冻结的产品权限。
	Execution ExecutionPolicy
}

// UserInputState 描述宿主创建的输入等待状态，供无活动计时器使用。
type UserInputState interface {
	// Pending 表示当前执行存在尚未解决的宿主输入。
	Pending() bool
	// Changed 在输入等待状态改变时非阻塞唤醒观察者。
	Changed() <-chan struct{}
}

// RunRequest 保存一次 Runtime 调用需要的完整结构化输入。
type RunRequest struct {
	// UserInputState 是当前执行的软件能力问答等待状态。
	UserInputState UserInputState
	// Target 指定 Runtime 及允许的本地启动覆盖。
	Target RuntimeTarget
	// Detection 是启动或用户手动刷新后持久化的 Runtime 探测快照。
	Detection DetectionResult
	// Workspace 指定受管工作目录和真实项目授权范围。
	Workspace Workspace
	// Instructions 是本次调用读取到的伙伴与项目指令。
	Instructions []Instruction
	// History 是新建原生 Session 时用于播种的权威历史。
	History []HistoryMessage
	// Message 是当前用户输入。
	Message Message
	// Skills 是本次调用可以按需读取的当前技能。
	Skills []Skill
	// Connectors 是本次调用可以使用的当前 MCP 连接。
	Connectors []Connector
	// Session 指定新建或恢复原生 Session。
	Session SessionDirective
	// Policy 保存模型选择和产品权限对应的执行范围。
	Policy RunPolicy
	// ApprovalHandler 处理 ACP Agent 发起的工具审批；为空时安全取消请求。
	ApprovalHandler ApprovalHandler
	// ElicitationHandler 处理 ACP Agent 发起的结构化用户输入；为空时安全取消请求。
	ElicitationHandler ElicitationHandler
}

// Workspace 保存一次 Runtime Invocation 的文件系统范围。
type Workspace struct {
	// Path 是子进程实际使用的工作目录。
	Path string
	// Managed 表示该目录是否由 NeuxBot 管理并允许 SDK 写入集成文件。
	Managed bool
	// AdditionalWritableDirectories 保存调用方额外授权的可写目录。
	AdditionalWritableDirectories []string
	// ProjectDirectories 保存 Agent 可见的项目目录及无法判断归属时使用的默认目录。
	ProjectDirectories []WorkspaceProjectDirectory
}

// WorkspaceProjectDirectory 保存 Runtime 工作区中的一个用户项目目录入口。
type WorkspaceProjectDirectory struct {
	// Path 是相对于 Runtime 工作目录的稳定项目目录路径。
	Path string
	// Default 表示无法从任务语义判断文件归属时是否使用该目录。
	Default bool
}

// SessionMode 是共享 Runtime Session 动作枚举的兼容别名。
type SessionMode = constants.RuntimeSessionMode

const (
	// SessionModeNew 表示创建一个新的原生 Session。
	SessionModeNew = constants.RuntimeSessionModeNew
	// SessionModeResume 表示恢复调用方提供的原生 Session。
	SessionModeResume = constants.RuntimeSessionModeResume
)

// SessionDirective 保存一次调用的原生 Session 输入。
type SessionDirective struct {
	// Mode 指定新建、恢复或不使用 Session。
	Mode SessionMode
	// ID 是 Resume 使用的 Session ID，或 Host 指定式新 Session ID。
	ID string
}

// ExecutionPolicy 保存调用方冻结的产品权限。
type ExecutionPolicy struct {
	// PermissionLevel 是调用方冻结的跨 Runtime 产品权限等级。
	PermissionLevel PermissionLevel
	// WorkspaceReadOnly 表示 Runtime 只能读取工作区；除内部控制面精确白名单外的审批必须取消。
	WorkspaceReadOnly bool
	// TrustedControlTools 是入口已授权、允许免用户确认的内部控制面 MCP 精确白名单。
	TrustedControlTools []TrustedControlTool
}

// TrustedControlTool 描述由上层内部能力签发、仍需服务端二次授权的 MCP 调用范围。
type TrustedControlTool struct {
	// Server 是 Runtime 原生 MCP 配置中的精确服务键。
	Server string
	// Tool 是允许调用的唯一 MCP 工具名。
	Tool string
	// Actions 是该工具 arguments.action 允许使用的精确值集合。
	Actions []string
}

// SessionResult 保存一次调用输入和输出的原生 Session 事实。
type SessionResult struct {
	// ForkMode 是适配器确认的原生分叉精度。
	ForkMode string
	// ForkPosition 是当前结束回合的不透明原生位置。
	ForkPosition string
	// Mode 是本次调用使用的 Session 动作。
	Mode SessionMode
	// InputID 是调用方传入的 Session ID。
	InputID string
	// OutputID 是 Runtime 创建、确认或捕获的 Session ID。
	OutputID string
	// State 是 SDK 根据输入、输出和失败分类得到的 Session 状态。
	State SessionState
}

// SessionState 是共享 Runtime Session 状态枚举的兼容别名。
type SessionState = constants.RuntimeSessionState

const (
	// SessionStateNone 表示本次调用没有使用原生 Session。
	SessionStateNone = constants.RuntimeSessionStateNone
	// SessionStateCreated 表示 Runtime 已经创建或确认新 Session。
	SessionStateCreated = constants.RuntimeSessionStateCreated
	// SessionStateResumed 表示 Runtime 已经成功恢复输入 Session。
	SessionStateResumed = constants.RuntimeSessionStateResumed
	// SessionStateMissing 表示输入 Session 已不存在或不可恢复。
	SessionStateMissing = constants.RuntimeSessionStateMissing
	// SessionStateUnknown 表示 SDK 无法确认 Session 最终状态。
	SessionStateUnknown = constants.RuntimeSessionStateUnknown
)

// SideEffectSummary 保存 SDK 从协议事件中明确观察到的副作用信号。
type SideEffectSummary struct {
	// VisibleOutput 表示已经产生面向用户的可见文本。
	VisibleOutput bool
	// ReasoningOutput 表示已经产生或开始产生推理内容。
	ReasoningOutput bool
	// ToolUse 表示 Runtime 已经请求执行工具。
	ToolUse bool
	// FileWrite 表示 Runtime 明确报告了文件修改。
	FileWrite bool
	// Artifact 表示 Runtime 明确报告了 Artifact 生成。
	Artifact bool
}

// Any 返回是否已经观察到任意会阻止 Host 安全重放的副作用。
func (summary SideEffectSummary) Any() bool {
	return summary.VisibleOutput ||
		summary.ReasoningOutput ||
		summary.ToolUse ||
		summary.FileWrite ||
		summary.Artifact
}

// InvocationFacts 保存一次调用实际使用且适合持久化的技术事实。
type InvocationFacts struct {
	// PrefixArgs 是调用时配置的前置参数，用于隔离自定义运行时分叉。
	PrefixArgs []string
	// ConfigHome 是调用时受控的原生状态主目录。
	ConfigHome string
	// InvocationID 是 Host 分配的本次调用标识。
	InvocationID string
	// RuntimeID 是调用方选择的 Runtime 或 Profile ID。
	RuntimeID string
	// BaseRuntimeID 是提供协议语义的内置 Runtime ID。
	BaseRuntimeID string
	// Binary 是实际启动的可执行文件路径。
	Binary string
	// Version 是本次调用使用的持久化探测快照中的 Runtime CLI 版本。
	Version string
	// Model 是实际写入 ACP Session 的模型选择。
	Model string
	// Reasoning 是实际写入 ACP Session 的推理强度。
	Reasoning string
	// ServiceTier 是本次调用保留的模型服务等级。
	ServiceTier string
	// Policy 是调用方显式授权的执行策略。
	Policy ExecutionPolicy
	// EnvironmentFingerprint 是完整最终环境的不可逆摘要。
	EnvironmentFingerprint string
	// Capabilities 是启动时冻结的能力快照。
	Capabilities CapabilitySet
}

// FailureCode 是共享 Runtime 失败码枚举的兼容别名。
type FailureCode = constants.RuntimeFailureCode

const (
	// FailureCodeRuntimeNotFound 表示请求的开放 Runtime ID 未注册。
	FailureCodeRuntimeNotFound = constants.RuntimeFailureCodeRuntimeNotFound
	// FailureCodeRuntimeUnavailable 表示 Runtime 可执行文件当前不可调用。
	FailureCodeRuntimeUnavailable = constants.RuntimeFailureCodeRuntimeUnavailable
	// FailureCodeRuntimeIncompatible 表示 Runtime 缺少必要协议能力。
	FailureCodeRuntimeIncompatible = constants.RuntimeFailureCodeRuntimeIncompatible
	// FailureCodeInvalidRequest 表示调用参数缺失或相互冲突。
	FailureCodeInvalidRequest = constants.RuntimeFailureCodeInvalidRequest
	// FailureCodePolicyUnsupported 表示 Runtime 无法准确表达调用方权限策略。
	FailureCodePolicyUnsupported = constants.RuntimeFailureCodePolicyUnsupported
	// FailureCodeSpawnFailed 表示操作系统没有成功创建 Runtime 进程。
	FailureCodeSpawnFailed = constants.RuntimeFailureCodeSpawnFailed
	// FailureCodeProcessExited 表示 Runtime 进程以失败状态结束。
	FailureCodeProcessExited = constants.RuntimeFailureCodeProcessExited
	// FailureCodeProtocolIncompatible 表示事件流缺少必要协议语义。
	FailureCodeProtocolIncompatible = constants.RuntimeFailureCodeProtocolIncompatible
	// FailureCodeContextCanceled 表示调用方 Context 结束了 Invocation。
	FailureCodeContextCanceled = constants.RuntimeFailureCodeContextCanceled
	// FailureCodeEventDelivery 表示 Event Sink 失败或超时。
	FailureCodeEventDelivery = constants.RuntimeFailureCodeEventDelivery
	// FailureCodeFrameTooLarge 表示单个协议帧超过硬限制。
	FailureCodeFrameTooLarge = constants.RuntimeFailureCodeFrameTooLarge
	// FailureCodeInactivityTimeout 表示 Runtime 超过允许时间没有原始进程活动。
	FailureCodeInactivityTimeout = constants.RuntimeFailureCodeInactivityTimeout
	// FailureCodeUnsupportedCapability 表示当前 Runtime 不支持请求的操作。
	FailureCodeUnsupportedCapability = constants.RuntimeFailureCodeUnsupportedCapability
	// FailureCodeInvocationClosed 表示调用已经结束或输入管道已经关闭。
	FailureCodeInvocationClosed = constants.RuntimeFailureCodeInvocationClosed
	// FailureCodeSessionNotFound 表示 Runtime 无法恢复调用方传入的 Session。
	FailureCodeSessionNotFound = constants.RuntimeFailureCodeSessionNotFound
	// FailureCodeAuthRequired 表示 Runtime 缺少有效鉴权凭据。
	FailureCodeAuthRequired = constants.RuntimeFailureCodeAuthRequired
	// FailureCodePlanUpgradeRequired 表示当前套餐不允许使用所选模型。
	FailureCodePlanUpgradeRequired = constants.RuntimeFailureCodePlanUpgradeRequired
	// FailureCodeQuotaExhausted 表示模型服务的可用额度已耗尽。
	FailureCodeQuotaExhausted = constants.RuntimeFailureCodeQuotaExhausted
	// FailureCodeRateLimited 表示模型服务拒绝了当前请求频率。
	FailureCodeRateLimited = constants.RuntimeFailureCodeRateLimited
	// FailureCodeModelUnavailable 表示所选模型不存在或当前不可用。
	FailureCodeModelUnavailable = constants.RuntimeFailureCodeModelUnavailable
	// FailureCodeUpstreamUnavailable 表示模型或 Provider 暂时不可用。
	FailureCodeUpstreamUnavailable = constants.RuntimeFailureCodeUpstreamUnavailable
)

// FailureTransience 是共享 Runtime 失败瞬时性枚举的兼容别名。
type FailureTransience = constants.RuntimeFailureTransience

const (
	// FailureTransienceUnknown 表示 SDK 无法可靠判断失败是否瞬时。
	FailureTransienceUnknown = constants.RuntimeFailureTransienceUnknown
	// FailureTransiencePermanent 表示相同输入不应直接自动重试。
	FailureTransiencePermanent = constants.RuntimeFailureTransiencePermanent
	// FailureTransienceTransient 表示失败可能由短暂上游状态导致。
	FailureTransienceTransient = constants.RuntimeFailureTransienceTransient
)

// Failure 保存一次已启动 Invocation 的结构化失败。
type Failure struct {
	// Code 是 Host 用于分类和重试决策的稳定失败码。
	Code FailureCode
	// Stage 是失败发生的共享执行阶段。
	Stage string
	// Message 是不含秘密值的技术说明。
	Message string
	// DisplayMessage 是从原始错误链或 ACP 结构化错误提取的可展示原因。
	DisplayMessage string
	// Transience 表示失败是否属于 Host 可以考虑重试的瞬时错误。
	Transience FailureTransience
	// SessionState 是失败发生后 SDK 确认的原生 Session 状态。
	SessionState SessionState
	// SideEffects 是失败前已经明确观察到的副作用信号。
	SideEffects SideEffectSummary
}

// Result 是 Runtime Invocation 的可重复读取终态。
type Result struct {
	// StopReason 是协议实际返回的原始停止原因；空值表示未收到该事实。
	StopReason string
	// TerminationReason 是协议停止原因的归一化结论，不替代 Failure 或执行状态。
	TerminationReason constants.RuntimeTerminationReason
	// ExitCode 是主进程退出码，未成功启动时为 -1。
	ExitCode int
	// Signal 是平台可以识别时的终止信号。
	Signal string
	// Session 保存原生 Session 输入和输出事实。
	Session SessionResult
	// Facts 保存本次调用实际使用的 Runtime 配置。
	Facts InvocationFacts
	// ProtocolHealthy 表示 Parser 观察到了必要协议语义。
	ProtocolHealthy bool
	// Diagnostics 保存 Run 启动前探测继承的非秘密诊断。
	Diagnostics []Diagnostic
	// SideEffects 保存协议流明确报告的副作用信号。
	SideEffects SideEffectSummary
	// Failure 保存失败终态，成功时为 nil。
	Failure *Failure
	// UndeliveredEvents 是 Sink 失败后无法继续交付的事件数量。
	UndeliveredEvents int
	// UndeliveredBytes 是 Sink 失败后无法继续交付的载荷字节数。
	UndeliveredBytes int64
}

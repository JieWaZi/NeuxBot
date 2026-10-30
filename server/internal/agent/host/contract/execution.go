// Package contract 描述业务调用、交互、事件与结果，不拥有技术会话。
package contract

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
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

// EventType 是共享 Runtime 事件类型枚举的引用。
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
}

// Connector 保存 Runtime 本次调用可以使用的一项 MCP 连接。
type Connector struct {
	// ID 是 MCP 连接器的稳定标识。
	ID string
	// Name 是 MCP 连接器向 Agent 展示的名称。
	Name string
	// RuntimeKey 是 MCP 连接器写入 Runtime 原生配置的稳定名称。
	RuntimeKey string
	// Transport 是 MCP 连接器使用的传输方式。
	Transport runtimeconfig.ConnectorTransport
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

// RunPolicy 保存一次调用的模型选择和产品权限对应的执行范围。
type RunPolicy struct {
	// Model 是本次调用选择的模型标识。
	Model string
	// Reasoning 是本次调用选择的推理强度。
	Reasoning string
	// Thinking 是独立任务明确要求的推理开关；为空时保留原生默认。
	Thinking *bool
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

// SessionMode 是共享 Runtime Session 动作枚举的引用。
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
	PermissionLevel runtimeconfig.PermissionLevel
	// WorkspaceReadOnly 表示宿主要求智能体只读取工作区。
	WorkspaceReadOnly bool
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

// SessionState 是共享 Runtime Session 状态枚举的引用。
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
	Capabilities runtimeconfig.CapabilitySet
}

// Failure 保存一次已启动 Invocation 的结构化失败。
type Failure struct {
	// Code 是 Host 用于分类和重试决策的稳定失败码。
	Code runtimediagnostic.FailureCode
	// Stage 是失败发生的共享执行阶段。
	Stage string
	// Message 是不含秘密值的技术说明。
	Message string
	// DisplayMessage 是从原始错误链或 ACP 结构化错误提取的可展示原因。
	DisplayMessage string
	// Transience 表示失败是否属于 Host 可以考虑重试的瞬时错误。
	Transience runtimediagnostic.FailureTransience
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
	Diagnostics []runtimeconfig.Diagnostic
	// SideEffects 保存协议流明确报告的副作用信号。
	SideEffects SideEffectSummary
	// Failure 保存失败终态，成功时为 nil。
	Failure *Failure
	// UndeliveredEvents 是 Sink 失败后无法继续交付的事件数量。
	UndeliveredEvents int
	// UndeliveredBytes 是 Sink 失败后无法继续交付的载荷字节数。
	UndeliveredBytes int64
}

// UsageData 是 Runtime SDK 归一化后交给 Host 的完整用量数据。
type UsageData struct {
	// Turn 保存当前 Turn 的累计 Token 用量；仅报告上下文时为空。
	Turn *TurnUsageData `json:"turn,omitempty"`
	// Context 保存当前原生 Session 的上下文占用快照；Runtime 不支持时为空。
	Context *ContextUsageData `json:"context,omitempty"`
}

// TurnUsageData 保存一次 Turn 内按统一语义归一化的 Token 用量。
type TurnUsageData struct {
	// InputTokens 是扣除缓存读取后的非缓存输入 Token 数。
	InputTokens int64 `json:"input_tokens,omitempty"`
	// CacheReadTokens 是由 Runtime 缓存命中的输入 Token 数。
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
	// CacheCreationTokens 是本次 Turn 写入 Runtime 缓存的输入 Token 数。
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// OutputTokens 是 Runtime 报告的输出 Token 数。
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// ReasoningOutputTokens 是 Runtime 单独报告的推理输出 Token 数。
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens,omitempty"`
}

// ContextUsageData 保存当前原生 Session 的上下文占用快照。
type ContextUsageData struct {
	// UsedTokens 是当前原生 Session 已占用的上下文 Token 数。
	UsedTokens int64 `json:"used_tokens,omitempty"`
	// WindowTokens 是当前模型可用的上下文窗口上限。
	WindowTokens int64 `json:"window_tokens,omitempty"`
}

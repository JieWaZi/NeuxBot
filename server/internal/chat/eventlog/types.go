// Package eventlog 管理每个 Chat 的权威追加事件日志。
package eventlog

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// Record 是 Chat JSONL 中一条已经提交的稳定事件信封。
type Record struct {
	// Offset 是该行在 JSONL 文件中的起始字节位置，不进入权威信封。
	Offset int64 `json:"-"`
	// Length 是该行包含结尾换行符的字节数，不进入权威信封。
	Length int64 `json:"-"`
	// SchemaVersion 是解释当前事件信封所需的格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// ChatID 是该事件所属 Chat 标识。
	ChatID string `json:"chatId"`
	// Sequence 是 Chat 内从 1 开始严格递增的游标。
	Sequence int64 `json:"sequence"`
	// RecordedAt 是事件提交到权威日志时的 Unix 毫秒时间。
	RecordedAt int64 `json:"recordedAt"`
	// Type 是事件的稳定业务类型。
	Type constants.ChatEventType `json:"type"`
	// MessageID 是事件关联的 Message 标识。
	MessageID string `json:"messageId,omitempty"`
	// ExecutionID 是事件关联的 Execution 标识。
	ExecutionID string `json:"executionId,omitempty"`
	// InvocationID 是事件关联的 Runtime Invocation 标识。
	InvocationID string `json:"invocationId,omitempty"`
	// Data 是事件类型定义的完整 JSON 载荷。
	Data json.RawMessage `json:"data,omitempty"`
}

// AppendInput 是尚未分配 Sequence 和提交时间的日志事件。
type AppendInput struct {
	// Type 是事件的稳定业务类型。
	Type constants.ChatEventType
	// MessageID 是事件关联的 Message 标识。
	MessageID string
	// ExecutionID 是事件关联的 Execution 标识。
	ExecutionID string
	// InvocationID 是事件关联的 Runtime Invocation 标识。
	InvocationID string
	// Data 是事件类型定义的完整 JSON 载荷。
	Data json.RawMessage
}

// AgentCommandData 只记录安全业务动作与真实回执身份，不复制正文、路径或凭据。
type AgentCommandData struct {
	// CommandID 是 Core 返回并可与真实工具结果关联的命令身份。
	CommandID string `json:"commandId"`
	// Operation 是统一操作目录中的稳定身份。
	Operation string `json:"operation"`
	// Completion 区分同步提交、异步接纳和候选结果。
	Completion string `json:"completion"`
}

// ChatCreatedData 保存 Chat 建立时锁定的业务事实。
type ChatCreatedData struct {
	// ForkConfiguration 保存分叉点冻结的执行设置，继承历史本身不产生执行。
	ForkConfiguration *ExecutionCreatedData `json:"forkConfiguration,omitempty"`
	// ForkSourceChatID 是分叉来源会话，删除来源不影响本会话。
	ForkSourceChatID string `json:"forkSourceChatId,omitempty"`
	// ForkSourceExecutionID 是来源中被选择的结束回合。
	ForkSourceExecutionID string `json:"forkSourceExecutionId,omitempty"`
	// WakerID 是 Chat 永久绑定的伙伴标识。
	WakerID string `json:"wakerId"`
	// ClientRequestID 是普通 Chat 首次发送用于确定重放的客户端标识；团队成员 Chat 为空。
	ClientRequestID string `json:"clientRequestId,omitempty"`
	// ClientRequestHash 是普通 Chat 首次发送规范化业务内容的摘要；团队成员 Chat 为空。
	ClientRequestHash string `json:"clientRequestHash,omitempty"`
	// ProjectID 是 Chat 首次发送时选择的项目标识，临时文件夹对话为空。
	ProjectID string `json:"projectId,omitempty"`
	// ProjectName 是 Chat 首次发送时冻结的项目或文件夹展示名称。
	ProjectName string `json:"projectName"`
	// WorkspaceDirectory 是应用数据目录中该 Chat 的对话执行工作区根目录。
	WorkspaceDirectory string `json:"workspaceDirectory"`
	// AdditionalWorkingDirectories 是首次发送时冻结并授权的真实目录，首项是用户终端起始目录。
	AdditionalWorkingDirectories []string `json:"additionalWorkingDirectories,omitempty"`
	// Title 是首次发送产生的本地临时标题。
	Title string `json:"title"`
	// ProjectSources 是工作区 projects/ 下全部来源入口的冻结快照。
	ProjectSources []ProjectSourceSnapshot `json:"projectSources,omitempty"`
	// HiddenFromWakerHistory 表示该 Chat 不进入伙伴普通对话列表。
	HiddenFromWakerHistory bool `json:"hiddenFromWakerHistory,omitempty"`
	// ThreadWorkspaceID 非空时表示该 Chat 绑定 thread 级共享执行工作区。
	ThreadWorkspaceID string `json:"threadWorkspaceId,omitempty"`
}

// ProjectSourceSnapshot 保存一个 Chat 工作区项目入口。
type ProjectSourceSnapshot struct {
	// EntryName 是工作区 projects/ 下的稳定入口名。
	EntryName string `json:"entryName"`
	// SourcePath 是首次发送时解析后的真实项目根目录。
	SourcePath string `json:"sourcePath"`
	// LoadInstructions 表示每次执行是否读取该来源的当前根指令。
	LoadInstructions bool `json:"loadInstructions"`
}

// ChatTitleUpdatedData 保存用户确认后的 Chat 展示标题。
type ChatTitleUpdatedData struct {
	// Title 是标准化并通过长度校验的新标题。
	Title string `json:"title"`
}

// AttachmentReference 是一条经过路径校验并可以安全重放的附件引用。
type AttachmentReference struct {
	// Name 是向用户和 Agent 展示的原始文件名。
	Name string `json:"name"`
	// Path 是相对 Chat 工作目录的稳定路径。
	Path string `json:"path"`
	// MediaType 是根据扩展名推断的 MIME 类型。
	MediaType string `json:"mediaType,omitempty"`
	// Managed 表示文件是否位于 NeuxBot 归属目录。
	Managed bool `json:"managed"`
}

// MessageSkillReference 冻结用户发送消息时明确选择的技能展示信息。
type MessageSkillReference struct {
	// ID 是技能包的稳定标识，用于打开技能详情。
	ID string `json:"id"`
	// Name 是发送时技能包的展示名称。
	Name string `json:"name"`
	// Version 是发送时实际交付给 Runtime 的技能版本号。
	Version string `json:"version"`
}

// MessageConnectorReference 冻结用户发送消息时明确选择的 MCP 连接器展示信息。
type MessageConnectorReference struct {
	// ID 是连接器的稳定标识，用于打开连接器详情。
	ID string `json:"id"`
	// Name 是发送时连接器的展示名称。
	Name string `json:"name"`
}

// CodeReference 保存整文件路径引用或连续文本区间快照。
type CodeReference struct {
	// Path 是整文件的工作区相对路径或代码片段发送时使用的路径。
	Path string `json:"path"`
	// StartLine 是选区起始的一基行号，整文件引用为零。
	StartLine int `json:"startLine"`
	// EndLine 是选区结束的一基行号，整文件引用为零。
	EndLine int `json:"endLine"`
	// Content 是发送时冻结的选中文本，整文件引用为空。
	Content string `json:"content"`
}

// MessageCreatedData 保存一条用户输入消息的完整内容。
type MessageCreatedData struct {
	// Role 是消息发送方。
	Role constants.MessageRole `json:"role"`
	// Actor 是产生该输入的入口无关语义参与者。
	Actor string `json:"actor,omitempty"`
	// SourceWakerID 是跨 Chat 投递的参与者消息原始伙伴标识，普通输入为空。
	SourceWakerID string `json:"sourceWakerId,omitempty"`
	// SourceChatID 是跨 Chat 投递的参与者消息原始 Chat 标识，普通输入为空。
	SourceChatID string `json:"sourceChatId,omitempty"`
	// Truncated 表示跨 Chat 投递的正文只包含受限预览，不能作为完整行动依据。
	Truncated bool `json:"truncated,omitempty"`
	// Content 是用户输入文本。
	Content string `json:"content"`
	// Attachments 是保持用户选择顺序的本地文件引用。
	Attachments []AttachmentReference `json:"attachments,omitempty"`
	// Skills 是本轮用户明确选择的技能快照。
	Skills []MessageSkillReference `json:"skills,omitempty"`
	// Plugins 是本轮用户明确选择的插件快照。
	Plugins []plugins.Snapshot `json:"plugins,omitempty"`
	// Connectors 是本轮用户明确选择的 MCP 连接器快照。
	Connectors []MessageConnectorReference `json:"connectors,omitempty"`
	// CodeReferences 是本轮用户选择的有序整文件或代码片段引用。
	CodeReferences []CodeReference `json:"codeReferences,omitempty"`
}

// ExecutionCreatedData 保存一次执行创建时冻结的选择。
type ExecutionCreatedData struct {
	// AutomaticRunID 是服务端冻结的自动任务来源，随原执行事实恢复。
	AutomaticRunID string `json:"automaticRunId,omitempty"`
	// SubmissionID 是入口无关的稳定输入身份，写入同一权威执行事实。
	SubmissionID string `json:"submissionId,omitempty"`
	// SubmissionHash 是规范化输入和冻结配置的摘要，拒绝同键异内容。
	SubmissionHash string `json:"submissionHash,omitempty"`
	// AccountID 是本次冻结的账号标识；空值使用智能体默认配置。
	AccountID string `json:"accountId,omitempty"`
	// UserMessageID 是触发执行的用户消息标识。
	UserMessageID string `json:"userMessageId"`
	// ParentExecutionID 是触发本次执行的父执行标识，普通用户回合为空。
	ParentExecutionID string `json:"parentExecutionId,omitempty"`
	// RuntimeID 是用户选择的开放 Runtime 标识。
	RuntimeID string `json:"runtimeId"`
	// Model 是本次执行选择的模型标识。
	Model string `json:"model"`
	// Reasoning 是本次执行选择的推理强度。
	Reasoning string `json:"reasoning"`
	// PermissionLevel 是本次发送冻结的跨 Runtime 产品权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// PluginResources 是接纳时冻结的插件身份、版本与配置修订。
	PluginResources []plugins.Snapshot `json:"pluginResources,omitempty"`
	// NativePluginScope 是接纳时冻结的外部原生插件摘要，供分叉续聊核验能力边界。
	NativePluginScope string `json:"nativePluginScope,omitempty"`
	// WorkspaceReadOnly 表示本次 Runtime 只能读取工作区且不能申请文件修改。
	WorkspaceReadOnly bool `json:"workspaceReadOnly,omitempty"`
}

// ApprovalOptionData 保存 ACP Agent 提供的一项安全审批选择。
type ApprovalOptionData struct {
	// ID 是必须原样回传 ACP Agent 的选项标识。
	ID string `json:"id"`
	// Name 是 ACP Agent 提供的用户可见名称。
	Name string `json:"name"`
	// Kind 是 ACP Agent 提供的选项类别；未知值原样保留。
	Kind string `json:"kind"`
}

// ApprovalLocationData 保存 ACP 声明的一处安全文件位置。
type ApprovalLocationData struct {
	// Path 是 ACP 提供的文件路径。
	Path string `json:"path"`
	// Line 是 ACP 提供的可选无符号行号；为空表示未提供。
	Line *int `json:"line,omitempty"`
}

// ApprovalRequestedData 保存一次等待用户处理的安全审批请求。
type ApprovalRequestedData struct {
	// ApprovalID 是 NeuxBot 为本次 ACP 回调生成的稳定交互标识。
	ApprovalID string `json:"approvalId"`
	// ToolCallID 是 ACP Session 内对应工具调用的稳定标识。
	ToolCallID string `json:"toolCallId"`
	// Title 是 ACP Agent 提供的安全操作标题。
	Title string `json:"title"`
	// Kind 是 ACP Agent 提供的工具类别。
	Kind string `json:"kind,omitempty"`
	// Summary 是 ACP Agent 提供或由标准字段形成的安全摘要。
	Summary string `json:"summary,omitempty"`
	// Command 是从 ACP 标准命令字段提取并限制长度的安全展示摘要。
	Command string `json:"command,omitempty"`
	// Locations 是 ACP Agent 提供的安全文件位置。
	Locations []ApprovalLocationData `json:"locations,omitempty"`
	// Options 是 ACP Agent 允许用户选择的全部有效选项。
	Options []ApprovalOptionData `json:"options"`
}

// ApprovalResolvedData 保存一次审批的权威处理结果。
type ApprovalResolvedData struct {
	// ApprovalID 是被处理的 NeuxBot 审批标识。
	ApprovalID string `json:"approvalId"`
	// ToolCallID 是对应 ACP 工具调用标识。
	ToolCallID string `json:"toolCallId"`
	// OptionID 是用户选择的 ACP 选项标识，取消或失效时为空。
	OptionID string `json:"optionId,omitempty"`
	// OptionName 是用户选择的 ACP 选项名称，取消或失效时为空。
	OptionName string `json:"optionName,omitempty"`
	// OptionKind 是用户选择的 ACP 选项类别，取消或失效时为空。
	OptionKind string `json:"optionKind,omitempty"`
	// Command 是本次审批对应的受限命令展示摘要。
	Command string `json:"command,omitempty"`
	// Result 是 selected、cancelled 或 invalidated。
	Result constants.ApprovalResult `json:"result"`
}

// ElicitationRequestedData 保存一次等待用户提交的结构化输入请求。
type ElicitationRequestedData struct {
	// CommandRequestID 是 CLI 问答的原请求身份；原生请求为空。
	CommandRequestID string `json:"commandRequestId,omitempty"`
	// CommandDigest 拒绝原请求身份的异内容重放。
	CommandDigest string `json:"commandDigest,omitempty"`
	// AllowSkip 是 CLI 问答冻结的跳过约束；原生请求不设置。
	AllowSkip *bool `json:"allowSkip,omitempty"`
	// ElicitationID 是 NeuxBot 生成的稳定交互标识。
	ElicitationID string `json:"elicitationId"`
	// ToolCallID 是对应 ACP 工具调用标识，URL 模式可能为空。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Mode 是 form 或 url。
	Mode string `json:"mode"`
	// Message 是 Agent 提供的用户可见说明。
	Message string `json:"message"`
	// RequestedSchema 是 form 模式下的 JSON Schema 对象。
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	// URL 是 url 模式下需要用户访问的地址。
	URL string `json:"url,omitempty"`
}

// ElicitationResolvedData 保存一次结构化输入的权威处理结果。
type ElicitationResolvedData struct {
	// ElicitationID 是被处理的 NeuxBot 交互标识。
	ElicitationID string `json:"elicitationId"`
	// ToolCallID 是对应 ACP 工具调用标识。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Content 是用户提交的表单内容，拒绝或取消时为空。
	Content map[string]any `json:"content,omitempty"`
	// Result 是 accepted、declined、cancelled 或 invalidated。
	Result constants.ElicitationResult `json:"result"`
}

// RuntimeEventData 保存 Runtime SDK 交付的完整归一化事件。
type RuntimeEventData struct {
	// Type 是 Runtime SDK 的开放事件类型。
	Type constants.RuntimeEventType `json:"type"`
	// Data 是该 Runtime 事件类型对应的完整载荷。
	Data json.RawMessage `json:"data"`
	// MessageRevision 是该事件应用后 Assistant 消息的单调递增修订号。
	MessageRevision int64 `json:"messageRevision"`
}

// RuntimeMessageContent 保存完整执行消息中的一项跨 Runtime 归一化内容。
type RuntimeMessageContent struct {
	// Type 是展示投影能够解释的开放内容类型。
	Type constants.RuntimeEventType `json:"type"`
	// Data 是该内容类型对应的完整归一化载荷。
	Data json.RawMessage `json:"data"`
}

// RuntimeMessageData 保存 Runtime 已经完整交付的一条执行消息。
type RuntimeMessageData struct {
	// Content 按 Runtime 原始发生顺序保存消息内容。
	Content []RuntimeMessageContent `json:"content"`
	// MessageRevision 是整条消息应用后 Assistant 展示消息的修订号。
	MessageRevision int64 `json:"messageRevision"`
}

// InvocationDiagnosticData 保存一次调用继承或产生的非秘密诊断。
type InvocationDiagnosticData struct {
	// Code 是可以稳定聚合和本地化的诊断码。
	Code string `json:"code"`
	// Level 是诊断对本次调用的影响等级。
	Level constants.RuntimeDiagnosticLevel `json:"level"`
	// Message 是供排障使用的简明技术说明。
	Message string `json:"message"`
}

// InvocationFinishedData 保存一次 Runtime Invocation 的完整可诊断事实。
type InvocationFinishedData struct {
	// StopReason 是协议实际返回的原始停止原因，未知扩展值原样保留。
	StopReason string `json:"stopReason,omitempty"`
	// TerminationReason 是可恢复的调用终止原因，旧日志缺失时为未知。
	TerminationReason constants.RuntimeTerminationReason `json:"terminationReason,omitempty"`
	// PrefixArgs 是调用时冻结的自定义运行时前置参数。
	PrefixArgs []string `json:"prefixArgs,omitempty"`
	// ConfigHome 是调用时冻结的原生配置与会话主目录，不进入公开接口。
	ConfigHome string `json:"configHome,omitempty"`
	// ForkMode 是本次适配器确认的原生分叉精度。
	ForkMode string `json:"forkMode,omitempty"`
	// ForkPosition 是终态确认的原生位置，不进入公开消息接口。
	ForkPosition string `json:"forkPosition,omitempty"`
	// WorkingDirectory 是本次 Runtime 调用实际使用的当前工作目录。
	WorkingDirectory string `json:"workingDirectory"`
	// AdditionalWritableDirectories 是本次 Runtime 调用实际授权的附加读写目录。
	AdditionalWritableDirectories []string `json:"additionalWritableDirectories,omitempty"`
	// WorkspaceWarningCode 是本次调用发生目录能力降级时的稳定提示码。
	WorkspaceWarningCode string `json:"workspaceWarningCode,omitempty"`
	// Attempt 是该 Invocation 在所属 Execution 中从一开始的尝试序号。
	Attempt int `json:"attempt"`
	// RecoveryStrategy 是 Host 本次采用的启动或恢复策略。
	RecoveryStrategy constants.InvocationRecoveryStrategy `json:"recoveryStrategy"`
	// TriggerReason 是触发重建或安全重试的稳定原因。
	TriggerReason constants.InvocationTriggerReason `json:"triggerReason,omitempty"`
	// RuntimeID 是调用方选择的开放 Runtime 标识。
	RuntimeID string `json:"runtimeId"`
	// BaseRuntimeID 是提供实际协议语义的内置 Runtime 标识。
	BaseRuntimeID string `json:"baseRuntimeId"`
	// Binary 是本次实际启动的可执行文件路径。
	Binary string `json:"binary"`
	// Version 是 Prepare 阶段探测到的 CLI 版本。
	Version string `json:"version"`
	// Model 是本次实际传给 CLI 的模型标识。
	Model string `json:"model"`
	// Reasoning 是本次实际传给 CLI 的推理强度。
	Reasoning string `json:"reasoning,omitempty"`
	// ServiceTier 是本次实际传给 CLI 的服务等级。
	ServiceTier string `json:"serviceTier,omitempty"`
	// PermissionLevel 是本次调用冻结的跨 Runtime 产品权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel,omitempty"`
	// WorkspaceReadOnly 表示本次调用禁止 Runtime 修改工作区。
	WorkspaceReadOnly bool `json:"workspaceReadOnly,omitempty"`
	// EnvironmentFingerprint 是最终进程环境的不可逆摘要。
	EnvironmentFingerprint string `json:"environmentFingerprint"`
	// Capabilities 是调用启动时冻结的 Runtime 能力集合。
	Capabilities []constants.RuntimeCapability `json:"capabilities"`
	// SessionMode 是本次调用要求的新建、恢复或禁用 Session 动作。
	SessionMode constants.RuntimeSessionMode `json:"sessionMode"`
	// SessionInputID 是本次恢复调用使用的输入 Session 句柄。
	SessionInputID string `json:"sessionInputId,omitempty"`
	// SessionOutputID 是 Runtime 创建、确认或捕获的 Session 句柄。
	SessionOutputID string `json:"sessionOutputId,omitempty"`
	// SessionState 是 SDK 在调用结束时确认的 Session 状态。
	SessionState constants.RuntimeSessionState `json:"sessionState"`
	// ExitCode 是 Runtime 主进程退出码。
	ExitCode int `json:"exitCode"`
	// Signal 是平台能够识别时的进程终止信号。
	Signal string `json:"signal,omitempty"`
	// ProtocolHealthy 表示 Parser 是否观察到必要协议语义。
	ProtocolHealthy bool `json:"protocolHealthy"`
	// VisibleOutput 表示调用已经产生面向用户的文本。
	VisibleOutput bool `json:"visibleOutput"`
	// ReasoningOutput 表示调用已经产生或开始产生推理内容。
	ReasoningOutput bool `json:"reasoningOutput"`
	// ToolUse 表示调用已经请求执行工具。
	ToolUse bool `json:"toolUse"`
	// FileWrite 表示调用明确报告了文件修改。
	FileWrite bool `json:"fileWrite"`
	// Artifact 表示调用明确报告了 Artifact 生成。
	Artifact bool `json:"artifact"`
	// FailureCode 是调用失败时的稳定技术错误码。
	FailureCode constants.RuntimeFailureCode `json:"failureCode,omitempty"`
	// FailureStage 是调用失败发生的共享执行阶段。
	FailureStage string `json:"failureStage,omitempty"`
	// FailureMessage 是不包含秘密值的技术错误说明。
	FailureMessage string `json:"failureMessage,omitempty"`
	// FailureTransience 表示失败是否属于 Host 可以安全考虑的瞬时错误。
	FailureTransience constants.RuntimeFailureTransience `json:"failureTransience,omitempty"`
	// Diagnostics 保存本次调用继承或产生的非秘密诊断。
	Diagnostics []InvocationDiagnosticData `json:"diagnostics,omitempty"`
	// UndeliveredEvents 是 Event Sink 失败后没有成功交付的事件数量。
	UndeliveredEvents int `json:"undeliveredEvents,omitempty"`
	// UndeliveredBytes 是 Event Sink 失败后没有成功交付的载荷字节数。
	UndeliveredBytes int64 `json:"undeliveredBytes,omitempty"`
}

// Invocation 是从权威事件重放出的一次 Runtime 调用事实。
type Invocation struct {
	// ID 是 Host 为本次 Runtime 调用分配的稳定标识。
	ID string `json:"id"`
	// ExecutionID 是拥有该调用的业务执行标识。
	ExecutionID string `json:"executionId"`
	// RecordedAt 是调用终态事实提交到 JSONL 的 Unix 毫秒时间。
	RecordedAt int64 `json:"recordedAt"`
	// Facts 是本次调用实际发生的完整可诊断事实。
	Facts InvocationFinishedData `json:"facts"`
}

// ExecutionFinishedData 保存执行终态和失败诊断。
type ExecutionFinishedData struct {
	// Retryable 表示失败已确认无可见输出及工具副作用，允许用户重新提交。
	Retryable bool `json:"retryable,omitempty"`
	// Status 是执行最终生命周期状态。
	Status constants.ExecutionStatus `json:"status"`
	// ErrorCode 是失败时供前端映射的稳定错误码。
	ErrorCode string `json:"errorCode,omitempty"`
	// ErrorMessage 是不包含秘密值的失败说明。
	ErrorMessage string `json:"errorMessage,omitempty"`
	// ExitCode 是 Runtime 主进程退出码。
	ExitCode int `json:"exitCode"`
	// Signal 是平台可以识别时的进程终止信号。
	Signal string `json:"signal,omitempty"`
	// Usage 是 Runtime 最后一次报告的规范化 Token 用量，整次执行只保存一次。
	Usage json.RawMessage `json:"usage,omitempty"`
	// MessageRevision 是终态前实时消息快照已经到达的最后修订号。
	MessageRevision int64 `json:"messageRevision,omitempty"`
}

// RuntimeSessionData 保存原生 Runtime Session 的续接事实。
type RuntimeSessionData struct {
	// RuntimeID 是拥有该 Session 的开放 Runtime 标识。
	RuntimeID string `json:"runtimeId"`
	// SessionID 是 Runtime 创建或确认的原生 Session 句柄。
	SessionID string `json:"sessionId"`
	// LastExecutionID 是最后成功使用该 Session 的执行标识。
	LastExecutionID string `json:"lastExecutionId"`
	// Reason 是 Session 更新、失效或重建的稳定原因。
	Reason constants.InvocationTriggerReason `json:"reason,omitempty"`
}

// ReplayInvocations 从权威日志恢复每次 Runtime Invocation 的实际事实。
func ReplayInvocations(records []Record) ([]Invocation, error) {
	invocations := make([]Invocation, 0)
	for _, record := range records {
		if record.Type != constants.ChatEventTypeInvocationFinished {
			continue
		}
		var facts InvocationFinishedData
		if err := json.Unmarshal(record.Data, &facts); err != nil {
			return nil, status_error.ChatLogCorrupted
		}
		if facts.TerminationReason == constants.RuntimeTerminationReasonUnspecified {
			facts.TerminationReason = constants.RuntimeTerminationReasonUnknown
		}
		invocations = append(invocations, Invocation{
			ID:          record.InvocationID,
			ExecutionID: record.ExecutionID,
			RecordedAt:  record.RecordedAt,
			Facts:       facts,
		})
	}
	return invocations, nil
}

// MarshalData 将类型安全载荷编码为日志 Data。
func MarshalData(value interface{}) (json.RawMessage, error) {
	return json.Marshal(value)
}

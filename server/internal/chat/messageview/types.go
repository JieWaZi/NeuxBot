// Package messageview 将权威 Chat 事件构建为跨 Runtime 的安全消息视图。
package messageview

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// MessageRuntimeEvent 是附着到 Assistant 消息的完整 Runtime 事件视图。
type MessageRuntimeEvent struct {
	// Sequence 是该事件在 Chat JSONL 中的权威游标。
	Sequence int64 `json:"sequence"`
	// RecordedAt 是事件提交到权威日志时的 Unix 毫秒时间。
	RecordedAt int64 `json:"recordedAt"`
	// InvocationID 是产生该事件的 Runtime Invocation 标识。
	InvocationID string `json:"invocationId,omitempty"`
	// Type 是 Runtime SDK 的开放事件类型。
	Type constants.RuntimeEventType `json:"type"`
	// Data 是该 Runtime 事件未经界面裁剪的完整载荷。
	Data json.RawMessage `json:"data"`
}

// ToolFileChangeSummary 是单个项目文件的安全变更摘要。
type ToolFileChangeSummary struct {
	// Path 是位于锁定工作目录内的项目相对路径。
	Path string `json:"path"`
	// AddedLines 是标准 diff 推导出的新增行数。
	AddedLines int `json:"addedLines,omitempty"`
	// DeletedLines 是标准 diff 推导出的删除行数。
	DeletedLines int `json:"deletedLines,omitempty"`
}

// MessageFileChangeDiff 是从一条 Assistant 消息的标准 ACP diff 投影出的执行级文件差异。
type MessageFileChangeDiff struct {
	// Path 是位于对话执行工作区内的安全相对路径。
	Path string `json:"path"`
	// OldContent 是本次执行首次修改该文件前的完整文本。
	OldContent string `json:"oldContent"`
	// NewContent 是本次执行最后一次成功修改该文件后的完整文本。
	NewContent string `json:"newContent"`
}

// ToolSafeSummary 是聊天接口允许展示的受限工具摘要。
type ToolSafeSummary struct {
	// Path 是位于锁定工作目录内的项目相对路径。
	Path string `json:"path,omitempty"`
	// Domain 是合法 HTTP 或 HTTPS URL 的域名。
	Domain string `json:"domain,omitempty"`
	// AddedLines 是工具明确结构化提供的新增行数。
	AddedLines *int `json:"addedLines,omitempty"`
	// DeletedLines 是工具明确结构化提供的删除行数。
	DeletedLines *int `json:"deletedLines,omitempty"`
	// Files 是标准 diff 推导出的有序文件变更摘要。
	Files []ToolFileChangeSummary `json:"files,omitempty"`
}

// ToolApprovalResult 是一次工具审批的安全展示结果。
type ToolApprovalResult struct {
	// Result 是 selected、cancelled 或 invalidated。
	Result constants.ApprovalResult `json:"result"`
	// OptionKind 是 ACP 提供的选项类别，取消或失效时为空。
	OptionKind string `json:"optionKind,omitempty"`
	// OptionName 是 ACP 提供的用户可见选项名称，取消或失效时为空。
	OptionName string `json:"optionName,omitempty"`
	// Command 是本次审批对应的受限命令展示摘要。
	Command string `json:"command,omitempty"`
}

// MessageRunMetrics 是聊天界面可安全展示的执行时间与 Token 用量摘要。
type MessageRunMetrics struct {
	// StartedAt 是 Host 确认执行开始的 Unix 毫秒时间。
	StartedAt int64 `json:"startedAt,omitempty"`
	// DurationMs 是从 Host 执行开始到终态提交的墙钟毫秒数。
	DurationMs int64 `json:"durationMs,omitempty"`
	// InputTokens 是扣除缓存命中后的输入 Token 数。
	InputTokens int64 `json:"inputTokens,omitempty"`
	// OutputTokens 是 Runtime 报告的输出 Token 数。
	OutputTokens int64 `json:"outputTokens,omitempty"`
	// ReasoningOutputTokens 是 Runtime 单独报告的推理输出 Token 数。
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens,omitempty"`
	// CacheReadTokens 是 Runtime 报告的缓存读取 Token 数。
	CacheReadTokens int64 `json:"cacheReadTokens,omitempty"`
	// CacheCreationTokens 是 Runtime 报告的缓存写入 Token 数。
	CacheCreationTokens int64 `json:"cacheCreationTokens,omitempty"`
	// ContextUsedTokens 是当前原生 Session 占用的上下文 Token 数。
	ContextUsedTokens int64 `json:"contextUsedTokens,omitempty"`
	// ContextWindowTokens 是当前模型可用的上下文窗口上限。
	ContextWindowTokens int64 `json:"contextWindowTokens,omitempty"`
}

// MessageDisplayPart 是聊天接口返回的最小跨 Runtime 展示部件。
// 工具展示身份包含 Runtime 调用作用域；原生工具标识用于审批与原生定位。
type MessageDisplayPart struct {
	// 展示片段标识
	ID string `json:"id"`
	// 片段类型
	Type constants.MessageDisplayPartType `json:"type"`
	// 片段正文
	Text string `json:"text,omitempty"`
	// 原生工具标识
	ToolCallID string `json:"toolCallId,omitempty"`
	// 工具类别
	ToolCategory constants.MessageToolCategory `json:"toolCategory,omitempty"`
	// 工具动作
	ToolAction constants.MessageToolAction `json:"toolAction,omitempty"`
	// 工具完成文案
	ToolLabel string `json:"toolLabel,omitempty"`
	// 工具运行文案
	ToolLoadingLabel string `json:"toolLoadingLabel,omitempty"`
	// 工具状态
	ToolStatus constants.MessageToolStatus `json:"toolStatus,omitempty"`
	// 工具安全摘要
	ToolSafeSummary *ToolSafeSummary `json:"toolSafeSummary,omitempty"`
	// 工具审批结果
	ToolApprovalResult *ToolApprovalResult `json:"toolApprovalResult,omitempty"`
}

// Message 是从权威事件重放出的聊天消息视图。
type Message struct {
	// Inherited 标记从来源会话复制的历史，不计作本会话的新执行。
	Inherited bool `json:"inherited,omitempty"`
	// Retryable 表示后端允许用户安全重试该失败回合。
	Retryable bool `json:"retryable,omitempty"`
	// ID 是消息的稳定标识。
	ID string `json:"id"`
	// Role 是消息发送方。
	Role constants.MessageRole `json:"role"`
	// Actor 是产生用户输入的入口无关参与者，用于聚合视图隐藏内部协调输入。
	Actor string `json:"actor,omitempty"`
	// SourceWakerID 是跨 Chat 投递消息的原始伙伴标识，用于展示真实作者。
	SourceWakerID string `json:"sourceWakerId,omitempty"`
	// SourceChatID 是跨 Chat 投递消息的原始 Chat 标识，用于下钻完整执行。
	SourceChatID string `json:"sourceChatId,omitempty"`
	// Truncated 表示正文只是受限预览，负责人不得把它视为完整报告。
	Truncated bool `json:"truncated,omitempty"`
	// Content 是截至当前日志游标构建出的完整文本。
	Content string `json:"content"`
	// Attachments 是用户消息携带的有序附件引用。
	Attachments []eventlog.AttachmentReference `json:"attachments,omitempty"`
	// Skills 是用户发送消息时明确选择的技能快照。
	Skills []eventlog.MessageSkillReference `json:"skills,omitempty"`
	// Plugins 是接纳时冻结的插件身份、版本与配置修订。
	Plugins []plugins.Snapshot `json:"plugins,omitempty"`
	// Connectors 是用户发送消息时明确选择的 MCP 连接器快照。
	Connectors []eventlog.MessageConnectorReference `json:"connectors,omitempty"`
	// CodeReferences 是用户消息携带的有序整文件或代码片段引用。
	CodeReferences []eventlog.CodeReference `json:"codeReferences,omitempty"`
	// RuntimeEvents 是仅供 Core 内部构建消息视图使用的有序完整 Runtime 事件。
	RuntimeEvents []MessageRuntimeEvent `json:"-"`
	// DisplayParts 是供聊天界面渲染的最小跨 Runtime 展示视图。
	DisplayParts []MessageDisplayPart `json:"displayParts,omitempty"`
	// FileChanges 是当前执行内全部成功文件修改工具聚合出的有序变更摘要。
	FileChanges []ToolFileChangeSummary `json:"fileChanges,omitempty"`
	// FileChangeDiffs 是仅供 Core 按需读取、不会进入聊天消息接口的执行级文件差异。
	FileChangeDiffs []MessageFileChangeDiff `json:"-"`
	// Revision 是最后一次改变公开消息快照的权威事件序号。
	Revision int64 `json:"revision"`
	// CreatedSequence 是消息建立事件在 Chat JSONL 中的权威序号，仅供 Core 游标分页。
	CreatedSequence int64 `json:"-"`
	// ExecutionID 是 Assistant 消息对应的执行标识。
	ExecutionID string `json:"executionId,omitempty"`
	// Status 是 Assistant 消息对应执行的当前状态。
	Status constants.ExecutionStatus `json:"status,omitempty"`
	// RunMetrics 是 Assistant 消息对应执行的安全时间与用量摘要。
	RunMetrics *MessageRunMetrics `json:"runMetrics,omitempty"`
	// ErrorCode 是失败时供界面映射的稳定错误码。
	ErrorCode string `json:"errorCode,omitempty"`
	// ErrorMessage 是失败时供用户诊断的简明说明。
	ErrorMessage string `json:"errorMessage,omitempty"`
	// CreatedAt 是消息建立时的 Unix 毫秒时间。
	CreatedAt int64 `json:"createdAt"`
	// UpdatedAt 是消息最近收到持久化事件时的 Unix 毫秒时间。
	UpdatedAt int64 `json:"updatedAt"`
}

// InheritedMessage 是只写入权威日志的继承快照，私有差异不进入公开消息 JSON。
type InheritedMessage struct {
	// SourceMessageID 保留直接来源 Chat 中的消息身份，供权威历史追溯。
	SourceMessageID string `json:"sourceMessageId"`
	// ForkInvocation 仅在继承末尾保存子原生 Session 的检查点，允许再次分叉。
	ForkInvocation *eventlog.InvocationFinishedData `json:"forkInvocation,omitempty"`
	// ForkConfiguration 是该检查点对应的冻结执行选择。
	ForkConfiguration *eventlog.ExecutionCreatedData `json:"forkConfiguration,omitempty"`
	// Message 保存重新分配身份的安全展示信息。
	Message
	// Diffs 保留按需读取的原生文件差异。
	Diffs []MessageFileChangeDiff `json:"inheritedDiffs,omitempty"`
}

package constants

//go:generate toolx gen enum RuntimeEventType

// RuntimeEventType 是 Runtime SDK 归一化后的事件类型。
type RuntimeEventType string

const (
	// RuntimeEventTypeUnspecified 表示未指定 Runtime 事件类型。
	RuntimeEventTypeUnspecified RuntimeEventType = ""
	// RuntimeEventTypeInvocationStarted 表示 Runtime 已完整接收本次首条用户输入。
	RuntimeEventTypeInvocationStarted RuntimeEventType = "invocation_started"
	// RuntimeEventTypeStatus 表示 Runtime 的非终态协议状态。
	RuntimeEventTypeStatus RuntimeEventType = "status"
	// RuntimeEventTypeTextDelta 表示面向用户的增量文本。
	RuntimeEventTypeTextDelta RuntimeEventType = "text_delta"
	// RuntimeEventTypeCommentaryDelta 表示工作过程中的用户可见说明增量。
	RuntimeEventTypeCommentaryDelta RuntimeEventType = "commentary_delta"
	// RuntimeEventTypeThinkingStart 表示一次可见思考阶段开始。
	RuntimeEventTypeThinkingStart RuntimeEventType = "thinking_start"
	// RuntimeEventTypeThinkingDelta 表示可见思考内容增量。
	RuntimeEventTypeThinkingDelta RuntimeEventType = "thinking_delta"
	// RuntimeEventTypeToolUse 表示 Runtime 请求执行一个工具。
	RuntimeEventTypeToolUse RuntimeEventType = "tool_use"
	// RuntimeEventTypeToolInputSnapshot 表示同一工具调用的累计参数状态，不表示再次执行。
	RuntimeEventTypeToolInputSnapshot RuntimeEventType = "tool_input_snapshot"
	// RuntimeEventTypeToolUpdate 表示同一工具调用的非终态执行进度、正文或差异更新，不表示再次请求执行。
	RuntimeEventTypeToolUpdate RuntimeEventType = "tool_update"
	// RuntimeEventTypeToolResult 表示一个工具调用已经返回。
	RuntimeEventTypeToolResult RuntimeEventType = "tool_result"
	// RuntimeEventTypeUsage 表示 Runtime 报告统一的 Turn Token 与 Session 上下文用量。
	RuntimeEventTypeUsage RuntimeEventType = "usage"
	// RuntimeEventTypeSession 表示 Runtime 创建或捕获了原生 Session。
	RuntimeEventTypeSession RuntimeEventType = "session"
	// RuntimeEventTypeError 表示 Runtime 协议报告错误。
	RuntimeEventTypeError RuntimeEventType = "error"
	// RuntimeEventTypeDiagnostic 表示 SDK 产生了不含秘密值的诊断。
	RuntimeEventTypeDiagnostic RuntimeEventType = "diagnostic"
	// RuntimeEventTypeRaw 表示 SDK 保留了尚未识别的原始输出。
	RuntimeEventTypeRaw RuntimeEventType = "raw"
)

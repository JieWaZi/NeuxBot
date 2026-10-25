// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum ChatEventType

// ChatEventType 表示 Chat 权威日志中的稳定事件类型。
type ChatEventType string

const (
	// ChatEventTypeAgentCommandCommitted 记录 Core 已确认的软件命令事实，不由模型生成。
	ChatEventTypeAgentCommandCommitted ChatEventType = "agent.command_committed"
	// ChatEventTypeMessageInherited 表示分叉复制的历史展示快照，不创建新的 Execution。
	ChatEventTypeMessageInherited ChatEventType = "message.inherited"
	// ChatEventTypeUnspecified 表示框架或存量数据产生的空事件类型。
	ChatEventTypeUnspecified ChatEventType = ""
	// ChatEventTypeCreated 表示 Chat 已经建立并锁定工作目录。
	ChatEventTypeCreated ChatEventType = "chat.created"
	// ChatEventTypeTitleUpdated 表示 Chat 标题已经更新。
	ChatEventTypeTitleUpdated ChatEventType = "chat.title_updated"
	// ChatEventTypeUserMessageCreated 表示一条用户消息已经持久化。
	ChatEventTypeUserMessageCreated ChatEventType = "message.user_created"
	// ChatEventTypeExecutionCreated 表示一次执行已经建立。
	ChatEventTypeExecutionCreated ChatEventType = "execution.created"
	// ChatEventTypeExecutionStarted 表示一次执行已经启动 Runtime。
	ChatEventTypeExecutionStarted ChatEventType = "execution.started"
	// ChatEventTypeRuntimeEvent 表示仅用于实时投影、不会进入权威日志的 Runtime 增量。
	ChatEventTypeRuntimeEvent ChatEventType = "runtime.event"
	// ChatEventTypeApprovalRequested 表示 Runtime 正在等待用户审批安全操作。
	ChatEventTypeApprovalRequested ChatEventType = "approval.requested"
	// ChatEventTypeApprovalResolved 表示一次审批已经选择、拒绝或因执行结束失效。
	ChatEventTypeApprovalResolved ChatEventType = "approval.resolved"
	// ChatEventTypeElicitationRequested 表示 Runtime 正在等待用户提交结构化输入。
	ChatEventTypeElicitationRequested ChatEventType = "elicitation.requested"
	// ChatEventTypeElicitationResolved 表示一次结构化输入已经提交、拒绝或因执行结束失效。
	ChatEventTypeElicitationResolved ChatEventType = "elicitation.resolved"
	// ChatEventTypeAssistantMessage 表示 Runtime 已完整形成一条 Assistant 执行消息。
	ChatEventTypeAssistantMessage ChatEventType = "assistant.message"
	// ChatEventTypeToolMessage 表示 Runtime 已完整形成一条工具结果执行消息。
	ChatEventTypeToolMessage ChatEventType = "tool.message"
	// ChatEventTypeInvocationFinished 表示一次 Runtime Invocation 的实际事实已经持久化。
	ChatEventTypeInvocationFinished ChatEventType = "invocation.finished"
	// ChatEventTypeExecutionCompleted 表示一次执行已经成功结束。
	ChatEventTypeExecutionCompleted ChatEventType = "execution.completed"
	// ChatEventTypeExecutionFailed 表示一次执行因错误结束。
	ChatEventTypeExecutionFailed ChatEventType = "execution.failed"
	// ChatEventTypeExecutionCanceled 表示一次执行已经被用户或进程关闭取消。
	ChatEventTypeExecutionCanceled ChatEventType = "execution.canceled"
	// ChatEventTypeNativeSessionUpdated 表示原生 Runtime Session 句柄已经更新。
	ChatEventTypeNativeSessionUpdated ChatEventType = "native_session.updated"
	// ChatEventTypeNativeSessionCleared 表示失效的原生 Session 句柄已经清除。
	ChatEventTypeNativeSessionCleared ChatEventType = "native_session.cleared"
)

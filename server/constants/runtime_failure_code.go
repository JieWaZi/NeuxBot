package constants

//go:generate toolx gen enum RuntimeFailureCode

// RuntimeFailureCode 是 Runtime SDK 的稳定技术失败码。
type RuntimeFailureCode string

const (
	// RuntimeFailureCodeUnspecified 表示当前没有 Runtime 失败码。
	RuntimeFailureCodeUnspecified RuntimeFailureCode = ""
	// RuntimeFailureCodeExecutionFailed 表示已确认失败但没有可靠细分类别。
	RuntimeFailureCodeExecutionFailed RuntimeFailureCode = "EXECUTION_FAILED"
	// RuntimeFailureCodeNetworkError 表示模型服务连接中断，提交结果可能未知。
	RuntimeFailureCodeNetworkError RuntimeFailureCode = "NETWORK_ERROR"
	// RuntimeFailureCodeRequestTimeout 表示请求超过期限，不等同于用户停止。
	RuntimeFailureCodeRequestTimeout RuntimeFailureCode = "REQUEST_TIMEOUT"
	// RuntimeFailureCodeInputTimeout 表示本次等待用户回答的期限到期。
	RuntimeFailureCodeInputTimeout RuntimeFailureCode = "INPUT_TIMEOUT"
	// RuntimeFailureCodeApprovalTimeout 表示本次等待用户审批的期限到期。
	RuntimeFailureCodeApprovalTimeout RuntimeFailureCode = "APPROVAL_TIMEOUT"
	// RuntimeFailureCodeContextTooLarge 表示当前上下文超过所选模型限制。
	RuntimeFailureCodeContextTooLarge RuntimeFailureCode = "CONTEXT_TOO_LARGE"
	// RuntimeFailureCodeRuntimeNotFound 表示请求的开放 Runtime ID 未注册。
	RuntimeFailureCodeRuntimeNotFound RuntimeFailureCode = "RUNTIME_NOT_FOUND"
	// RuntimeFailureCodeRuntimeUnavailable 表示 Runtime 可执行文件当前不可调用。
	RuntimeFailureCodeRuntimeUnavailable RuntimeFailureCode = "RUNTIME_UNAVAILABLE"
	// RuntimeFailureCodeRuntimeIncompatible 表示 Runtime 缺少必要协议能力。
	RuntimeFailureCodeRuntimeIncompatible RuntimeFailureCode = "RUNTIME_INCOMPATIBLE"
	// RuntimeFailureCodeInvalidRequest 表示调用参数缺失或相互冲突。
	RuntimeFailureCodeInvalidRequest RuntimeFailureCode = "INVALID_REQUEST"
	// RuntimeFailureCodePolicyUnsupported 表示 Runtime 无法准确表达调用方权限策略。
	RuntimeFailureCodePolicyUnsupported RuntimeFailureCode = "POLICY_UNSUPPORTED"
	// RuntimeFailureCodeSpawnFailed 表示操作系统没有成功创建 Runtime 进程。
	RuntimeFailureCodeSpawnFailed RuntimeFailureCode = "SPAWN_FAILED"
	// RuntimeFailureCodeProcessExited 表示 Runtime 进程以失败状态结束。
	RuntimeFailureCodeProcessExited RuntimeFailureCode = "PROCESS_EXITED"
	// RuntimeFailureCodeProtocolIncompatible 表示事件流缺少必要协议语义。
	RuntimeFailureCodeProtocolIncompatible RuntimeFailureCode = "PROTOCOL_INCOMPATIBLE"
	// RuntimeFailureCodeContextCanceled 表示调用方 Context 结束了 Invocation。
	RuntimeFailureCodeContextCanceled RuntimeFailureCode = "CONTEXT_CANCELED"
	// RuntimeFailureCodeEventDelivery 表示 Event Sink 失败或超时。
	RuntimeFailureCodeEventDelivery RuntimeFailureCode = "EVENT_DELIVERY_FAILED"
	// RuntimeFailureCodeEventBudgetExceeded 表示本轮事件数量或累计载荷超过保护预算。
	RuntimeFailureCodeEventBudgetExceeded RuntimeFailureCode = "EVENT_BUDGET_EXCEEDED"
	// RuntimeFailureCodeFrameTooLarge 表示单个协议帧超过硬限制。
	RuntimeFailureCodeFrameTooLarge RuntimeFailureCode = "FRAME_TOO_LARGE"
	// RuntimeFailureCodeInactivityTimeout 表示 Runtime 超过允许时间没有原始进程活动。
	RuntimeFailureCodeInactivityTimeout RuntimeFailureCode = "INACTIVITY_TIMEOUT"
	// RuntimeFailureCodeUnsupportedCapability 表示当前 Runtime 不支持请求的操作。
	RuntimeFailureCodeUnsupportedCapability RuntimeFailureCode = "UNSUPPORTED_CAPABILITY"
	// RuntimeFailureCodeInvocationClosed 表示调用已经结束或输入管道已经关闭。
	RuntimeFailureCodeInvocationClosed RuntimeFailureCode = "INVOCATION_CLOSED"
	// RuntimeFailureCodeSessionNotFound 表示 Runtime 无法恢复调用方传入的 Session。
	RuntimeFailureCodeSessionNotFound RuntimeFailureCode = "SESSION_NOT_FOUND"
	// RuntimeFailureCodeAuthRequired 表示 Runtime 缺少有效鉴权凭据。
	RuntimeFailureCodeAuthRequired RuntimeFailureCode = "AUTH_REQUIRED"
	// RuntimeFailureCodePlanUpgradeRequired 表示当前套餐不允许使用所选模型。
	RuntimeFailureCodePlanUpgradeRequired RuntimeFailureCode = "PLAN_UPGRADE_REQUIRED"
	// RuntimeFailureCodeQuotaExhausted 表示模型服务的可用额度已耗尽。
	RuntimeFailureCodeQuotaExhausted RuntimeFailureCode = "QUOTA_EXHAUSTED"
	// RuntimeFailureCodeRateLimited 表示模型服务拒绝了当前请求频率。
	RuntimeFailureCodeRateLimited RuntimeFailureCode = "RATE_LIMITED"
	// RuntimeFailureCodeModelUnavailable 表示所选模型不存在或当前不可用。
	RuntimeFailureCodeModelUnavailable RuntimeFailureCode = "MODEL_UNAVAILABLE"
	// RuntimeFailureCodeUpstreamUnavailable 表示模型或 Provider 暂时不可用。
	RuntimeFailureCodeUpstreamUnavailable RuntimeFailureCode = "UPSTREAM_UNAVAILABLE"
)

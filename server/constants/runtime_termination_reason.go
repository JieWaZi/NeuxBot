package constants

//go:generate toolx gen enum RuntimeTerminationReason

// RuntimeTerminationReason 表示 Runtime 协议确认的调用终止原因，与执行生命周期分别保存。
type RuntimeTerminationReason string

const (
	// RuntimeTerminationReasonUnspecified 表示尚未记录终止原因，读取时作为未知处理。
	RuntimeTerminationReasonUnspecified RuntimeTerminationReason = ""
	// RuntimeTerminationReasonUnknown 表示协议没有提供可确认的终止原因。
	RuntimeTerminationReasonUnknown RuntimeTerminationReason = "unknown"
	// RuntimeTerminationReasonNormal 表示模型明确报告本轮正常结束。
	RuntimeTerminationReasonNormal RuntimeTerminationReason = "normal"
	// RuntimeTerminationReasonTruncated 表示输出达到 Token 上限而截断。
	RuntimeTerminationReasonTruncated RuntimeTerminationReason = "truncated"
	// RuntimeTerminationReasonTurnLimit 表示达到模型请求回合上限。
	RuntimeTerminationReasonTurnLimit RuntimeTerminationReason = "turn_limit"
	// RuntimeTerminationReasonRefusal 表示模型拒绝回答。
	RuntimeTerminationReasonRefusal RuntimeTerminationReason = "refusal"
	// RuntimeTerminationReasonCancelled 表示协议确认调用已取消。
	RuntimeTerminationReasonCancelled RuntimeTerminationReason = "cancelled"
)

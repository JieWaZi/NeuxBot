package contract

// FaultCode 是独立模块的技术错误码。
type FaultCode string

const (
	FaultInvalidSpec         FaultCode = "invalid_spec"         // FaultInvalidSpec 表示描述不合法。
	FaultUnsupported         FaultCode = "unsupported"          // FaultUnsupported 表示能力尚未支持。
	FaultNotStarted          FaultCode = "not_started"          // FaultNotStarted 表示未开放接纳。
	FaultClosed              FaultCode = "closed"               // FaultClosed 表示已停止接纳。
	FaultCapacity            FaultCode = "capacity_exceeded"    // FaultCapacity 表示保护预算耗尽。
	FaultSessionBusy         FaultCode = "session_busy"         // FaultSessionBusy 表示同会话重入。
	FaultSessionMissing      FaultCode = "session_missing"      // FaultSessionMissing 表示会话不存在。
	FaultSessionMismatch     FaultCode = "session_mismatch"     // FaultSessionMismatch 表示静态或安全范围不兼容。
	FaultDuplicateInvocation FaultCode = "duplicate_invocation" // FaultDuplicateInvocation 表示活动调用身份冲突。
	FaultPreparation         FaultCode = "preparation_failed"   // FaultPreparation 表示资源或原生准备失败。
	FaultDriver              FaultCode = "driver_failed"        // FaultDriver 表示驱动执行失败。
	FaultDelivery            FaultCode = "delivery_failed"      // FaultDelivery 表示观察交付失败。
	FaultLimit               FaultCode = "limit_exceeded"       // FaultLimit 表示事件预算耗尽。
	FaultInteractionRequired FaultCode = "interaction_required" // FaultInteractionRequired 表示需要人工输入。
	FaultCancelled           FaultCode = "cancelled"            // FaultCancelled 表示调用取消。
	FaultCleanup             FaultCode = "cleanup_failed"       // FaultCleanup 表示释放失败。
	FaultProtocol            FaultCode = "protocol_error"       // FaultProtocol 表示驱动契约损坏。
)

// Fault 在模块边界保留准确技术语义，宿主只转换一次。
type Fault struct {
	Code    FaultCode // Code 是稳定技术错误码。
	Stage   string    // Stage 是故障发生阶段。
	Message string    // Message 是无秘密的英文技术说明。
	Cause   error     `json:"-"` // Cause 是仅供根因判断的底层错误，不进入默认 JSON。
}

// Error 返回可脱敏的技术说明，不拼接底层秘密。
func (f *Fault) Error() string {
	if f == nil {
		return ""
	}
	return string(f.Code) + ": " + f.Message
}

// Unwrap 暴露底层根因供 errors.Is 判断。
func (f *Fault) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

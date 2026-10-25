package constants

//go:generate toolx gen enum RemoteTransferState

// RemoteTransferState 仅记录客户端发送与观察结果，不替代远端任务状态。
type RemoteTransferState string

const (
	// RemoteTransferStateNotSent 表示尚未发出实际业务写入。
	RemoteTransferStateNotSent RemoteTransferState = "not_sent"
	// RemoteTransferStateSending 表示原发送已登记但响应尚未确认。
	RemoteTransferStateSending RemoteTransferState = "sending"
	// RemoteTransferStateAccepted 表示远端真实接纳已验证。
	RemoteTransferStateAccepted RemoteTransferState = "accepted"
	// RemoteTransferStateFailed 表示原请求已确认拒绝。
	RemoteTransferStateFailed RemoteTransferState = "failed"
	// RemoteTransferStateUnknown 表示可能已接纳，禁止自动重传。
	RemoteTransferStateUnknown RemoteTransferState = "unknown"
)

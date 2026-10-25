package constants

//go:generate toolx gen enum PluginAuthorizationState

// PluginAuthorizationState 表示一次共享授权会话的真实阶段。
type PluginAuthorizationState string

const (
	// PluginAuthorizationStateUnspecified 表示尚未发起授权。
	PluginAuthorizationStateUnspecified PluginAuthorizationState = ""
	// PluginAuthorizationStateWaiting 表示正在等待外部确认。
	PluginAuthorizationStateWaiting PluginAuthorizationState = "waiting"
	// PluginAuthorizationStateReady 表示已确认身份及所需权限。
	PluginAuthorizationStateReady PluginAuthorizationState = "ready"
	// PluginAuthorizationStateFailed 表示授权或身份验证失败。
	PluginAuthorizationStateFailed PluginAuthorizationState = "failed"
	// PluginAuthorizationStateExpired 表示本次设备等待已到期。
	PluginAuthorizationStateExpired PluginAuthorizationState = "expired"
	// PluginAuthorizationStateCancelled 表示用户或进程取消等待。
	PluginAuthorizationStateCancelled PluginAuthorizationState = "cancelled"
)

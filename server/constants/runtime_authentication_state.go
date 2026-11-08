package constants

//go:generate toolx gen enum RuntimeAuthenticationState

// RuntimeAuthenticationState 表示系统原生账号认证任务的当前事实。
type RuntimeAuthenticationState string

const (
	// RuntimeAuthenticationStateUnspecified 表示尚未建立状态。
	RuntimeAuthenticationStateUnspecified RuntimeAuthenticationState = ""
	// RuntimeAuthenticationStateIdle 表示当前没有认证任务。
	RuntimeAuthenticationStateIdle RuntimeAuthenticationState = "idle"
	// RuntimeAuthenticationStateChecking 表示正在读取原生认证能力。
	RuntimeAuthenticationStateChecking RuntimeAuthenticationState = "checking"
	// RuntimeAuthenticationStateAuthenticating 表示正在进行用户触发的原生认证。
	RuntimeAuthenticationStateAuthenticating RuntimeAuthenticationState = "authenticating"
	// RuntimeAuthenticationStateSucceeded 表示原生认证已确认成功。
	RuntimeAuthenticationStateSucceeded RuntimeAuthenticationState = "succeeded"
	// RuntimeAuthenticationStateFailed 表示原生认证失败。
	RuntimeAuthenticationStateFailed RuntimeAuthenticationState = "failed"
	// RuntimeAuthenticationStateCancelled 表示原生认证已取消并完成回收。
	RuntimeAuthenticationStateCancelled RuntimeAuthenticationState = "cancelled"
)

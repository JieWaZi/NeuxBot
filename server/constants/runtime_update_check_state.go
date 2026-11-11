package constants

//go:generate toolx gen enum RuntimeUpdateCheckState

// RuntimeUpdateCheckState 表示官方版本检查的当前事实。
type RuntimeUpdateCheckState string

const (
	// RuntimeUpdateCheckStateUnspecified 表示尚未建立检查状态。
	RuntimeUpdateCheckStateUnspecified RuntimeUpdateCheckState = ""
	// RuntimeUpdateCheckStateUnknown 表示尚未显式检查。
	RuntimeUpdateCheckStateUnknown RuntimeUpdateCheckState = "unknown"
	// RuntimeUpdateCheckStateChecking 表示正在读取官方元数据。
	RuntimeUpdateCheckStateChecking RuntimeUpdateCheckState = "checking"
	// RuntimeUpdateCheckStateChecked 表示已经取得官方版本。
	RuntimeUpdateCheckStateChecked RuntimeUpdateCheckState = "checked"
	// RuntimeUpdateCheckStateFailed 表示本次检查失败，不能判定是否最新。
	RuntimeUpdateCheckStateFailed RuntimeUpdateCheckState = "failed"
)

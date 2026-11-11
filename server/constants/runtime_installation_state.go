package constants

//go:generate toolx gen enum RuntimeInstallationState

// RuntimeInstallationState 表示受管程序安装任务的当前事实。
type RuntimeInstallationState string

const (
	// RuntimeInstallationStateUnspecified 表示尚未建立安装快照。
	RuntimeInstallationStateUnspecified RuntimeInstallationState = ""
	// RuntimeInstallationStateIdle 表示尚未安装受管程序。
	RuntimeInstallationStateIdle RuntimeInstallationState = "idle"
	// RuntimeInstallationStateInstalling 表示安装任务正在处理。
	RuntimeInstallationStateInstalling RuntimeInstallationState = "installing"
	// RuntimeInstallationStateInstalled 表示程序已校验并完成发布。
	RuntimeInstallationStateInstalled RuntimeInstallationState = "installed"
	// RuntimeInstallationStateFailed 表示最近的安装任务失败。
	RuntimeInstallationStateFailed RuntimeInstallationState = "failed"
	// RuntimeInstallationStateCancelled 表示安装已取消并完成回收。
	RuntimeInstallationStateCancelled RuntimeInstallationState = "cancelled"
)

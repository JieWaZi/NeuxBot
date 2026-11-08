package constants

//go:generate toolx gen enum RuntimeInstallationSource

// RuntimeInstallationSource 表示当前选用程序的真实来源。
type RuntimeInstallationSource string

const (
	// RuntimeInstallationSourceUnspecified 表示尚未建立来源快照。
	RuntimeInstallationSourceUnspecified RuntimeInstallationSource = ""
	// RuntimeInstallationSourceUnknown 表示尚未确认程序来源。
	RuntimeInstallationSourceUnknown RuntimeInstallationSource = "unknown"
	// RuntimeInstallationSourceSystem 表示使用本机已有安装。
	RuntimeInstallationSourceSystem RuntimeInstallationSource = "system"
	// RuntimeInstallationSourceManaged 表示使用应用验证并发布的程序。
	RuntimeInstallationSourceManaged RuntimeInstallationSource = "managed"
)

package constants

//go:generate toolx gen enum RuntimeDetectionState

// RuntimeDetectionState 表示最近一次探测结论，不决定实际调用资格。
type RuntimeDetectionState string

const (
	// RuntimeDetectionStateUnspecified 表示尚未执行 Runtime 探测。
	RuntimeDetectionStateUnspecified RuntimeDetectionState = ""
	// RuntimeDetectionStateUnavailable 表示最近探测未找到程序或未能完成调用。
	RuntimeDetectionStateUnavailable RuntimeDetectionState = "unavailable"
	// RuntimeDetectionStateIncompatible 表示最近探测未确认必要协议能力。
	RuntimeDetectionStateIncompatible RuntimeDetectionState = "incompatible"
	// RuntimeDetectionStateReady 表示最近探测确认了必要能力。
	RuntimeDetectionStateReady RuntimeDetectionState = "ready"
)

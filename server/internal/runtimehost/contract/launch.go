package contract

// LaunchFacts 是已有探测与账号准备共同确认的原生启动事实，不创建协议连接。
type LaunchFacts struct {
	// BaseRuntimeID 决定公开适配器的协议类型。
	BaseRuntimeID string
	// Executable 是有效探测已经解析的真实程序路径。
	Executable string
	// PrefixArgs 是 Profile 与启动覆盖合并后的前置参数。
	PrefixArgs []string
	// Environment 是账号隔离、Profile 覆盖与工具路径补充后的完整环境。
	Environment []string
}

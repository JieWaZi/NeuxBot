package constants

//go:generate toolx gen enum PluginBindingTarget

// PluginBindingTarget 表示客户端扩展可配置的 MCP 注入位置。
type PluginBindingTarget string

const (
	// PluginBindingTargetUnspecified 表示尚未选择注入位置。
	PluginBindingTargetUnspecified PluginBindingTarget = ""
	// PluginBindingTargetEnvironment 表示标准输入输出服务的环境变量。
	PluginBindingTargetEnvironment PluginBindingTarget = "env"
	// PluginBindingTargetHeader 表示客户端管理的远程授权请求头。
	PluginBindingTargetHeader PluginBindingTarget = "header"
)

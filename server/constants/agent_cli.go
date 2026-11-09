package constants

const (
	// AgentCLIProtocolVersion 是随 Core、CLI 和系统技能一起发布的协议版本。
	AgentCLIProtocolVersion = 1
	// AgentCLIPath 是与桌面管理认证隔离的本机能力入口。
	AgentCLIPath = "/agent/v1"
	// AgentCLIMCPCatalogPath 只向待接纳或已接纳的授权上下文发布固定工具目录。
	AgentCLIMCPCatalogPath = "/mcp/catalog"
	// AgentCLIBinaryEnvironment 允许应用装配明确提供受管 CLI 的实际路径。
	AgentCLIBinaryEnvironment = "ALLYCLI_BINARY_PATH"
	// AgentCLIRequestMaximumBytes 限制普通命令载荷与连接响应。
	AgentCLIRequestMaximumBytes = 4 << 20
	// AgentCLIContextMaximumBytes 限制本机连接文件，避免读取任意大文件。
	AgentCLIContextMaximumBytes = 16 << 10
	// AgentCLICompletionImmediate 表示本次同步操作已经提交。
	AgentCLICompletionImmediate = "immediate"
	// AgentCLICompletionAccepted 表示持久接纳，业务结果需单独观察。
	AgentCLICompletionAccepted = "accepted"
	// AgentCLICompletionCandidate 表示候选已提交，尚未成为执行终态。
	AgentCLICompletionCandidate = "candidate"
	// AgentCLIEffectRead 表示操作只观察事实。
	AgentCLIEffectRead = "read"
	// AgentCLIEffectMutation 表示操作修改领域事实。
	AgentCLIEffectMutation = "mutation"
	// AgentCLIEffectExternal 表示操作接纳外部效果。
	AgentCLIEffectExternal = "external"
	// AgentCLISkillName 是软件能力的保留技能名。
	AgentCLISkillName = "ally"
)

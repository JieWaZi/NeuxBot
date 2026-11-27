package constants

const (
	// AgentCLIProtocolVersion 是随 Core、CLI 和系统技能一起发布的协议版本。
	AgentCLIProtocolVersion = 1
	// AgentCLIPath 是与桌面管理认证隔离的本机能力入口。
	AgentCLIPath = "/agent/v1"
	// AgentCLIMCPCatalogPath 只向待接纳或已接纳的授权上下文发布固定工具目录。
	AgentCLIMCPCatalogPath = "/mcp/catalog"
	// AgentCLIExecutionPath 提供常驻连接当前接纳的执行标识。
	AgentCLIExecutionPath = "/execution"
	// AgentCLIExecutionHeader 携带命令发起时由宿主返回的执行标识。
	AgentCLIExecutionHeader = "X-NeuxBot-Execution-ID"
	// AgentCLIConnectionHeader 定位共享 Core token 下的宿主连接。
	AgentCLIConnectionHeader = "X-NeuxBot-Connection-ID"
	// AgentCLIBinaryEnvironment 允许应用装配明确提供受管 CLI 的实际路径。
	AgentCLIBinaryEnvironment = "BOTCLI_BINARY_PATH"
	// AgentCLIContextEnvironment 是宿主交给原生进程的能力连接文件路径。
	AgentCLIContextEnvironment = "BOTCLI_CONTEXT_FILE"
	// AgentCLICodexThreadEnvironment 是 Codex 为实际 Shell 调用提供的原生线程标识。
	AgentCLICodexThreadEnvironment = "CODEX_THREAD_ID"
	// AgentCLIContextDirectory 在 Core 数据根内保存受管连接。
	AgentCLIContextDirectory = "agent-contexts"
	// AgentCLISystemSkillsDirectory 在 Core 数据根内保存随版本发布的不可变系统技能。
	AgentCLISystemSkillsDirectory = "system-skills"
	// AgentCLISkillScriptsDirectory 是系统技能中的可执行文件目录。
	AgentCLISkillScriptsDirectory = "scripts"
	// AgentCLIBinaryName 是系统技能中受管命令的可执行文件基础名称。
	AgentCLIBinaryName = "botcli"
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
	AgentCLISkillName = "bot"
)

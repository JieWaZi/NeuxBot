package constants

// KnowledgeLibraryScanBatchSize 限制目录搜索与活动任务恢复每批读取的库元数据。
const KnowledgeLibraryScanBatchSize = 100

// RuntimeUserInputMCPServerName 是运行时受管问答服务的稳定协议名称。
const RuntimeUserInputMCPServerName = "acp_go_user_input"

// RuntimeUserInputToolName 是受管问答与原生问答共用的提问工具名。
const RuntimeUserInputToolName = "AskUserQuestion"

// WorkflowWorkerSkipRequestedEvent 表示用户请求跳过本次伙伴尝试。
const WorkflowWorkerSkipRequestedEvent = "workflow.worker.skip_requested"

// WorkflowEventHistoryPageSize 限制每次事件或日志历史读取的条数。
const WorkflowEventHistoryPageSize = 500

// WorkResultTrackingID 是唯一阅读跟踪启用记录的身份。
const WorkResultTrackingID = "00000000000000000000000000000001"

// WorkflowFileFormat 标记以字符串传递的工作流文件参数及产物。
const WorkflowFileFormat = "file"

// WorkflowFileReferencePrefix 区分受管上传引用与本地路径。
const WorkflowFileReferencePrefix = "ally-file:"

// WorkflowFileDirectoryName 保存工作流上传的原始文件和归属信息。
const WorkflowFileDirectoryName = "workflow-files"

// WorkflowInputFileDirectoryName 隔离每次运行已冻结的输入副本。
const WorkflowInputFileDirectoryName = "workflow-inputs"

// ManagedFilesDirectoryName 是用户主目录下受管业务文件的默认根目录名。
const ManagedFilesDirectoryName = ".neuxally"

// RuntimeStateDirectoryName 保存当前工作区独有的智能体执行配置和状态。
const RuntimeStateDirectoryName = "runtime"

// ChatForkDirectoryName 保存聊天分叉操作记录和暂存聚合目录。
const ChatForkDirectoryName = "forks"

// WorkflowFileMaximumBytes 限制单次上传或下载的文件字节数。
const WorkflowFileMaximumBytes int64 = 100 << 20

// SkillPartnerPreviewLimit 是独立技能卡片最多返回的安装伙伴预览数。
const SkillPartnerPreviewLimit = 4

const (
	// WorkflowInstanceBudget 标识整次工作流累计节点实例数的预算错误。
	WorkflowInstanceBudget = "instances"
	// WorkflowResultMissingCode 表示执行已结束但没有形成所需的节点结果。
	WorkflowResultMissingCode = "RESULT_MISSING"

	// AiAccountSystemID 表示使用智能体内置登录的账号身份。
	AiAccountSystemID = "system"

	// AutomaticTaskDefaultAPIIP 是独立任务触发入口默认监听的本机地址。
	AutomaticTaskDefaultAPIIP = "127.0.0.1"
	// AutomaticTaskDefaultAPIPort 是独立本机任务触发入口的默认端口。
	AutomaticTaskDefaultAPIPort = 8431
	// AutomaticTaskInstructionMaximumRunes 是任务指令的 Unicode 字符上限。
	AutomaticTaskInstructionMaximumRunes = 10000
	// AutomaticTaskInputMaximumRunes 是本次 API 附加输入的 Unicode 字符上限。
	AutomaticTaskInputMaximumRunes = 4000
	// AutomaticTaskRequestMaximumBytes 是本机 JSON 触发请求的字节上限。
	AutomaticTaskRequestMaximumBytes = 64 * 1024
	// AutomaticTaskAPIRequestsPerMinute 是同一任务每分钟允许接纳的新请求数量。
	AutomaticTaskAPIRequestsPerMinute = 60
	// AutomaticTaskLocalTimeLayout 是一次性计划使用的当地分钟时间格式。
	AutomaticTaskLocalTimeLayout = "2006-01-02T15:04"
	// AutomaticTaskActor 是自动输入在标准 Chat 中的独立参与者身份。
	AutomaticTaskActor = "automatic_task"
	// AutomaticTaskScheduleGraceSeconds 是持续运行期间允许的调度宽限。
	AutomaticTaskScheduleGraceSeconds = 60
)

const (
	// WorkflowWorkspaceDirectoryName 是作者和运行独立受管工作区的父目录。
	WorkflowWorkspaceDirectoryName = "workflow-workspaces"
	// ApplicationName 是 Core 对外报告的产品名称。
	ApplicationName = "NeuxAlly"
	// CoreDefaultHost 是桌面 Core 默认监听的回环地址。
	CoreDefaultHost = "127.0.0.1"
	// CoreDefaultPort 是桌面 Core 默认监听的端口。
	CoreDefaultPort = 8432
	// CoreGraceExitSeconds 是桌面 Core 默认优雅退出等待秒数。
	CoreGraceExitSeconds = 5
)

const (
	// RuntimeAdditionalDirectoriesUnsupportedWarning 表示 Runtime 未能获得真实项目目录授权。
	RuntimeAdditionalDirectoriesUnsupportedWarning = "runtime_additional_directories_unsupported"
	// CreateRequestIDMaximumLength 是各类资源首次创建请求标识允许的最大长度。
	CreateRequestIDMaximumLength = 64
	// RuntimeHistoryDefaultTurns 是新建原生会话时默认携带的最近完整对话轮数。
	RuntimeHistoryDefaultTurns = 5
	// RuntimeHistoryMaximumTurns 是用户可选择携带的最大完整对话轮数。
	RuntimeHistoryMaximumTurns = 20
	// RuntimeHistoryMaximumBytes 是新原生会话历史正文的总字节上限。
	RuntimeHistoryMaximumBytes = 65536
	// RuntimeHandoffMaximumBytes 是隐藏交接内容注入新会话的最大字节数。
	RuntimeHandoffMaximumBytes = 16384
)

const (
	// BuiltinMemoryRuntimeInstructionName 是注入 Runtime 的记忆指令英文标题。
	BuiltinMemoryRuntimeInstructionName = "System Memory"
	// BuiltinMemoryMCPConnectorRuntimeKey 是内置记忆 MCP 连接器的固定 Runtime 注入键。
	BuiltinMemoryMCPConnectorRuntimeKey = "ally_memory_builtin"
	// BuiltinChannelTasksMCPConnectorRuntimeKey 是内置团队任务 MCP 的固定 Runtime 注入键。
	BuiltinChannelTasksMCPConnectorRuntimeKey = "ally_channel_tasks_builtin"
	// MemorySearchDefaultTopK 是记忆检索默认返回条数，用于有界返回最相关记忆。
	MemorySearchDefaultTopK = 20
	// MemorySearchMaxTopK 是记忆检索允许的最大返回条数。
	MemorySearchMaxTopK = 100
	// MemoryContentMaxRunes 是单条记忆正文的最大字符数。
	MemoryContentMaxRunes = 4096
	// MemoryManagementDefaultPageSize 是记忆管理列表的默认单页条数。
	MemoryManagementDefaultPageSize = 50
	// MemoryManagementMaxPageSize 是记忆管理列表允许的最大单页条数。
	MemoryManagementMaxPageSize = 100
)

const (
	// ChannelMinimumMemberCount 是创建团队所需的最少成员数。
	ChannelMinimumMemberCount = 2
	// ChannelMaximumMemberCount 是团队允许配置的最多成员数。
	ChannelMaximumMemberCount = 5
	// ChannelDefaultMaximumParallelTasks 是团队对话默认并行任务上限。
	ChannelDefaultMaximumParallelTasks = 3
	// ChannelDefaultMaximumTasks 是团队对话默认任务总数上限。
	ChannelDefaultMaximumTasks = 12
	// ChannelDefaultMaximumFailureAttempts 是单项任务默认失败尝试上限。
	ChannelDefaultMaximumFailureAttempts = 2
	// ChannelMaximumInputBytes 是团队消息和补充允许的最大字节数。
	ChannelMaximumInputBytes = 1024 * 1024
	// ChannelMaximumTaskSubjectRunes 是任务主题允许的最大字符数。
	ChannelMaximumTaskSubjectRunes = 200
	// ChannelAutomaticTaskSubjectRunes 是系统从用户正文提取任务主题时保留的最大字符数。
	ChannelAutomaticTaskSubjectRunes = 80
	// ChannelRosterMaximumNameRunes 是负责人 roster 中单成员名称最大字符数。
	ChannelRosterMaximumNameRunes = 80
	// ChannelRosterMaximumRoleNameRunes 是负责人 roster 中单成员角色名称最大字符数。
	ChannelRosterMaximumRoleNameRunes = 80
	// ChannelRosterMaximumBioRunes 是负责人每轮看到的单成员简介最大字符数。
	ChannelRosterMaximumBioRunes = 240
	// ChannelRosterMaximumExpertiseCount 是负责人每轮看到的单成员专长最大数量。
	ChannelRosterMaximumExpertiseCount = 6
	// ChannelRosterMaximumExpertiseTitleRunes 是负责人 roster 中单项专长标题最大字符数。
	ChannelRosterMaximumExpertiseTitleRunes = 80
	// ChannelRosterMaximumExpertiseDescriptionRunes 是负责人 roster 中单项专长说明最大字符数。
	ChannelRosterMaximumExpertiseDescriptionRunes = 240
	// ChannelEventDefaultLimit 是团队事件接口默认返回条数。
	ChannelEventDefaultLimit = 100
	// ChannelEventMaximumLimit 是团队事件接口允许的最大返回条数。
	ChannelEventMaximumLimit = 500
	// ChannelCollectionDefaultPageSize 是团队与对话列表的默认单页数量。
	ChannelCollectionDefaultPageSize = 50
	// ChannelCollectionMaximumPageSize 是团队与对话列表允许的最大单页数量。
	ChannelCollectionMaximumPageSize = 100
	// ChannelNotificationBatchLimit 是单轮交给负责人的最大结果通知数。
	ChannelNotificationBatchLimit = 50
	// ChannelMemberReportMaximumRunes 是单条成员结果报告进入负责人上下文的最大字符数。
	ChannelMemberReportMaximumRunes = 4_000
	// ChannelMemberReportFinalInstruction 是成员报告成功后避免再次输出完整成果的短提示。
	ChannelMemberReportFinalInstruction = "Report saved. End this turn with one short acknowledgement; do not repeat the report."
	// ChannelEventHeartbeatIntervalSeconds 是团队事件流心跳间隔。
	ChannelEventHeartbeatIntervalSeconds = 15
	// ChannelTasksMCPTitle 是团队协作工具的展示名称。
	ChannelTasksMCPTitle = "NeuxAlly Team Tasks"
	// ChannelTasksMCPTokenBytes 是团队协作工具执行期租约标识的随机字节数。
	ChannelTasksMCPTokenBytes = 32
	// ChannelCapabilityTTLHours 是单次团队执行能力的强制最长存活小时数。
	ChannelCapabilityTTLHours = 24
	// ChannelCapabilityRoleLead 是协作 MCP 的负责人调用角色。
	ChannelCapabilityRoleLead = "lead"
	// ChannelCapabilityRoleMember 是协作 MCP 的执行成员调用角色。
	ChannelCapabilityRoleMember = "member"
	// ChannelTasksMCPCapabilityInstructions 是注入 Runtime 的短能力说明，细则见 TEAM.md。
	ChannelTasksMCPCapabilityInstructions = "Follow TEAM.md to use the shared task board or submit a member work report."
	// TeamInstructionFileName 是团队协作虚拟上下文文件名。
	TeamInstructionFileName = "TEAM.md"
	// ChannelAttemptWatcherMaximumImmediateRetries 是终态提交在重新订阅前的最大立即重试次数。
	ChannelAttemptWatcherMaximumImmediateRetries = 3
	// ChannelDeliveryWatcherMaximumConsecutiveFailures 是持久投递监听转入补偿前允许的连续失败次数。
	ChannelDeliveryWatcherMaximumConsecutiveFailures = 8
	// ChannelAttemptWatcherInitialRetryDelayMilliseconds 是任务尝试监听的初始重试间隔。
	ChannelAttemptWatcherInitialRetryDelayMilliseconds = 100
	// ChannelAttemptWatcherMaximumRetryDelayMilliseconds 是任务尝试监听的最大重试间隔。
	ChannelAttemptWatcherMaximumRetryDelayMilliseconds = 5_000
	// ChannelRecoveryPollIntervalMilliseconds 是团队重启恢复队列的轮询间隔。
	ChannelRecoveryPollIntervalMilliseconds = 1_000
	// ChannelTaskReviewDecisionApproved 表示用户通过任务的人工验收。
	ChannelTaskReviewDecisionApproved = "approved"
	// ChannelTimelineDefaultLimit 是团队聚合时间线的默认单页消息数。
	ChannelTimelineDefaultLimit = 50
	// ChannelTimelineMaxLimit 是团队聚合时间线允许的最大单页消息数。
	ChannelTimelineMaxLimit = 100
)

// MemorySearchScoreThreshold 是语义检索的最低相似度阈值，用于过滤低相关命中。
const MemorySearchScoreThreshold = 0.1

const (
	// MemorySearchMCPToolName 是检索长期记忆的稳定工具名。
	MemorySearchMCPToolName = "memory_search"
	// MemorySearchMCPToolDescription 是协议与连接器详情共用的英文检索说明。
	MemorySearchMCPToolDescription = "Search long-term memory before tasks that may depend on prior preferences or project conventions. Scope accepts partner, project, or all; the default is all."
	// MemoryWriteMCPToolName 是新增长期记忆的稳定工具名。
	MemoryWriteMCPToolName = "memory_write"
	// MemoryWriteMCPToolDescription 是协议与连接器详情共用的英文保存说明。
	MemoryWriteMCPToolDescription = "Save one long-term memory. Call memory_search first to avoid duplicates. Scope accepts partner or project."
	// MemoryUpdateMCPToolName 是更新已有记忆的稳定工具名。
	MemoryUpdateMCPToolName = "memory_update"
	// MemoryUpdateMCPToolDescription 是协议与连接器详情共用的英文更新说明。
	MemoryUpdateMCPToolDescription = "Replace the content of a saved memory without changing its scope."
	// MemoryForgetMCPToolName 是遗忘已有记忆的稳定工具名。
	MemoryForgetMCPToolName = "memory_forget"
	// MemoryForgetMCPToolDescription 是协议与连接器详情共用的英文遗忘说明。
	MemoryForgetMCPToolDescription = "Forget one memory by deleting its current content while retaining deletion history."
	// BuiltinMemoryRuntimeInstructions 是执行时注入 Runtime 的统一记忆使用策略。
	BuiltinMemoryRuntimeInstructions = "Prefer system memory: search it when relevant before answering; " +
		"save stable, reusable user preferences, facts, or project conventions; " +
		"when the user asks to remember, update, or forget something, perform the corresponding operation and rely on the tool result. " +
		"Do not use the Runtime's built-in memory."
)

const (
	// MCPConnectorDefaultTimeoutMS 是 MCP 连接器默认请求超时毫秒数。
	MCPConnectorDefaultTimeoutMS = int64(10_000)
	// MCPConnectorMinimumTimeoutMS 是 MCP 连接器允许的最小请求超时毫秒数。
	MCPConnectorMinimumTimeoutMS = int64(100)
	// MCPConnectorMaximumTimeoutMS 是 MCP 连接器允许的最大请求超时毫秒数。
	MCPConnectorMaximumTimeoutMS = int64(300_000)
	// PluginConnectorDiagnosticCacheLimit 是当前 Core 最多保留的插件连接诊断数量。
	PluginConnectorDiagnosticCacheLimit = 256
	// MCPConnectorMaximumToolCount 是单个连接器允许公开的最大工具数。
	MCPConnectorMaximumToolCount = 200
	// MCPConnectorMaximumSummaryLength 是连接器测试摘要允许的最大字符数。
	MCPConnectorMaximumSummaryLength = 2_000
	// MCPConnectorMaximumRuntimeKeyLength 是 Runtime 注入键允许的最大长度。
	MCPConnectorMaximumRuntimeKeyLength = 64
	// MCPConnectorRuntimeKeySuffixLength 是 Runtime 注入键冲突摘要后缀长度。
	MCPConnectorRuntimeKeySuffixLength = 8
)

const (
	// RuntimeIDCodex 是内置 Codex CLI Runtime 标识。
	RuntimeIDCodex = "codex"
	// RuntimeCodexDefaultModeRequestUserInput 在用户未配置时启用 Codex 普通模式的结构化提问。
	RuntimeCodexDefaultModeRequestUserInput = true
	// RuntimeIDClaude 是内置 Claude Code Runtime 标识。
	RuntimeIDClaude = "claude"
	// RuntimeIDKimi 是内置 Kimi CLI Runtime 标识。
	RuntimeIDKimi = "kimi"
	// RuntimeIDCursor 是内置 Cursor CLI Runtime 标识。
	RuntimeIDCursor = "cursor"
	// RuntimeIDPi 是通过 Go Adapter 直连原生 RPC 的 Pi Runtime 标识。
	RuntimeIDPi = "pi"
	// RuntimeIDOpenCode 是官方 OpenCode ACP 运行时。
	RuntimeIDOpenCode = "opencode"
	// RuntimeIDGemini 是官方 Gemini CLI ACP 运行时。
	RuntimeIDGemini = "gemini"
	// RuntimeIDGrok 是官方 Grok Build ACP 运行时。
	RuntimeIDGrok = "grok"
	// RuntimeIDWorkBuddy 通过 CodeBuddy Code 标准 ACP 提供 WorkBuddy 执行能力。
	RuntimeIDWorkBuddy = "workbuddy"
	// RuntimeIDQoder 是官方 Qoder CLI ACP 运行时。
	RuntimeIDQoder = "qoder"
	// RuntimeIDQoderCN 是官方 Qoder CN CLI ACP 运行时。
	RuntimeIDQoderCN = "qoder-cn"
	// RuntimeQoderDefaultModeID 是 Qoder 的默认权限模式。
	RuntimeQoderDefaultModeID = "default"
	// RuntimeQoderBypassModeID 是 Qoder ACP 的跳过权限模式。
	RuntimeQoderBypassModeID = "bypassPermissions"
	// RuntimeQoderYoloModeID 是当前 Qoder ACP 声明的完全访问模式。
	RuntimeQoderYoloModeID = "yolo"
	// RuntimeQoderAutoModeID 是支持自动风险审查的 Qoder ACP 模式。
	RuntimeQoderAutoModeID = "auto"
	// RuntimeWorkBuddyFastModelID 是本阶段不开放的快速自动路由模型标识。
	RuntimeWorkBuddyFastModelID = "fast-model"
	// RuntimeWorkBuddyBalancedModelID 是本阶段不开放的均衡自动路由模型标识。
	RuntimeWorkBuddyBalancedModelID = "balanced-model"
	// RuntimeWorkBuddyDeepModelID 是本阶段不开放的极致自动路由模型标识。
	RuntimeWorkBuddyDeepModelID = "deep-model"
	// RuntimeWorkBuddyThinkingEnabled 是使用模型默认思考强度的开关值，不属于具体强度档位。
	RuntimeWorkBuddyThinkingEnabled = "enabled"
	// RuntimeWorkBuddyThinkingDisabled 是关闭思考的开关值，不属于具体强度档位。
	RuntimeWorkBuddyThinkingDisabled = "disabled"
	// RuntimeWorkBuddyZeroCreditDescription 是 WorkBuddy ACP 明确返回的零积分模型描述。
	RuntimeWorkBuddyZeroCreditDescription = "x0.00 credits"
	// RuntimeModelFreeNameSuffix 标注上游当前目录明确声明免费的模型。
	RuntimeModelFreeNameSuffix = " (free)"
	// AIAccountGoogleURL 是 Google GenerateContent 默认版本化入口。
	AIAccountGoogleURL = "https://generativelanguage.googleapis.com/v1beta"
	// AIAccountOpenCodeProvider 是受管 OpenCode 服务商命名空间。
	AIAccountOpenCodeProvider = "ally"
)

const (
	// RuntimeACPFailureStageConnect 表示 ACP Agent 连接阶段失败。
	RuntimeACPFailureStageConnect = "acp_connect"
	// RuntimeACPFailureStageMCP 表示 MCP 连接器准备阶段失败。
	RuntimeACPFailureStageMCP = "mcp"
	// RuntimeACPFailureStagePromptContent 表示 Prompt 内容块构造阶段失败。
	RuntimeACPFailureStagePromptContent = "prompt_content"
	// RuntimeACPFailureStageSessionOpen 表示 ACP Session 打开阶段失败。
	RuntimeACPFailureStageSessionOpen = "session_open"
	// RuntimeACPFailureStageSessionConfig 表示 ACP Session 配置阶段失败。
	RuntimeACPFailureStageSessionConfig = "session_config"
	// RuntimeACPFailureStageSessionActive 表示 ACP Session 激活阶段失败。
	RuntimeACPFailureStageSessionActive = "session_active"
	// RuntimeACPFailureStageEventQueue 表示 ACP 事件队列初始化阶段失败。
	RuntimeACPFailureStageEventQueue = "event_queue"
	// RuntimeACPFailureStagePromptWrite 表示 ACP Prompt 写入阶段失败。
	RuntimeACPFailureStagePromptWrite = "prompt_write"
	// RuntimeACPFailureStageInitialDelivery 表示 invocation_started 首事件交付阶段失败。
	RuntimeACPFailureStageInitialDelivery = "initial_delivery"
	// RuntimeACPFailureStagePrompt 表示 ACP Prompt 执行阶段失败。
	RuntimeACPFailureStagePrompt = "prompt"
	// RuntimeACPFailureStageSessionClose 表示 ACP Session 关闭阶段失败。
	RuntimeACPFailureStageSessionClose = "session_close"
	// RuntimeACPFailureStageInactivityWatchdog 表示 ACP 无活动超时保护触发失败。
	RuntimeACPFailureStageInactivityWatchdog = "inactivity_watchdog"
	// RuntimeACPFailureStageEventDelivery 表示 ACP 事件串行交付阶段失败。
	RuntimeACPFailureStageEventDelivery = "event_delivery"
	// RuntimeRunFailureStageValidate 表示 Run 请求校验阶段失败。
	RuntimeRunFailureStageValidate = "validate"
	// RuntimeRunFailureStagePrepare 表示 Run 前置探测与准备阶段失败。
	RuntimeRunFailureStagePrepare = "prepare"
	// RuntimeRunFailureStageRuntimeFiles 表示 Runtime 指令与技能文件准备阶段失败。
	RuntimeRunFailureStageRuntimeFiles = "runtime_files"
	// RuntimeExecutionFailureStageStart 表示 Execution 启动 Runtime 阶段失败。
	RuntimeExecutionFailureStageStart = "runtime_start"
	// RuntimeACPMetaKeyErrorKind 是 ACP RequestError 数据中的 upstream 错误类别键。
	RuntimeACPMetaKeyErrorKind = "errorKind"
	// RuntimeACPMetaKeySteering 是 ACP initialize 元数据中的 steering 能力键。
	RuntimeACPMetaKeySteering = "steering"
	// RuntimeACPMetaKeySupported 是 ACP 扩展能力声明中的 supported 键。
	RuntimeACPMetaKeySupported = "supported"
	// RuntimeACPMetaKeyTerminalOutput 是 ACP 终端输出扩展元数据键。
	RuntimeACPMetaKeyTerminalOutput = "terminal_output"
	// RuntimeACPMetaKeyCodex 是 Codex Adapter 保留的消息元数据命名空间键。
	RuntimeACPMetaKeyCodex = "codex"
	// RuntimeACPMetaKeyPhase 是 Adapter 消息阶段元数据键。
	RuntimeACPMetaKeyPhase = "phase"
	// RuntimeACPMessagePhaseCommentary 是 Agent 消息 commentary 阶段标识。
	RuntimeACPMessagePhaseCommentary = "commentary"
	// RuntimeACPProviderErrorKindNetworkError 表示连接中断且请求结果可能未知。
	RuntimeACPProviderErrorKindNetworkError = "network_error"
	// RuntimeACPProviderErrorKindTimeout 表示请求超时。
	RuntimeACPProviderErrorKindTimeout = "timeout"
	// RuntimeACPProviderErrorKindContextTooLarge 表示模型上下文超限。
	RuntimeACPProviderErrorKindContextTooLarge = "context_window_exceeded"
	// RuntimeACPProviderErrorKindAuthenticationFailed 是 upstream 报告的鉴权失败类别。
	RuntimeACPProviderErrorKindAuthenticationFailed = "authentication_failed"
	// RuntimeACPProviderErrorKindOAuthOrgNotAllowed 是 upstream 报告的组织 OAuth 不允许类别。
	RuntimeACPProviderErrorKindOAuthOrgNotAllowed = "oauth_org_not_allowed"
	// RuntimeACPProviderErrorKindBillingError 是 upstream 报告的计费失败类别。
	RuntimeACPProviderErrorKindBillingError = "billing_error"
	// RuntimeACPProviderErrorKindPlanUpgradeRequired 是 upstream 报告的套餐升级类别。
	RuntimeACPProviderErrorKindPlanUpgradeRequired = "plan_upgrade_required"
	// RuntimeACPProviderErrorKindQuotaExhausted 是 upstream 报告的额度耗尽类别。
	RuntimeACPProviderErrorKindQuotaExhausted = "quota_exhausted"
	// RuntimeACPProviderErrorKindRateLimit 是 upstream 报告的限流类别。
	RuntimeACPProviderErrorKindRateLimit = "rate_limit"
	// RuntimeACPProviderErrorKindOverloaded 是 upstream 报告的服务过载类别。
	RuntimeACPProviderErrorKindOverloaded = "overloaded"
	// RuntimeACPProviderErrorKindServerError 是 upstream 报告的服务端错误类别。
	RuntimeACPProviderErrorKindServerError = "server_error"
	// RuntimeACPProviderErrorKindInvalidRequest 是 upstream 报告的请求无效类别。
	RuntimeACPProviderErrorKindInvalidRequest = "invalid_request"
	// RuntimeACPProviderErrorKindModelNotFound 是 upstream 报告的模型不存在类别。
	RuntimeACPProviderErrorKindModelNotFound = "model_not_found"
	// RuntimeACPMetaKeyCursorErrorCode 是 Cursor Adapter 保留的原生错误码键。
	RuntimeACPMetaKeyCursorErrorCode = "cursorErrorCode"
	// RuntimeACPCursorErrorCodeUpgrade 表示 Cursor 要求升级套餐。
	RuntimeACPCursorErrorCodeUpgrade = "upgrade"
)

const (
	// ChatExecutionWorkspaceOwnerKind 标识对话执行工作区归属标记。
	ChatExecutionWorkspaceOwnerKind = "chat-execution-workspace"
	// ThreadExecutionWorkspaceOwnerKind 标识团队对话 thread 级共享执行工作区归属标记。
	ThreadExecutionWorkspaceOwnerKind = "thread-execution-workspace"
	// ThreadMemberExecutionWorkspaceOwnerKind 标识团队成员独占 Runtime cwd 的归属标记。
	ThreadMemberExecutionWorkspaceOwnerKind = "thread-member-execution-workspace"
	// ChatMessageAttachmentsOwnerKind 标识消息附件目录归属标记。
	ChatMessageAttachmentsOwnerKind = "message-attachments"
	// ExecutionWorkspaceProjectsDirectory 是 Chat 工作区内项目软链接子目录名。
	ExecutionWorkspaceProjectsDirectory = "projects"
	// ExecutionWorkspaceAttachmentsDirectory 是 Chat 工作区内消息附件和会话生成文件的受管根目录名。
	ExecutionWorkspaceAttachmentsDirectory = "attachments"
	// ExecutionWorkspaceGeneratedFilesDirectory 是 attachments/ 内伙伴生成会话文件的子目录名。
	ExecutionWorkspaceGeneratedFilesDirectory = "generated"
)

const (
	// CompanionAvatarPrefix 标识稳定还原身体风格的随机形象种子。
	CompanionAvatarPrefix = "companion:v2:"
	// PersonalAssistantAvatar 固定内置私人助理的毛绒形象身份，由前端风格目录统一还原。
	PersonalAssistantAvatar = "companion:v1:fe9a9eb52f5e4d7dac1959fe4f9559f2"
	// WikiResearcherAvatar 固定内置维基研究员的形象身份，由共享形象目录还原。
	WikiResearcherAvatar = CompanionAvatarPrefix + "854de954b7bd4e2cb9582fd0930dcd6d"
	// DefaultAvatar 是未指定头像时使用的默认预设头像。
	DefaultAvatar = "preset:generic"
	// ChatLogSchemaVersion 是当前 Chat JSONL 信封版本。
	ChatLogSchemaVersion = 1
	// ChatLogDirectoryName 是应用数据目录中的权威日志子目录。
	ChatLogDirectoryName = "chat-logs"
	// ChatWorkspaceDirectoryName 是应用数据目录中的受管 Chat 工作区子目录。
	ChatWorkspaceDirectoryName = "workspaces"
	// BlobDirectoryName 是应用数据目录中的内容寻址大载荷子目录。
	BlobDirectoryName = "blobs"
	// ChatContextOwnerFileName 是受管目录的归属标记文件。
	ChatContextOwnerFileName = "workspace-owner.json"
	// DefaultChatTitleMaxRunes 是本地临时标题保留的最大字符数。
	DefaultChatTitleMaxRunes = 28
	// ChatLogFailureCode 是执行无法继续提交权威对话事实时保存的稳定错误码。
	ChatLogFailureCode = "CHAT_LOG_ERROR"
	// LocalCORSAllowedHeaders 是桌面 Renderer 跨源访问 Core 时允许携带的请求头。
	LocalCORSAllowedHeaders = "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With, Last-Event-ID"
	// LocalCORSAllowedMethods 是桌面 Renderer 跨源访问 Core 时允许使用的 HTTP 方法。
	LocalCORSAllowedMethods = "POST, OPTIONS, GET, PUT, DELETE"
	// MessageToolSafeSummaryMaxRunes 是工具安全摘要单个文本字段的最大字符数。
	MessageToolSafeSummaryMaxRunes = 120
	// MessageToolSafeSummaryMaxInt 是工具安全摘要单个变更量字段的最大绝对值。
	MessageToolSafeSummaryMaxInt = 1000000
	// MessageToolSafeSummaryMaxFiles 是单次工具调用允许公开展示的文件变更数上限。
	MessageToolSafeSummaryMaxFiles = 100
	// MessageFileChangeDiffMaxBytes 是单份执行 Diff 的新旧文本各自允许返回的最大字节数。
	MessageFileChangeDiffMaxBytes = 10 * 1024 * 1024
)

const (
	// ProfileDocumentIdentityFileName 是身份与擅长领域档案在 Runtime 上下文中的文件名。
	ProfileDocumentIdentityFileName = "IDENTITY.md"
	// ProfileDocumentPersonaFileName 是工作风格档案在 Runtime 上下文中的文件名。
	ProfileDocumentPersonaFileName = "PERSONA.md"
	// ProfileDocumentPlaybookFileName 是工作方式档案在 Runtime 上下文中的文件名。
	ProfileDocumentPlaybookFileName = "PLAYBOOK.md"
	// ProfileDocumentMemoryFileName 是伙伴记忆档案在 Runtime 上下文中的文件名。
	ProfileDocumentMemoryFileName = "MEMORY.md"
	// ProfileDocumentDefaultFileName 是未识别档案种类在 Runtime 上下文中的兜底文件名。
	ProfileDocumentDefaultFileName = "PROFILE.md"
)

const (
	// AIAccountOpenAIURL 是 OpenAI 官方 API 根地址。
	AIAccountOpenAIURL = "https://api.openai.com/v1"
	// AIAccountAnthropicURL 是 Anthropic 官方 API 根地址。
	AIAccountAnthropicURL = "https://api.anthropic.com"
	// AIAccountOpenCodeGoAnthropicURL 是 OpenCode Go 使用 API Key 请求头的 Anthropic 入口。
	AIAccountOpenCodeGoAnthropicURL = "https://opencode.ai/zen/go"
	// AIAccountKimiURL 是 Kimi Code 套餐的官方 API 根地址。
	AIAccountKimiURL = "https://api.kimi.com/coding/v1"
	// AIAccountKimiCompletionTokenCap 限制第三方账号单次生成量，避免 CLI 将完整上下文窗口作为输出请求上限。
	AIAccountKimiCompletionTokenCap = 32768
	// AIAccountIdentityEnvironment 是账号及版本对应的非秘密进程指纹。
	AIAccountIdentityEnvironment = "ALLY_ACCOUNT_IDENTITY"
)

const (
	// CodexOAuthOrigin 是 设备码认证服务源。
	CodexOAuthOrigin = "https://auth.openai.com"
	// CodexOAuthClientID 是官方 Codex CLI 公开 OAuth 客户端标识。
	CodexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// CodexOAuthRedirectURI 是设备授权兑换码的官方回调标识。
	CodexOAuthRedirectURI = "https://auth.openai.com/deviceauth/callback"
	// CodexOAuthVerificationURI 是向用户展示的官方设备授权页。
	CodexOAuthVerificationURI = "https://auth.openai.com/codex/device"
)

// AIAccountDiagnosticUnavailable 表示已关联账号暂不能准备原生配置。
const AIAccountDiagnosticUnavailable = "AI_ACCOUNT_UNAVAILABLE"

// 工作流路径名称供宿主源码投影和画布协议使用，不决定调度。
const (
	// WorkflowGroupDefaultFailureMode 表示分组未声明策略时在首个失败后停止其他任务。
	WorkflowGroupDefaultFailureMode = "fail-fast"
	// WorkflowControlPathTrue 表示条件成立的互斥路径。
	WorkflowControlPathTrue = "true"
	// WorkflowControlPathFalse 表示条件不成立的互斥路径。
	WorkflowControlPathFalse = "false"
	// WorkflowControlPathTest 表示每轮循环重新求值的条件。
	WorkflowControlPathTest = "test"
	// WorkflowControlPathUpdate 表示 for 循环在正常结束或 continue 后执行的更新表达式。
	WorkflowControlPathUpdate = "update"
	// WorkflowControlPathBody 表示循环或异常保护的正常执行块。
	WorkflowControlPathBody = "body"
	// WorkflowControlPathCatch 表示抛出异常后执行的处理块。
	WorkflowControlPathCatch = "catch"
	// WorkflowControlPathFinally 表示控制退出前始终执行的清理块。
	WorkflowControlPathFinally = "finally"
	// WorkflowControlPathItems 表示数量由运行输入决定的逐项任务模板。
	WorkflowControlPathItems = "items"
)

// 企业微信智能机器人使用官方长连接通道，不开放任意服务器地址。
const (
	// IMWecomQRExpirySeconds 是扫码等待上限，与官方 CLI 的五分钟窗口一致。
	IMWecomQRExpirySeconds = 300
	// IMWecomQRPollSeconds 是官方扫码状态查询间隔，取消或确认后退出。
	IMWecomQRPollSeconds = 3
	// IMWecomQRBaseURL 是官方 CLI 使用的扫码注册入口。
	IMWecomQRBaseURL = "https://work.weixin.qq.com/ai/qc"
	// IMWecomWebSocketURL 是企业微信公有云长连接入口。
	IMWecomWebSocketURL = "wss://openws.work.weixin.qq.com"
	// IMWecomSubscribe 是机器人凭据订阅命令。
	IMWecomSubscribe = "aibot_subscribe"
	// IMWecomHeartbeat 是应用层心跳命令。
	IMWecomHeartbeat = "ping"
	// IMWecomMessageCallback 是消息推送命令。
	IMWecomMessageCallback = "aibot_msg_callback"
	// IMWecomEventCallback 是事件推送命令。
	IMWecomEventCallback = "aibot_event_callback"
	// IMWecomSendMessage 是向已发现会话主动发送的命令。
	IMWecomSendMessage = "aibot_send_msg"
	// IMWecomUploadInit 初始化临时素材上传。
	IMWecomUploadInit = "aibot_upload_media_init"
	// IMWecomUploadChunk 上传素材分片。
	IMWecomUploadChunk = "aibot_upload_media_chunk"
	// IMWecomUploadFinish 完成临时素材上传。
	IMWecomUploadFinish = "aibot_upload_media_finish"
	// IMWecomDisconnected 表示同一机器人被其他连接替换。
	IMWecomDisconnected = "disconnected_event"
)

// IMFileMaximumBytes 是单个 IM 文件允许上传或下载的最大字节数。
const IMFileMaximumBytes int64 = 20 * 1024 * 1024

// IMPageSize 是管理列表每页默认条目数。
const IMPageSize = 50

// IMWeixinBaseURL 是腾讯 iLink 的官方机器人 API。
const IMWeixinBaseURL = "https://ilinkai.weixin.qq.com"

// IMWeixinCDNBaseURL 是腾讯 iLink 的官方媒体服务。
const IMWeixinCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"

// IMBuiltinConnectorID 是 IM 投递内置能力的固定身份。
const IMBuiltinConnectorID = "b17d96aa55314c33ab46c172e8cab012"

// IMBuiltinRuntimeKey 是模型可见且跨执行稳定的服务名。
const IMBuiltinRuntimeKey = "ally_im"

// IMBuiltinToolName 是统一发现与发送投递的内部工具。
const IMBuiltinToolName = "im_delivery"

const (
	// IMActionListTargets 查询当前获准聊天目录。
	IMActionListTargets = "targets.list"
	// IMActionCreateDelivery 创建独立的耐久交付。
	IMActionCreateDelivery = "deliveries.create"
	// IMActionGetDelivery 查询已登记交付的真实状态。
	IMActionGetDelivery = "deliveries.get"
	// IMRecoverySelectTarget 要求重新选择真实获准目标。
	IMRecoverySelectTarget = "select_im_target"
	// IMRecoveryAuthorizeChat 要求恢复聊天授权。
	IMRecoveryAuthorizeChat = "authorize_im_chat"
	// IMRecoveryEnableWorkflowDelivery 要求允许工作流向聊天发送。
	IMRecoveryEnableWorkflowDelivery = "enable_workflow_delivery"
	// IMRecoveryEnableConnection 要求启用平台连接。
	IMRecoveryEnableConnection = "enable_im_connection"
	// IMRecoverySignIn 要求重新完成平台登录。
	IMRecoverySignIn = "sign_in_im"
	// IMRecoveryDeclareTarget 要求在工作流动作中声明发送范围。
	IMRecoveryDeclareTarget = "declare_im_target"
)

// IMBuiltinPlaceholderURL 只属于管理目录，不可作为实际发送端点。
const IMBuiltinPlaceholderURL = "http://127.0.0.1/ally-im-placeholder"
const (
	// BuiltinWorkflowMCPConnectorID 是编排、结果和调用共同使用的内置目录身份。
	BuiltinWorkflowMCPConnectorID = "99b8c72f56224adebc48c6a7b47e98ef"
	// BuiltinWorkflowMCPConnectorName 是连接器管理页中的工作流名称。
	BuiltinWorkflowMCPConnectorName = "工作流"
	// BuiltinWorkflowMCPConnectorFallbackName 在公开连接器占用默认名称时保持启动可用。
	BuiltinWorkflowMCPConnectorFallbackName = "工作流 · 系统"
	// BuiltinWorkflowMCPConnectorDescription 说明工作流工具的按场景挂载和查询方式。
	BuiltinWorkflowMCPConnectorDescription = "内置的工作流能力：按当前场景提供编排、步骤结果提交或获准工作流调用；说明与参数按需查询，由系统管理启用。"
	// BuiltinWorkflowMCPConnectorPlaceholderURL 仅用于目录展示，不能直接挂载到智能体。
	BuiltinWorkflowMCPConnectorPlaceholderURL = "http://127.0.0.1/ally-workflow"
	// BuiltinWorkflowMCPConnectorTimeoutMS 是工作流 MCP 的连接诊断超时。
	BuiltinWorkflowMCPConnectorTimeoutMS = int64(30_000)
	// WorkflowMCPToolName 是所有工作流场景共用的模型可见工具名。
	WorkflowMCPToolName = "workflow"
	// WorkflowResultPageDecisionInstructions 要求创作时逐个评估伙伴输出，不能省略展示决策。
	WorkflowResultPageDecisionInstructions = "Every worker, including workers inside loops, branches and parallel groups, requires exactly one output declaration: meta.nodeResultPages[id] for composed in-app reading, meta.nodeResultFiles[id] with a readable title for actual file delivery, or meta.nodeResultPageOmissions[id] with a concrete reason for internal technical/control data without reading value or output the user explicitly declined to display. Intermediate output is not automatically exempt. File delivery uses static type:string, format:file schemas, or arrays/objects of these references, and the existing file viewer. Keep the report's prose, evidence and coverage in the actual file. Select the presentation feature only for composed pages; preparation includes its Catalog. Reuse that content and query the presentation topic only if it is missing. Revisit the declaration when the user changes the deliverable."
	// WorkflowActionQuery 按本次能力返回知识或动作契约。
	WorkflowActionQuery = "query"
	// WorkflowActionSourceGet 读取绑定工作流源码。
	WorkflowActionSourceGet = "source.get"
	// WorkflowActionSourceValidate 校验完整工作流源码。
	WorkflowActionSourceValidate = "source.validate"
	// WorkflowActionSourceUpsert 原子保存完整源码和新版本。
	WorkflowActionSourceUpsert = "source.upsert"
	// WorkflowActionAnnotationsList 读取当前工作流的修改要求。
	WorkflowActionAnnotationsList = "annotations.list"
	// WorkflowActionRunsGet 读取当前工作流的运行记录。
	WorkflowActionRunsGet = "runs.get"
	// WorkflowActionResultSubmit 提交当前节点的结构化候选结果。
	WorkflowActionResultSubmit = "result.submit"
	// WorkflowActionRunStart 接纳本次获准的工作流运行。
	WorkflowActionRunStart = "run.start"
	// WorkflowActionRunGet 查询本能力接纳的运行。
	WorkflowActionRunGet = "run.get"
	// WorkflowActionRunCancel 停止本能力接纳的运行。
	WorkflowActionRunCancel = "run.cancel"
	// WorkflowQueryCapabilities 查询当前能力允许的动作及参数。
	WorkflowQueryCapabilities = "capabilities"
	// WorkflowQueryAuthoring 按实际节点组合创作资料与当前版本。
	WorkflowQueryAuthoring = "authoring"
	// WorkflowQuerySDK 按主题查询原生 DSL 声明。
	WorkflowQuerySDK = "sdk"
	// WorkflowQueryExamples 按主题查询源码示例。
	WorkflowQueryExamples = "examples"
	// WorkflowQueryResources 分页查询创作可引用的真实资源。
	WorkflowQueryResources = "resources"
	// WorkflowQueryWorkflows 查询本轮获准调用的工作流。
	WorkflowQueryWorkflows = "workflows"
	// WorkflowQueryResultSchema 查询当前节点要求的结果格式。
	WorkflowQueryResultSchema = "result_schema"
)

const (
	// PluginManifestSchema 是本地支持的通用插件清单版本。
	PluginManifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	// PluginMCPSchema 是本地支持的插件 MCP 配置版本。
	PluginMCPSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
	// PluginFormatPortable 表示通用插件包。
	PluginFormatPortable = "agent-plugins"
	// PluginFormatClaude 表示仅导入可支持组件的 Claude 插件包。
	PluginFormatClaude = "claude"
	// PluginFormatCodex 表示仅导入可支持组件的 Codex 兼容插件包。
	PluginFormatCodex = "codex"
	// PluginExtensionNamespace 是客户端扩展空间。
	PluginExtensionNamespace = "com.ally"
)

const (
	// PluginProviderFeishu 表示固定飞书官方 CLI 适配。
	PluginProviderFeishu = "feishu"
	// PluginEventTopic 是插件资源唯一状态事件主题。
	PluginEventTopic = "plugins"
	// PluginProviderGitHub 表示固定 GitHub 官方 CLI 与远程 MCP 适配。
	PluginProviderGitHub = "github"
	// GitHubCLIVersion 是固定 GitHub 官方程序版本。
	GitHubCLIVersion = "2.102.0"
	// GitHubCLIBuildVersion 标识包含应用钥匙串隔离补丁的固定构建。
	GitHubCLIBuildVersion = "2.102.0-neuxally.1"
	// GitHubMCPURL 是允许共享身份交付的精确官方服务地址。
	GitHubMCPURL = "https://api.githubcopilot.com/mcp/"
	// GitHubMCPToolsets 限定首版官方工具范围。
	GitHubMCPToolsets = "context,repos,issues,pull_requests,users"
	// FeishuCLIVersion 是首批固定飞书程序版本。
	FeishuCLIVersion = "1.0.98"
)

// PluginStdioMode 是 Core 内部的原生 MCP 工作目录子进程模式。
const PluginStdioMode = "plugin-stdio"

// KnowledgeSourceStateQueued 表示来源已进入本次任务，尚未被真实阅读阶段领取。
const KnowledgeSourceStateQueued = "queued"

// KnowledgeSourceStateProcessing 表示来源已被真实阅读阶段领取，任务尚未结算。
const KnowledgeSourceStateProcessing = "processing"

// KnowledgeChatOrganizeOperation 标识普通聊天整理请求的持久输入锚点。
const KnowledgeChatOrganizeOperation = "chat.organize"
const (
	// PluginLocaleEnglish 是插件英文展示与默认文档语言。
	PluginLocaleEnglish = "en"
	// PluginLocaleChinese 是插件简体中文展示与文档语言。
	PluginLocaleChinese = "zh-CN"
	// PluginCatalogHTTPSource 是默认公开软件目录。
	PluginCatalogHTTPSource = "https://github.com/JieWaZi/NeuxAlly-Plugins/releases/latest/download/catalog.json"
	// PluginAuthCommandStrategy 是包内授权桥接策略。
	PluginAuthCommandStrategy = "plugin-command"
	// PluginAuthProtocol 是逐行JSON-RPC授权扩展版本。
	PluginAuthProtocol = "neuxally-auth/1"
	// PluginAuthPreparationTimeoutSeconds 为首次挑战前的受管资产下载与启动保留有界准备时间。
	PluginAuthPreparationTimeoutSeconds = 150
	// PluginAuthQRPNGPrefix 标识桥接提供的内联 PNG 二维码原图。
	PluginAuthQRPNGPrefix = "data:image/png;base64,"
	// PluginAuthQRPNGMaxBytes 限制二维码原图解码后的字节数。
	PluginAuthQRPNGMaxBytes = 256 * 1024
	// PluginAuthQRPNGMaxDimension 限制二维码原图的单边像素数。
	PluginAuthQRPNGMaxDimension = 1024
	// PluginAuthEnvironmentPrefix 隔离原生插件桥接秘密的进程环境键。
	PluginAuthEnvironmentPrefix = "ALLY_PLUGIN_AUTH_"
	// AIAccountCodexAPIKeyEnvironment 仅在执行进程中传递所选 Codex API 账号凭据。
	AIAccountCodexAPIKeyEnvironment = "ALLY_CODEX_MODEL_API_KEY"
	// PluginNativeMarketplace 是应用私有Codex原生索引身份。
	PluginNativeMarketplace = "neuxally-managed"
	// PluginNativeScopeDirectory 是工作区内原生插件能力摘要的独占目录。
	PluginNativeScopeDirectory = "native-plugins"
	// NativeWorktreesDirectory 是原生 CLI 配置根内存放独立 Git 工作区的目录。
	NativeWorktreesDirectory = "worktrees"
	// PluginDeliveryNative 是完整原生包交付。
	PluginDeliveryNative = "native"
	// PluginDeliveryProjection 是Skill/MCP投射交付。
	PluginDeliveryProjection = "skill-mcp"
	// PluginLoadStagePrepared 表示固定产物已准备，尚不代表模型调用成功。
	PluginLoadStagePrepared = "prepared"
	// PluginAuthStartMethod 发起桥接授权。
	PluginAuthStartMethod = "auth/start"
	// PluginAuthStatusMethod 核验实际身份。
	PluginAuthStatusMethod = "auth/status"
	// PluginAuthCancelMethod 停止上游等待。
	PluginAuthCancelMethod = "auth/cancel"
	// PluginAuthResolveMethod 解析获准组件秘密。
	PluginAuthResolveMethod = "auth/resolve"
	// PluginAuthLogoutMethod 清理插件私有身份。
	PluginAuthLogoutMethod = "auth/logout"
	// PluginAuthChallengeNotification 交付当前临时挑战。
	PluginAuthChallengeNotification = "auth/challenge"
	// PluginAuthStateNotification 交付协议实际阶段。
	PluginAuthStateNotification = "auth/state"
)

// KnowledgeMCPMaxRequestBytes 限制 MCP 请求和显式原件字节的传输体积。
const KnowledgeMCPMaxRequestBytes = 16 << 20

// KnowledgeRetryAuthorityOperation 标记依核心重试回执生效的宿主授权候选。
const KnowledgeRetryAuthorityOperation = "job.retry.authority"

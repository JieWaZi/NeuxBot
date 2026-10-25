// Package status_error 定义对外稳定的 NeuxBot 业务错误。
package status_error

import "net/http"

const (
	// @errZH 软件能力执行身份无效或已过期
	// @errEN capability execution context is invalid or expired
	AgentContextUnauthorized StatusError = http.StatusUnauthorized*1e8 + 9001
)

const (
	// @errZH 当前执行未获准使用此软件能力
	// @errEN operation is not authorized for the current execution
	AgentOperationForbidden StatusError = http.StatusForbidden*1e8 + 9001
)

const (
	// @errZH 软件能力命令参数不符合契约
	// @errEN command input does not satisfy the operation contract
	AgentInputInvalid StatusError = http.StatusBadRequest*1e8 + 9001
)

const (
	// @errZH 软件能力连接不可用或协议不匹配
	// @errEN capability connection is unavailable or its protocol does not match
	AgentConnectionUnavailable StatusError = http.StatusServiceUnavailable*1e8 + 9001
)

const (
	// @errZH 软件能力命令提交结果尚不确定，请查询原请求回执
	// @errEN command outcome is uncertain; inspect the original request receipt
	AgentCommandOutcomeUnknown StatusError = http.StatusConflict*1e8 + 9001
)

const (
	// @errZH 软件能力连接文件无法安全准备或读取
	// @errEN capability context file could not be safely prepared or read
	AgentContextStorageError StatusError = http.StatusInternalServerError*1e8 + 9001
	// @errZH 同一命令请求标识不能用于不同内容
	// @errEN command request identifier was already used with different content
	AgentCommandRequestConflict StatusError = http.StatusConflict*1e8 + 9002
	// @errZH 当前范围内尚无该命令的接纳回执
	// @errEN command acceptance receipt was not found in the current scope
	AgentReceiptNotFound StatusError = http.StatusNotFound*1e8 + 9001
)

const (
	// @errZH 正在处理其他数据，请稍后重试
	// @errEN processing other data; please try again later
	DatabaseBusy StatusError = http.StatusServiceUnavailable*1e8 + 1001
)

const (
	// @errZH 数据库操作已取消
	// @errEN database operation was cancelled
	DatabaseOperationCancelled StatusError = http.StatusRequestTimeout*1e8 + 1001
)

const (
	// @errZH 工作流源码或契约不合法，请查看编译诊断
	// @errEN invalid workflow source or contract; inspect compiler diagnostics
	WorkflowSourceInvalid StatusError = http.StatusBadRequest*1e8 + iota + 3001
	// @errZH 工作流参数或结果不符合声明的契约
	// @errEN workflow input or output does not satisfy its schema
	WorkflowSchemaInvalid
	// @errZH 当前执行后端不支持工作流要求的能力
	// @errEN selected execution backend does not support the required workflow capability
	WorkflowCapabilityUnsupported
	// @errZH 工作流资源或工作范围不可用
	// @errEN workflow resource or workspace is unavailable
	WorkflowResourceInvalid
	// @errZH 工作流运行预算已经耗尽
	// @errEN workflow execution budget is exhausted
	WorkflowBudgetExceeded
	// @errZH 工作流文件引用、文件名或所属范围不合法
	// @errEN invalid workflow file reference, name, or ownership
	WorkflowFileInvalid
	// @errZH 发送目标未声明为工作流依赖，请通过优化工作流补充发送目标范围
	// @errEN delivery target is not declared as a workflow dependency; optimize the workflow to declare its target scope
	WorkflowIMTargetNotDeclared
)

const (
	// @errZH 工作流、版本或运行不存在
	// @errEN workflow definition, revision, or run was not found
	WorkflowNotFound StatusError = http.StatusNotFound*1e8 + iota + 3001
	// @errZH 工作流文件不存在或不在允许范围内
	// @errEN workflow file was not found within the allowed scope
	WorkflowFileNotFound
)

const (
	// @errZH 工作流版本已变化，请重新读取后保存
	// @errEN workflow revision changed; reload before saving
	WorkflowRevisionConflict StatusError = http.StatusConflict*1e8 + iota + 3001
	// @errZH 相同请求标识已经用于不同内容
	// @errEN request identity was already used for different content
	WorkflowRequestConflict
	// @errZH 此工作流请求已经解决或当前状态不允许操作
	// @errEN workflow request was resolved or its current state disallows the operation
	WorkflowStateConflict
	// @errZH 工作流运行所有者已经变化
	// @errEN workflow runner ownership changed
	WorkflowFenceConflict
	// @errZH 工作流输入已变化，请重新配置自动任务
	// @errEN workflow input changed; reconfigure the automatic task
	AutomaticTaskInputChanged
	// @errZH 工作流创作权限已由新执行接替，请停止旧执行的操作并在当前创作对话继续
	// @errEN workflow authoring was superseded; stop old calls and continue in the current authoring chat
	WorkflowAuthoringSuperseded
	// @errZH 工作流创作执行已结束，请在创作对话发送新消息后继续
	// @errEN workflow authoring execution ended; send a new message in the authoring chat to continue
	WorkflowAuthoringFinished
)

const (
	// @errZH 工作流效果执行失败
	// @errEN workflow effect failed
	WorkflowEffectFailed StatusError = http.StatusInternalServerError*1e8 + iota + 3001
	// @errZH 工作流执行已取消
	// @errEN workflow execution was cancelled
	WorkflowCancelled
	// @errZH 工作流效果超时
	// @errEN workflow effect timed out
	WorkflowTimeout
	// @errZH 工作流内部执行状态损坏
	// @errEN workflow execution state is corrupt
	WorkflowStateInvalid
	// @errZH 工作流文件存储失败
	// @errEN workflow file storage failed
	WorkflowFileStorageError
)

const (
	// @errZH 内置角色预设定义不完整或存在重复内容
	// @errEN built-in role preset definition is incomplete or contains duplicate entries
	BuiltinRolePresetDefinitionInvalid StatusError = http.StatusInternalServerError*1e8 + iota + 2001
	// @errZH 内置团队协作工具描述不完整或不一致
	// @errEN built-in collaboration tool descriptor is incomplete or inconsistent
	ChannelToolDescriptorInvalid
)

const (
	// @errZH 所选 Runtime 尚未注册
	// @errEN selected runtime is not registered
	RuntimeNotRegistered StatusError = http.StatusBadRequest*1e8 + iota + 2001
)

const (
	// @errZH 定时规则或时区不合法，或没有未来的运行时间
	// @errEN invalid schedule or timezone, or no future occurrence
	AutomaticScheduleInvalid StatusError = http.StatusBadRequest*1e8 + iota + 1001
	// @errZH 请选择当前伙伴的可用项目或一个本地目录
	// @errEN select an available project owned by the waker or a local directory
	AutomaticTaskWorkspaceInvalid
	// @errZH 工作流任务参数来源或调用参数不合法
	// @errEN invalid workflow task input binding or invocation argument
	AutomaticTaskInputInvalid
)

const (
	// @errZH 自动任务密钥无效或已经失效
	// @errEN automatic task token is invalid or revoked
	AutomaticTaskTokenInvalid StatusError = http.StatusUnauthorized*1e8 + iota + 1001
)

const (
	// @errZH 自动任务触发频率超过每分钟上限，请稍后重试
	// @errEN automatic task request rate exceeds the per-minute limit
	AutomaticTaskRateLimited StatusError = http.StatusTooManyRequests*1e8 + iota + 1001
)

const (
	// @errZH 自动任务服务配置不合法
	// @errEN automatic task service configuration is invalid
	AutomaticTaskServiceInvalid StatusError = http.StatusInternalServerError*1e8 + iota + 1001
	// @errZH 无法生成自动任务调用密钥
	// @errEN failed to generate an automatic task token
	AutomaticTaskTokenGenerationFailed
	// @errZH 无法监听本机任务入口，请检查端口占用
	// @errEN failed to listen on the local task endpoint; check port availability
	AutomaticTaskAPIUnavailable
)

//go:generate toolx gen error -p error_codes -c StatusError

// StatusError 表示可以稳定映射为 HTTP 状态和前端本地化 Key 的错误。
type StatusError int

const (
	// @errZH 请求参数不合法
	// @errEN invalid request
	BadRequest StatusError = http.StatusBadRequest*1e8 + iota + 1
	// @errZH 档案文档种类不合法
	// @errEN invalid profile document kind
	ProfileDocumentKindInvalid
	// @errZH 单份档案文档不能超过 1 MiB
	// @errEN profile document exceeds 1 MiB
	ProfileDocumentTooLarge
	// @errZH 档案文档必须使用 UTF-8 编码
	// @errEN profile document must be UTF-8
	ProfileDocumentEncodingInvalid
	// @errZH 删除确认名称与当前伙伴名称不一致
	// @errEN waker delete confirmation does not match
	WakerDeleteConfirmationMismatch
	// @errZH 无法导入技能，请检查 Git 地址、ref、子目录或 ZIP 内容
	// @errEN unable to import skill source
	SkillImportFailed
	// @errZH 尚未配置 Embedding，无法重建语义索引
	// @errEN embedding is not configured, cannot rebuild semantic index
	MemoryEmbeddingNotConfigured
	// @errZH 当前对话未绑定项目，project 范围不可用
	// @errEN current chat is not bound to a project
	MemoryProjectNotBound
	// @errZH Embedding 模型或维度与已建语义索引不一致，请在“记忆”页面重建索引后再使用语义检索
	// @errEN embedding model or dimensions conflict with the semantic index
	MemoryEmbeddingConflict
	// @errZH 对话标识不合法
	// @errEN invalid chat identifier
	ChatIDInvalid
	// @errZH 对话事件游标不合法
	// @errEN invalid chat event cursor
	ChatEventCursorInvalid
	// @errZH 对话消息游标不合法
	// @errEN invalid chat message cursor
	ChatMessageCursorInvalid
	// @errZH 技能内容不合法
	// @errEN invalid skill content
	SkillContentInvalid
	// @errZH 技能内容超过允许的文件、大小或路径限制
	// @errEN skill content exceeds the allowed file, size, or path limits
	SkillContentLimitExceeded
	// @errZH 技能导入来源不合法
	// @errEN invalid skill import source
	SkillImportSourceInvalid
	// @errZH 技能导入内容与扫描结果不一致
	// @errEN skill import content does not match the scanned result
	SkillImportIntegrityInvalid
	// @errZH 消息附件不合法或不可读取
	// @errEN message attachment is invalid or unreadable
	MessageAttachmentInvalid
	// @errZH 团队必须包含 2 至 5 位互不重复的成员
	// @errEN channel must contain 2 to 5 distinct members
	ChannelMemberCountInvalid
	// @errZH 团队负责人必须且只能是所选成员之一
	// @errEN channel lead must be exactly one selected member
	ChannelLeadInvalid

	// @errZH 账号配置不合法或与智能体不兼容
	// @errEN invalid account configuration or incompatible agent
	AIAccountConfigurationInvalid
)

const (
	// @errZH 操作已取消
	// @errEN operation canceled
	OperationCanceled StatusError = http.StatusRequestTimeout*1e8 + iota + 1
	// @errZH 操作等待超时
	// @errEN operation timed out
	OperationTimeout
)

const (
	// @errZH 本地访问令牌无效
	// @errEN invalid local access token
	Unauthorized StatusError = http.StatusUnauthorized*1e8 + iota + 1
	// @errZH 记忆调用上下文无效或已过期
	// @errEN memory invocation context is invalid or expired
	MemoryContextUnauthorized
)

const (
	// @errZH 伙伴不存在：{{.WakerID}}
	// @errEN waker not found: {{.WakerID}}
	WakerNotFound StatusError = http.StatusNotFound*1e8 + iota + 1
	// @errZH 角色预设不存在：{{.RolePresetID}}
	// @errEN role preset not found: {{.RolePresetID}}
	RolePresetNotFound
	// @errZH 档案文档不存在
	// @errEN profile document not found
	ProfileDocumentNotFound
	// @errZH 执行不存在：{{.ExecutionID}}
	// @errEN execution not found: {{.ExecutionID}}
	ExecutionNotFound
	// @errZH 对话不存在：{{.ChatID}}
	// @errEN chat not found: {{.ChatID}}
	ChatNotFound
	// @errZH 项目不存在：{{.ProjectID}}
	// @errEN project not found: {{.ProjectID}}
	ProjectNotFound
	// @errZH 技能不存在：{{.SkillID}}
	// @errEN skill not found: {{.SkillID}}
	SkillNotFound
	// @errZH 技能版本不存在：{{.VersionID}}
	// @errEN skill version not found: {{.VersionID}}
	SkillVersionNotFound
	// @errZH MCP 连接器不存在：{{.ConnectorID}}
	// @errEN mcp connector not found: {{.ConnectorID}}
	MCPConnectorNotFound
	// @errZH 记忆不存在或不属于当前伙伴范围
	// @errEN memory not found or outside the current waker scope
	MemoryNotFound
	// @errZH 消息中的文件改动不存在：{{.Path}}
	// @errEN message file change not found: {{.Path}}
	ChatMessageFileChangeNotFound
	// @errZH 团队不存在：{{.ChannelID}}
	// @errEN channel not found: {{.ChannelID}}
	ChannelNotFound
	// @errZH 团队对话不存在：{{.ThreadID}}
	// @errEN channel thread not found: {{.ThreadID}}
	ChannelThreadNotFound
	// @errZH 伙伴不属于当前团队对话：{{.WakerID}}
	// @errEN waker is not a member of the channel thread: {{.WakerID}}
	ChannelMemberNotFound
	// @errZH 团队任务不存在：{{.TaskID}}
	// @errEN channel task not found: {{.TaskID}}
	ChannelTaskNotFound
	// @errZH 团队任务当前尝试不存在：{{.TaskID}}
	// @errEN current channel task attempt not found: {{.TaskID}}
	ChannelTaskAttemptNotFound

	// @errZH AI 账号不存在
	// @errEN AI account not found
	AIAccountNotFound
	// @errZH 自动任务不存在或不属于当前伙伴
	// @errEN automatic task not found or outside the current waker scope
	AutomaticTaskNotFound
	// @errZH 自动任务运行记录不存在
	// @errEN automatic task run not found
	AutomaticRunNotFound
)

const (
	// @errZH 资源状态冲突
	// @errEN resource state conflict
	Conflict StatusError = http.StatusConflict*1e8 + iota + 1
	// @errZH 伙伴名称已存在
	// @errEN waker name already exists
	WakerNameConflict

	// @errZH 角色预设名称已存在
	// @errEN role preset name already exists
	RolePresetNameConflict
	// @errZH 系统角色预设为只读
	// @errEN system role preset is read-only
	SystemRolePresetReadOnly
	// @errZH 伙伴仍有运行中的工作
	// @errEN waker still has running work
	WakerHasRunningWork
	// @errZH 当前对话已有运行中的执行
	// @errEN chat already has an active execution
	ChatHasActiveExecution
	// @errZH 当前伙伴下已存在同名项目
	// @errEN project name already exists for this waker
	ProjectNameConflict
	// @errZH 当前范围内已存在同名技能
	// @errEN skill name already exists in this scope
	SkillNameConflict
	// @errZH 内置技能不能卸载
	// @errEN built-in skill cannot be uninstalled
	BuiltInSkillCannotUninstall
	// @errZH MCP 连接器名称已存在
	// @errEN mcp connector name already exists
	MCPConnectorNameConflict
	// @errZH 内置 MCP 连接器不能删除
	// @errEN built-in mcp connector cannot be deleted
	BuiltInMCPConnectorCannotDelete
	// @errZH 内置 MCP 连接器定义为只读
	// @errEN built-in mcp connector definition is read-only
	BuiltInMCPConnectorReadOnly
	// @errZH MCP 连接器需要重新确认信任
	// @errEN mcp connector trust confirmation is required
	MCPConnectorTrustRequired
	// @errZH 该审批已处理
	// @errEN approval has already been resolved
	ApprovalAlreadyResolved
	// @errZH 该结构化输入请求已处理
	// @errEN structured user input has already been resolved
	ElicitationAlreadyResolved
	// @errZH 团队名称已存在
	// @errEN channel name already exists
	ChannelNameConflict
	// @errZH 团队仍有运行中或待执行的工作，请先停止当前工作再删除
	// @errEN channel still has active or pending work; stop current work before deleting
	ChannelHasActiveWork
	// @errZH 团队任务当前状态不允许此操作
	// @errEN channel task state does not allow this operation
	ChannelTaskStateConflict
	// @errZH 团队对话已达到任务数量上限
	// @errEN channel thread reached its task limit
	ChannelTaskLimitReached
	// @errZH 团队任务失败重试次数已用尽
	// @errEN channel task failure retry limit has been reached
	ChannelTaskRetryLimitReached
	// @errZH 旧的团队任务尝试不能提交结果
	// @errEN stale channel task attempt cannot submit a result
	ChannelTaskAttemptStale
	// @errZH 同一首次发送标识对应的资源仍在创建，请稍后重试
	// @errEN the resource for this create request is still being created; retry later
	CreateRequestInProgress
	// @errZH 同一首次发送标识不能用于不同内容，请修改内容后重试
	// @errEN a create request identifier cannot be reused with different content
	CreateRequestConflict
	// @errZH 默认伙伴不能删除
	// @errEN default waker cannot be deleted
	DefaultWakerCannotDelete
	// @errZH 此回合没有可靠的原生分叉位置
	// @errEN this turn has no reliable native fork position
	ChatForkUnavailable
	// @errZH 原生分叉结果尚未确认，不能重复创建，请先核对运行时会话
	// @errEN native fork outcome is unconfirmed; reconcile before retrying
	ChatForkIndeterminate
	// @errZH 账号正在使用，请稍后再修改
	// @errEN account is in use; try again later
	AIAccountBusy
	// @errZH 账号配置已更改，请刷新后重试
	// @errEN account has changed; refresh and try again
	AIAccountRevisionConflict
	// @errZH 账号仍被智能体或聊天引用，请先切换账号
	// @errEN account is referenced by an agent or chat; switch accounts first
	AIAccountInUse
	// @errZH 账号名称已存在，请使用其他名称
	// @errEN account name already exists
	AIAccountNameConflict
	// @errZH 当前回复不能直接重试，请按错误提示处理后重新发送
	// @errEN this response cannot be retried directly
	ExecutionRetryUnavailable
	// @errZH 重试请求正在处理，请稍后检查状态
	// @errEN the retry request is being processed; check its status shortly
	ExecutionRetryInProgress
	// @errZH 上一次自动运行尚未结束，请先处理对应对话
	// @errEN the previous automatic run is still active
	AutomaticTaskBusy
	// @errZH 自动任务已停用，不能接纳新的运行
	// @errEN the automatic task is disabled
	AutomaticTaskDisabled
	// @errZH 请求标识已被不同的附加输入使用
	// @errEN the request identifier was used with different input
	AutomaticTaskRequestConflict
	// @errZH 自动任务配置已变化，请刷新后重试
	// @errEN automatic task configuration has changed
	AutomaticTaskRevisionConflict
	// @errZH 接纳中断且配置已撤销，请使用新的请求标识
	// @errEN admission was interrupted and its configuration was revoked; use a new request identifier
	AutomaticTaskAdmissionRevoked
)

const (
	// @errZH 本地服务内部错误
	// @errEN core internal error
	InternalServerError StatusError = http.StatusInternalServerError*1e8 + iota + 1
	// @errZH 数据库操作失败
	// @errEN database operation failed
	DatabaseError
	// @errZH 对话执行服务尚未启动
	// @errEN chat execution service is not started
	ChatServiceNotStarted
	// @errZH 对话权威日志读写失败
	// @errEN chat log operation failed
	ChatLogError
	// @errZH 项目来源目录不可用
	// @errEN project source directory is unavailable
	ProjectSourceUnavailable
	// @errZH 所选 Runtime 当前不可用
	// @errEN selected runtime is unavailable
	RuntimeUnavailable
	// @errZH 受管对话目录归属校验失败
	// @errEN managed chat directory ownership check failed
	ChatDirectoryOwnershipInvalid
	// @errZH 当前技能内容暂时无法交付给 Runtime
	// @errEN current skill content cannot be delivered to runtime
	SkillUnavailable
	// @errZH 记忆语义索引重建失败
	// @errEN memory semantic index rebuild failed
	MemoryReindexFailed
	// @errZH Embedding 连接测试失败
	// @errEN embedding connection test failed
	MemoryEmbeddingTestFailed
	// @errZH 所选 Runtime 无法准确表达当前权限策略
	// @errEN selected runtime cannot accurately express the current policy
	RuntimePolicyUnsupported
	// @errZH Runtime 协议响应不完整或与当前 Adapter 不兼容
	// @errEN runtime protocol response is incomplete or incompatible with the current adapter
	RuntimeProtocolIncompatible
	// @errZH Runtime 在允许时间内没有任何活动更新
	// @errEN runtime had no activity updates within the allowed time
	RuntimeInactivityTimeout
	// @errZH 对话权威日志内容已损坏或不兼容
	// @errEN authoritative chat log is corrupted or incompatible
	ChatLogCorrupted
	// @errZH 对话派生索引内容已损坏或不兼容
	// @errEN derived chat index is corrupted or incompatible
	ChatLogIndexCorrupted
	// @errZH 待写入的对话日志事实不合法
	// @errEN chat log facts are invalid
	ChatLogRecordInvalid
	// @errZH 无法构造 Embedding 请求
	// @errEN failed to build embedding request
	MemoryEmbeddingRequestFailed
	// @errZH Embedding 响应格式不合法
	// @errEN embedding response is invalid
	MemoryEmbeddingResponseInvalid
	// @errZH 记忆凭据不可用或已损坏
	// @errEN memory credential is unavailable or corrupted
	MemoryCredentialError
	// @errZH 无法签发记忆调用上下文令牌
	// @errEN failed to issue a memory invocation context token
	MemoryContextTokenGenerationFailed
	// @errZH 无法启动内置记忆 MCP 端点
	// @errEN failed to start the built-in memory mcp endpoint
	MemoryMCPEndpointFailed
	// @errZH MCP 连接器凭据不可用或已损坏
	// @errEN mcp connector credential is unavailable or corrupted
	MCPConnectorCredentialError
	// @errZH 本地服务配置不合法
	// @errEN invalid core configuration
	CoreConfigurationInvalid
	// @errZH 无法准备本地数据目录
	// @errEN failed to prepare core data directory
	CoreDataDirectoryError
	// @errZH 无法初始化业务数据库
	// @errEN failed to initialize application database
	CoreDatabaseInitializationFailed
	// @errZH 无法初始化记忆服务
	// @errEN failed to initialize memory service
	CoreMemoryInitializationFailed
	// @errZH 无法初始化 Runtime 服务
	// @errEN failed to initialize runtime service
	CoreRuntimeInitializationFailed
	// @errZH 无法初始化对话执行服务
	// @errEN failed to initialize chat execution service
	CoreChatInitializationFailed
	// @errZH 本地服务关闭失败
	// @errEN failed to close core resources
	CoreShutdownFailed
	// @errZH 无法生成本地访问令牌
	// @errEN failed to generate local access token
	CoreTokenGenerationFailed
	// @errZH 技能文件存储操作失败
	// @errEN skill file storage operation failed
	SkillStorageError
	// @errZH 消息附件存储操作失败
	// @errEN message attachment storage operation failed
	MessageAttachmentStorageError
	// @errZH 受管工作区操作失败
	// @errEN managed workspace operation failed
	ManagedWorkspaceError
	// @errZH 项目服务当前不可用
	// @errEN project service is unavailable
	ProjectServiceUnavailable
	// @errZH Runtime 环境服务当前不可用
	// @errEN runtime environment service is unavailable
	RuntimeEnvironmentServiceUnavailable
	// @errZH 记忆服务当前不可用
	// @errEN memory service is unavailable
	MemoryServiceUnavailable
	// @errZH 无法生成档案压缩包
	// @errEN failed to generate profile archive
	ProfileArchiveGenerationFailed
	// @errZH 当前 HTTP 写入器不支持对话事件流
	// @errEN current HTTP writer does not support chat event streaming
	ChatEventStreamUnavailable
	// @errZH 无法编码 Runtime 事件
	// @errEN failed to encode runtime event
	RuntimeEventEncodingFailed
	// @errZH Runtime 配置不合法
	// @errEN runtime configuration is invalid
	RuntimeConfigurationInvalid
	// @errZH Runtime 资源关闭失败
	// @errEN failed to close runtime resources
	RuntimeShutdownFailed
	// @errZH 无法初始化团队编排服务
	// @errEN failed to initialize channel orchestration service
	CoreChannelInitializationFailed
	// @errZH 原生 Runtime 会话已失效，请新建会话后继续
	// @errEN the native runtime session is no longer available; start a new session to continue
	RuntimeSessionNotFound
	// @errZH Cursor 或 Runtime 需要重新登录后再试
	// @errEN Cursor or the runtime requires signing in again
	RuntimeAuthRequired
	// @errZH {{.Detail}}
	// @errEN {{.Detail}}
	RuntimeExecutionFailed

	// @errZH 无法读取或保存 AI 账号凭据
	// @errEN failed to read or save AI account credentials
	AIAccountCredentialError
	// @errZH 无法准备 AI 账号隔离配置
	// @errEN failed to prepare isolated AI account configuration
	AIAccountWorkspaceError
	// @errZH 账号登录失败，请重新登录
	// @errEN account sign-in failed; sign in again
	AIAccountLoginFailed
	// @errZH 交接内容无效，请改用近期对话继续
	// @errEN handoff content is invalid; continue with recent conversation
	RuntimeHandoffInvalid
)

const (
	// @errZH Embedding 服务当前不可用
	// @errEN embedding service is unavailable
	MemoryEmbeddingUnavailable StatusError = http.StatusBadGateway*1e8 + iota + 1
	// @errZH MCP 连接器返回了不合法的协议响应
	// @errEN mcp connector returned an invalid protocol response
	MCPConnectorProtocolInvalid
	// @errZH 模型目录服务暂时不可用
	// @errEN model catalog service is unavailable
	AIAccountModelsUnavailable
	// @errZH 模型目录响应不合法
	// @errEN model catalog response is invalid
	AIAccountModelsResponseInvalid
)

const (
	// @errZH IM 连接、聊天或投递记录不存在
	// @errEN im resource was not found
	IMResourceNotFound StatusError = http.StatusNotFound*1e8 + 4001
	// @errZH IM 配置不完整或执行目标不可用
	// @errEN im configuration is invalid
	IMConfigurationInvalid StatusError = http.StatusBadRequest*1e8 + 4001
	// @errZH IM 配置已变化，请刷新后重试
	// @errEN im configuration changed; refresh and retry
	IMConfigurationConflict StatusError = http.StatusConflict*1e8 + 4001
	// @errZH 无法读取或保存 IM 凭据
	// @errEN failed to read or save im credentials
	IMCredentialError StatusError = http.StatusInternalServerError*1e8 + 4001
	// @errZH IM 平台暂时不可用
	// @errEN im platform is unavailable
	IMPlatformUnavailable StatusError = http.StatusBadGateway*1e8 + 4001
	// @errZH IM 平台响应不符合当前协议
	// @errEN im platform returned an invalid protocol response
	IMProtocolInvalid StatusError = http.StatusBadGateway*1e8 + 4002
	// @errZH IM 登录已失效，请重新登录
	// @errEN im sign-in is required
	IMLoginRequired StatusError = http.StatusUnauthorized*1e8 + 4001
	// @errZH 此 IM 聊天尚未获准接入
	// @errEN im chat is not authorized
	IMChatNotAuthorized StatusError = http.StatusForbidden*1e8 + 4001
	// @errZH 当前微信聊天缺少可用发送上下文，请先给机器人发消息
	// @errEN weixin delivery context is unavailable
	IMDeliveryContextUnavailable StatusError = http.StatusConflict*1e8 + 4002
	// @errZH 投递结果不确定，请核对后再重发
	// @errEN delivery result is unknown; verify before resending
	IMDeliveryRetryUnsafe StatusError = http.StatusConflict*1e8 + 4003
	// @errZH IM 文件不存在、超出范围或过大
	// @errEN im file is missing, outside scope, or too large
	IMFileInvalid StatusError = http.StatusBadRequest*1e8 + 4002
	// @errZH IM 集成服务当前不可用
	// @errEN im integration service is unavailable
	IMServiceUnavailable StatusError = http.StatusServiceUnavailable*1e8 + 4001
)

const (
	// @errZH 发送目标已不存在，请优化工作流并重新选择获准聊天
	// @errEN delivery target no longer exists; optimize the workflow and select an approved chat
	IMTargetUnavailable StatusError = http.StatusNotFound*1e8 + 4002
	// @errZH 此聊天未允许工作流发送，请在聊天配置中开启工作流发送
	// @errEN workflow delivery is disabled for this chat; enable it in the chat configuration
	IMWorkflowDeliveryNotAllowed StatusError = http.StatusForbidden*1e8 + 4002
	// @errZH 连接仍被运行中的工作流或待发送消息使用，请等待完成或先停止相关任务
	// @errEN connection is used by active workflows or pending deliveries; wait or stop the related work
	IMConnectionInUse StatusError = http.StatusConflict*1e8 + 4004
	// @errZH 连接被已保存工作流引用，请查看受影响流程并确认删除；依赖变化后需重新确认
	// @errEN saved workflows reference this connection; review the affected workflows and confirm deletion again if dependencies change
	IMConnectionHasWorkflowDependencies StatusError = http.StatusConflict*1e8 + 4005
	// @errZH 此聊天尚未授权或授权已撤销，请在聊天配置中授权
	// @errEN chat is not authorized or authorization was revoked; authorize it in the chat configuration
	IMTargetNotAuthorized StatusError = http.StatusForbidden*1e8 + 4003
	// @errZH 发送连接已停用，请在连接设置中启用后重试
	// @errEN delivery connection is disabled; enable it in connection settings and retry
	IMConnectionDisabled StatusError = http.StatusConflict*1e8 + 4006
	// @errZH 企业微信机器人已在其他客户端连接，请关闭其他连接后保存设置以重连
	// @errEN wecom bot connected elsewhere; close the other connection and save settings to reconnect
	IMConnectionReplaced StatusError = http.StatusConflict*1e8 + 4007
)

const (
	// @errZH 插件清单无效或格式版本不受支持
	// @errEN plugin manifest is invalid or its format version is unsupported
	PluginManifestInvalid StatusError = http.StatusBadRequest*1e8 + 6501
	// @errZH 插件内容或路径不合法
	// @errEN plugin content or package path is invalid
	PluginContentInvalid StatusError = http.StatusBadRequest*1e8 + 6502
	// @errZH 插件内容超过导入限制
	// @errEN plugin content exceeds import limits
	PluginContentLimitExceeded StatusError = http.StatusBadRequest*1e8 + 6503
	// @errZH 插件文件无法安全准备或读取
	// @errEN plugin files could not be safely prepared or read
	PluginStorageError StatusError = http.StatusInternalServerError*1e8 + 6501
	// @errZH 插件或版本不存在
	// @errEN plugin or version was not found
	PluginNotFound StatusError = http.StatusNotFound*1e8 + 6501
	// @errZH 插件版本或内容已变化，请重新确认
	// @errEN plugin version or content changed; confirm the operation again
	PluginVersionConflict StatusError = http.StatusConflict*1e8 + 6501
	// @errZH 插件共享配置已变化或不兼容，请重新确认
	// @errEN plugin shared configuration changed or is incompatible
	PluginConfigurationConflict StatusError = http.StatusConflict*1e8 + 6502
	// @errZH 请先完成插件所需配置、认证和程序准备
	// @errEN complete required plugin configuration, authentication and dependencies first
	PluginNotReady StatusError = http.StatusConflict*1e8 + 6503
	// @errZH 此插件不属于当前伙伴可用范围
	// @errEN plugin is outside the current partner scope
	PluginForbidden StatusError = http.StatusForbidden*1e8 + 6501
	// @errZH 插件认证未完成或外部授权失效
	// @errEN plugin authentication did not complete or external authorization expired
	PluginAuthorizationFailed StatusError = http.StatusBadGateway*1e8 + 6501
	// @errZH 插件目录或安装包暂时无法下载，请检查网络后重试
	// @errEN plugin catalog or archive could not be downloaded; check the network and retry
	PluginDistributionUnavailable StatusError = http.StatusBadGateway*1e8 + 6502
	// @errZH 技能声明名称相同但内容不同，请调整已启用的技能或插件
	// @errEN skills with the same declared name have different content
	PluginSkillConflict StatusError = http.StatusConflict*1e8 + 6504
	// @errZH 认证进程或工作仍在使用共享账号，请稍后重试
	// @errEN shared account is in use by work or authorization; try again after it exits
	PluginBusy StatusError = http.StatusConflict*1e8 + 6505
)

const (
	// @errZH 工作区授权范围无效或已过期
	// @errEN workspace scope is invalid or expired
	WorkspaceScopeInvalid StatusError = http.StatusForbidden*1e8 + 7101
	// @errZH 工作区路径不合法
	// @errEN workspace path is invalid
	WorkspacePathInvalid StatusError = http.StatusBadRequest*1e8 + 7102
	// @errZH 工作区资源不存在
	// @errEN workspace resource was not found
	WorkspaceResourceNotFound StatusError = http.StatusNotFound*1e8 + 7103
	// @errZH 工作区资源已变化，请重新读取
	// @errEN workspace resource changed; read it again
	WorkspaceResourceConflict StatusError = http.StatusConflict*1e8 + 7104
	// @errZH 工作区资源超过大小限制
	// @errEN workspace resource exceeds the size limit
	WorkspaceResourceTooLarge StatusError = http.StatusRequestEntityTooLarge*1e8 + 7105
	// @errZH 工作区文件操作失败
	// @errEN workspace storage operation failed
	WorkspaceStorageFailed StatusError = http.StatusInternalServerError*1e8 + 7106
	// @errZH Git 操作失败
	// @errEN workspace git operation failed
	WorkspaceGitFailed StatusError = http.StatusInternalServerError*1e8 + 7107
	// @errZH 终端服务不可用
	// @errEN terminal service is unavailable
	TerminalUnavailable StatusError = http.StatusServiceUnavailable*1e8 + 7108
	// @errZH 终端请求无效
	// @errEN terminal request is invalid
	TerminalInvalid StatusError = http.StatusBadRequest*1e8 + 7109
	// @errZH 终端会话不存在或已结束
	// @errEN terminal session was not found or has ended
	TerminalNotFound StatusError = http.StatusNotFound*1e8 + 7110
	// @errZH 当前会话无权操作此终端
	// @errEN terminal session is not authorized
	TerminalForbidden StatusError = http.StatusForbidden*1e8 + 7111
)

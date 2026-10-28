package constants

const (
	// WorkflowCanvasFormatVersion 是源码派生画布投影的版本。
	WorkflowCanvasFormatVersion = 2
	// WorkflowHumanMaximumChoices 限制单次用户决策的候选数量。
	WorkflowHumanMaximumChoices = 500
	// WorkflowHumanChoicesLimitBytes 限制冻结候选快照字节数。
	WorkflowHumanChoicesLimitBytes = 256 * 1024
	// WorkflowPageMaximumBytes 限制单页面字面量体积。
	WorkflowPageMaximumBytes = 128 * 1024
	// WorkflowPagesMaximumBytes 限制同版本节点页面总大小。
	WorkflowPagesMaximumBytes = 512 * 1024
	// WorkflowPageMaximumElements 限制静态组件数量。
	WorkflowPageMaximumElements = 256
	// WorkflowPageMaximumDepth 限制组件及表达式嵌套。
	WorkflowPageMaximumDepth = 32
	// WorkflowPageFormatVersion 是原生页面文档版本。
	WorkflowPageFormatVersion = 1
	// WorkflowResultCatalogVersion 是只读节点产出的组件目录版本。
	WorkflowResultCatalogVersion = "workflow-result-v1"
	// WorkflowInputCatalogVersion 是参数输入的组件目录版本。
	WorkflowInputCatalogVersion = "workflow-input-v1"
	// WorkflowSDKVersion 是创作源码与宿主共同支持的原生契约版本。
	WorkflowSDKVersion = "ally.workflow/2"
	// WorkflowCompilerVersion 是编译产物中固定的编译器版本。
	WorkflowCompilerVersion = "2.0.0"
	// WorkflowIRVersion 是可持久化 Go 指令格式的版本。
	WorkflowIRVersion = 1
	// WorkflowSourceLimitBytes 限制单份创作源码的 UTF-8 大小。
	WorkflowSourceLimitBytes = 1 << 20
	// WorkflowSlotsLimitBytes 限制一次运行的持久中间数据大小。
	WorkflowSlotsLimitBytes = 8 << 20
	// WorkflowOutputLimitBytes 限制单节点结果大小。
	WorkflowOutputLimitBytes = 1 << 20
	// WorkflowLogLimitBytes 限制单条运行日志大小。
	WorkflowLogLimitBytes = 8 << 10
	// WorkflowDefaultConcurrency 是一次运行默认的外部效果并发数。
	WorkflowDefaultConcurrency = 4
	// WorkflowGlobalConcurrency 是整个应用默认的外部效果并发数。
	WorkflowGlobalConcurrency = 16
	// WorkflowDefaultTimeoutMillis 是运行的缺省总期限。
	WorkflowDefaultTimeoutMillis int64 = 24 * 60 * 60 * 1000
	// WorkflowMaxInstances 是单次运行的节点实例预算。
	WorkflowMaxInstances = 10000
	// WorkflowMaxLoopSteps 是单次运行的循环推进预算。
	WorkflowMaxLoopSteps = 10000
	// WorkflowMaxPureSteps 是单次运行的纯指令预算。
	WorkflowMaxPureSteps = 1000000
	// WorkflowCheckpointSteps 是连续纯指令的检查点间隔。
	WorkflowCheckpointSteps = 1000
	// WorkflowMaxChildDepth 是子工作流调用深度上限。
	WorkflowMaxChildDepth = 5
	// WorkflowMaxChildren 是单个调用树的子运行总数上限。
	WorkflowMaxChildren = 100
	// WorkflowLeaseMillis 是运行所有者租约期限。
	WorkflowLeaseMillis int64 = 15000
	// WorkflowLeaseRenewMillis 是租约续期和恢复扫描间隔。
	WorkflowLeaseRenewMillis int64 = 5000
	// WorkflowWorkerTimeoutMillis 为零，伙伴节点不设置独立期限。
	WorkflowWorkerTimeoutMillis int64 = 0
	// WorkflowFunctionTimeoutMillis 是纯计算进程默认期限。
	WorkflowFunctionTimeoutMillis int64 = 10 * 60 * 1000
	// WorkflowFunctionMaxTimeoutMillis 是纯计算进程期限硬上限。
	WorkflowFunctionMaxTimeoutMillis int64 = 10 * 60 * 1000
	// WorkflowHTTPTimeoutMillis 是 HTTP 动作默认期限。
	WorkflowHTTPTimeoutMillis int64 = 30 * 1000
	// WorkflowHTTPMaxTimeoutMillis 是 HTTP 动作期限硬上限。
	WorkflowHTTPMaxTimeoutMillis int64 = 5 * 60 * 1000
	// WorkflowScriptTimeoutMillis 是命令动作默认期限。
	WorkflowScriptTimeoutMillis int64 = 10 * 60 * 1000
	// WorkflowScriptMaxTimeoutMillis 是命令动作期限硬上限。
	WorkflowScriptMaxTimeoutMillis int64 = 10 * 60 * 1000
)

const (
	// WorkflowDefinitionCreatedEvent 是空定义提交事件。
	WorkflowDefinitionCreatedEvent = "workflow.definition.created"
	// WorkflowRevisionSavedEvent 是完整版本提交事件。
	WorkflowRevisionSavedEvent = "workflow.revision.saved"
	// WorkflowAnnotationChangedEvent 表示批注建立或被真实保存回执处理。
	WorkflowAnnotationChangedEvent = "workflow.annotation.changed"
	// WorkflowRunnerTickMillis 是运行推进、租约和期限调度的业务时间间隔。
	WorkflowRunnerTickMillis = 250
	// WorkflowEventHeartbeatSeconds 是订阅空闲心跳间隔。
	WorkflowEventHeartbeatSeconds = 15
)

const (
	// WorkflowRunHistoryLimit 是每个工作流可见执行历史的默认保留上限。
	WorkflowRunHistoryLimit = 30
	// WorkflowRunCreatedEvent 表示运行接纳已提交。
	WorkflowRunCreatedEvent = "workflow.run.created"
	// WorkflowRunHistoryDeletedEvent 表示一条已结束执行被移出历史列表。
	WorkflowRunHistoryDeletedEvent = "workflow.run.history.deleted"
	// WorkflowRunHistoryPrunedEvent 表示超过保留上限的执行历史已批量清理。
	WorkflowRunHistoryPrunedEvent = "workflow.run.history.pruned"
	// WorkflowRunChangedEvent 表示运行的持久状态已提交。
	WorkflowRunChangedEvent = "workflow.run.changed"
	// WorkflowPhaseEvent 表示 Go 指令进入一个源码阶段。
	WorkflowPhaseEvent = "workflow.phase.entered"
	// WorkflowLogEvent 是有界的源码运行日志。
	WorkflowLogEvent = "workflow.log"
	// WorkflowMCPPath 是内部 scoped 工具端点前缀。
	WorkflowMCPPath = "/workflow-mcp/"
)

// WorkflowMCPRuntimeKey 是内置工作流各场景在 CLI 中共用的稳定服务身份。
const WorkflowMCPRuntimeKey = "ally_workflow"

// WorkflowFailureStopRequestedField 在外部结果未知期间保留用户已经发出的停止意图。
const WorkflowFailureStopRequestedField = "stopRequested"

// WorkflowNodeDescriptionMaxRunes 限制节点卡片业务说明的 Unicode 字符数。
const WorkflowNodeDescriptionMaxRunes = 500

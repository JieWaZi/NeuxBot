package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkflowRun{})
	dbhelper.RegisterTable(&WorkflowFiber{})
	dbhelper.RegisterTable(&WorkflowGroupRun{})
	dbhelper.RegisterTable(&WorkflowNodeInstance{})
	dbhelper.RegisterTable(&WorkflowEffect{})
	dbhelper.RegisterTable(&WorkflowHumanRequest{})
	dbhelper.RegisterTable(&WorkflowOperationRequest{})
	dbhelper.RegisterTable(&WorkflowActionCall{})
}

// WorkflowExecutionDefaults 是运行接纳时冻结的真实智能体配置。
type WorkflowExecutionDefaults struct {
	// RuntimeID 是用户明确选定的智能体。
	RuntimeID string `json:"runtimeId"`
	// AccountID 是账号身份，空值交给该智能体的既有账号规则。
	AccountID string `json:"accountId"`
	// Model 是选定模型。
	Model string `json:"model"`
	// Reasoning 是推理强度。
	Reasoning string `json:"reasoning"`
	// PermissionLevel 是运行默认工具权限，业务审批不改变它。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// WorkspaceReadOnly 表示批准范围只允许读取。
	WorkspaceReadOnly bool `json:"workspaceReadOnly"`
}

// WorkflowRun 固定版本、参数、执行配置和租约，不读取后续创作修改。
type WorkflowRun struct {
	// AutomaticRunID 是服务端传入的原自动运行，不能从用户或模型正文绑定。
	AutomaticRunID string `json:"-"`
	// WorkerConfigurations 按源码节点身份冻结接纳时解析的有效执行配置，不包含凭据。
	WorkerConfigurations map[string]WorkflowExecutionDefaults `gorm:"serializer:json;type:text;not null" json:"-"`
	// RootRunID 绑定调用树，统一限制全部后代的数量。
	RootRunID string `gorm:"index;size:32" json:"rootRunId"`
	// ID 是运行身份。
	ID string `gorm:"type:varchar(32);primaryKey;index:idx_workflow_board_identity,priority:1;index:idx_workflow_board_activity,priority:3,sort:desc;index:idx_workflow_board_executor,priority:4,sort:desc" json:"id"`
	// DefinitionID 是所属定义。
	DefinitionID string `gorm:"index:idx_workflow_board_identity,priority:2;type:varchar(32);not null;index;index:idx_workflow_board_executor,priority:1;index:idx_workflow_run_history,priority:1" json:"definitionId"`
	// RevisionID 是本次不可变版本。
	RevisionID string `gorm:"type:varchar(32);not null;index:idx_workflow_run_revision_status,priority:1" json:"revisionId"`
	// RevisionOrdinal 是展示所需的冻结版本号。
	RevisionOrdinal int64 `json:"revisionOrdinal"`
	// Status 是持久运行生命周期。
	Status constants.WorkflowRunStatus `gorm:"type:varchar(32);not null;index;index:idx_workflow_run_revision_status,priority:2" json:"status"`
	// Args 是已填显式默认值并校验的运行输入。
	Args any `gorm:"serializer:json;type:text" json:"args"`
	// Defaults 是本次执行配置快照。
	Defaults WorkflowExecutionDefaults `gorm:"serializer:json;type:text;not null" json:"defaults"`
	// Limits 是该不可变版本接纳时冻结的流程预算。
	Limits workflowdsl.Limits `gorm:"serializer:json;type:text" json:"limits"`
	// ScopeBinding 是接纳时批准的工作范围快照。
	ScopeBinding WorkflowScopeBinding `gorm:"serializer:json;type:text;not null" json:"scopeBinding"`
	// Context 是只读流程上下文，不包含账号密钥。
	Context map[string]any `gorm:"serializer:json;type:text;not null" json:"flowContext"`
	// RootFiberID 是主程序的执行帧。
	RootFiberID string `gorm:"type:varchar(32);not null" json:"rootFiberId"`
	// ParentRunID 是 joined 或 detached 子运行的来源。
	ParentRunID string `gorm:"type:varchar(32);not null;default:'';index;index:idx_workflow_board_activity,priority:1;index:idx_workflow_board_executor,priority:2" json:"parentRunId"`
	// Joined 表示父运行取消时也应停止本运行。
	Joined bool `json:"joined"`
	// Depth 是调用树深度。
	Depth int `json:"depth"`
	// Result 是通过最终输出 schema 的返回 JSON。
	Result any `gorm:"serializer:json;type:text" json:"result"`
	// Failure 是稳定错误数据，不保存运行时凭据。
	Failure *workflowdsl.Value `gorm:"serializer:json;type:text" json:"failure,omitempty"`
	// Version 是每次持久提交增加的 CAS 版本。
	Version int64 `gorm:"not null" json:"version"`
	// LeaseOwner 是本次进程的随机所有者。
	LeaseOwner string `gorm:"type:varchar(32);not null;default:''" json:"-"`
	// Fence 是每次取得租约时增加的写入隔离代数。
	Fence int64 `gorm:"not null" json:"-"`
	// LeaseUntil 是租约期限的 Unix 毫秒。
	LeaseUntil int64 `gorm:"not null" json:"-"`
	// StartedAt 是冻结的时间来源。
	StartedAt int64 `json:"startedAt"`
	// DeadlineAt 是运行总预算期限。
	DeadlineAt int64 `json:"deadlineAt"`
	// CompletedAt 是真实终态时间。
	CompletedAt *int64 `json:"completedAt,omitempty"`
	// HistoryDeletedAt 是移除历史入口的时间；运行事实继续支持幂等、子运行和交付关联。
	HistoryDeletedAt *int64 `gorm:"index:idx_workflow_run_history,priority:2" json:"-"`
	// CreatedAt 是接纳时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最后提交时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli;index:idx_workflow_board_activity,priority:2,sort:desc;index:idx_workflow_board_executor,priority:3,sort:desc" json:"updatedAt"`
}

func (WorkflowRun) TableName() string { return "workflow_runs" }

// WorkflowFiber 保存可以从 PC 恢复的主流程或一项任务。
type WorkflowFiber struct {
	// ID 是独立执行帧身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// RunID 是所属运行。
	RunID string `gorm:"type:varchar(32);not null;index" json:"runId"`
	// GroupID 是拥有此任务的持久并行或管道组，主流程为空。
	GroupID string `gorm:"type:varchar(32);not null;default:'';index" json:"groupId"`
	// ItemIndex 是输入有序索引。
	ItemIndex int `json:"itemIndex"`
	// StageIndex 是当前管道阶段索引。
	StageIndex int `json:"stageIndex"`
	// Path 是嵌套组与输入项组成的稳定实例路径。
	Path string `gorm:"type:text;not null" json:"path"`
	// State 是 Go 指令帧，不包含 VM 栈。
	State workflowdsl.MachineState `gorm:"serializer:json;type:text;not null" json:"-"`
	// Status 是任务帧当前状态。
	Status constants.WorkflowStepStatus `gorm:"type:varchar(32);not null" json:"status"`
	// Boundary 是已经求值冻结的效果输入。
	Boundary *workflowdsl.EffectBoundary `gorm:"serializer:json;type:text" json:"-"`
	// WaitingID 是当前效果、人工请求或组的身份。
	WaitingID string `gorm:"type:varchar(32);not null;default:''" json:"waitingId"`
}

func (WorkflowFiber) TableName() string { return "workflow_fibers" }

// WorkflowGroupRun 保存有序收集与逐项推进的调度事实。
type WorkflowGroupRun struct {
	// ID 是一次组调用的身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// RunID 是所属运行。
	RunID string `gorm:"type:varchar(32);not null;index" json:"runId"`
	// ParentFiberID 是等待本组回执的执行帧。
	ParentFiberID string `gorm:"type:varchar(32);not null" json:"parentFiberId"`
	// NodeID 是源码中的组身份。
	NodeID string `json:"nodeId"`
	// Kind 区分 parallel 与 pipeline。
	Kind string `json:"kind"`
	// Policy 是 collect-null 或 fail-fast。
	Policy string `json:"policy"`
	// Concurrency 是本组活动任务限制。
	Concurrency int `json:"concurrency"`
	// Stages 是管道任务的静态 continuation。
	Stages []workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// OriginalItems 是按输入序号冻结的原始 JSON。
	OriginalItems []workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// Results 是按输入位置保存的最终结果，失败位置使用普通 null。
	Results []workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// Finished 是每个输入位置是否收敛，避免把正常 null 当失败。
	Finished []bool `gorm:"serializer:json;type:text" json:"-"`
	// Status 是组的收敛状态。
	Status constants.WorkflowStepStatus `json:"status"`
	// Failure 是 fail-fast 时首个真实错误。
	Failure *workflowdsl.Value `gorm:"serializer:json;type:text" json:"failure,omitempty"`
}

func (WorkflowGroupRun) TableName() string { return "workflow_group_runs" }

// WorkflowNodeInstance 是一个源码职责在实际项、轮次和分支中的执行。
type WorkflowNodeInstance struct {
	// Input 是效果求值后冻结的真实参数，不包含能力令牌。
	Input []workflowdsl.Value `gorm:"serializer:json;type:text" json:"input"`
	// Options 是节点配置快照，凭据只允许引用身份。
	Options map[string]any `gorm:"serializer:json;type:text" json:"options"`
	// GroupID 是拥有该节点的并行或管道组实例。
	GroupID string `gorm:"size:32;index" json:"groupId"`
	// ItemIndex 是该组原始输入中的有序索引。
	ItemIndex int `json:"itemIndex"`
	// StageIndex 是管道中的实际阶段索引。
	StageIndex int `json:"stageIndex"`
	// AttemptGeneration 是结果能力绑定的独立尝试代际。
	AttemptGeneration int64 `json:"attemptGeneration"`
	// ID 是节点实例身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// RunID 是所属运行。
	RunID string `gorm:"type:varchar(32);not null;index" json:"runId"`
	// FiberID 是执行该实例的帧。
	FiberID string `json:"fiberId"`
	// NodeID 是稳定源码节点身份。
	NodeID string `json:"nodeId"`
	// Kind 是 SDK 能力名称。
	Kind string `json:"kind"`
	// Path 是输入项和源码循环轮次。
	Path string `json:"path"`
	// Label 是本实例已求值的业务名称。
	Label string `json:"label"`
	// PhaseID 是所属阶段。
	PhaseID string `json:"phaseId"`
	// Status 是实际节点状态。
	Status constants.WorkflowStepStatus `json:"status"`
	// EffectID 是唯一效果身份。
	EffectID string `json:"effectId"`
	// ChatID 是真实伙伴对话，非 worker 节点为空。
	ChatID string `gorm:"index" json:"chatId"`
	// ExecutionID 是首次派发的权威执行。
	ExecutionID string `json:"executionId"`
	// Output 是经过 schema 校验的真实节点结果。
	Output any `gorm:"serializer:json;type:text" json:"output"`
	// Failure 是节点错误。
	Failure *workflowdsl.Value `gorm:"serializer:json;type:text" json:"failure,omitempty"`
	// CreatedAt 是实例建立时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// CompletedAt 是收敛时间。
	CompletedAt *int64 `json:"completedAt,omitempty"`
}

func (WorkflowNodeInstance) TableName() string { return "workflow_node_instances" }

// WorkflowEffect 是派发前持久保存的 outbox 与不可变效果收据。
type WorkflowEffect struct {
	// FinalMessageID 绑定本次成功执行的权威最终助手消息，续聊不会替换此锚点。
	FinalMessageID string `json:"finalMessageId,omitempty"`
	// BuiltinState 是独立内置 handler 的持久 Go 帧。
	BuiltinState *workflowdsl.MachineState `gorm:"serializer:json;type:text" json:"-"`
	// ID 是幂等派发身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// RunID 是所属运行。
	RunID string `gorm:"type:varchar(32);not null;index" json:"runId"`
	// FiberID 是等待此效果的执行帧。
	FiberID string `json:"fiberId"`
	// InstanceID 是所属节点实例。
	InstanceID string `json:"instanceId"`
	// Boundary 是完整冻结的派发输入。
	Boundary workflowdsl.EffectBoundary `gorm:"serializer:json;type:text;not null" json:"-"`
	// Status 是 outbox 与收据状态。
	Status constants.WorkflowStepStatus `json:"status"`
	// SlotReserved 是事务派发前预留的跨运行实际外部调用槽，终态后不再计数。
	SlotReserved bool `gorm:"index" json:"-"`
	// SkipRequested 是用户明确请求跳过，真实执行收敛前仍保留非终态。
	SkipRequested bool `json:"-"`
	// AttemptGeneration 区分真实尝试，不随 Runner 所有者改变。
	AttemptGeneration int64 `json:"attemptGeneration"`
	// ExecutionID 是派发前预分配的 Core 执行身份。
	ExecutionID string `json:"executionId"`
	// ExecutionStatus 保存本次真实 Execution 收据，自动重试只接受已停止的失败尝试。
	ExecutionStatus constants.ExecutionStatus `json:"executionStatus"`
	// ChatID 是 Core 接纳后的稳定对话身份。
	ChatID string `gorm:"index" json:"chatId"`
	// ChildRunID 是已接纳的真实子运行身份，不能从普通 JSON 伪造。
	ChildRunID string `json:"childRunId,omitempty"`
	// Result 是经过验证且不能被第二次提交覆盖的结果。
	Result *workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// Candidate 是 worker MCP 首次有效提交，执行成功前不作为最终结果。
	Candidate *workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// Failure 是效果失败数据。
	Failure *workflowdsl.Value `gorm:"serializer:json;type:text" json:"failure,omitempty"`
	// AuthToken 是恢复派发所需的随机能力凭证，仅保存在受管数据库且不向 API 输出。
	AuthToken string `json:"-"`
	// TokenDigest 是 scoped 工具令牌摘要，API 不公开原始令牌。
	TokenDigest string `json:"-"`
	// DeadlineAt 是本效果的期限。
	DeadlineAt int64 `json:"deadlineAt"`
	// CreatedAt 是准备时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

func (WorkflowEffect) TableName() string { return "workflow_effects" }

// WorkflowHumanRequest 是业务输入，不复用 Runtime 的内存交互请求。
type WorkflowHumanRequest struct {
	// ID 是人工请求身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// RunID 是所属运行。
	RunID string `gorm:"type:varchar(32);not null;index" json:"runId"`
	// EffectID 是等待答案的效果。
	EffectID string `json:"effectId"`
	// InstanceID 是所属节点实例。
	InstanceID string `json:"instanceId"`
	// Prompt 是已求值的问题正文。
	Prompt string `json:"prompt"`
	// Options 是已冻结的 schema、选项及跳过规则。
	Options map[string]any `gorm:"serializer:json;type:text;not null" json:"options"`
	// Status 是回答、跳过和超时的 CAS 状态。
	Status constants.WorkflowStepStatus `gorm:"type:varchar(32);not null;index" json:"status"`
	// Value 是用户实际提交的答案。
	Value any `gorm:"serializer:json;type:text" json:"value"`
	// Source 区分 user、skip 和 timeout。
	Source string `json:"source"`
	// DeadlineAt 是可选等待期限，零值表示不限期。
	DeadlineAt int64 `json:"deadlineAt"`
	// CreatedAt 是等待建立时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// AnsweredAt 是首个有效回答提交时间。
	AnsweredAt *int64 `json:"answeredAt,omitempty"`
}

func (WorkflowHumanRequest) TableName() string { return "workflow_human_requests" }

// WorkflowOperationRequest 是宿主操作许可，与业务人工输入和 Runtime 授权分别持久化。
type WorkflowOperationRequest struct {
	// CreatedAt 是宿主操作请求首次持久化的真实等待起点。
	CreatedAt int64 `gorm:"autoCreateTime:milli;not null;default:0" json:"createdAt"`
	// ID 是一次操作许可的稳定身份。
	ID string `gorm:"primaryKey;size:32" json:"id"`
	// RunID 限定所属运行。
	RunID string `gorm:"index;size:32" json:"runId"`
	// EffectID 绑定版本中冻结的实际动作。
	EffectID string `gorm:"uniqueIndex;size:32" json:"effectId"`
	// InstanceID 指向运行页面中等待授权的节点。
	InstanceID string `gorm:"size:32" json:"instanceId"`
	// Description 说明即将发生的操作与本机权限。
	Description string `gorm:"type:text" json:"description"`
	// Descriptor 保存可审核的目标、命令和凭据引用，禁止保存凭据值。
	Descriptor map[string]any `gorm:"serializer:json;type:text" json:"descriptor"`
	// DescriptorDigest 绑定用户回答时审核的具体操作。
	DescriptorDigest string `gorm:"size:64" json:"descriptorDigest"`
	// Status 区分等待、已回答与取消。
	Status constants.WorkflowStepStatus `gorm:"size:32;index" json:"status"`
	// Allowed 是用户明确选择的许可，等待状态下不参与判断。
	Allowed bool `json:"allowed"`
	// AnsweredAt 是第一次合法回答的提交时刻。
	AnsweredAt *int64 `json:"answeredAt,omitempty"`
}

func (WorkflowOperationRequest) TableName() string { return "workflow_operation_requests" }

// WorkflowActionCall 保存 handler 内每个稳定工具步骤的 outbox 与回执。
type WorkflowActionCall struct {
	// ConnectorID 是冻结的真实连接器身份。
	ConnectorID string `gorm:"size:32" json:"connectorId"`
	// ToolName 是本次实际调用的工具名称。
	ToolName string `json:"toolName"`
	// Input 是调用前持久保存的结构化参数。
	Input any `gorm:"serializer:json;type:text" json:"input"`
	// ID 是内部调用的存储身份。
	ID string `gorm:"primaryKey;size:32" json:"id"`
	// RunID 限定所属运行。
	RunID string `gorm:"index;size:32" json:"runId"`
	// EffectID 限定所属 handler 效果。
	EffectID string `gorm:"uniqueIndex:workflow_action_step;size:32" json:"effectId"`
	// CallID 是创作为每项步骤提供的稳定身份。
	CallID string `gorm:"uniqueIndex:workflow_action_step;size:64" json:"callId"`
	// PayloadDigest 拒绝同一步骤的参数变化。
	PayloadDigest string `gorm:"size:64" json:"payloadDigest"`
	// Status 区分已准备、已回执和结果不明。
	Status constants.WorkflowStepStatus `json:"status"`
	// Result 是已提交的真实工具结果。
	Result *workflowdsl.Value `gorm:"serializer:json;type:text" json:"-"`
	// Failure 是可交给源码 catch 的稳定错误。
	Failure *workflowdsl.Value `gorm:"serializer:json;type:text" json:"failure,omitempty"`
}

func (WorkflowActionCall) TableName() string { return "workflow_action_calls" }

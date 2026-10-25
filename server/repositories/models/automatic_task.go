package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&AutomaticTask{})
	dbhelper.RegisterTable(&AutomaticRun{})
}

// AutomaticTaskSchedule 保存可预览与持久化的当地定时规则。
type AutomaticTaskSchedule struct {
	// Kind 是一次性、常用周期或自定义 Cron 的规则类型。
	Kind constants.AutomaticScheduleKind `json:"kind"`
	// Timezone 是保存与计算规则使用的 IANA 时区。
	Timezone string `json:"timezone" validate:"max=120"`
	// OnceAt 是一次性计划的当地日期时间，格式为 YYYY-MM-DDTHH:mm。
	OnceAt string `json:"onceAt" validate:"max=16"`
	// Minute 是每小时或固定时间规则的分钟。
	Minute int `json:"minute"`
	// Hour 是每天、每周或每月规则的小时。
	Hour int `json:"hour"`
	// Weekday 是每周规则的星期，星期日为 0。
	Weekday int `json:"weekday"`
	// DayOfMonth 是每月规则的日期，不存在的日期跳过该月。
	DayOfMonth int `json:"dayOfMonth"`
	// Cron 是仅包含分、时、日、月、周的自定义表达式。
	Cron string `json:"cron" validate:"max=120"`
}

// AutomaticTaskMemberConfiguration 保存一位成员在此自动任务中的完整覆盖配置。
type AutomaticTaskMemberConfiguration struct {
	// WakerID 是应用覆盖配置的团队成员。
	WakerID string `json:"wakerId" validate:"required,max=32"`
	// RuntimeID 是此成员使用的智能体。
	RuntimeID string `json:"runtimeId" validate:"required,max=120"`
	// AccountID 是此成员选用的账号。
	AccountID string `json:"accountId" validate:"max=32"`
	// Model 是此成员选用的模型，空值使用智能体默认模型。
	Model string `json:"model" validate:"max=200"`
	// Reasoning 是此成员选用的推理强度，空值使用默认强度。
	Reasoning string `json:"reasoning" validate:"max=120"`
	// PermissionLevel 是此成员执行时使用的权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
}

// AutomaticTaskConfiguration 是用户可编辑并在每次接纳时冻结的执行配置。
type AutomaticTaskConfiguration struct {
	// Knowledge 固定用户明确选中的知识维护资源和操作，普通任务为空。
	Knowledge *KnowledgeMaintenanceAuthorization `gorm:"serializer:json;type:text" json:"knowledge,omitempty"`
	// TargetKind 是伙伴、团队或工作流中的唯一目标类型。
	TargetKind constants.AutomaticTaskTargetKind `gorm:"size:24;not null;default:''" json:"targetKind"`
	// WorkflowID 是工作流目标，其他目标必须为空。
	WorkflowID string `gorm:"size:32;not null;default:'';index" json:"workflowId" validate:"max=32"`
	// WorkflowRevisionPolicy 在每次机会领取时选择当前或固定版本。
	WorkflowRevisionPolicy constants.WorkflowRevisionPolicy `gorm:"size:24;not null;default:''" json:"workflowRevisionPolicy"`
	// WorkflowRevisionID 是 pinned 策略下指定的不可变版本。
	WorkflowRevisionID string `gorm:"size:32;not null;default:''" json:"workflowRevisionId" validate:"max=32"`
	// WorkflowInputRevisionID 保存参数来源配置依据的不可变工作流版本。
	WorkflowInputRevisionID string `gorm:"size:32;not null;default:''" json:"workflowInputRevisionId" validate:"max=32"`
	// WorkflowInputBindings 为每个顶层参数保存唯一来源与可选固定值。
	WorkflowInputBindings map[string]workflowinput.Binding `gorm:"serializer:json;type:text" json:"workflowInputBindings"`
	// WorkspaceReadOnly 冻结工作流宿主和伙伴操作的只读约束。
	WorkspaceReadOnly bool `json:"workspaceReadOnly"`
	// MemberConfigs 是团队成员的任务专属覆盖；未列出的成员沿用运行时团队设置。
	MemberConfigs []AutomaticTaskMemberConfiguration `gorm:"serializer:json;type:text" json:"memberConfigs" validate:"max=5,dive"`
	// ExecutorWakerID 是选择的独立执行伙伴，团队任务为空；历史记录使用任务的 WakerID。
	ExecutorWakerID string `gorm:"type:varchar(32);not null;default:''" json:"executorWakerId" validate:"max=32"`
	// ChannelID 是执行任务的团队；空值表示由所属伙伴独立执行。
	ChannelID string `gorm:"type:varchar(32);not null;default:'';index" json:"channelId" validate:"max=32"`
	// Name 是自动任务的用户可见名称。
	Name string `json:"name" validate:"required,max=120"`
	// Instruction 是每次执行发送给伙伴的完整工作指令。
	Instruction string `json:"instruction" validate:"max=10000"`
	// Trigger 是任务唯一的持久触发类型。
	Trigger constants.AutomaticTaskTrigger `json:"trigger"`
	// Schedule 是定时任务的规则，API 任务使用零值。
	Schedule AutomaticTaskSchedule `gorm:"serializer:json;type:text" json:"schedule"`
	// ProjectID 是所属伙伴的项目，使用临时目录时为空。
	ProjectID string `json:"projectId" validate:"max=32"`
	// WorkingDirectory 是用户授权的本地目录，选择项目时为空。
	WorkingDirectory string `json:"workingDirectory" validate:"max=4096"`
	// RuntimeID 是执行时使用的智能体环境标识。
	RuntimeID string `json:"runtimeId" validate:"max=120"`
	// AccountID 是选用的受管账号；保存时将空值规范化为 system，固定使用智能体原生登录。
	AccountID string `json:"accountId" validate:"max=32"`
	// Model 是执行使用的模型，空值使用智能体默认值。
	Model string `json:"model" validate:"max=200"`
	// Reasoning 是执行时选择的推理强度。
	Reasoning string `json:"reasoning" validate:"max=120"`
	// PermissionLevel 是自动执行冻结的产品权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// Enabled 表示允许接纳新的运行，停用不取消已有执行。
	Enabled bool `json:"enabled"`
}

// AutomaticTask 保存伙伴或团队的自动工作配置和计划游标。
type AutomaticTask struct {
	// WorkflowTriggerDetached 表示已从流程触发配置移除，保留既有运行与密钥审计记录。
	WorkflowTriggerDetached bool `gorm:"not null;default:false" json:"-"`
	// ID 是任务的稳定标识。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// WakerID 是独立执行伙伴或最近保存时的团队负责人，兼容已有管理接口的作用域。
	WakerID string `gorm:"type:varchar(32);not null;index" json:"wakerId"`
	// AutomaticTaskConfiguration 是允许编辑的任务配置。
	AutomaticTaskConfiguration `gorm:"embedded"`
	// Revision 是编辑和领取运行时使用的乐观并发版本。
	Revision int64 `gorm:"not null" json:"-"`
	// ConfigurationVersion 在编辑、启停或密钥轮换时变化，用于拒绝接续已撤销配置。
	ConfigurationVersion int64 `gorm:"not null" json:"revision"`
	// TriggerVersion 是本机密钥及 API 幂等请求所属的触发代际。
	TriggerVersion int64 `gorm:"not null" json:"-"`
	// TokenHash 是当前任务密钥的摘要，不对管理接口公开。
	TokenHash []byte `gorm:"type:blob" json:"-"`
	// NextRunAt 是下一次 UTC 计划时间，没有后续计划时为空。
	NextRunAt *int64 `json:"nextRunAt"`
	// LastMissedAt 是最近一次因应用停止或休眠错过的计划时间。
	LastMissedAt *int64 `json:"lastMissedAt"`
	// MissedCount 是未逐条保存的离线错过机会数量。
	MissedCount int64 `gorm:"not null;default:0" json:"missedCount"`
	// ActiveRunID 是当前占用自动运行槽的运行标识。
	ActiveRunID string `gorm:"type:varchar(32);not null;default:''" json:"activeRunId"`
	// Deleting 表示聚合删除已封闭新的触发。
	Deleting bool `gorm:"not null;default:false" json:"-"`
	// CreatedAt 是任务建立的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是任务配置或计划游标最近更新的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回自动任务的表名。
func (AutomaticTask) TableName() string { return "automatic_tasks" }

// AutomaticRun 保存一次机会的不可变输入和标准 Chat 接纳关系。
type AutomaticRun struct {
	// IntegrationInvocation 是原集成调用的受信身份，nil 保留定时与旧 Token 行为。
	IntegrationInvocation *IntegrationInvocation `gorm:"serializer:json;type:text" json:"-"`
	// WorkflowRunID 是已接纳的工作流运行，伙伴和团队目标为空。
	WorkflowRunID string `gorm:"size:32;not null;default:'';index" json:"workflowRunId"`
	// FinalState 是团队首次整体结束时的投影，防止用户后续续聊重新占用自动运行槽。
	FinalState constants.AutomaticRunState `gorm:"type:varchar(24);not null;default:''" json:"-"`
	// FinalCompletedAt 是团队整体结束的权威事实时间，独立伙伴运行仍直接读取首次 Execution。
	FinalCompletedAt *int64 `json:"-"`
	// ThreadID 是团队运行创建的会话；独立伙伴运行为空。
	ThreadID string `gorm:"type:varchar(32);not null;default:'';index;index:idx_auto_run_chat_thread,priority:2" json:"threadId"`
	// ID 是运行记录及派生首次发送身份的稳定标识。
	ID string `gorm:"type:varchar(32);primaryKey;index:idx_automatic_run_latest,priority:3,sort:desc" json:"id"`
	// TaskID 是运行所属自动任务。
	TaskID string `gorm:"type:varchar(32);not null;uniqueIndex:automatic_run_occurrence;index;index:idx_automatic_run_latest,priority:1" json:"taskId"`
	// WakerID 是实际执行的独立伙伴或该次团队会话的负责人。
	WakerID string `gorm:"type:varchar(32);not null;index" json:"wakerId"`
	// OccurrenceKey 是在同一任务内唯一的定时或请求机会。
	OccurrenceKey string `gorm:"type:varchar(200);not null;uniqueIndex:automatic_run_occurrence" json:"-"`
	// RequestHash 是规范化附加输入的摘要，用于识别异内容同键请求。
	RequestHash string `gorm:"type:varchar(64);not null" json:"-"`
	// WorkflowCallArgs 保存本次调用的结构化参数，区分缺失与显式空值。
	WorkflowCallArgs map[string]any `gorm:"serializer:json;type:text" json:"-"`
	// WorkflowResolvedArgs 保存接纳时已合成的完整参数，恢复不重新取默认值。
	WorkflowResolvedArgs map[string]any `gorm:"serializer:json;type:text" json:"workflowResolvedArgs,omitempty"`
	// Input 是伙伴或团队运行的本次附加文本。
	Input string `gorm:"type:text;not null" json:"-"`
	// Source 是定时、API 或立即运行来源。
	Source constants.AutomaticRunSource `gorm:"type:varchar(16);not null" json:"source"`
	// Configuration 是接纳时冻结的执行配置，不包含 API 密钥。
	Configuration AutomaticTaskConfiguration `gorm:"serializer:json;type:text;not null" json:"configuration"`
	// ConfigurationRevision 是该机会被领取时的任务版本。
	ConfigurationRevision int64 `gorm:"not null" json:"-"`
	// ChatID 是与首次发送身份稳定派生的关联 Chat 标识。
	ChatID string `gorm:"type:varchar(32);not null;default:'';index;index:idx_auto_run_chat_thread,priority:1" json:"chatId"`
	// ExecutionID 是该次自动运行预先分配的首次执行标识。
	ExecutionID string `gorm:"type:varchar(32);not null;default:''" json:"executionId"`
	// Admission 是是否已经进入 Chat 权威事实的接纳阶段。
	Admission constants.AutomaticRunAdmission `gorm:"type:varchar(24);not null;index" json:"admission"`
	// ErrorCode 是失败或跳过原因的稳定错误名称。
	ErrorCode string `gorm:"type:varchar(120);not null;default:''" json:"errorCode"`
	// ScheduledAt 是本次定时机会的 UTC 时间，其他来源为空。
	ScheduledAt *int64 `json:"scheduledAt"`
	// CreatedAt 是机会登记的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli;index:idx_automatic_run_latest,priority:2,sort:desc" json:"createdAt"`
	// UpdatedAt 是接纳关系最近更新的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回自动运行机会的表名。
func (AutomaticRun) TableName() string { return "automatic_runs" }

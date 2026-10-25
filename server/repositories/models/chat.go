// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&Chat{})
	dbhelper.RegisterTable(&RuntimeSession{})
	dbhelper.RegisterTable(&ChatDeletionIntent{})
}

// ChatDeletionIntent 是聚合目录 staging 与数据库删除事务之间的权威提交标记。
type ChatDeletionIntent struct {
	// ID 是文件 manifest 与数据库 tombstone 共用的随机标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// State 是 prepared 或 committed；只有 committed 才允许最终清理 staging。
	State string `gorm:"column:state;type:varchar(16);not null;index" json:"state"`
	// ChatIDs 是本次在一个事务中删除的全部 Chat 标识。
	ChatIDs []string `gorm:"column:chat_ids;type:text;not null;serializer:json" json:"chatIds"`
	// CreatedAt 是准备删除的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是删除提交状态最近变化的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli;index" json:"updatedAt"`
}

// TableName 返回 Chat 聚合删除 tombstone 的表名。
func (ChatDeletionIntent) TableName() string { return "chat_deletion_intents" }

// PendingApprovalOption 保存未决审批的一项安全 ACP 选择。
type PendingApprovalOption struct {
	// ID 是 ACP 选项标识。
	ID string `json:"id"`
	// Name 是 ACP 提供的用户可见名称。
	Name string `json:"name"`
	// Kind 是 ACP 提供的选项类别；未知值原样保留。
	Kind string `json:"kind"`
}

// PendingApprovalLocation 保存未决审批的一处安全文件位置。
type PendingApprovalLocation struct {
	// Path 是 ACP 提供的文件路径。
	Path string `json:"path"`
	// Line 是 ACP 提供的可选无符号行号；为空表示未提供。
	Line *int `json:"line,omitempty"`
}

// PendingApproval 保存 Chat 查询投影中的安全未决审批。
type PendingApproval struct {
	// ApprovalID 是 NeuxBot 生成的稳定交互标识。
	ApprovalID string `json:"approvalId"`
	// ToolCallID 是 ACP Session 内对应工具调用标识。
	ToolCallID string `json:"toolCallId"`
	// Title 是 ACP Agent 提供的安全操作标题。
	Title string `json:"title"`
	// Kind 是 ACP Agent 提供的工具类别。
	Kind string `json:"kind,omitempty"`
	// Summary 是 ACP Agent 提供或由标准字段形成的安全摘要。
	Summary string `json:"summary,omitempty"`
	// Command 是从 ACP 标准命令字段提取并限制长度的安全展示摘要。
	Command string `json:"command,omitempty"`
	// Locations 是 ACP Agent 提供的安全文件位置。
	Locations []PendingApprovalLocation `json:"locations,omitempty"`
	// Options 是 ACP Agent 允许用户选择的全部有效选项。
	Options []PendingApprovalOption `json:"options"`
	// RequestedAt 是请求进入权威日志的 Unix 毫秒时间。
	RequestedAt int64 `json:"requestedAt"`
	// RequestedSequence 是请求在 Chat 权威日志中的创建序号。
	RequestedSequence int64 `json:"requestedSequence"`
}

// PendingElicitation 保存 Chat 查询投影中的安全未决结构化输入。
type PendingElicitation struct {
	// AllowSkip 明确 CLI 问答是否允许跳过；原生请求沿用默认行为。
	AllowSkip *bool `json:"allowSkip,omitempty"`
	// ElicitationID 是 NeuxBot 生成的稳定交互标识。
	ElicitationID string `json:"elicitationId"`
	// ToolCallID 是对应 ACP 工具调用标识，URL 模式可能为空。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Mode 是 form 或 url。
	Mode string `json:"mode"`
	// Message 是 Agent 提供的用户可见说明。
	Message string `json:"message"`
	// RequestedSchema 是 form 模式下的 JSON Schema 对象。
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	// URL 是 url 模式下需要用户访问的地址。
	URL string `json:"url,omitempty"`
	// RequestedAt 是请求进入权威日志的 Unix 毫秒时间。
	RequestedAt int64 `json:"requestedAt"`
	// RequestedSequence 是请求在 Chat 权威日志中的创建序号。
	RequestedSequence int64 `json:"requestedSequence"`
}

// Chat 保存可由权威 JSONL 重建的对话查询索引。
type Chat struct {
	// LastEndedExecutionID 是最近真实结束回合的身份，可从 JSONL 重建。
	LastEndedExecutionID string `gorm:"size:32;not null;default:''" json:"lastEndedExecutionId,omitempty"`
	// LastEndedExecutionStatus 是最近结束回合的结果，不代表对话永久完成。
	LastEndedExecutionStatus constants.ExecutionStatus `gorm:"index:idx_chat_board_terminal_count,priority:1,where:active_execution_id = '';size:24;not null;default:''" json:"lastEndedExecutionStatus,omitempty"`
	// LastEndedExecutionAt 是最近结束回合的权威日志时间。
	LastEndedExecutionAt *int64 `json:"lastEndedExecutionAt,omitempty"`
	// ForkSourceChatID 是分叉来源会话，删除来源不影响本会话。
	ForkSourceChatID string `json:"forkSourceChatId,omitempty"`
	// ForkSourceExecutionID 是来源中被选择的结束回合。
	ForkSourceExecutionID string `json:"forkSourceExecutionId,omitempty"`
	// LastAccountID 是最近一次发送固定使用的账号，重开聊天时恢复该选择。
	LastAccountID string `gorm:"type:varchar(32);not null;default:''" json:"lastAccountId"`
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"index:idx_chat_pending_id,where:active_execution_id <> '' AND (waiting_approval = 1 OR waiting_elicitation = 1);index:idx_chat_board_count,priority:2;index:idx_chat_board_executor_count,priority:3;index:idx_chat_board_terminal_count,priority:4,where:active_execution_id = '';column:id;type:varchar(32);primaryKey;index:idx_chat_board_identity,priority:1;index:idx_chat_board_activity,priority:2,sort:desc;index:idx_chat_board_executor,priority:3,sort:desc;index:idx_chat_board_title,priority:2" json:"id"`
	// WakerID 是对话永久绑定的伙伴标识。
	WakerID string `gorm:"index:idx_chat_board_count,priority:3;index:idx_chat_board_executor_count,priority:1;index:idx_chat_board_terminal_count,priority:2,where:active_execution_id = '';index:idx_chat_board_identity,priority:2;column:waker_id;type:varchar(32);not null;index;index:idx_chat_board_executor,priority:1" json:"wakerId"`
	// ProjectID 是首次发送时选择的项目标识，临时文件夹对话为空。
	ProjectID string `gorm:"column:project_id;type:varchar(32);not null;default:'';index" json:"projectId,omitempty"`
	// ProjectName 是首次发送时冻结的项目展示名称或临时文件夹名称。
	ProjectName string `gorm:"column:project_name;type:varchar(80);not null;default:''" json:"projectName"`
	// WorkspaceDirectory 是该 Chat 的受管 Runtime cwd，保存独立指令、技能与附件。
	WorkspaceDirectory string `gorm:"column:workspace_directory;type:text;not null" json:"workspaceDirectory"`
	// AdditionalWorkingDirectories 是首次发送时冻结并授权的真实目录，首项是用户终端起始目录。
	AdditionalWorkingDirectories []string `gorm:"column:additional_working_directories;type:text;not null;serializer:json" json:"additionalWorkingDirectories"`
	// Title 是任务列表和标题栏使用的当前标题投影。
	Title string `gorm:"column:title;type:varchar(200);not null;index:idx_chat_board_title,priority:1" json:"title"`
	// LatestSequence 是权威 JSONL 已提交的最大连续序号。
	LatestSequence int64 `gorm:"column:latest_sequence;not null;default:0" json:"latestSequence"`
	// ActiveExecutionID 是当前活跃执行标识，没有活跃执行时为空。
	ActiveExecutionID string `gorm:"index:idx_chat_board_terminal_count,priority:6,where:active_execution_id = '';column:active_execution_id;type:varchar(32);not null;default:'';index" json:"activeExecutionId,omitempty"`
	// WaitingApproval 表示当前活跃执行正在等待至少一个工具审批。
	WaitingApproval bool `gorm:"index:idx_chat_board_terminal_count,priority:7,where:active_execution_id = '';column:waiting_approval;not null;default:false;index" json:"waitingApproval"`
	// PendingApprovals 是按到达顺序保存的安全未决审批查询投影。
	PendingApprovals []PendingApproval `gorm:"column:pending_approvals;type:text;serializer:json" json:"pendingApprovals"`
	// WaitingElicitation 表示当前活跃执行正在等待至少一个结构化输入。
	WaitingElicitation bool `gorm:"index:idx_chat_board_terminal_count,priority:8,where:active_execution_id = '';column:waiting_elicitation;not null;default:false;index" json:"waitingElicitation"`
	// PendingElicitations 是按到达顺序保存的安全未决结构化输入查询投影。
	PendingElicitations []PendingElicitation `gorm:"column:pending_elicitations;type:text;serializer:json" json:"pendingElicitations"`
	// LastRuntimeID 是最近一次执行选择的开放 Runtime 标识。
	LastRuntimeID string `gorm:"column:last_runtime_id;type:varchar(120);not null;default:''" json:"lastRuntimeId,omitempty"`
	// LastReasoning 是最近一次执行或分叉边界冻结的推理强度。
	LastReasoning string `gorm:"column:last_reasoning;type:varchar(120);not null;default:''" json:"lastReasoning,omitempty"`
	// LastModel 是最近一次执行选择的模型标识。
	LastModel string `gorm:"column:last_model;type:varchar(200);not null;default:''" json:"lastModel,omitempty"`
	// LastPermissionLevel 是最近一次实际发送冻结的产品权限等级。
	LastPermissionLevel constants.PermissionLevel `gorm:"column:last_permission_level;type:varchar(24);not null;default:''" json:"lastPermissionLevel,omitempty"`
	// HiddenFromWakerHistory 表示该 Chat 由其他产品入口拥有，不出现在伙伴普通对话列表。
	HiddenFromWakerHistory bool `gorm:"index:idx_chat_board_count,priority:4;index:idx_chat_board_executor_count,priority:4;index:idx_chat_board_terminal_count,priority:5,where:active_execution_id = '';column:hidden_from_waker_history;not null;default:false;index" json:"hiddenFromWakerHistory,omitempty"`
	// CreatedAt 是 Chat 建立时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近一次权威事件提交时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"index:idx_chat_board_count,priority:1;index:idx_chat_board_executor_count,priority:2;index:idx_chat_board_terminal_count,priority:3,where:active_execution_id = '';column:updated_at;autoUpdateTime:milli;index;index:idx_chat_board_activity,priority:1,sort:desc;index:idx_chat_board_executor,priority:2,sort:desc" json:"updatedAt"`
}

// TableName 返回对话索引模型使用的表名。
func (Chat) TableName() string {
	return "chats"
}

// RuntimeSession 保存可由对话日志重建的 Runtime 续接索引。
type RuntimeSession struct {
	// ChatID 是续接 Session 所属 Chat 标识。
	ChatID string `gorm:"column:chat_id;type:varchar(32);primaryKey" json:"chatId"`
	// RuntimeID 是拥有该续接句柄的开放 Runtime 标识。
	RuntimeID string `gorm:"column:runtime_id;type:varchar(120);primaryKey" json:"runtimeId"`
	// SessionID 是 Runtime 创建或确认的续接 Session 句柄。
	SessionID string `gorm:"column:session_id;type:text;not null" json:"-"`
	// LastExecutionID 是最后成功使用该续接 Session 的执行标识。
	LastExecutionID string `gorm:"column:last_execution_id;type:varchar(32);not null" json:"lastExecutionId"`
	// UpdatedAt 是 Runtime 续接索引最近更新时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回 Runtime 续接索引模型使用的表名。
func (RuntimeSession) TableName() string {
	return "runtime_sessions"
}

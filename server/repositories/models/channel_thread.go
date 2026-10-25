package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
)

// ChannelThread 是一个持续复用成员 Chat 上下文的团队对话。
type ChannelThread struct {
	// AutomaticRunID 是服务端传入的原自动运行，不能从用户或模型正文绑定。
	AutomaticRunID string `json:"-"`
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey;index:idx_thread_board_identity,priority:1;index:idx_thread_board_activity,priority:2,sort:desc;index:idx_thread_board_executor,priority:3,sort:desc;index:idx_thread_board_title,priority:2" json:"id"`
	// ChannelID 是创建该对话的团队标识。
	ChannelID string `gorm:"index:idx_thread_board_identity,priority:2;column:channel_id;type:varchar(32);not null;index;uniqueIndex:idx_channel_thread_client_request,priority:1;index:idx_thread_board_executor,priority:1" json:"channelId"`
	// ClientRequestID 是桌面首发请求提供的可选幂等标识；历史或测试数据可以为空。
	ClientRequestID *string `gorm:"column:client_request_id;type:varchar(64);uniqueIndex:idx_channel_thread_client_request,priority:2" json:"-"`
	// ClientRequestHash 是首发业务内容的规范化摘要，用于拒绝同键不同内容。
	ClientRequestHash string `gorm:"column:client_request_hash;type:varchar(64);not null;default:''" json:"-"`
	// Title 是团队对话列表使用的当前标题。
	Title string `gorm:"column:title;type:varchar(200);not null;index:idx_thread_board_title,priority:1" json:"title"`
	// LeadWakerID 是首发时冻结的唯一团队负责人伙伴标识。
	LeadWakerID string `gorm:"column:lead_waker_id;type:varchar(32);not null" json:"leadWakerId"`
	// LeadChatID 是负责人用于成员执行、团队规划、协调和最终汇总的同一个 Chat 标识。
	LeadChatID string `gorm:"column:lead_chat_id;type:varchar(32);not null;uniqueIndex" json:"leadChatId"`
	// ProjectID 是首发时冻结的可选项目标识。
	ProjectID string `gorm:"column:project_id;type:varchar(32);not null;default:'';index" json:"projectId,omitempty"`
	// ProjectName 是首发时冻结的项目或临时目录展示名称。
	ProjectName string `gorm:"column:project_name;type:varchar(80);not null;default:''" json:"projectName"`
	// WorkspaceDirectory 是首发时由负责人 Chat 解析出的受管工作区。
	WorkspaceDirectory string `gorm:"column:workspace_directory;type:text;not null" json:"workspaceDirectory"`
	// AdditionalWorkingDirectories 是首发时冻结并授权给全部成员 Chat 的真实目录。
	AdditionalWorkingDirectories []string `gorm:"column:additional_working_directories;type:text;not null;serializer:json" json:"additionalWorkingDirectories"`
	// MaxParallel 是该对话允许同时运行的团队任务数量快照。
	MaxParallel int `gorm:"column:max_parallel;not null;default:3" json:"maxParallel"`
	// MaxTasks 是该对话允许创建的任务总数快照。
	MaxTasks int `gorm:"column:max_tasks;not null;default:12" json:"maxTasks"`
	// TaskCount 是该对话已经原子占用的任务名额，只随聚合删除而清零。
	TaskCount int `gorm:"column:task_count;not null;default:0" json:"-"`
	// MaxFailureAttempts 是单个任务允许的失败尝试总数快照。
	MaxFailureAttempts int `gorm:"column:max_failure_attempts;not null;default:2" json:"maxFailureAttempts"`
	// ProvisioningID 是首次发送持久锚点与 staging 工作区共同使用的唯一标识。
	ProvisioningID string `gorm:"column:provisioning_id;type:varchar(32);not null;default:'';index" json:"-"`
	// ReadyAt 是全部成员 Chat 与快照完成后的可选 Unix 毫秒时间；空表示仍在补偿窗口。
	ReadyAt *int64 `gorm:"column:ready_at;index" json:"readyAt,omitempty"`
	// CancelingAt 是停止全部工作开始的 Unix 毫秒时间；非空时禁止接纳或派发新工作。
	CancelingAt *int64 `gorm:"column:canceling_at;index" json:"cancelingAt,omitempty"`
	// CreatedAt 是首次发送原子创建对话时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是团队对话最近发生持久变化时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli;index;index:idx_thread_board_activity,priority:1,sort:desc;index:idx_thread_board_executor,priority:2,sort:desc" json:"updatedAt"`
	// ActivityState 是列表查询按任务与成员 Chat 聚合出的只读活动状态。
	ActivityState constants.ChannelActivityState `gorm:"column:activity_state;->;-:migration" json:"activityState,omitempty"`
}

// TableName 返回团队对话使用的表名。
func (ChannelThread) TableName() string { return "channel_threads" }

// ChannelThreadMember 保存一位团队成员在团队对话中的固定 Chat 映射和可调执行配置。
type ChannelThreadMember struct {
	// ThreadID 是成员快照所属团队对话标识。
	ThreadID string `gorm:"column:thread_id;type:varchar(32);primaryKey" json:"threadId"`
	// WakerID 是快照成员对应的伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);primaryKey;index" json:"wakerId"`
	// DisplayName 是首次发送时冻结的成员展示名称，伙伴后续改名或删除不影响历史对话。
	DisplayName string `gorm:"column:display_name;type:varchar(80);not null;default:''" json:"displayName"`
	// Avatar 是首次发送时冻结的成员头像，伙伴后续改动不影响历史对话。
	Avatar string `gorm:"column:avatar;type:text" json:"avatar"`
	// RoleName 是首次发送时冻结并裁剪的成员角色名称。
	RoleName string `gorm:"column:role_name;type:varchar(80);not null;default:''" json:"roleName"`
	// Bio 是首次发送时冻结并裁剪的成员简介。
	Bio string `gorm:"column:bio;type:text;not null;default:''" json:"bio"`
	// Expertise 是首次发送时冻结并裁剪的成员专长摘要。
	Expertise []profile.Expertise `gorm:"column:expertise;type:text;not null;serializer:json" json:"expertise"`
	// ChatID 是该成员在当前团队对话中始终复用的独立 Chat 标识。
	ChatID string `gorm:"column:chat_id;type:varchar(32);not null;uniqueIndex" json:"chatId"`
	// IsLead 表示该成员是否为首发时冻结的唯一团队负责人。
	IsLead bool `gorm:"column:is_lead;not null;default:false" json:"isLead"`
	// RuntimeID 是该成员后续新执行使用的 Runtime。
	RuntimeID string `gorm:"column:runtime_id;type:varchar(120);not null" json:"runtimeId"`
	// AccountID 是该成员后续新执行使用的账号；system 表示原生登录。
	AccountID string `gorm:"column:account_id;type:varchar(32);not null;default:''" json:"accountId"`
	// Model 是该成员后续新执行使用的模型。
	Model string `gorm:"column:model;type:varchar(200);not null;default:''" json:"model"`
	// Reasoning 是该成员后续新执行使用的推理强度。
	Reasoning string `gorm:"column:reasoning;type:varchar(80);not null;default:''" json:"reasoning"`
	// PermissionLevel 是该成员后续新执行使用的权限等级。
	PermissionLevel constants.PermissionLevel `gorm:"column:permission_level;type:varchar(24);not null" json:"permissionLevel"`
	// Position 是成员在首发团队快照中的稳定顺序。
	Position int `gorm:"column:position;not null;default:0" json:"position"`
	// CreatedAt 是成员对话快照建立时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是成员执行配置最近调整时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回团队对话成员快照使用的表名。
func (ChannelThreadMember) TableName() string { return "channel_thread_members" }

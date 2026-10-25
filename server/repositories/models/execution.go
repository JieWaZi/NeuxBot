package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// Execution 保存一次 Chat 执行在服务边界流转的领域状态。
//
// 历史执行事实只写入 Chat JSONL；该结构不注册为 GORM 表，也不形成 SQLite 历史投影。
type Execution struct {
	// AutomaticRunID 是原自动运行的受信来源，不由模型或任务正文提供。
	AutomaticRunID string `json:"automaticRunId,omitempty"`
	// SubmissionID 是入口无关的稳定输入身份，写入同一权威执行事实。
	SubmissionID string `json:"submissionId,omitempty"`
	// SubmissionHash 是规范化输入和冻结配置的摘要，拒绝同键异内容。
	SubmissionHash string `json:"submissionHash,omitempty"`
	// AccountID 是本次冻结的账号标识；空值使用智能体默认配置。
	AccountID string `gorm:"type:varchar(32);not null;default:''" json:"accountId"`
	// ID 是 32 位无连字符 UUID。
	ID string `json:"id"`
	// ChatID 是本次执行所属的 Chat 标识。
	ChatID string `json:"chatId,omitempty"`
	// UserMessageID 是触发本次执行的用户消息标识。
	UserMessageID string `json:"userMessageId,omitempty"`
	// ParentExecutionID 是触发本次执行的父执行标识，普通用户回合为空。
	ParentExecutionID string `json:"parentExecutionId,omitempty"`
	// RuntimeID 是本次执行选择的开放 Runtime 标识。
	RuntimeID string `json:"runtimeId,omitempty"`
	// Model 是本次执行选择的模型标识。
	Model string `json:"model,omitempty"`
	// Reasoning 是本次执行选择的推理强度标识。
	Reasoning string `json:"reasoning,omitempty"`
	// PermissionLevel 是本次执行冻结的跨 Runtime 产品权限等级。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// WorkspaceReadOnly 表示本次 Runtime 只能读取工作区且不能申请文件修改。
	WorkspaceReadOnly bool `json:"workspaceReadOnly"`
	// PluginResources 是接纳时冻结的插件身份、版本与配置修订。
	PluginResources []plugins.Snapshot `json:"pluginResources,omitempty"`
	// Status 是执行当前生命周期状态。
	Status constants.ExecutionStatus `json:"status"`
	// Revision 是执行生命周期状态变化后递增的版本。
	Revision int64 `json:"revision"`
	// ErrorCode 是失败时供调用方本地化的稳定错误码。
	ErrorCode string `json:"errorCode,omitempty"`
	// CompletedAt 是执行进入终态时的 Unix 毫秒时间。
	CompletedAt *int64 `json:"completedAt,omitempty"`
	// CreatedAt 是领域对象建立时的 Unix 毫秒时间。
	CreatedAt int64 `json:"createdAt"`
	// UpdatedAt 是领域状态最近变化时的 Unix 毫秒时间。
	UpdatedAt int64 `json:"updatedAt"`
}

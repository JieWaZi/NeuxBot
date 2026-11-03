package models

import (
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkflowCreationSession{})
	dbhelper.RegisterTable(&WorkflowAnnotation{})
}

// WorkflowCreationSession 绑定真实创作 Chat 和当前执行能力，回复不构成源码回执。
type WorkflowCreationSession struct {
	// ID 是创作会话身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// DefinitionID 是唯一可修改的工作流。
	DefinitionID string `gorm:"type:varchar(32);not null;index" json:"definitionId"`
	// WakerID 是不可在伙伴页面展示的内置工作流执行身份。
	WakerID string `json:"-"`
	// ChatID 是独立创作 Chat。
	ChatID string `gorm:"index" json:"chatId"`
	// ActiveExecutionID 是当前拥有修改工具的真实执行。
	ActiveExecutionID string `json:"activeExecutionId"`
	// AcceptedExecutionID 是已经返回权威接纳收据的本轮执行。
	AcceptedExecutionID string `json:"acceptedExecutionId"`
	// RequestID 是当前创作消息的幂等身份。
	RequestID string `json:"requestId"`
	// Content 是尚未形成 Core 接纳收据的完整用户要求。
	Content string `gorm:"type:text;not null" json:"-"`
	// ContextInstructions 是当前轮已验证引用的执行上下文，与接纳锚点同存，不进入用户消息。
	ContextInstructions []workinput.Instruction `gorm:"serializer:json;type:text" json:"-"`
	// Defaults 是本轮用户选择的真实执行配置。
	Defaults WorkflowExecutionDefaults `gorm:"serializer:json;type:text;not null" json:"defaults"`
	// AuthToken 是当前轮能力凭证，不向 API 公开。
	AuthToken string `json:"-"`
	// LastSavedRevisionID 是工具提交的最新回执，不由回复解析。
	LastSavedRevisionID string `json:"lastSavedRevisionId"`
	// CreatedAt 是创建时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最后一轮或保存时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

func (WorkflowCreationSession) TableName() string { return "workflow_creation_sessions" }

// WorkflowAnnotation 是绑定版本与稳定职责的待处理修改要求。
type WorkflowAnnotation struct {
	// ID 是批注身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// DefinitionID 是所属工作流。
	DefinitionID string `gorm:"type:varchar(32);not null;index" json:"definitionId"`
	// BaseRevisionID 是用户阅读的基准版本。
	BaseRevisionID string `json:"baseRevisionId"`
	// NodeID 是目标稳定节点或阶段身份。
	NodeID string `json:"nodeId"`
	// Text 是完整修改要求。
	Text string `gorm:"type:text;not null" json:"text"`
	// ResolvedRevisionID 是实际处理批注的保存版本，空值仍待处理。
	ResolvedRevisionID string `json:"resolvedRevisionId"`
	// CreatedAt 是建立时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

func (WorkflowAnnotation) TableName() string { return "workflow_annotations" }

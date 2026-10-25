package models

import (
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&WorkflowDefinition{})
	dbhelper.RegisterTable(&WorkflowRevision{})
	dbhelper.RegisterTable(&WorkflowCommandReceipt{})
	dbhelper.RegisterTable(&WorkflowEvent{})
}

// WorkflowScopeBinding 是用户选定的工作范围，不是流程逻辑的副本。
type WorkflowScopeBinding struct {
	// ProjectID 是真实项目身份，空值表示无项目。
	ProjectID string `json:"projectId"`
	// WorkingDirectory 是明确批准的目录，空值时由运行创建受管目录。
	WorkingDirectory string `json:"workingDirectory"`
}

// WorkflowDefinition 只拥有稳定身份、当前版本及创作和范围关系。
type WorkflowDefinition struct {
	// ID 是工作流的稳定身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// Title 是 Agent 从流程目标生成的标题，空定义尚未命名。
	Title string `gorm:"type:varchar(120);not null" json:"title"`
	// Description 是当前版本的业务用途。
	Description string `gorm:"type:text;not null" json:"description"`
	// WhenToUse 是当前版本的使用场景。
	WhenToUse string `gorm:"type:text;not null" json:"whenToUse"`
	// CurrentRevisionID 指向完整保存的不可变版本，空定义为空。
	CurrentRevisionID string `gorm:"type:varchar(32);not null;default:''" json:"currentRevisionId"`
	// Ordinal 是最后成功保存的版本序号。
	Ordinal int64 `gorm:"not null;default:0" json:"ordinal"`
	// CreationWakerID 是服务端绑定的工作流内置伙伴身份。
	CreationWakerID string `gorm:"type:varchar(32);not null" json:"-"`
	// ScopeBinding 是用户选定的批准范围。
	ScopeBinding WorkflowScopeBinding `gorm:"serializer:json;type:text;not null" json:"scopeBinding"`
	// CreatedAt 是建立时间的 Unix 毫秒。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是当前版本或资源关系最近变化时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

func (WorkflowDefinition) TableName() string { return "workflow_definitions" }

// WorkflowRevision 是原始源码、投影与 Go 指令的不可变绑定。
type WorkflowRevision struct {
	// ID 是不可变版本身份。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// DefinitionID 是所属工作流身份。
	DefinitionID string `gorm:"type:varchar(32);not null;uniqueIndex:workflow_revision_ordinal;index" json:"definitionId"`
	// Ordinal 是所属工作流内单调增加的版本序号。
	Ordinal int64 `gorm:"not null;uniqueIndex:workflow_revision_ordinal" json:"ordinal"`
	// Source 是唯一权威的原始 UTF-8 源码。
	Source string `gorm:"type:text;not null" json:"-"`
	// SourceDigest 是原始源码字节摘要。
	SourceDigest string `gorm:"type:varchar(64);not null" json:"sourceDigest"`
	// ManifestDigest 绑定此版本的只读投影。
	ManifestDigest string `gorm:"type:varchar(64);not null" json:"manifestDigest"`
	// IRDigest 绑定此版本的执行指令。
	IRDigest string `gorm:"type:varchar(64);not null" json:"irDigest"`
	// SDKVersion 是创作源码的原生契约版本。
	SDKVersion string `gorm:"type:varchar(80);not null" json:"sdkVersion"`
	// CompilerVersion 是保存时固定的编译器版本。
	CompilerVersion string `gorm:"type:varchar(80);not null" json:"compilerVersion"`
	// IRVersion 是保存时固定的持久指令格式版本。
	IRVersion int `gorm:"not null" json:"irVersion"`
	// Manifest 是仅供阅读的图与节点详情。
	Manifest workflowdsl.Manifest `gorm:"serializer:json;type:text;not null" json:"manifest"`
	// Bundle 是 Go Runner 唯一使用的执行程序。
	Bundle workflowdsl.ProgramBundle `gorm:"serializer:json;type:text;not null" json:"-"`
	// CreationSessionID 关联产生此版本的真实创作会话。
	CreationSessionID string `gorm:"type:varchar(32);not null;default:''" json:"creationSessionId"`
	// RestoredFromRevisionID 记录恢复的原版本，不移动历史指针。
	RestoredFromRevisionID string `gorm:"type:varchar(32);not null;default:''" json:"restoredFromRevisionId"`
	// CreatedAt 是版本提交时间的 Unix 毫秒。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// DeletedAt 是从历史列表移除的时间；源码与指令继续供既有运行读取。
	DeletedAt *int64 `gorm:"index" json:"-"`
}

func (WorkflowRevision) TableName() string { return "workflow_revisions" }

// WorkflowCommandReceipt 保存同作用域请求的不可变结果，避免重复产生副作用。
type WorkflowCommandReceipt struct {
	// AggregateID 是请求拥有的工作流或运行身份。
	AggregateID string `gorm:"type:varchar(64);primaryKey" json:"aggregateId"`
	// RequestID 是调用方复用的稳定请求身份。
	RequestID string `gorm:"type:varchar(128);primaryKey" json:"requestId"`
	// Kind 区分创建、保存、运行与回答命令。
	Kind string `gorm:"type:varchar(32);not null" json:"kind"`
	// PayloadDigest 拒绝相同请求身份的异内容重放。
	PayloadDigest string `gorm:"type:varchar(64);not null" json:"payloadDigest"`
	// Result 是首次提交后可以重放的非秘密回执。
	Result map[string]any `gorm:"serializer:json;type:text;not null" json:"result"`
	// CreatedAt 是回执提交时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

func (WorkflowCommandReceipt) TableName() string { return "workflow_command_receipts" }

// WorkflowEvent 是提交后 SSE 与断线重放共用的有界持久事件。
type WorkflowEvent struct {
	// StreamID 是拥有独立连续序列的工作流或运行身份。
	StreamID string `gorm:"type:varchar(64);primaryKey" json:"streamId"`
	// Sequence 是该聚合内单调连续的事件序号。
	Sequence int64 `gorm:"primaryKey;autoIncrement:false" json:"sequence"`
	// EventID 是事件的稳定去重身份。
	EventID string `gorm:"type:varchar(32);not null;uniqueIndex" json:"eventId"`
	// Type 是定义、版本、运行或实例变化的事件名称。
	Type string `gorm:"type:varchar(80);not null" json:"type"`
	// EntityID 是事件指向的具体对象。
	EntityID string `gorm:"type:varchar(32);not null" json:"entityId"`
	// Payload 只含有界摘要，不复制伙伴消息或账号凭据。
	Payload map[string]any `gorm:"serializer:json;type:text;not null" json:"payload"`
	// CreatedAt 是提交时间的 Unix 毫秒。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

func (WorkflowEvent) TableName() string { return "workflow_events" }

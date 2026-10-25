package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&KnowledgeConnection{})
	dbhelper.RegisterTable(&KnowledgeOperationLink{})
}

// KnowledgeConnection 保存连接配置，不保存知识内容或远端凭据正文。
type KnowledgeConnection struct {
	// ID 是本机稳定连接身份。
	ID string `gorm:"primaryKey" json:"id"`
	// Backend 是存量 weknora/custom 或统一 integration 协议。
	Backend string `json:"backend"`
	// Name 是用户可见名称。
	Name string `json:"name"`
	// BaseURL 是已验证的服务基址。
	BaseURL string `json:"baseUrl"`
	// CredentialRef 是安全凭据引用，禁止返回 Renderer。
	CredentialRef string `json:"-"`
	// AuthMode 选择远端认证协议，不包含凭据内容。
	AuthMode string `json:"authMode"`
	// Revision 是连接配置的乐观并发版本。
	Revision int64 `json:"revision"`
	// Detection 保存当前连接版本的只读识别证据；凭据或地址变化必须重新检测。
	Detection remote.Detection `gorm:"serializer:json;type:text" json:"detection"`
	// Enabled 控制是否允许通过此连接发起请求。
	Enabled bool `json:"enabled"`
}

// KnowledgeOperationLink 连接两个独立库的真实回执，不复制任务状态机。
type KnowledgeOperationLink struct {
	// TransferState 仅保存客户端发送及响应事实，不表示远端知识处理完成。
	TransferState constants.RemoteTransferState `gorm:"not null;default:''" json:"-"`
	// UploadMode 固定发送时已确认的处理模式。
	UploadMode string `json:"-"`
	// NativeUpload 表示第三方原生接纳，不伪造自有知识核心回执。
	NativeUpload bool `gorm:"not null;default:false" json:"-"`
	// CreatedAt 是本机操作引用首次登记时间，不代表远端任务执行时间。
	CreatedAt int64 `gorm:"index:idx_knowledge_chat_operation,priority:3" json:"-"`
	// ConnectionRevision 固定发出远端请求时的连接版本，防止重配后向其他服务器对账。
	ConnectionRevision int64 `json:"-"`
	// RemoteErrorCode 仅记录已确认拒绝的稳定错误码，零表示仍可能存在远端提交。
	RemoteErrorCode int64 `json:"-"`
	// HostState 仅表示宿主驱动active/blocked/idle，不替代核心知识任务终态。
	HostState string `json:"-"`
	// HostErrorCode 是宿主失败的稳定错误码，不含原始私有参数。
	HostErrorCode string `json:"-"`
	// HostDiagnostic 是可展示且不含账号、路径和来源正文的失败说明。
	HostDiagnostic string `json:"-"`

	// ParentRequestID 是远端聊天多步操作的稳定父请求引用。
	ParentRequestID string `json:"-"`
	// ReceiptRequestID 关联远端聊天父步骤及实际发送的幂等请求身份。
	ReceiptRequestID string `json:"-"`
	// ReceiptConfirmed 表示已按权威回执补齐引用，不复制任务运行状态。
	ReceiptConfirmed bool `gorm:"index;index:idx_knowledge_operation_ref,priority:4" json:"-"`
	// RequestID 是主库和核心共用的稳定幂等身份。
	RequestID string `gorm:"primaryKey" json:"requestId"`
	// Operation 是实际领域操作。
	Operation string `gorm:"index:idx_knowledge_chat_operation,priority:2;index:idx_knowledge_retry_authority,priority:1" json:"operation"`
	// Digest 保护相同请求不可替换内容。
	Digest string `json:"-"`
	// Backend 是完整资源协议。
	Backend string `gorm:"index:idx_knowledge_operation_ref,priority:1" json:"backend"`
	// ConnectionID 是完整资源连接身份。
	ConnectionID string `gorm:"index:idx_knowledge_operation_ref,priority:2" json:"connectionId"`
	// KnowledgeBaseID 是核心回执确认的库身份。
	KnowledgeBaseID string `gorm:"index:idx_knowledge_operation_ref,priority:3" json:"knowledgeBaseId"`
	// ResourceID 是核心回执确认的资源身份。
	ResourceID string `json:"resourceId"`
	// ExpectedResourceID 冻结写入前已知的目标身份，供未知结果对账校验，不代表已提交。
	ExpectedResourceID string `gorm:"index:idx_knowledge_retry_authority,priority:2" json:"-"`
	// JobID 是核心知识任务引用。
	JobID string `gorm:"index" json:"jobId"`
	// ChatID 是用户通过普通聊天发起操作时的聊天引用。
	ChatID string `gorm:"index:idx_knowledge_chat_operation,priority:1" json:"chatId"`
	// ExecutionID 是用户通过普通聊天发起操作时的执行引用，不参与知识模型调用。
	ExecutionID string `gorm:"index" json:"executionId"`
	// TargetAttempt 标记重试候选对应的核心尝试号，只有真实核心回执可使候选生效。
	TargetAttempt int `gorm:"index:idx_knowledge_retry_authority,priority:3" json:"-"`
	// TargetVersion 是接纳重试前的核心任务版本，用于核对回执。
	TargetVersion int64 `json:"-"`
	// ConfigurationID 是知识作业独立冻结模型选择的引用。
	ConfigurationID string `gorm:"index" json:"configurationId"`
}

// TableName 返回本机知识引用表名。
func (KnowledgeConnection) TableName() string { return "knowledge_connections" }

// TableName 返回本机知识引用表名。
func (KnowledgeOperationLink) TableName() string { return "knowledge_operation_links" }

// KnowledgeMaintenanceAuthorization 记录用户明确允许的精确维护范围。
type KnowledgeMaintenanceAuthorization struct {
	// 服务类型
	Backend string `json:"backend" validate:"required,oneof=local"`
	// 连接标识
	ConnectionID string `json:"connectionId" validate:"required"`
	// 知识库标识
	KnowledgeBaseID string `json:"knowledgeBaseId" validate:"required"`
	// 维护操作
	Operation string `json:"operation" validate:"required,oneof=organize"`
	// 附件序号
	AttachmentIndexes []int `json:"attachmentIndexes" validate:"max=100"`
	// 来源版本
	SourceVersionIDs []string `json:"sourceVersionIds" validate:"max=100"`
}

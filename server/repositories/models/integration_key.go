package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&IntegrationKey{})
	dbhelper.RegisterTable(&IntegrationKeyMutation{})
}

// TableName 返回集成主体表名。
func (IntegrationKey) TableName() string { return "integration_keys" }

// TableName 返回密钥签发回执表名。
func (IntegrationKeyMutation) TableName() string { return "integration_key_mutations" }

// IntegrationGrant 保存同类能力集合与明确资源集合，不维护逐资源权限矩阵。
type IntegrationGrant struct {
	// ResourceKind 是本组资源的类型。
	ResourceKind constants.IntegrationResourceKind `json:"resourceKind"`
	// ResourceIDs 是保存时明确展开的资源身份，空集不授予访问。
	ResourceIDs []string `json:"resourceIds"`
	// Capabilities 应用于本组全部资源。
	Capabilities []constants.IntegrationCapability `json:"capabilities"`
}

// IntegrationKey 是应用颁发的稳定集成主体，修改授权不会更换凭据。
type IntegrationKey struct {
	// ID 是密钥的稳定调用主体。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// Name 是本机用户为集成填写的名称。
	Name string `json:"name"`
	// KeyHash 是完整凭据的 SHA-256 摘要，永不返回调用方。
	KeyHash string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	// DisplayPrefix 只供区分密钥，不包含可恢复凭据。
	DisplayPrefix string `json:"displayPrefix"`
	// CredentialGeneration 固定签发时的凭据代次，用于核对已接纳知识任务。
	CredentialGeneration int64 `gorm:"not null" json:"credentialGeneration"`
	// GrantVersion 在有效授权变化时递增，冻结的知识工作不得扩权。
	GrantVersion int64 `gorm:"not null" json:"grantVersion"`
	// Revision 用于管理页的条件修改。
	Revision int64 `gorm:"not null" json:"revision"`
	// Grants 是每类最多一组的明确资源与能力。
	Grants []IntegrationGrant `gorm:"serializer:json;type:text" json:"grants"`
	// Enabled 表示当前是否接受该主体认证。
	Enabled bool `gorm:"not null" json:"enabled"`
	// ExpiresAt 是可选的凭据失效时间，单位为 Unix 毫秒。
	ExpiresAt *int64 `json:"expiresAt"`
	// RevokedAt 保留已撤销记录的失效事实，管理页可删除此类记录。
	RevokedAt *int64 `json:"revokedAt"`
	// LastUsedAt 是节流后的最近成功认证时间。
	LastUsedAt *int64 `json:"lastUsedAt"`
	// CreatedAt 是首次签发的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近管理修改的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// IntegrationKeyMutation 记录签发意图，回执不保存可回显秘密。
type IntegrationKeyMutation struct {
	// RequestID 是本机用户动作的稳定请求身份。
	RequestID string `gorm:"primaryKey" json:"requestId"`
	// Digest 绑定该动作完整的非秘密输入。
	Digest string `json:"-"`
	// KeyID 是已预留的稳定主体。
	KeyID string `gorm:"index" json:"keyId"`
	// Action 固定签发动作类型，历史回执不允许重新签发。
	Action string `json:"action"`
	// Completed 表示摘要与主体已原子写入；未完成意图不自动重新签发。
	Completed bool `json:"completed"`
	// CreatedAt 是意图登记的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

// IntegrationIdentity 只包含已由服务端验证的主体与授权代次。
type IntegrationIdentity struct {
	// KeyID 是稳定调用主体。
	KeyID string `json:"keyId"`
	// CredentialGeneration 是接纳时的凭据代次。
	CredentialGeneration int64 `json:"credentialGeneration"`
	// GrantVersion 是接纳时的授权版本。
	GrantVersion int64 `json:"grantVersion"`
}

// IntegrationAuthorization 保存逐次提交所需的受信身份，不接受客户端自报。
type IntegrationAuthorization struct {
	// Scope 固定同一主体的实际授权来源范围。
	Scope []string `json:"scope,omitempty"`
	// Identity 是首次接纳的授权身份。
	Identity IntegrationIdentity `json:"identity"`
	// ResourceKind 是原目标的资源类型。
	ResourceKind constants.IntegrationResourceKind `json:"resourceKind"`
	// ResourceID 是原目标的实际资源身份。
	ResourceID string `json:"resourceId"`
	// Required 是原操作要求的全部能力。
	Required []constants.IntegrationCapability `json:"required"`
	// RemoteAddr 是原请求实际 TCP 对端。
	RemoteAddr string `json:"remoteAddr"`
	// ForwardedFor 是仅经明确受信代理解释的原转发链。
	ForwardedFor string `json:"forwardedFor"`
}

// IntegrationInvocation 记录自动任务调用来源，任务知识范围仍由原运行配置拥有。
type IntegrationInvocation struct {
	// Identity 是调用任务时已认证的主体。
	Identity IntegrationIdentity `json:"identity"`
	// TaskID 是最初授权调用的任务。
	TaskID string `json:"taskId"`
	// TaskConfigurationVersion 只用于追溯原冻结配置。
	TaskConfigurationVersion int64 `json:"taskConfigurationVersion"`
	// RemoteAddr 是原请求的实际对端。
	RemoteAddr string `json:"remoteAddr"`
	// ForwardedFor 是原受信代理链。
	ForwardedFor string `json:"forwardedFor"`
}

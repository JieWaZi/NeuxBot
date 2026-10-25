package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() { dbhelper.RegisterTable(&AIAccount{}); dbhelper.RegisterTable(&AIAccountStorageKey{}) }

// AIAccount 保存可由智能体默认配置及聊天选择的受管账号。
type AIAccount struct {
	// ID 是账号的稳定标识。
	ID string `gorm:"type:varchar(32);primaryKey" json:"id"`
	// Name 是去除首尾空白后全局唯一的账号名称。
	Name string `gorm:"type:varchar(120);not null;uniqueIndex" json:"name"`
	// AuthMode 区分 API Key 与官方订阅登录。
	AuthMode constants.AccountAuthMode `gorm:"type:varchar(24);not null" json:"authMode"`

	// Endpoints 保存账号唯一的请求协议及服务地址；数组形式兼容已有存储。
	Endpoints []AIAccountEndpoint `gorm:"type:text;not null;serializer:json" json:"endpoints"`
	// ModelCatalog 保存最近一次从该账号服务获取的模型标识。
	ModelCatalog []string `gorm:"type:text;not null;default:'[]';serializer:json" json:"modelCatalog"`
	// SelectedModels 保存用户允许在聊天中使用的该账号模型标识。
	SelectedModels []string `gorm:"type:text;not null;default:'[]';serializer:json" json:"selectedModels"`
	// CustomModels 保存该账号需要手工补充到模型目录中的模型标识。
	CustomModels []string `gorm:"type:text;not null;default:'[]';serializer:json" json:"customModels"`
	// ModelContextSizes 保存各模型的上下文长度，未设置时由原生智能体使用默认值。
	ModelContextSizes map[string]int `gorm:"type:text;not null;default:'{}';serializer:json" json:"modelContextSizes"`
	// ModelDisplayNames 保存模型在聊天和 Codex 菜单中的显示名称，键为实际请求模型 ID。
	ModelDisplayNames map[string]string `gorm:"type:text;not null;default:'{}';serializer:json" json:"modelDisplayNames"`
	// ModelReasoningLevels 保存用户为实际模型声明的原生推理等级。
	ModelReasoningLevels map[string][]string `gorm:"type:text;not null;default:'{}';serializer:json" json:"modelReasoningLevels"`
	// ClaudeModelRoles 保存 Claude Code 各内置模型角色对应的实际请求模型 ID。
	ClaudeModelRoles map[string]string `gorm:"type:text;not null;default:'{}';serializer:json" json:"claudeModelRoles"`
	// DefaultModel 是选择该账号时优先使用的模型标识。
	DefaultModel string `gorm:"type:varchar(200);not null;default:''" json:"defaultModel"`
	// DefaultReasoning 是选择该账号时优先使用的推理强度标识。
	DefaultReasoning string `gorm:"type:varchar(80);not null;default:''" json:"defaultReasoning"`
	// LiveAuthPath 仅保留旧版单执行认证投射的恢复入口，新调用通过账号刷新权威持久化。
	LiveAuthPath string `gorm:"type:text;not null;default:''" json:"-"`
	// Revision 用于更新冲突检测和隔离进程缓存。
	Revision int64 `gorm:"not null" json:"revision"`
	// CredentialCiphertext 是绑定账号身份加密后的 Key 或订阅令牌，不对外返回。
	CredentialCiphertext string `gorm:"type:text;not null" json:"-"`
	// HasCredential 表示已保存 API Key 或完整订阅令牌。
	HasCredential bool `gorm:"not null" json:"hasCredential"`
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近修改时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回受管 AI 账号表名。
func (AIAccount) TableName() string { return "ai_accounts" }

// AIAccountStorageKey 保存当前数据库的账号凭据加密密钥。
type AIAccountStorageKey struct {
	// ID 是当前数据库唯一的固定密钥标识。
	ID string `gorm:"primaryKey" json:"-"`
	// Key 是随机生成的 AES-256 密钥，不对外返回。
	Key []byte `gorm:"not null" json:"-"`
}

// TableName 返回账号密钥表名。
func (AIAccountStorageKey) TableName() string { return "ai_account_storage_keys" }

// AIAccountEndpoint 是同一账号原生支持的一种协议入口。
type AIAccountEndpoint struct {
	// 协议类型
	Protocol constants.AccountProtocol `json:"protocol" validate:"required"`
	// 服务地址
	BaseURL string `json:"baseUrl" validate:"required,max=4096"`
}

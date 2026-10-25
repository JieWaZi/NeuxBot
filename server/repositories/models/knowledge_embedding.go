package models

import (
	"encoding/json"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&LibraryEmbeddingProfile{})
	dbhelper.RegisterTable(&LibraryEmbeddingSecret{})
	dbhelper.RegisterTable(&LibraryEmbeddingMutation{})
}

// LibraryEmbeddingProfile 是按库隔离的唯一嵌入服务配置，不继承记忆配置。
type LibraryEmbeddingProfile struct {
	// LibraryID 是唯一所属本地库。
	LibraryID string `gorm:"primaryKey" json:"libraryId"`
	// Version 是独立配置CAS版本，未配置为零。
	Version int64 `json:"version"`
	// LibraryVersion 是该次保存成功时核心库版本，GET投影当前版本。
	LibraryVersion int64 `json:"libraryVersion"`
	// BaseURL 是OpenAI兼容嵌入服务地址。
	BaseURL string `json:"baseUrl"`
	// Model 是此库独立选择的模型。
	Model string `json:"model"`
	// HasAPIKey 表示存在该库专属加密凭据。
	HasAPIKey bool `json:"hasApiKey"`
	// UpdatedAt 是保存时间，单位毫秒。
	UpdatedAt int64 `json:"updatedAt"`
}

func (LibraryEmbeddingProfile) TableName() string { return "knowledge_library_embedding_profiles" }

// LibraryEmbeddingSecret 仅保存绑定库身份的密文。
type LibraryEmbeddingSecret struct {
	// LibraryID 是密文AAD归属。
	LibraryID string `gorm:"primaryKey" json:"-"`
	// Ciphertext 不返回API或日志。
	Ciphertext string `json:"-"`
}

func (LibraryEmbeddingSecret) TableName() string { return "knowledge_library_embedding_secrets" }

// LibraryEmbeddingMutation 冻结跨核心与宿主的请求，未知结果可重放原身份。
type LibraryEmbeddingMutation struct {
	// RequestID 是原请求稳定身份。
	RequestID string `gorm:"primaryKey" json:"-"`
	// LibraryID 限定回执归属。
	LibraryID string `gorm:"index" json:"-"`
	// Digest 固定显式用户输入，不存明文凭据。
	Digest string `json:"-"`
	// ExpectedVersion 是宿主配置预期版本。
	ExpectedVersion int64 `json:"-"`
	// CoreInput 固定派生核心更新字段，避免重试覆盖后来配置。
	CoreInput json.RawMessage `gorm:"type:text" json:"-"`
	// Ciphertext 是待提交密钥的加密快照。
	Ciphertext string `json:"-"`
	// Snapshot 是本次不可变配置回执。
	Snapshot LibraryEmbeddingProfile `gorm:"serializer:json;type:text" json:"-"`
	// Committed 仅在宿主配置与回执同事务提交后为真。
	Committed bool `json:"-"`
}

func (LibraryEmbeddingMutation) TableName() string { return "knowledge_library_embedding_mutations" }

package models

import (
	"encoding/json"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() { dbhelper.RegisterTable(&KnowledgeTaskConfiguration{}) }

// KnowledgeTaskConfiguration 保存知识作业首次接纳的模型选择，与聊天和伙伴生命周期独立。
type KnowledgeTaskConfiguration struct {
	// PublicationAuthorizations 保存每次已接纳外部重试累积的提交约束，不改变原授权。
	PublicationAuthorizations []json.RawMessage `gorm:"serializer:json;type:text" json:"-"`
	ID                        string            `gorm:"primaryKey" json:"id"`                  // 稳定接纳配置身份。
	Backend                   string            `json:"backend"`                               // 知识提供者类型。
	ConnectionID              string            `json:"connectionId"`                          // 完整连接身份。
	KnowledgeBaseID           string            `gorm:"index" json:"knowledgeBaseId"`          // 原生知识库身份。
	RuntimeID                 string            `json:"runtimeId"`                             // 冻结的智能体标识。
	AccountID                 string            `json:"accountId"`                             // 冻结的账号引用。
	Model                     string            `json:"model"`                                 // 冻结的模型标识。
	Message                   string            `json:"-"`                                     // 用户本次明确要求。
	RemoteSubject             string            `json:"-"`                                     // 服务端验证的实际主体。
	RemoteProfileID           string            `json:"-"`                                     // 服务端固定模型配置引用。
	RemoteAuthorization       json.RawMessage   `gorm:"type:text" json:"-"`                    // 不含凭据正文的可恢复授权身份。
	CreatedAt                 int64             `gorm:"autoCreateTime:milli" json:"createdAt"` // 配置首次保存的毫秒时间。
}

func (KnowledgeTaskConfiguration) TableName() string { return "knowledge_task_configurations" }

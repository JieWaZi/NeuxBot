package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() { dbhelper.RegisterTable(&KnowledgeLibrarySummary{}) }

// KnowledgeLibrarySummary 仅缓存远端展示摘要，不参与知识读写授权。
type KnowledgeLibrarySummary struct {
	// Backend 为远端协议类型。
	Backend string `gorm:"primaryKey"`
	// ConnectionID 为本机连接身份。
	ConnectionID string `gorm:"primaryKey"`
	// ConnectionRevision 隔离端点、凭据与权限配置变更。
	ConnectionRevision int64 `gorm:"primaryKey"`
	// KnowledgeBaseID 为远端知识库身份。
	KnowledgeBaseID string `gorm:"primaryKey"`
	// PageTotal 为最近一次未筛选查询返回的页面总数，零为已知空库。
	PageTotal int64
	// ObservedAt 为请求开始的毫秒时间，阻止旧响应覆盖较新的观察。
	ObservedAt int64
}

func (KnowledgeLibrarySummary) TableName() string { return "knowledge_library_summaries" }

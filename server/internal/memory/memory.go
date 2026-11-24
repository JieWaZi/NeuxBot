// Package memory 提供伙伴的长期记忆、检索及变更记录。
// 内容、关键词与向量在独立数据库中维护，写操作通过受限命令调用。
package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
)

// Memory 是一条伙伴的长期记忆。
//
// 内容与内容摘要分别用于阅读和精确去重。
type Memory struct {
	// Seq 是存储内部的自增序号，仅在存储层使用。
	Seq int64
	// ID 是记忆对外唯一标识，32 位无连字符 UUID。
	ID string
	// PartnerID 是记忆唯一属主。
	PartnerID string
	// Data 是记忆正文。
	Data string
	// Hash 是正文 MD5，仅用于 exact duplicate 判重，不承担密码学安全用途。
	Hash string
	// SourceRunID 是写入该记忆来源执行的标识，便于审计。
	SourceRunID string
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64
	// UpdatedAt 是最近更新时间的 Unix 毫秒值。
	UpdatedAt int64
	// Core 表示是否作为合格会话的核心上下文。
	Core bool
	// Revision 区分更新及用户接管，同一毫秒的编辑也不会合并。
	Revision uint64
	// Origin 是条目的首次写入来源。
	Origin constants.MemoryTimelineSource
	// control 记录正文接管或手动固定为核心的保护。
	control memoryControl
}

// memoryControl 保存在 user_controlled 列。已有的 1 保留为持续保护。
type memoryControl uint8

const (
	memoryControlAutomatic memoryControl = iota
	memoryControlUser
	memoryControlPinned
)

func (m Memory) reviewEditable() bool {
	return m.Origin == constants.MemoryTimelineSourceReview && m.control == memoryControlAutomatic
}

// ScoredMemory 是一次检索命中的记忆及其相似度。
type ScoredMemory struct {
	// Memory 是命中的记忆内容。
	Memory Memory
	// Score 是归一化相似度，越大越相似，落在 (0,1]，用于相似度排序。
	Score float64
}

// Context 是服务端绑定的伙伴记忆访问身份。
type Context struct {
	// RunID 是当前执行标识。
	RunID string
	// ChatID 是当前对话标识。
	ChatID string
	// PartnerID 是当前对话伙伴标识。
	PartnerID string
}

// WriteResult 是创建记忆的结果。
type WriteResult struct {
	// Memory 是写入或命中去重后返回的记忆。
	Memory Memory
	// Duplicate 表示本次写入命中 exact hash 去重，返回的是已有记忆。
	Duplicate bool
}

// SearchResult 是记忆检索的结果。
type SearchResult struct {
	// Results 是按相似度降序排列的命中记忆。
	Results []ScoredMemory
	// Mode 是本次检索实际使用的召回方式。
	Mode constants.MemoryRetrievalMode
	// Conflict 表示 Embedding 配置与已建索引指纹不一致，本次降级为关键词召回。
	Conflict bool
}

// Status 是记忆服务的当前检索与索引状态，供独立记忆页面展示。
type Status struct {
	// Mode 是当前检索召回方式。
	Mode constants.MemoryRetrievalMode
	// Configured 表示 Embedding 三项配置是否齐全。
	Configured bool
	// Conflict 表示 Embedding 配置与已建语义索引指纹不一致。
	Conflict bool
	// Model 是已建索引使用的 Embedding 模型名；尚未建立索引时为空。
	Model string
	// Dimensions 是已建索引的向量维度；尚未建立索引时为 0。
	Dimensions int
	// MemoryCount 是当前记忆总条数。
	MemoryCount int64
	// VectorCount 是当前语义索引中的向量条数。
	VectorCount int64
	// NeedsReindex 表示当前配置可用，但语义索引尚未覆盖全部记忆。
	NeedsReindex bool
}

// ListFilter 描述伙伴记忆列表与时间线使用的筛选和分页条件。
type ListFilter struct {
	// PartnerID 是要查询的伙伴标识。
	PartnerID string
	// Core 选择一个分类；为空时包含全部条目。
	Core *bool
	// Query 是记忆正文的可选关键词过滤条件。
	Query string
	// UpdatedAtStart 是最近更新时间下界的 Unix 毫秒值；0 表示不限。
	UpdatedAtStart int64
	// UpdatedAtEnd 是最近更新时间上界的 Unix 毫秒值；0 表示不限。
	UpdatedAtEnd int64
	// Offset 是分页起始偏移量。
	Offset int
	// Limit 是单页最大条数。
	Limit int
}

// MemoryPage 是一页当前记忆。
type MemoryPage struct {
	// Items 是当前页记忆。
	Items []Memory
	// Total 是符合条件的记忆总数。
	Total int64
}

// TimelinePage 是一页追加式记忆变更事件。
type TimelinePage struct {
	// Items 是当前页时间线事件。
	Items []HistoryEntry
	// Total 是符合条件的事件总数。
	Total int64
}

// EmbeddingSettings 是可安全展示的嵌入服务配置。
type EmbeddingSettings struct {
	// BaseURL 是 OpenAI 兼容服务的基础地址。
	BaseURL string
	// Model 是嵌入模型名。
	Model string
	// APIKeyConfigured 表示凭据是否已经保存。
	APIKeyConfigured bool
}

// EmbeddingSettingsUpdate 描述嵌入服务配置的显式更新语义。
type EmbeddingSettingsUpdate struct {
	// BaseURL 是更新后的基础地址，允许清空。
	BaseURL string
	// Model 是更新后的模型名，允许清空。
	Model string
	// APIKey 是本次写入的新凭据；空值表示保留旧凭据。
	APIKey string
	// ClearAPIKey 表示明确删除已保存凭据。
	ClearAPIKey bool
}

// ReindexResult 是一次语义索引重建的结果。
type ReindexResult struct {
	// VectorCount 是重建后语义索引中的向量条数。
	VectorCount int64
	// Model 是重建使用的 Embedding 模型名。
	Model string
	// Dimensions 是重建后的向量维度。
	Dimensions int
}

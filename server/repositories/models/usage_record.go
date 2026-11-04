package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&UsageRecord{})
	dbhelper.RegisterTable(&UsageCheckpoint{})
}

// UsageRecord is the rebuildable projection of one canonical provider terminal.
type UsageRecord struct {
	// ID 是固定查询快照边界的单调投影序号。
	ID int64 `gorm:"primaryKey;autoIncrement;index:idx_usage_time,priority:2;index:idx_usage_waker_time,priority:3"`
	// NativeID 是原生后端、账号主体、会话和调用标识的摘要。
	NativeID []byte `gorm:"not null;uniqueIndex"`
	// RecordedAt 是终态首次持久提交的 Unix 毫秒时间。
	RecordedAt int64 `gorm:"not null;index:idx_usage_time,priority:1;index:idx_usage_waker_time,priority:2"`
	// WakerID 是首次消耗输入所关联的伙伴标识。
	WakerID string `gorm:"not null;index:idx_usage_waker_time,priority:1"`
	// ChatID 是已知的原始公开对话标识。
	ChatID string `gorm:"not null"`
	// Provider 是原生会话身份声明的后端标识。
	Provider string `gorm:"not null;default:''"`
	// AccountID 是建立连接时使用的宿主账号标识。
	AccountID string `gorm:"not null"`
	// Source 是已核实的调用入口。
	Source string `gorm:"not null"`
	// Model 是由原生证据确定的唯一模型标识。
	Model string `gorm:"not null"`
	// Coverage 表示主 Token 观测覆盖的调用范围。
	Coverage string
	// Complete 表示观测覆盖完整调用，个别计数仍可未知。
	Complete bool
	// Input 是缓存读取和写入以外的输入 Token 数。
	Input *int64
	// CacheRead 是从缓存读取的输入 Token 数。
	CacheRead *int64
	// CacheWrite 是写入缓存的输入 Token 数。
	CacheWrite *int64
	// Output 是包含推理在内的输出 Token 数。
	Output *int64
	// Reasoning 是单独上报的推理 Token 数，属于输出的子集。
	Reasoning *int64
	// Amount 是可归属本次调用的精确十进制金额。
	Amount string
	// Currency 是金额币种。
	Currency string
	// Valuation 区分上报金额和估算金额。
	Valuation string
	// CostSource 是独立的金额计算依据。
	CostSource string
	// CostOrigin 区分原生金额和宿主估算。
	CostOrigin string
	// CostComplete 表示金额覆盖完整调用。
	CostComplete bool
	// PriceVersion 是适用的公开价格版本。
	PriceVersion string
}

func (UsageRecord) TableName() string { return "usage_records" }

// UsageCheckpoint records a canonical stream cursor in the same projection transaction.
type UsageCheckpoint struct {
	// Shard identifies a native stream. The reserved value -1 holds the projection generation.
	Shard int `gorm:"primaryKey;autoIncrement:false"`
	// Cursor is the last fully projected position in this stream.
	Cursor int64
	// Generation is replaced when the projection is rebuilt.
	Generation string
}

func (UsageCheckpoint) TableName() string { return "usage_checkpoints" }

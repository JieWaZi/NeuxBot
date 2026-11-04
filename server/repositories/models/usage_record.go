package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&UsageRecord{})
	dbhelper.RegisterTable(&UsageCheckpoint{})
}

// UsageRecord is the rebuildable projection of one canonical Codex terminal.
type UsageRecord struct {
	// ID is the monotonic projection sequence used for snapshots.
	ID int64 `gorm:"primaryKey;autoIncrement;index:idx_usage_time,priority:2;index:idx_usage_waker_time,priority:3"`
	// NativeID is the digest of provider, subject, session, and invocation.
	NativeID []byte `gorm:"not null;uniqueIndex"`
	// RecordedAt is the first durable terminal commit time in Unix milliseconds.
	RecordedAt int64 `gorm:"not null;index:idx_usage_time,priority:1;index:idx_usage_waker_time,priority:2"`
	// WakerID is the partner frozen on the first consumed input.
	WakerID string `gorm:"not null;index:idx_usage_waker_time,priority:1"`
	// ChatID is the original public chat, when known.
	ChatID string `gorm:"not null"`
	// AccountID identifies the host account that opened the connection.
	AccountID string `gorm:"not null"`
	// Source is the verified invocation entry point.
	Source string `gorm:"not null"`
	// Model is the single model identified by native evidence.
	Model string `gorm:"not null"`
	// Coverage describes the calls covered by the primary token observation.
	Coverage string
	// Complete says the observation covers the invocation even if a counter is unknown.
	Complete bool
	// Input counts input tokens outside cache reads and writes.
	Input *int64
	// CacheRead counts input tokens read from cache.
	CacheRead *int64
	// CacheWrite counts input tokens written to cache.
	CacheWrite *int64
	// Output counts output tokens including reasoning.
	Output *int64
	// Reasoning is the separately reported subset of output.
	Reasoning *int64
	// Amount is the exact decimal safely attributed to this invocation.
	Amount string
	// Currency is the currency of the amount.
	Currency string
	// Valuation distinguishes reported and estimated amounts.
	Valuation string
	// CostSource is the independent valuation source.
	CostSource string
	// CostOrigin distinguishes native amounts and host estimates.
	CostOrigin string
	// CostComplete says the valuation covers the entire invocation.
	CostComplete bool
	// PriceVersion identifies the applicable published rate version.
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

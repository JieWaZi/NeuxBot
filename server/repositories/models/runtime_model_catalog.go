package models

import (
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&RuntimeModelCatalog{})
}

// RuntimeModelCatalog 保存一个运行时与账号组合的最近探测快照。
type RuntimeModelCatalog struct {
	// ID 是快照条目的 32 位标识。
	ID string `gorm:"primaryKey;type:varchar(32)"`
	// RuntimeID 标识用户选择的运行环境。
	RuntimeID string `gorm:"not null;uniqueIndex:runtime_model_catalog_scope"`
	// AccountID 区分系统登录与各受管账号。
	AccountID string `gorm:"not null;uniqueIndex:runtime_model_catalog_scope"`
	// AuthGeneration 是探测时确认的账号认证代次。
	AuthGeneration uint64 `gorm:"not null"`
	// Fingerprint 绑定本次探测使用的配置与认证代次。
	Fingerprint string `gorm:"not null"`
	// ProbeStartedAt 用于拒绝覆盖较新快照的迟到探测。
	ProbeStartedAt int64 `gorm:"not null"`
	// Snapshot 包含模型、强度与其他已发现的原生能力。
	Snapshot agent.ModelCatalogSnapshot `gorm:"not null;serializer:json"`
}

// TableName 返回原生模型目录快照表名。
func (RuntimeModelCatalog) TableName() string { return "runtime_model_catalogs" }

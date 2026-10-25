package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&RuntimeEnvironment{})
}

// RuntimeEnvironment 保存当前设备上一条 Runtime 的用户状态和探测快照。
type RuntimeEnvironment struct {
	// DefaultAccountID 是新聊天默认使用的受管账号；空值沿用当前 CLI 配置。
	DefaultAccountID string `gorm:"type:varchar(32);not null;default:''" json:"defaultAccountId"`
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// RuntimeID 是 Runtime Registry 使用的开放字符串标识。
	RuntimeID string `gorm:"column:runtime_id;type:varchar(120);not null;uniqueIndex" json:"runtimeId"`
	// BaseRuntimeID 是提供协议语义的内置 Runtime 标识。
	BaseRuntimeID string `gorm:"column:base_runtime_id;type:varchar(120);not null" json:"baseRuntimeId"`
	// Name 是当前 Runtime Definition 的展示名称。
	Name string `gorm:"column:name;type:varchar(120);not null" json:"name"`
	// Enabled 表示该 Runtime 是否允许用于后续新执行。
	Enabled bool `gorm:"column:enabled;not null;default:true" json:"enabled"`
	// IsDefault 表示该条目是否是新对话优先使用的全局默认 Runtime。
	IsDefault bool `gorm:"column:is_default;not null;default:false;index" json:"isDefault"`
	// Binary 是用户保存的可执行文件名或绝对路径。
	Binary string `gorm:"column:binary;type:text;not null" json:"binary"`
	// PrefixArgs 是放在 Runtime 子命令之前的用户前置参数。
	PrefixArgs []string `gorm:"column:prefix_args;type:text;not null;serializer:json" json:"prefixArgs"`
	// ConfigHome 是由 Runtime Definition 映射到受控环境变量的配置主目录。
	ConfigHome string `gorm:"column:config_home;type:text;not null;default:''" json:"configHome,omitempty"`
	// CustomModels 是用户为动态探测目录补充的模型标识。
	CustomModels []string `gorm:"column:custom_models;type:text;not null;default:'[]';serializer:json" json:"customModels"`
	// DefaultModel 是切换到该 Runtime 时优先使用的模型标识。
	DefaultModel string `gorm:"column:default_model;type:varchar(200);not null;default:''" json:"defaultModel"`
	// DefaultReasoning 是切换到该 Runtime 时优先使用的推理强度标识。
	DefaultReasoning string `gorm:"column:default_reasoning;type:varchar(80);not null;default:''" json:"defaultReasoning"`
	// DefaultPermissionLevel 是该 Runtime 新执行默认使用的产品权限。
	DefaultPermissionLevel constants.PermissionLevel `gorm:"column:default_permission_level;type:varchar(24);not null;default:'full-access'" json:"defaultPermissionLevel"`
	// Detection 是最近一次完整且可供执行准备使用的探测快照。
	Detection runtimes.DetectionResult `gorm:"column:detection;type:text;not null;serializer:json" json:"-"`
	// DetectedAt 是最近一次探测完成的 Unix 毫秒时间，尚未探测时为零。
	DetectedAt int64 `gorm:"column:detected_at;not null;default:0" json:"detectedAt"`
	// CreatedAt 是条目首次建立的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是用户配置或探测快照最近更新的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回运行时环境条目使用的表名。
func (RuntimeEnvironment) TableName() string {
	return "runtime_environments"
}

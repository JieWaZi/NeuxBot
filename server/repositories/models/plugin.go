package models

import (
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&Plugin{})
	dbhelper.RegisterTable(&PluginVersion{})
	dbhelper.RegisterTable(&WakerPlugin{})
	dbhelper.RegisterTable(&PluginConfiguration{})
}

// Plugin 保存稳定的本机插件资源与归属，展示名不参与身份判断。
type Plugin struct {
	// ID 是稳定资源标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是用户可修改的展示名称。
	Name string `gorm:"column:name;not null" json:"name"`
	// Description 是用途说明。
	Description string `gorm:"column:description;not null;default:''" json:"description"`
	// OwnerWakerID 是所属伙伴，空值表示全局资源。
	OwnerWakerID string `gorm:"column:owner_waker_id;not null;default:'';uniqueIndex:idx_plugin_source,priority:1;index" json:"ownerWakerId,omitempty"`
	// SourceKey 是明确来源身份；来源未核验时使用内容身份而非展示名称。
	SourceKey string `gorm:"column:source_key;not null;uniqueIndex:idx_plugin_source,priority:2" json:"sourceKey"`
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近修改时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli;index" json:"updatedAt"`
}

// TableName 返回插件资源表名。
func (Plugin) TableName() string { return "plugins" }

// PluginVersion 保存完整不可变内容和由其解析的组件快照。
type PluginVersion struct {
	// ID 是版本标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// PluginID 是所属资源。
	PluginID string `gorm:"column:plugin_id;not null;uniqueIndex:idx_plugin_version,priority:1" json:"pluginId"`
	// Version 是原包版本或明确的本机版本号。
	Version string `gorm:"column:version;not null;uniqueIndex:idx_plugin_version,priority:2" json:"version"`
	// ContentPath 是数据目录内受管相对内容路径。
	ContentPath string `gorm:"column:content_path;not null" json:"-"`
	// Digest 是完整内容摘要。
	Digest string `gorm:"column:digest;not null" json:"digest"`
	// Package 是从相同不可变内容产生的元数据与组件描述。
	Package plugins.Package `gorm:"column:package;type:text;not null;serializer:json" json:"package"`
	// TrustedAt 是用户对该不可变 MCP 内容确认执行配置的时间。
	TrustedAt int64 `gorm:"column:trusted_at;not null;default:0" json:"trustedAt"`
	// Provenance 是分发来源原始记录，不包含凭据。
	Provenance map[string]any `gorm:"column:provenance;type:text;serializer:json" json:"provenance"`
	// CreatedAt 是版本登记时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回插件版本表名。
func (PluginVersion) TableName() string { return "plugin_versions" }

// WakerPlugin 保存一个伙伴固定版本的安装关系与独立启停。
type WakerPlugin struct {
	// ID 是关系标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// WakerID 是安装伙伴。
	WakerID string `gorm:"column:waker_id;not null;uniqueIndex:idx_waker_plugin,priority:1" json:"wakerId"`
	// PluginID 是插件资源。
	PluginID string `gorm:"column:plugin_id;not null;uniqueIndex:idx_waker_plugin,priority:2;index" json:"pluginId"`
	// VersionID 是伙伴明确选择的版本。
	VersionID string `gorm:"column:version_id;not null;index" json:"versionId"`
	// Enabled 表示后续执行是否可使用该插件。
	Enabled bool `gorm:"column:enabled;not null" json:"enabled"`
	// CreatedAt 是安装时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是关系最近变更时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回伙伴插件关系表名。
func (WakerPlugin) TableName() string { return "waker_plugins" }

// PluginConfiguration 保存资源实例唯一的共享配置，凭据由 Repository 加密。
type PluginConfiguration struct {
	// PluginID 是唯一资源身份。
	PluginID string `gorm:"column:plugin_id;primaryKey" json:"pluginId"`
	// Revision 是乐观并发修订。
	Revision int64 `gorm:"column:revision;not null" json:"revision"`
	// Values 是普通非敏感配置。
	Values map[string]string `gorm:"column:values;type:text;not null;serializer:json" json:"values"`
	// Secrets 是加密保存、仅在服务端解密使用的敏感配置。
	Secrets []MCPSecretDefinition `gorm:"column:secrets;type:text;not null;serializer:json" json:"-"`
	// UpdatedAt 是本地配置最近修改时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回共享配置表名。
func (PluginConfiguration) TableName() string { return "plugin_configurations" }

// PluginState 是一个有界资源窗口关联补齐的查询结果，不增加持久化事实。
type PluginState struct {
	// Plugin 是原资源。
	Plugin Plugin
	// Versions 是资源保留版本。
	Versions []PluginVersion
	// Installations 是真实伙伴关系。
	Installations []WakerPlugin
	// PartnerNames 是由真实伙伴记录批量补齐的展示名，不持久化。
	PartnerNames map[string]string
	// PartnerAvatars 是同批伙伴记录中的真实头像声明，不持久化。
	PartnerAvatars map[string]string
	// Configuration 是服务端可用共享设置。
	Configuration PluginConfiguration
}

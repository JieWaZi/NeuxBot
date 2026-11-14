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
	dbhelper.RegisterTable(&PluginAccount{})
}

// Plugin 保存稳定的本机插件资源，展示名不参与身份判断。
type Plugin struct {
	// ID 是稳定资源标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是用户可修改的展示名称。
	Name string `gorm:"column:name;not null" json:"name"`
	// Description 是用途说明。
	Description string `gorm:"column:description;not null;default:''" json:"description"`
	// Categories 是用户选择的分类，与资源原始内容独立保存。
	Categories []string `gorm:"column:categories;type:text;not null;default:'[]';serializer:json" json:"categories"`
	// SourceKey 是明确来源身份；来源未核验时使用内容身份而非展示名称。
	SourceKey string `gorm:"column:source_key;not null;uniqueIndex:idx_plugin_source" json:"sourceKey"`
	// RollbackVersionID 记录最近一次成功升级前所有伙伴共用的固定版本。
	RollbackVersionID string `gorm:"column:rollback_version_id;not null;default:''" json:"-"`
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
	// CatalogBinding 固定 Core 从可信归档核验的来源和内容，仅由目录准备写入。
	CatalogBinding string `gorm:"column:catalog_binding;not null;default:''" json:"-"`
	// PreparedUntil 为未安装版本保留短暂的确认窗口，单位为 Unix 毫秒。
	PreparedUntil int64 `gorm:"column:prepared_until;not null;default:0" json:"-"`
	// Retired 表示只保留版本身份和摘要，重新准备才能使用其文件和包声明。
	Retired bool `gorm:"column:retired;not null;default:false" json:"-"`
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
	// Account 是同批读取的共享账号摘要；普通管理窗口不补齐私有材料。
	Account PluginAccount
	// Configuration 仅保存普通展示所需配置修订，不含配置值或秘密。
	Configuration PluginConfiguration
}

// PluginExecutionDeclaration 是伙伴启用安装的执行声明，不携带明文秘密或授权材料。
type PluginExecutionDeclaration struct {
	// ID 是已启用插件的资源身份。
	ID string
	// Name 是明确选择插件时交付给智能体的资源名称。
	Name string
	// VersionID 是伙伴固定的不可变版本身份。
	VersionID string
	// Digest 是固定版本的完整程序摘要。
	Digest string
	// TrustedAt 是该固定版本的执行配置确认时间。
	TrustedAt int64
	// AccountGeneration 是接纳时冻结的共享账号代次。
	AccountGeneration int64
	// ConfigurationRevision 是共享配置的冻结修订，零表示尚无配置。
	ConfigurationRevision int64
	// ContentPath 是不可变来源相对数据目录的位置。
	ContentPath string
	// Package 只含固定版本的目标、组件和配置声明，不含 README 等展示正文。
	Package plugins.Package `gorm:"serializer:json"`
	// Values 是判断目标配置就绪所需的普通值。
	Values map[string]string `gorm:"serializer:json"`
	// ConfiguredSecrets 仅保存已配置秘密的名称，不包含密文和明文。
	ConfiguredSecrets []string `gorm:"serializer:json"`
}

// PluginReference 是输入候选使用的已启用固定版本引用。
type PluginReference struct {
	// 插件标识
	ID string `json:"id"`
	// 插件名称
	Name string `json:"name"`
	// 插件说明
	Description string `json:"description"`
	// 固定版本
	Version string `json:"version"`
	// 版本标识
	VersionID string `json:"versionId"`
}

// PluginBrowseVersion 只保存真实安装版本所需的公开展示与身份缓存查询字段。
type PluginBrowseVersion struct {
	// ID 是实际被安装关系引用的版本身份。
	ID string
	// PluginID 是版本所属的资源身份。
	PluginID string
	// Version 是用于更新比较的原包版本。
	Version string
	// Digest 用于固定已有授权缓存身份，不触发内容校验。
	Digest string
	// SourcePath 是该不可变包内的实际入口。
	SourcePath string
	// ManifestName 是标准清单中的名称。
	ManifestName string
	// ManifestDescription 是标准清单的简介。
	ManifestDescription string
	// DisplayName 是包声明的默认展示名称。
	DisplayName string
	// ShortDescription 是包声明的默认短描述。
	ShortDescription string
	// DisplayNames 是受支持语言的公开名称。
	DisplayNames map[string]string `gorm:"serializer:json"`
	// LocalizedDescriptions 是受支持语言的公开简介。
	LocalizedDescriptions map[string]string `gorm:"serializer:json"`
	// Category 是实际安装包声明的标准分类。
	Category string
	// Icons 是实际安装包的安全明暗主题图标。
	Icons plugins.ThemeValues `gorm:"serializer:json"`
	// Icon 是已核验的内嵌图标。
	Icon string
	// AuthenticationStrategy 是包内通用授权声明的策略。
	AuthenticationStrategy string
	// AuthenticationProtocol 是包内通用授权声明的协议。
	AuthenticationProtocol string
}

// PluginBrowseState 是当前浏览窗口的窄列补齐结果，不保存管理配置或伙伴资料。
type PluginBrowseState struct {
	// Versions 只包括实际安装引用的版本，保持登记时间降序与身份升序。
	Versions []PluginBrowseVersion
	// InstalledCount 是包含停用关系的真实安装伙伴数。
	InstalledCount int64
}

// PluginAccount 独占插件共享身份的持久代次和加密交付材料。
type PluginAccount struct {
	// PluginID 是共享账号所属的唯一插件身份。
	PluginID string `gorm:"column:plugin_id;primaryKey" json:"pluginId"`
	// Generation 是账号或交付材料变化的乐观并发代次。
	Generation int64 `gorm:"column:generation;not null" json:"generation"`
	// AuthorizedVersionID 固定退出所需的授权实现，空值表示已清理，nil 表示尚未确认。
	AuthorizedVersionID *string `gorm:"column:authorized_version_id" json:"-"`
	// DeliveryMaterials 是按固定版本、组件和端点加密保存的私有材料。
	DeliveryMaterials []MCPSecretDefinition `gorm:"column:delivery_materials;type:text;serializer:json" json:"-"`
	// UpdatedAt 是最近一次账号代次变更的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回独立共享账号表名。
func (PluginAccount) TableName() string { return "plugin_accounts" }

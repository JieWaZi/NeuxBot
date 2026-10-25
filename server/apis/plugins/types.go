package plugins

import (
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 插件详情
type PluginDetailResponse struct {
	// 插件资源
	Plugin models.Plugin `json:"plugin"`
	// 当前解析版本
	ResolvedVersionID string `json:"resolvedVersionId"`
	// 保留版本
	Versions []models.PluginVersion `json:"versions"`
	// 组件授权标识
	ComponentIDs map[string]string `json:"componentIds"`
	// 安装伙伴名称
	PartnerNames map[string]string `json:"partnerNames"`
	// 伙伴头像
	PartnerAvatars map[string]string `json:"partnerAvatars"`
	// 安装关系
	Installations []models.WakerPlugin `json:"installations"`
	// 普通配置
	Values map[string]string `json:"values"`
	// 配置定义
	Fields []packageplugin.ConfigurationField `json:"fields"`
	// 已配置密钥
	ConfiguredSecrets []string `json:"configuredSecrets"`
	// 配置修订
	ConfigurationRevision int64 `json:"configurationRevision"`
	// 当前就绪
	Ready bool `json:"ready"`
	// 阻塞原因
	Blockers []string `json:"blockers"`
	// 共享身份
	Account authorization.Account `json:"account"`
	// 更新摘要
	Update PluginUpdateResponse `json:"update"`
	// 目录状态
	CatalogSource catalog.SourceState `json:"catalogSource"`
}

// 插件更新摘要
type PluginUpdateResponse struct {
	// 存在更新
	Available bool `json:"available"`
	// 目录标识
	CatalogKey string `json:"catalogKey"`
	// 目标版本
	Version string `json:"version"`
	// 归档摘要
	ArchiveSHA256 string `json:"archiveSHA256"`
	// 落后伙伴
	WakerIDs []string `json:"wakerIds"`
}

// 插件列表响应
type ListPluginsResponse struct {
	// 目录状态
	CatalogSource catalog.SourceState `json:"catalogSource"`
	// 资源列表
	Resources []PluginDetailResponse `json:"resources"`
	// 总数
	TotalSize int64 `json:"totalSize"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// 插件安装请求
type InstallPluginRequest struct {
	// 目标版本
	VersionID string `json:"versionId" validate:"required"`
	// 所选伙伴
	WakerIDs []string `json:"wakerIds" validate:"required,min=1,max=100"`
	// 预期配置修订
	ExpectedConfigurationRevision int64 `json:"expectedConfigurationRevision"`
}

// 伙伴安装结果
type PartnerInstallResult struct {
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 安装成功
	Installed bool `json:"installed"`
	// 错误标识
	ErrorCode string `json:"errorCode,omitempty"`
}

// 插件安装结果
type InstallPluginResponse struct {
	// 插件标识
	PluginID string `json:"pluginId"`
	// 逐伙伴结果
	Results []PartnerInstallResult `json:"results"`
}

// 插件密钥输入
type SecretValue struct {
	// 配置键
	Key string `json:"key"`
	// 新密钥值
	Value string `json:"value,omitempty"`
	// 清除已有值
	Clear bool `json:"clear"`
}

// 伙伴版本切换
type VersionSwitch struct {
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 原版本标识
	ExpectedVersionID string `json:"expectedVersionId"`
	// 新版本标识
	VersionID string `json:"versionId"`
}

// 插件配置请求
type UpdateConfigurationRequest struct {
	// 预期修订
	ExpectedRevision int64 `json:"expectedRevision"`
	// 普通配置
	Values map[string]string `json:"values"`
	// 密钥更新
	Secrets []SecretValue `json:"secrets"`
	// 版本切换
	Switches []VersionSwitch `json:"switches"`
}

// 插件授权响应
type PluginAuthorizationResponse struct {
	// 授权提供者
	Provider string `json:"provider"`
	// 所需权限
	RequiredScopes []string `json:"requiredScopes"`
	// 缺少权限
	MissingScopes []string `json:"missingScopes"`
	// 占用工作
	ActiveUses []authorization.Usage `json:"activeUses"`
	// 共享身份
	Account authorization.Account `json:"account"`
	// 设备等待
	Session authorization.Session `json:"session"`
}

// 插件资料请求
type UpdatePluginRequest struct {
	// 展示名称
	Name string `json:"name"`
	// 用途说明
	Description string `json:"description"`
}

// 插件删除请求
type DeletePluginRequest struct {
	// 已确认影响
	Confirmed bool `json:"confirmed"`
	// 原安装集合
	Installations []VersionSwitch `json:"installations"`
}

// 插件启停请求
type PluginEnabledRequest struct {
	// 启用状态
	Enabled bool `json:"enabled"`
}

// 飞书配置请求
type ConfigureAuthorizationRequest struct {
	// 应用标识
	AppID string `json:"appId" validate:"required"`
	// 应用密钥
	AppSecret string `json:"appSecret" validate:"required"`
}

// AuthorizationScopeRequest 描述用户明确请求的补充权限。
type AuthorizationScopeRequest struct {
	// 补充权限
	AdditionalScopes []string `json:"additionalScopes" validate:"max=100,dive,max=200"`
}

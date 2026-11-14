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
	// 当前版本包状态
	PackageStatus string `json:"packageStatus"`
	// 当前版本包错误标识
	PackageErrorCode string `json:"packageErrorCode,omitempty"`
	// 保留版本
	Versions []PluginVersionResponse `json:"versions"`
	// 安装伙伴名称
	PartnerNames map[string]string `json:"partnerNames"`
	// 伙伴头像
	PartnerAvatars map[string]string `json:"partnerAvatars"`
	// 安装关系
	Installations []models.WakerPlugin `json:"installations"`
	// 配置修订
	ConfigurationRevision int64 `json:"configurationRevision"`
	// 账号代次
	AccountGeneration int64 `json:"accountGeneration"`
	// 共享身份
	Account authorization.Account `json:"account"`
	// 更新摘要
	Update PluginUpdateResponse `json:"update"`
	// 回滚摘要
	Rollback *PluginRollbackResponse `json:"rollback,omitempty"`
	// 目录状态
	CatalogSource catalog.SourceState `json:"catalogSource"`
}

// 插件公开版本
type PluginVersionResponse struct {
	// 版本标识
	ID string `json:"id"`
	// 插件标识
	PluginID string `json:"pluginId"`
	// 版本名称
	Version string `json:"version"`
	// 内容摘要
	Digest string `json:"digest"`
	// 展示资料
	Package packageplugin.PublicPackage `json:"package"`
	// 信任时间
	TrustedAt int64 `json:"trustedAt"`
	// 分发来源
	Provenance map[string]any `json:"provenance"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
}

// 插件目录条目
type PluginCatalogEntryResponse struct {
	// 来源身份
	SourceKey string `json:"sourceKey"`
	// 目录标识
	Key string `json:"key"`
	// 来源提交
	SourceCommit string `json:"sourceCommit"`
	// 归档摘要
	ArchiveSHA256 string `json:"archiveSHA256"`
	// 归档地址
	ArchiveURL string `json:"archiveURL,omitempty"`
	// 逻辑入口
	PackagePath string `json:"packagePath,omitempty"`
	// 资料地址
	DetailsURL string `json:"detailsURL,omitempty"`
	// 资料摘要
	DetailsSHA256 string `json:"detailsSHA256,omitempty"`
	// 资料大小
	DetailsSize int64 `json:"detailsSize,omitempty"`
	// 展示资料
	Package packageplugin.PublicPackage `json:"package"`
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

// 插件回滚摘要
type PluginRollbackResponse struct {
	// 可以回滚
	Available bool `json:"available"`
	// 目标标识
	VersionID string `json:"versionId"`
	// 目标版本
	Version string `json:"version"`
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
	ExpectedConfigurationRevision int64 `json:"expectedConfigurationRevision" validate:"min=0"`
	// 账号代次
	ExpectedAccountGeneration int64 `json:"expectedAccountGeneration" validate:"min=0"`
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
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=0"`
	// 普通配置
	Values map[string]string `json:"values"`
	// 密钥更新
	Secrets []SecretValue `json:"secrets"`
}

// 插件授权响应
type PluginAuthorizationResponse struct {
	// 所选版本
	VersionID string `json:"versionId"`
	// 配置修订
	ConfigurationRevision int64 `json:"configurationRevision"`
	// 账号代次
	AccountGeneration int64 `json:"accountGeneration"`
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
	// 用户分类
	Categories *[]string `json:"categories,omitempty"`
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

// AuthorizationScopeRequest 描述用户明确请求的补充权限。
type AuthorizationScopeRequest struct {
	// 账号上下文
	PluginAccountRequest
	// 发起所有权
	OwnershipNonce string `json:"ownershipNonce,omitempty" validate:"omitempty,max=128"`
	// 补充权限
	AdditionalScopes []string `json:"additionalScopes" validate:"max=100,dive,max=200"`
}

// 发起插件授权结果
type StartPluginAuthorizationResponse struct {
	// 授权会话
	authorization.Session
	// 本次请求新建授权
	Created bool `json:"created"`
}

// 取消插件授权请求
type CancelPluginAuthorizationRequest struct {
	// 账号上下文
	PluginAccountRequest
	// 发起所有权
	OwnershipNonce string `json:"ownershipNonce,omitempty" validate:"omitempty,max=128"`
}

// 账号上下文请求
type PluginAccountRequest struct {
	// 配置修订
	ExpectedConfigurationRevision int64 `json:"expectedConfigurationRevision" validate:"min=0"`
	// 账号代次
	ExpectedAccountGeneration int64 `json:"expectedAccountGeneration" validate:"min=0"`
}

// 插件统一更新请求
type UpdatePluginInstallationsRequest struct {
	// 配置修订
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=0"`
	// 账号代次
	ExpectedAccountGeneration int64 `json:"expectedAccountGeneration" validate:"min=0"`
	// 普通配置
	Values map[string]string `json:"values"`
	// 密钥更新
	Secrets []SecretValue `json:"secrets"`
	// 版本切换
	Switches []VersionSwitch `json:"switches" validate:"required,min=1,max=100"`
	// 回滚版本
	Rollback bool `json:"rollback,omitempty"`
}

func (r UpdatePluginInstallationsRequest) configuration() UpdateConfigurationRequest {
	return UpdateConfigurationRequest{ExpectedRevision: r.ExpectedRevision, Values: r.Values, Secrets: r.Secrets}
}

package plugins

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"golang.org/x/mod/semver"
)

// 浏览插件
type BrowsePlugins struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
	// 仅已安装
	InstalledOnly bool `in:"query" name:"installedOnly"`
	// 展示语言
	Locale string `in:"query" name:"locale"`
}

// 插件浏览响应
type BrowsePluginsResponse struct {
	// 目录状态
	CatalogSource catalog.SourceState `json:"catalogSource"`
	// 浏览条目
	Entries []PluginBrowseEntry `json:"entries"`
	// 完整总数
	TotalSize *int64 `json:"totalSize,omitempty"`
	// 安装总数
	InstalledTotalSize int64 `json:"installedTotalSize"`
	// 还有条目
	HasMore bool `json:"hasMore"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// 插件浏览条目
type PluginBrowseEntry struct {
	// 来源身份
	SourceKey string `json:"sourceKey"`
	// 插件标识
	PluginID string `json:"pluginId,omitempty"`
	// 目录标识
	CatalogKey string `json:"catalogKey,omitempty"`
	// 展示版本
	VersionID string `json:"versionId,omitempty"`
	// 展示名称
	Name string `json:"name"`
	// 简短介绍
	Description string `json:"description"`
	// 分类
	Category string `json:"category,omitempty"`
	// 主题图标
	Icons packageplugin.ThemeValues `json:"icons,omitempty"`
	// 插件图标
	Icon string `json:"icon"`
	// 安装伙伴数
	InstalledCount int64 `json:"installedCount"`
	// 需要授权
	RequiresAuthorization bool `json:"requiresAuthorization"`
	// 更新摘要
	Update PluginBrowseUpdate `json:"update"`
}

// 插件浏览更新
type PluginBrowseUpdate struct {
	// 存在更新
	Available bool `json:"available"`
	// 目录标识
	CatalogKey string `json:"catalogKey,omitempty"`
	// 目标版本
	Version string `json:"version,omitempty"`
	// 归档摘要
	ArchiveSHA256 string `json:"archiveSHA256,omitempty"`
}

// Path 返回全局插件的统一轻量浏览入口。
func (*BrowsePlugins) Path() string { return "/browse" }

// Validate 规范分页和支持的展示语言。
func (o *BrowsePlugins) Validate(*gin.Context) error {
	return o.normalizeBrowseWindow()
}

func (o *BrowsePlugins) normalizeBrowseWindow() error {
	if o.PageNum < 0 || o.PageSize < 0 || o.PageSize > constants.PluginBrowseMaximumPageSize {
		return status_error.BadRequest
	}
	if o.PageNum == 0 {
		o.PageNum = 1
	}
	if o.PageSize == 0 {
		o.PageSize = constants.PluginBrowseDefaultPageSize
	}
	// 在查询之前拒绝会溢出的页码，避免偏移回绕到首屏。
	if o.PageNum-1 > int(^uint(0)>>1)/o.PageSize {
		return status_error.BadRequest
	}
	if o.Locale == "" {
		o.Locale = constants.PluginLocaleEnglish
	}
	if o.Locale != constants.PluginLocaleEnglish && o.Locale != constants.PluginLocaleChinese {
		return status_error.BadRequest
	}
	return nil
}

// Output 返回合并来源后的公开浏览窗口，不读取共享配置或管理详情。
func (o *BrowsePlugins) Output(ctx *gin.Context) (interface{}, error) {
	if err := o.normalizeBrowseWindow(); err != nil {
		return nil, ginx.WithStack(err)
	}
	// 先固定唯一目录快照；后续数据库短读事务不访问网络或包文件。
	entries, source := catalog.Snapshot(global.PluginCatalogSource())
	hasCatalogSnapshot := entries != nil
	bySource := make(map[string]catalog.Entry, len(entries))
	for _, entry := range entries {
		bySource[entry.SourceKey] = entry
	}
	result := BrowsePluginsResponse{CatalogSource: source, Entries: []PluginBrowseEntry{}, PageNum: o.PageNum, PageSize: o.PageSize}
	offset := (o.PageNum - 1) * o.PageSize
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		result.Entries = []PluginBrowseEntry{}
		result.TotalSize = nil
		resources, installedTotal, err := global.PluginControllerManager.BrowseInstalled(txctx, o.Query, offset, o.PageSize)
		if err != nil {
			return err
		}
		result.InstalledTotalSize = installedTotal
		localTotal := int64(0)
		if !o.InstalledOnly {
			local, total, err := global.PluginControllerManager.BrowseLocalUninstalled(txctx, o.Query, max(offset-int(installedTotal), 0), o.PageSize-len(resources))
			if err != nil {
				return err
			}
			resources = append(resources, local...)
			localTotal = total
		}
		ids := make([]string, 0, len(resources))
		for _, resource := range resources {
			ids = append(ids, resource.ID)
		}
		states, err := global.PluginControllerManager.ReadBrowseStates(txctx, ids)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			result.Entries = append(result.Entries, installedBrowseEntry(resource, states[resource.ID], bySource, o.Locale))
		}
		total := installedTotal + localTotal
		if !o.InstalledOnly && hasCatalogSnapshot {
			installedSources := map[string]bool{}
			for start := 0; start < len(entries); start += constants.PluginBrowseMaximumPageSize {
				end := min(start+constants.PluginBrowseMaximumPageSize, len(entries))
				keys := make([]string, 0, end-start)
				for _, entry := range entries[start:end] {
					keys = append(keys, entry.SourceKey)
				}
				keys, err = global.PluginControllerManager.InstalledSources(txctx, keys)
				if err != nil {
					return err
				}
				for _, key := range keys {
					installedSources[key] = true
				}
			}
			catalogEntries := []PluginBrowseEntry{}
			for _, entry := range entries {
				if installedSources[entry.SourceKey] || !catalogBrowseMatches(entry, o.Query) {
					continue
				}
				name, description := browsePresentation(entry.Package.Manifest.Name, entry.Package.Manifest.Description, entry.Package.Presentation, "", o.Locale)
				catalogEntries = append(catalogEntries, PluginBrowseEntry{SourceKey: entry.SourceKey, CatalogKey: entry.Key, Name: name, Description: description, Category: entry.Package.Presentation.Category, Icons: entry.Package.Presentation.Icons, Icon: entry.Package.Icon})
			}
			sort.Slice(catalogEntries, func(i, j int) bool { return catalogEntries[i].SourceKey < catalogEntries[j].SourceKey })
			total += int64(len(catalogEntries))
			start := min(max(int64(offset)-installedTotal-localTotal, 0), int64(len(catalogEntries)))
			end := min(start+int64(o.PageSize-len(result.Entries)), int64(len(catalogEntries)))
			result.Entries = append(result.Entries, catalogEntries[start:end]...)
		}
		if o.InstalledOnly || hasCatalogSnapshot {
			result.TotalSize = &total
		}
		result.HasMore = int64(offset)+int64(len(result.Entries)) < total
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

func installedBrowseEntry(resource models.Plugin, state models.PluginBrowseState, catalogEntries map[string]catalog.Entry, locale string) PluginBrowseEntry {
	result := PluginBrowseEntry{SourceKey: resource.SourceKey, PluginID: resource.ID, Name: resource.Name, Description: resource.Description, InstalledCount: state.InstalledCount}
	if entry, ok := catalogEntries[resource.SourceKey]; ok {
		result.CatalogKey = entry.Key
		result.Update = browseUpdate(state.Versions, entry)
	}
	if len(state.Versions) == 0 {
		return result
	}
	// 实际安装固定其版本；无安装的本地封装使用唯一保存版本，目录准备不能覆盖此身份。
	version := state.Versions[0]
	result.VersionID = version.ID
	result.Icon = version.Icon
	result.Icons = version.Icons
	result.Category = version.Category
	result.Name, result.Description = browsePresentation(version.ManifestName, version.ManifestDescription, packageplugin.Presentation{DisplayName: version.DisplayName, ShortDescription: version.ShortDescription, DisplayNames: version.DisplayNames, LocalizedDescriptions: version.LocalizedDescriptions}, resource.Name, locale)
	if result.Description == "" {
		result.Description = strings.TrimSpace(resource.Description)
	}
	if version.AuthenticationStrategy == constants.PluginAuthCommandStrategy && version.AuthenticationProtocol == constants.PluginAuthProtocol && global.PluginBridges != nil {
		account := global.PluginBridges.CachedAccount(resource.ID, version.Digest+":"+filepath.ToSlash(version.SourcePath))
		result.RequiresAuthorization = state.InstalledCount > 0 && account.CheckedAt > 0 && !account.Available
	}
	return result
}

func browsePresentation(manifestName, manifestDescription string, presentation packageplugin.Presentation, resourceName, locale string) (string, string) {
	name := ""
	authored := resourceName == manifestName || resourceName == presentation.DisplayName
	for _, value := range presentation.DisplayNames {
		authored = authored || resourceName == value
	}
	if resourceName != "" && !authored {
		name = resourceName
	} else {
		name = firstBrowseText(presentation.DisplayNames[locale], presentation.DisplayNames[constants.PluginLocaleEnglish], presentation.DisplayName, resourceName, manifestName)
	}
	return name, firstBrowseText(presentation.LocalizedDescriptions[locale], presentation.LocalizedDescriptions[constants.PluginLocaleEnglish], presentation.ShortDescription, manifestDescription)
}

func firstBrowseText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func catalogBrowseMatches(entry catalog.Entry, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	values := []string{entry.SourceKey, entry.Package.Manifest.Name, entry.Package.Manifest.Description, entry.Package.Presentation.DisplayName, entry.Package.Presentation.ShortDescription}
	for _, value := range entry.Package.Presentation.DisplayNames {
		values = append(values, value)
	}
	for _, value := range entry.Package.Presentation.LocalizedDescriptions {
		values = append(values, value)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func browseUpdate(versions []models.PluginBrowseVersion, entry catalog.Entry) PluginBrowseUpdate {
	result := PluginBrowseUpdate{}
	target := normalizedPluginVersion(entry.Package.Manifest.Version)
	if !semver.IsValid(target) || len(versions) == 0 {
		return result
	}
	available := false
	for _, version := range versions {
		current := normalizedPluginVersion(version.Version)
		if !semver.IsValid(current) || semver.Compare(target, current) < 0 {
			return result
		}
		available = available || semver.Compare(target, current) > 0
	}
	if available {
		result = PluginBrowseUpdate{Available: true, CatalogKey: entry.Key, Version: entry.Package.Manifest.Version, ArchiveSHA256: entry.ArchiveSHA256}
	}
	return result
}

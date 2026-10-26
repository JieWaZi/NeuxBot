// Package plugins 提供本机插件库、版本、共享配置与伙伴安装接口。
package plugins

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
	"golang.org/x/mod/semver"
)

func visiblePlugin(ctx context.Context, id, waker string) (models.Plugin, error) {
	resource, err := global.PluginControllerManager.Get(ctx, id)
	if err != nil {
		return resource, err
	}
	if resource.OwnerWakerID != "" && resource.OwnerWakerID != waker {
		return resource, status_error.PluginForbidden
	}
	return resource, nil
}
func secretValues(config models.PluginConfiguration) map[string]string {
	values := map[string]string{}
	for _, secret := range config.Secrets {
		if secret.Configured {
			values[secret.Name] = secret.Value
		}
	}
	return values
}
func pluginDetail(ctx context.Context, resource models.Plugin, selectedVersion ...string) (PluginDetailResponse, error) {
	result := PluginDetailResponse{Plugin: resource, Versions: []models.PluginVersion{}, Installations: []models.WakerPlugin{}, Values: map[string]string{}, Fields: []packageplugin.ConfigurationField{}, ConfiguredSecrets: []string{}, Blockers: []string{}, Account: authorization.Account{Scopes: []string{}}}
	states, err := global.PluginControllerManager.ReadStates(ctx, []string{resource.ID})
	if err != nil {
		return result, err
	}
	result, err = detailFromState(ctx, resource, states[resource.ID], selectedVersion...)
	entries, source := catalog.Snapshot(global.PluginCatalogSource())
	applyPluginUpdate(&result, entries, source)
	return result, err
}
func detailFromState(ctx context.Context, resource models.Plugin, state models.PluginState, selectedVersion ...string) (PluginDetailResponse, error) {
	result := PluginDetailResponse{Plugin: resource, PartnerNames: state.PartnerNames, PartnerAvatars: state.PartnerAvatars, Versions: state.Versions, Installations: state.Installations, Values: map[string]string{}, Fields: []packageplugin.ConfigurationField{}, ConfiguredSecrets: []string{}, Blockers: []string{}, Account: authorization.Account{Scopes: []string{}}}
	config := state.Configuration

	result.Values = config.Values
	result.ConfigurationRevision = config.Revision
	for _, secret := range config.Secrets {
		if secret.Configured {
			result.ConfiguredSecrets = append(result.ConfiguredSecrets, secret.Name)
		}
	}
	if len(result.Versions) == 0 {
		result.Blockers = append(result.Blockers, status_error.PluginNotReady.Key())
		return result, nil
	}
	version := result.Versions[0]
	// 普通全局入口优先展示真实安装中最近登记的版本；准备结果仍显式固定目标版本。
	if len(selectedVersion) == 0 || selectedVersion[0] == "" {
		installed := map[string]bool{}
		for _, relation := range state.Installations {
			installed[relation.VersionID] = true
		}
		for _, candidate := range result.Versions {
			if installed[candidate.ID] {
				version = candidate
				break
			}
		}
	}
	if len(selectedVersion) > 0 && selectedVersion[0] != "" {
		found := false
		for _, candidate := range result.Versions {
			if candidate.ID == selectedVersion[0] {
				version = candidate
				found = true
				break
			}
		}
		if !found {
			return result, status_error.PluginVersionConflict
		}
	}
	result.ResolvedVersionID = version.ID
	extension, err := packageplugin.ClientExtension(version.Package)
	if err != nil {
		// 旧版本仍可查看与移除；无效声明只能作为阻塞原因，不能取得执行能力。
		result.Blockers = append(result.Blockers, errorKey(err))
		return result, nil
	}
	result.ComponentIDs = map[string]string{}
	for _, skill := range version.Package.Skills {
		result.ComponentIDs["skill:"+skill.Key] = packageplugin.ComponentID(resource.ID, "skill:"+skill.Key)
	}
	for _, server := range version.Package.Servers {
		result.ComponentIDs["mcp:"+server.Key] = packageplugin.ComponentID(resource.ID, "mcp:"+server.Key)
	}
	result.Fields = extension.Config
	result.Account, err = installationService().Summary(version, config)
	result.Ready = err == nil
	if err != nil {
		result.Blockers = append(result.Blockers, errorKey(err))
	}
	return result, nil
}
func errorKey(err error) string {
	var coded interface{ Key() string }
	if errors.As(err, &coded) {
		return coded.Key()
	}
	return status_error.InternalServerError.Key()
}
func stagedRoot() string { return filepath.Join(global.Config.DataDirectory, "plugins", "staging") }
func installationService() *plugininstallation.Service {
	return plugininstallation.New(plugininstallation.Config{Contents: global.PluginContents, DataDirectory: global.Config.DataDirectory, Bridges: global.PluginBridges, BridgeEnvironment: global.PluginBridgeEnvironment})
}

func listPluginResources(ctx context.Context, waker, query string, page, size int, installedOnly bool) (ListPluginsResponse, error) {
	if page < 0 || size < 0 || size > 100 {
		return ListPluginsResponse{}, status_error.BadRequest
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 50
	}
	if waker != "" {
		if _, err := global.WakerControllerManager.Get(ctx, waker); err != nil {
			return ListPluginsResponse{}, err
		}
	}
	var resources []models.Plugin
	var total int64
	var states map[string]models.PluginState
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		resources, total, err = global.PluginControllerManager.List(txctx, "", waker, query, (page-1)*size, size, installedOnly)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(resources))
		for _, resource := range resources {
			ids = append(ids, resource.ID)
		}
		states, err = global.PluginControllerManager.ReadStates(txctx, ids)
		return err
	})
	if err != nil {
		return ListPluginsResponse{}, err
	}

	entries, source := catalog.Snapshot(global.PluginCatalogSource())
	result := ListPluginsResponse{CatalogSource: source, Resources: []PluginDetailResponse{}, TotalSize: total, PageNum: page, PageSize: size}
	for _, resource := range resources {
		selectedVersion := ""
		if waker != "" {
			for _, relation := range states[resource.ID].Installations {
				if relation.WakerID == waker {
					selectedVersion = relation.VersionID
				}
			}
		}
		detail, err := detailFromState(ctx, resource, states[resource.ID], selectedVersion)
		if err != nil {
			return result, err
		}
		applyPluginUpdate(&detail, entries, source)
		for index := range detail.Versions {
			detail.Versions[index].Package.Readme = ""
		}
		result.Resources = append(result.Resources, detail)
	}
	return result, nil
}

// applyPluginUpdate 仅从同来源目录和真实伙伴安装派生更新，不读取包文件或变更安装。
func applyPluginUpdate(detail *PluginDetailResponse, entries []catalog.Entry, source catalog.SourceState) {
	detail.CatalogSource = source
	detail.Update = PluginUpdateResponse{WakerIDs: []string{}}
	for _, entry := range entries {
		if entry.SourceKey != detail.Plugin.SourceKey {
			continue
		}
		target := normalizedPluginVersion(entry.Package.Manifest.Version)
		if !semver.IsValid(target) {
			return
		}
		versions := map[string]string{}
		for _, version := range detail.Versions {
			versions[version.ID] = normalizedPluginVersion(version.Version)
		}
		for _, relation := range detail.Installations {
			current := versions[relation.VersionID]
			// 更新会覆盖全体伙伴，未知版本或目录回退不能让其他伙伴被动降级。
			if !semver.IsValid(current) || semver.Compare(target, current) < 0 {
				detail.Update.WakerIDs = []string{}
				return
			}
			if semver.Compare(target, current) > 0 {
				detail.Update.WakerIDs = append(detail.Update.WakerIDs, relation.WakerID)
			}
		}
		if len(detail.Update.WakerIDs) > 0 {
			sort.Strings(detail.Update.WakerIDs)
			detail.Update.Available = true
			detail.Update.CatalogKey = entry.Key
			detail.Update.Version = entry.Package.Manifest.Version
			detail.Update.ArchiveSHA256 = entry.ArchiveSHA256
		}
		return
	}
}

func normalizedPluginVersion(version string) string {
	return "v" + strings.TrimPrefix(version, "v")
}

// acquirePluginPresentation 只固定文件读取的占用，不扫描完整包；执行入口仍独立复验。
func acquirePluginPresentation(version models.PluginVersion) (string, func(), error) {
	// README 是有界短读；持有同一内容锁阻止回收，不在释放时触发全库扫描。
	release := func() {}
	if global.PluginContents != nil {
		release = global.PluginContents.Guard()
	}
	source, err := packageplugin.ContentPath(global.Config.DataDirectory, version.ContentPath)
	if err != nil {
		release()
		return "", nil, err
	}
	return source, release, nil
}

// acquirePluginReadme 为原文下载复验固定快照，并保留文件占用到读取结束。
func acquirePluginReadme(version models.PluginVersion) (string, func(), error) {
	source, release, err := acquirePluginPresentation(version)
	if err != nil {
		return "", nil, err
	}
	if err = packageplugin.VerifyContent(source, version.Digest); err != nil {
		release()
		return "", nil, err
	}
	return source, release, nil
}

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
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
	"golang.org/x/mod/semver"
)

func visiblePlugin(ctx context.Context, id, waker string) (models.Plugin, error) {
	resource, err := global.PluginControllerManager.Get(ctx, id)
	if err != nil {
		return resource, err
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
	result := PluginDetailResponse{Plugin: resource, Versions: []PluginVersionResponse{}, Installations: []models.WakerPlugin{}, Account: authorization.Account{Scopes: []string{}}}
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
	result := PluginDetailResponse{Plugin: resource, PartnerNames: state.PartnerNames, PartnerAvatars: state.PartnerAvatars, Versions: publicPluginVersions(state.Versions), Installations: state.Installations, Account: authorization.Account{Scopes: []string{}}}
	config := state.Configuration
	result.AccountGeneration = state.Account.Generation

	result.ConfigurationRevision = config.Revision
	result.Rollback = rollbackSummary(state)
	if len(result.Versions) == 0 {
		return result, nil
	}
	version := state.Versions[0]
	// 普通全局入口优先展示真实安装中最近登记的版本；准备结果仍显式固定目标版本。
	if len(selectedVersion) == 0 || selectedVersion[0] == "" {
		installed := map[string]bool{}
		for _, relation := range state.Installations {
			installed[relation.VersionID] = true
		}
		for _, candidate := range state.Versions {
			if installed[candidate.ID] {
				version = candidate
				break
			}
		}
	}
	if len(selectedVersion) > 0 && selectedVersion[0] != "" {
		found := false
		for _, candidate := range state.Versions {
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
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return result, err
	}
	if global.PluginBridges != nil && version.Package.Integration.Authentication != nil {
		result.Account, _, _ = global.PluginBridges.CachedAuthorization(resource.ID, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), commandbridge.OperationContext{PluginID: resource.ID, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: config.Revision, AccountGeneration: state.Account.Generation})
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
func publicPluginVersions(versions []models.PluginVersion) []PluginVersionResponse {
	result := make([]PluginVersionResponse, 0, len(versions))
	for _, version := range versions {
		result = append(result, PluginVersionResponse{ID: version.ID, PluginID: version.PluginID, Version: version.Version, Digest: version.Digest, Package: version.Package.Public(), TrustedAt: version.TrustedAt, Provenance: version.Provenance, CreatedAt: version.CreatedAt})
	}
	return result
}

func publicCatalogEntry(entry catalog.Entry) PluginCatalogEntryResponse {
	return PluginCatalogEntryResponse{SourceKey: entry.SourceKey, Key: entry.Key, SourceCommit: entry.SourceCommit, ArchiveSHA256: entry.ArchiveSHA256, ArchiveURL: entry.ArchiveURL, PackagePath: entry.PackagePath, DetailsURL: entry.DetailsURL, DetailsSHA256: entry.DetailsSHA256, DetailsSize: entry.DetailsSize, Package: entry.Package.Public()}
}
func stagedRoot() string { return filepath.Join(global.Config.DataDirectory, "plugins", "staging") }
func installationService() *plugininstallation.Service {
	return plugininstallation.New(plugininstallation.Config{Transactions: global.TransactionControllerManager, Contents: global.PluginContents, DataDirectory: global.Config.DataDirectory, Bridges: global.PluginBridges, BridgeEnvironment: global.PluginBridgeEnvironment, Store: global.PluginControllerManager, CloseIdlePlugin: closeIdlePlugin})
}

func listPluginResources(ctx context.Context, waker, query string, page, size int) (ListPluginsResponse, error) {
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
		resources, total, err = global.PluginControllerManager.List(txctx, waker, query, (page-1)*size, size)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(resources))
		for _, resource := range resources {
			ids = append(ids, resource.ID)
		}
		if waker != "" {
			states, err = global.PluginControllerManager.ReadInstalledStates(txctx, ids)
		} else {
			states, err = global.PluginControllerManager.ReadStates(txctx, ids)
		}
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
	byID := make(map[string]string, len(detail.Versions))
	for _, version := range detail.Versions {
		byID[version.ID] = version.Version
	}
	for _, entry := range entries {
		if entry.SourceKey != detail.Plugin.SourceKey {
			continue
		}
		versions := make([]string, 0, len(detail.Installations))
		for _, relation := range detail.Installations {
			versions = append(versions, byID[relation.VersionID])
		}
		target := entry.Package.Manifest.Version
		for _, index := range upgradeIndices(target, versions) {
			detail.Update.WakerIDs = append(detail.Update.WakerIDs, detail.Installations[index].WakerID)
		}
		if len(detail.Update.WakerIDs) != 0 {
			sort.Strings(detail.Update.WakerIDs)
			detail.Update.Available = true
			detail.Update.CatalogKey = entry.Key
			detail.Update.Version = target
			detail.Update.ArchiveSHA256 = entry.ArchiveSHA256
		}
		return
	}
}

// upgradeIndices 返回统一切换中实际需要升级的安装，任一未知版本或降级都会使整个候选无效。
func upgradeIndices(target string, installed []string) []int {
	target = normalizedPluginVersion(target)
	if !semver.IsValid(target) {
		return nil
	}
	var lower []int
	for index, version := range installed {
		current := normalizedPluginVersion(version)
		if !semver.IsValid(current) || semver.Compare(target, current) < 0 {
			return nil
		}
		if semver.Compare(target, current) > 0 {
			lower = append(lower, index)
		}
	}
	return lower
}

// rollbackSummary 只从当前安装与上一次成功切换保存的版本推导，不读取公开目录。
func rollbackSummary(state models.PluginState) *PluginRollbackResponse {
	if state.Plugin.RollbackVersionID == "" || len(state.Installations) == 0 {
		return nil
	}
	currentID := state.Installations[0].VersionID
	for _, relation := range state.Installations {
		if relation.VersionID != currentID {
			return nil
		}
	}
	var current models.PluginVersion
	var previous models.PluginVersion
	for _, version := range state.Versions {
		switch version.ID {
		case currentID:
			current = version
		case state.Plugin.RollbackVersionID:
			previous = version
		}
	}
	if previous.ID == "" || previous.Retired || packageplugin.ValidateContract(previous.Package) != nil {
		return nil
	}
	from, to := normalizedPluginVersion(current.Version), normalizedPluginVersion(previous.Version)
	if !semver.IsValid(from) || !semver.IsValid(to) || semver.Compare(to, from) >= 0 {
		return nil
	}
	return &PluginRollbackResponse{Available: true, VersionID: previous.ID, Version: previous.Version}
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

// closeIdlePlugin 将身份变更前的消费者回收交回真实会话所有者。
func closeIdlePlugin(ctx context.Context, id string) error {
	if global.AgentThreads != nil {
		return global.AgentThreads.CloseIdlePlugin(ctx, id)
	}
	return nil
}

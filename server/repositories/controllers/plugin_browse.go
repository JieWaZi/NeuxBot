package controllers

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// BrowseInstalled 只在真实全局安装集合中过滤和分页；版本搜索不展开包或历史资料。
func (c *pluginController) BrowseInstalled(ctx context.Context, query string, offset, limit int) ([]models.Plugin, int64, error) {
	if offset < 0 || limit < 1 || limit > constants.PluginBrowseMaximumPageSize {
		return nil, 0, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	installed := db.Model(&models.WakerPlugin{}).Select("plugin_id")
	candidates := db.Model(&models.Plugin{}).Where("id IN (?)", installed)
	if query = strings.TrimSpace(query); query != "" {
		// 搜索全部实际安装版本的公开名称与简介；准备版本和未安装历史不参与。
		match := "instr(LOWER(COALESCE(json_extract(package, ?), '')), ?) > 0"
		versionMatch := []string{}
		arguments := []any{}
		for _, path := range []string{"$.manifest.name", "$.manifest.description", "$.presentation.displayName", "$.presentation.shortDescription", "$.presentation.displayNames.en", `$.presentation.displayNames."zh-CN"`, "$.presentation.localizedDescriptions.en", `$.presentation.localizedDescriptions."zh-CN"`} {
			versionMatch = append(versionMatch, match)
			arguments = append(arguments, path, strings.ToLower(query))
		}
		versions := db.Model(&models.PluginVersion{}).Select("plugin_id").Where("id IN (?)", db.Model(&models.WakerPlugin{}).Select("version_id")).Where(strings.Join(versionMatch, " OR "), arguments...)
		needle := strings.ToLower(query)
		candidates = candidates.Where("instr(LOWER(name), ?) > 0 OR instr(LOWER(description), ?) > 0 OR instr(LOWER(source_key), ?) > 0 OR id IN (?)", needle, needle, needle, versions)
	}
	var total int64
	if err := candidates.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_browse_plugins", "", err)
	}
	resources := []models.Plugin{}
	if err := candidates.Select("id", "name", "description", "source_key").Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&resources).Error; err != nil {
		return nil, 0, databaseError("browse_installed_plugins", "", err)
	}
	return resources, total, nil
}

// BrowseLocalUninstalled 只补明确生成的本地技能插件，不将目录准备记录计为已安装。
func (c *pluginController) BrowseLocalUninstalled(ctx context.Context, query string, offset, limit int) ([]models.Plugin, int64, error) {
	if offset < 0 || limit < 0 || limit > constants.PluginBrowseMaximumPageSize {
		return nil, 0, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	candidates := db.Model(&models.Plugin{}).
		Where("source_key >= ? AND source_key < ?", constants.PluginLocalSkillSourcePrefix, constants.PluginLocalSkillSourcePrefix+"\uffff").
		Where("id NOT IN (?)", db.Model(&models.WakerPlugin{}).Select("plugin_id")).
		Where("EXISTS (SELECT 1 FROM plugin_versions WHERE plugin_versions.plugin_id = plugins.id AND plugins.source_key = ? || plugins.id AND json_extract(plugin_versions.provenance, '$.kind') = ?)", constants.PluginLocalSkillSourcePrefix, constants.PluginSkillBundleSourceKind)
	if query = strings.TrimSpace(query); query != "" {
		needle := strings.ToLower(query)
		candidates = candidates.Where("instr(LOWER(name), ?) > 0 OR instr(LOWER(description), ?) > 0 OR instr(LOWER(source_key), ?) > 0", needle, needle, needle)
	}
	var total int64
	if err := candidates.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_local_uninstalled_plugins", "", err)
	}
	resources := []models.Plugin{}
	if limit > 0 {
		if err := candidates.Select("id", "name", "description", "source_key").Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&resources).Error; err != nil {
			return nil, 0, databaseError("browse_local_uninstalled_plugins", "", err)
		}
	}
	return resources, total, nil
}

// InstalledSources 对目录来源做有界存在性读取，不受当前浏览搜索或分页影响。
func (c *pluginController) InstalledSources(ctx context.Context, keys []string) ([]string, error) {
	result := []string{}
	if len(keys) == 0 {
		return result, nil
	}
	if len(keys) > constants.PluginBrowseMaximumPageSize {
		return nil, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	if err := db.Model(&models.Plugin{}).Where("source_key IN ? AND id IN (?)", keys, db.Model(&models.WakerPlugin{}).Select("plugin_id")).Pluck("source_key", &result).Error; err != nil {
		return nil, databaseError("browse_installed_plugin_sources", "", err)
	}
	return result, nil
}

// ReadBrowseStates 窄取实际安装或未安装本地初始版本的公开列，不读取配置表或包内文件。
func (c *pluginController) ReadBrowseStates(ctx context.Context, ids []string) (map[string]models.PluginBrowseState, error) {
	result := map[string]models.PluginBrowseState{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > constants.PluginBrowseMaximumPageSize {
		return nil, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	versions := []models.PluginBrowseVersion{}
	columns := []string{"id", "plugin_id", "version", "digest"}
	for _, field := range []struct {
		// path 是公开包字段的固定 JSON 路径。
		path string
		// column 是窄列结果对应的字段名。
		column string
	}{
		{"$.sourcePath", "source_path"}, {"$.manifest.name", "manifest_name"}, {"$.manifest.description", "manifest_description"},
		{"$.presentation.displayName", "display_name"}, {"$.presentation.shortDescription", "short_description"},
		{"$.presentation.displayNames", "display_names"}, {"$.presentation.localizedDescriptions", "localized_descriptions"},
		{"$.presentation.category", "category"}, {"$.presentation.icons", "icons"}, {"$.icon", "icon"}, {"$.integration.authentication.strategy", "authentication_strategy"}, {"$.integration.authentication.protocol", "authentication_protocol"},
	} {
		columns = append(columns, "json_extract(package, '"+field.path+"') AS "+field.column)
	}
	installedVersions := db.Model(&models.WakerPlugin{}).Select("version_id").Where("plugin_id IN ?", ids)
	// 本地封装资源只拥有一份初始不可变版本；无安装时显式补这一版本，目录准备仍不参与。
	localVersion := "version = ? AND json_extract(provenance, '$.kind') = ? AND EXISTS (SELECT 1 FROM plugins WHERE plugins.id = plugin_versions.plugin_id AND plugins.source_key = ? || plugins.id AND plugins.id NOT IN (SELECT plugin_id FROM waker_plugins))"
	if err := db.Model(&models.PluginVersion{}).Select(strings.Join(columns, ", ")).Where("plugin_id IN ?", ids).Where("id IN (?) OR ("+localVersion+")", installedVersions, constants.PluginSkillBundleInitialVersion, constants.PluginSkillBundleSourceKind, constants.PluginLocalSkillSourcePrefix).Order("created_at DESC, id ASC").Find(&versions).Error; err != nil {
		return nil, databaseError("browse_plugin_versions", "", err)
	}
	for _, version := range versions {
		state := result[version.PluginID]
		state.Versions = append(state.Versions, version)
		result[version.PluginID] = state
	}
	var counts []struct {
		// PluginID 是被统计的资源身份。
		PluginID string
		// Total 是包含停用关系的实际伙伴数量。
		Total int64
	}
	if err := db.Model(&models.WakerPlugin{}).Select("plugin_id, COUNT(*) AS total").Where("plugin_id IN ?", ids).Group("plugin_id").Find(&counts).Error; err != nil {
		return nil, databaseError("browse_plugin_install_counts", "", err)
	}
	for _, count := range counts {
		state := result[count.PluginID]
		state.InstalledCount = count.Total
		result[count.PluginID] = state
	}
	return result, nil
}

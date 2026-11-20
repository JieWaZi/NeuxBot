package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

type pluginReferenceVersion struct {
	// ID 是用来展示图标的已保留版本。
	ID string
	// PluginID 是该版本的全局资源标识。
	PluginID string
	// Version 是包的发布版本。
	Version string
	// Icon 是包内登记的兼容图标。
	Icon string
	// IconDefault 是包内声明的常规主题图标。
	IconDefault string
	// IconDark 是包内声明的深色主题图标。
	IconDark string
}

// readPluginReferenceVersions 窄取相同展示字段，兼容旧包和已损坏包的图标缺失。
func readPluginReferenceVersions(db *gorm.DB, versionIDs *gorm.DB) ([]pluginReferenceVersion, error) {
	var versions []pluginReferenceVersion
	err := db.Model(&models.PluginVersion{}).Select("id", "plugin_id", "version",
		"CASE WHEN json_valid(package) THEN json_extract(package, '$.icon') END AS icon",
		"CASE WHEN json_valid(package) THEN json_extract(package, '$.presentation.icons.default') END AS icon_default",
		"CASE WHEN json_valid(package) THEN json_extract(package, '$.presentation.icons.dark') END AS icon_dark",
	).Where("id IN (?)", versionIDs).Find(&versions).Error
	return versions, err
}

func (v pluginReferenceVersion) reference(resource models.Plugin) models.PluginReference {
	ref := models.PluginReference{
		ID: resource.ID, Name: resource.Name, Description: resource.Description, SourceKey: resource.SourceKey,
		Version: v.Version, VersionID: v.ID, Icon: plugins.SanitizeIcon(v.Icon),
	}
	icons := plugins.ThemeValues{Default: ref.Icon, Dark: plugins.SanitizeIcon(v.IconDark)}
	if v.IconDefault != "" && v.IconDefault != v.Icon {
		icons.Default = plugins.SanitizeIcon(v.IconDefault)
	}
	if icons != (plugins.ThemeValues{}) {
		ref.Icons = &icons
	}
	return ref
}

// ReferenceIcons 查询指定插件的最新保留版本，与全局详情使用相同的版本顺序。
func (c *pluginController) ReferenceIcons(ctx context.Context, ids []string) ([]models.PluginReference, error) {
	if len(ids) == 0 || len(ids) > constants.PluginBrowseMaximumPageSize {
		return nil, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	var resources []models.Plugin
	if err := db.Select("id", "source_key").Where("id IN ?", ids).Find(&resources).Error; err != nil {
		return nil, databaseError("plugin_reference_icon_resources", "", err)
	}
	result := make([]models.PluginReference, 0, len(resources))
	if len(resources) == 0 {
		return result, nil
	}
	latest := db.Model(&models.PluginVersion{}).Select("id").
		Where("plugin_id = plugins.id").Order("created_at DESC, id ASC").Limit(1)
	versionIDs := db.Model(&models.Plugin{}).Select("(?)", latest).Where("id IN ?", ids)
	versions, err := readPluginReferenceVersions(db, versionIDs)
	if err != nil {
		return nil, databaseError("plugin_reference_icon_versions", "", err)
	}
	byPlugin := make(map[string]pluginReferenceVersion, len(versions))
	for _, version := range versions {
		byPlugin[version.PluginID] = version
	}
	for _, resource := range resources {
		result = append(result, byPlugin[resource.ID].reference(resource))
	}
	return result, nil
}

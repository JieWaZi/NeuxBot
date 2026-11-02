package plugins

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"golang.org/x/mod/semver"
)

// 保存插件配置
type UpdatePluginConfiguration struct {
	ginx.MethodPut
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body UpdateConfigurationRequest `in:"body"`
}

// Path 返回共享配置及原子版本变更入口。
func (*UpdatePluginConfiguration) Path() string { return "/:pluginId/configuration" }

// Output 在完整安装集合通过校验后原子保存配置，并将全部伙伴切换到同一目标版本。
func (o *UpdatePluginConfiguration) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	store := global.PluginControllerManager
	var preparedMaterials []models.MCPSecretDefinition
	// 外部内容与认证检查不进入数据库事务；占用保持到版本切换提交完成。
	if len(o.Body.Switches) > 0 {
		config, err := store.Configuration(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if config.Revision != o.Body.ExpectedRevision {
			return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
		}
		config = configurationChange(config, o.Body)
		versions, err := store.Versions(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		byID := map[string]models.PluginVersion{}
		for _, v := range versions {
			byID[v.ID] = v
		}
		targets := []models.PluginVersion{}
		seen := map[string]bool{}
		for _, change := range o.Body.Switches {
			version, exists := byID[change.VersionID]
			if !exists {
				return nil, ginx.WithStack(status_error.PluginVersionConflict)
			}
			if !seen[version.ID] {
				if err := validateCatalogInstallation(resource, version); err != nil {
					return nil, ginx.WithStack(err)
				}
				targets = append(targets, version)
				seen[version.ID] = true
			}
		}
		release, err := prepareInstallationVersions(ctx, targets, &config)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer release()
		preparedMaterials = config.DeliveryMaterials
	}
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		config, err := store.Configuration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if config.Revision != o.Body.ExpectedRevision {
			return status_error.PluginConfigurationConflict
		}
		config = configurationChange(config, o.Body)
		if len(o.Body.Switches) > 0 {
			config.DeliveryMaterials = preparedMaterials
		}
		versions, err := store.Versions(txctx, o.PluginID)
		if err != nil {
			return err
		}
		byID := map[string]models.PluginVersion{}
		for _, version := range versions {
			byID[version.ID] = version
		}
		installed, err := store.Installations(txctx, o.PluginID, "")
		if err != nil {
			return err
		}
		switches := map[string]VersionSwitch{}
		targetVersionID := ""
		for _, item := range o.Body.Switches {
			if _, exists := switches[item.WakerID]; exists {
				return status_error.PluginVersionConflict
			}
			if targetVersionID != "" && targetVersionID != item.VersionID {
				return status_error.PluginVersionConflict
			}
			targetVersionID = item.VersionID
			switches[item.WakerID] = item
		}
		// 更新固定确认时的全体安装；新增、移除和版本变化均须重新确认。
		if len(switches) > 0 && len(switches) != len(installed) {
			return status_error.PluginVersionConflict
		}
		checked := map[string]bool{}
		for _, relation := range installed {
			target := relation.VersionID
			if change, exists := switches[relation.WakerID]; exists {
				if change.ExpectedVersionID != relation.VersionID {
					return status_error.PluginVersionConflict
				}
				if change.VersionID != relation.VersionID {
					current, hasCurrent := byID[relation.VersionID]
					next, hasNext := byID[change.VersionID]
					currentVersion, nextVersion := normalizedPluginVersion(current.Version), normalizedPluginVersion(next.Version)
					if !hasCurrent || !hasNext || !semver.IsValid(currentVersion) || !semver.IsValid(nextVersion) || semver.Compare(nextVersion, currentVersion) <= 0 {
						return status_error.PluginVersionConflict
					}
				}
				target = change.VersionID
				checked[relation.WakerID] = true
			}
			version, exists := byID[target]
			if !exists {
				return status_error.PluginVersionConflict
			}
			if err := packageplugin.ValidateValues(version.Package, config.Values, secretValues(config)); err != nil {
				return status_error.PluginConfigurationConflict
			}
		}
		if len(installed) == 0 {
			if len(versions) == 0 {
				return status_error.PluginNotFound
			}
			if err := packageplugin.ValidateValues(versions[0].Package, config.Values, secretValues(config)); err != nil {
				return err
			}
		}
		if len(checked) != len(switches) {
			return status_error.PluginVersionConflict
		}
		if err := store.UpdateConfiguration(txctx, config, o.Body.ExpectedRevision); err != nil {
			return err
		}
		for _, item := range o.Body.Switches {
			if err := store.SwitchVersion(txctx, item.WakerID, o.PluginID, item.ExpectedVersionID, item.VersionID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

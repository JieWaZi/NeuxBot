package plugins

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"golang.org/x/mod/semver"
)

// 更新插件安装
type UpdatePluginInstallations struct {
	ginx.MethodPost
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body UpdatePluginInstallationsRequest `in:"body"`
}

// Path 返回共享配置及原子版本变更入口。
func (*UpdatePluginInstallations) Path() string { return "/:pluginId/updates" }

// Output 在完整安装集合通过校验后原子保存配置，并将全部伙伴切换到同一目标版本。
func (o *UpdatePluginInstallations) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	store := global.PluginControllerManager
	preparedAccount, err := store.Account(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if preparedAccount.Generation != o.Body.ExpectedAccountGeneration {
		return nil, ginx.WithStack(status_error.PluginAccountConflict)
	}
	if len(o.Body.Switches) == 0 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	var targets []models.PluginVersion
	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	// 外部内容与认证检查不进入数据库事务；占用保持到版本切换提交完成。
	if len(o.Body.Switches) > 0 {
		config, err := store.Configuration(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if config.Revision != o.Body.ExpectedRevision {
			return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
		}
		config = configurationChange(config, o.Body.configuration())
		versions, err := store.Versions(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		byID := map[string]models.PluginVersion{}
		for _, v := range versions {
			byID[v.ID] = v
		}
		seen := map[string]bool{}
		for _, change := range o.Body.Switches {
			version, exists := byID[change.VersionID]
			if !exists {
				return nil, ginx.WithStack(status_error.PluginVersionConflict)
			}
			if !seen[version.ID] {
				if err := validatePluginInstallationSource(ctx.Request.Context(), resource, version); err != nil {
					return nil, ginx.WithStack(err)
				}
				targets = append(targets, version)
				seen[version.ID] = true
			}
		}
		release, err := prepareInstallationVersions(ctx, targets, &config, &preparedAccount)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer release()
	}
	err = global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
		config, err := store.Configuration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if config.Revision != o.Body.ExpectedRevision {
			return status_error.PluginConfigurationConflict
		}
		config = configurationChange(config, o.Body.configuration())
		generation, err := store.AccountGeneration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if generation != o.Body.ExpectedAccountGeneration {
			return status_error.PluginAccountConflict
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
		if len(preparedAccount.DeliveryMaterials) > 0 {
			if err := store.UpdateAccount(txctx, preparedAccount, o.Body.ExpectedAccountGeneration); err != nil {
				return err
			}
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
	generation := o.Body.ExpectedAccountGeneration
	if len(preparedAccount.DeliveryMaterials) > 0 {
		generation++
	}
	commitInstallationAccount(targets, o.Body.ExpectedRevision, o.Body.ExpectedAccountGeneration, o.Body.ExpectedRevision+1, generation)
	return ginx.CommonSuccessResponse(), nil
}

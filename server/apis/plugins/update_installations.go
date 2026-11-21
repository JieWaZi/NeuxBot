package plugins

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
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
	generation, err := store.AccountGeneration(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if generation != o.Body.ExpectedAccountGeneration {
		return nil, ginx.WithStack(status_error.PluginAccountConflict)
	}
	if len(o.Body.Switches) == 0 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	revision, err := store.ConfigurationRevision(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if revision != o.Body.ExpectedRevision {
		return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
	}
	plan, err := o.planVersionSwitch(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 托管授权包没有配置字段，切换时只保留修订进行条件提交。
	config := models.PluginConfiguration{PluginID: o.PluginID, Revision: revision}
	if _, hosted := plan.target.Package.HostedOAuthServer(); !plan.changesAuthorization || !hosted {
		config, err = store.Configuration(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if config.Revision != revision {
			return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
		}
	}
	config = configurationChange(config, o.Body.configuration())

	account := models.PluginAccount{PluginID: o.PluginID, Generation: generation}
	if !plan.changesAuthorization {
		account, err = store.Account(ctx, o.PluginID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if account.Generation != generation {
			return nil, ginx.WithStack(status_error.PluginAccountConflict)
		}
	}
	if err := validatePluginInstallationSource(ctx.Request.Context(), resource, plan.target); err != nil {
		return nil, ginx.WithStack(err)
	}
	if plan.invalidVersionOrder {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}

	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	targets := []models.PluginVersion{plan.target}
	release, err := prepareInstallationVersions(ctx, targets, &config, &account, plan.changesAuthorization)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	err = global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
		latestRevision, err := store.ConfigurationRevision(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if latestRevision != revision {
			return status_error.PluginConfigurationConflict
		}
		generation, err := store.AccountGeneration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if generation != o.Body.ExpectedAccountGeneration {
			return status_error.PluginAccountConflict
		}
		latest, err := o.planVersionSwitch(txctx)
		if err != nil {
			return err
		}
		if latest.invalidVersionOrder || latest.target.Digest != plan.target.Digest || latest.target.TrustedAt != plan.target.TrustedAt ||
			latest.changesAuthorization != plan.changesAuthorization {
			return status_error.PluginVersionConflict
		}
		if !plan.changesAuthorization && plan.switchesVersion {
			if err := installationService().ValidateAuthorizationBinding(txctx, latest.target); err != nil {
				return err
			}
		}
		if err := store.UpdateConfiguration(txctx, config, o.Body.ExpectedRevision); err != nil {
			return err
		}
		if plan.changesAuthorization {
			if err := store.ResetAuthorization(txctx, o.PluginID, generation); err != nil {
				return err
			}
		} else if len(account.DeliveryMaterials) > 0 {
			if err := store.UpdateAccount(txctx, account, generation); err != nil {
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
	if !plan.changesAuthorization {
		generation := o.Body.ExpectedAccountGeneration
		if len(account.DeliveryMaterials) > 0 {
			generation++
		}
		commitInstallationAccount(targets, o.Body.ExpectedRevision, o.Body.ExpectedAccountGeneration, o.Body.ExpectedRevision+1, generation)
	}
	return ginx.CommonSuccessResponse(), nil
}

type pluginVersionSwitchPlan struct {
	// target 是所有伙伴共同切换到的固定版本。
	target models.PluginVersion
	// switchesVersion 表示至少一条安装的固定版本将更新。
	switchesVersion bool
	// invalidVersionOrder 表示目标版本未高于任一被替换版本。
	invalidVersionOrder bool
	// changesAuthorization 表示新版本不能继续使用切换前的共享授权。
	changesAuthorization bool
}

func (o *UpdatePluginInstallations) planVersionSwitch(ctx context.Context) (pluginVersionSwitchPlan, error) {
	var plan pluginVersionSwitchPlan
	if len(o.Body.Switches) == 0 {
		return plan, status_error.BadRequest
	}
	store := global.PluginControllerManager
	targetID := o.Body.Switches[0].VersionID
	switches := make(map[string]VersionSwitch, len(o.Body.Switches))
	for _, change := range o.Body.Switches {
		if _, exists := switches[change.WakerID]; exists || change.VersionID != targetID {
			return plan, status_error.PluginVersionConflict
		}
		switches[change.WakerID] = change
	}
	installed, err := store.Installations(ctx, o.PluginID, "")
	if err != nil {
		return plan, err
	}
	if len(installed) != len(switches) {
		return plan, status_error.PluginVersionConflict
	}
	target, err := store.Version(ctx, targetID)
	if err != nil {
		if errors.Is(err, status_error.PluginNotFound) {
			return plan, status_error.PluginVersionConflict
		}
		return plan, err
	}
	if target.PluginID != o.PluginID {
		return plan, status_error.PluginVersionConflict
	}
	versions, err := store.InstalledVersions(ctx, o.PluginID)
	if err != nil {
		return plan, err
	}
	byID := make(map[string]models.PluginVersion, len(versions))
	for _, version := range versions {
		byID[version.ID] = version
	}
	plan.target = target
	to := normalizedPluginVersion(target.Version)
	for _, relation := range installed {
		change, exists := switches[relation.WakerID]
		if !exists || change.ExpectedVersionID != relation.VersionID {
			return plan, status_error.PluginVersionConflict
		}
		if targetID == relation.VersionID {
			continue
		}
		current, exists := byID[relation.VersionID]
		if !exists {
			return plan, status_error.PluginVersionConflict
		}
		from := normalizedPluginVersion(current.Version)
		if !semver.IsValid(from) || !semver.IsValid(to) || semver.Compare(to, from) <= 0 {
			plan.invalidVersionOrder = true
		}
		plan.switchesVersion = true
	}
	for _, version := range versions {
		if version.ID != targetID && version.Package.ChangesHostedOAuthBinding(target.Package) {
			plan.changesAuthorization = true
			break
		}
	}
	return plan, nil
}

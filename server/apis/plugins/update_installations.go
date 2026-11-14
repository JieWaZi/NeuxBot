package plugins

import (
	"context"
	"time"

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

// Output 固定已核验的目标，并原子提交全部伙伴、共享配置、账号材料及回滚目标。
func (o *UpdatePluginInstallations) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if len(o.Body.Switches) == 0 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	store := global.PluginControllerManager
	target, err := store.Version(ctx, o.Body.Switches[0].VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := validatePluginInstallationSource(ctx.Request.Context(), resource, target, o.Body.Rollback); err != nil {
		return nil, ginx.WithStack(err)
	}

	config, err := store.Configuration(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if config.Revision != o.Body.ExpectedRevision {
		return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
	}
	config = configurationChange(config, o.Body.configuration())
	account, err := store.Account(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.Generation != o.Body.ExpectedAccountGeneration {
		return nil, ginx.WithStack(status_error.PluginAccountConflict)
	}
	if _, err := o.validateSwitch(ctx, target); err != nil {
		return nil, ginx.WithStack(err)
	}
	previousAccount := account
	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	// 先完成可取消的内容、配置和授权检查，再进入短事务。
	release, err := installationService().PrepareVersion(ctx.Request.Context(), target, config, &account, true)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()

	accountChanged := installationAccountChanged(previousAccount, account)
	err = global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
		revision, err := store.ConfigurationRevision(txctx, resource.ID)
		if err != nil {
			return err
		}
		if revision != o.Body.ExpectedRevision {
			return status_error.PluginConfigurationConflict
		}
		generation, err := store.AccountGeneration(txctx, resource.ID)
		if err != nil {
			return err
		}
		if generation != o.Body.ExpectedAccountGeneration {
			return status_error.PluginAccountConflict
		}
		previous, err := o.validateSwitch(txctx, target)
		if err != nil {
			return err
		}
		if err := store.UpdateConfiguration(txctx, config, revision); err != nil {
			return err
		}
		if accountChanged {
			if err := store.UpdateAccount(txctx, account, generation); err != nil {
				return err
			}
		}
		for _, item := range o.Body.Switches {
			if err := store.SwitchVersion(txctx, item.WakerID, o.PluginID, item.ExpectedVersionID, target.ID); err != nil {
				return err
			}
		}
		if o.Body.Rollback {
			previous = ""
		}
		return store.SetRollbackVersion(txctx, resource.ID, previous)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	generation := account.Generation
	if accountChanged {
		generation++
	}
	commitInstallationAccount(target, config.Revision, account.Generation, config.Revision+1, generation)
	return ginx.CommonSuccessResponse(), nil
}

// validateSwitch 检查实际安装集合与用户确认的一致，且只允许升级或明确回到上一个已安装版本。
func (o *UpdatePluginInstallations) validateSwitch(ctx context.Context, target models.PluginVersion) (string, error) {
	store := global.PluginControllerManager
	resource, err := store.Get(ctx, o.PluginID)
	if err != nil {
		return "", err
	}
	currentTarget, err := store.Version(ctx, target.ID)
	if err != nil {
		return "", err
	}
	if currentTarget.PluginID != resource.ID || currentTarget.Digest != target.Digest || currentTarget.Retired {
		return "", status_error.PluginVersionConflict
	}
	if o.Body.Rollback && target.ID != resource.RollbackVersionID {
		return "", status_error.PluginVersionConflict
	}
	installed, err := store.Installations(ctx, o.PluginID, "")
	if err != nil {
		return "", err
	}
	if len(installed) != len(o.Body.Switches) {
		return "", status_error.PluginVersionConflict
	}
	expected := make(map[string]string, len(o.Body.Switches))
	for _, item := range o.Body.Switches {
		if _, exists := expected[item.WakerID]; exists || item.VersionID != target.ID || item.ExpectedVersionID == "" {
			return "", status_error.PluginVersionConflict
		}
		expected[item.WakerID] = item.ExpectedVersionID
	}
	targetVersion := normalizedPluginVersion(target.Version)
	if !semver.IsValid(targetVersion) {
		return "", status_error.PluginVersionConflict
	}
	previous := installed[0].VersionID
	checked := make(map[string]bool, len(installed))
	changed := false
	pinned := target.ID == resource.RollbackVersionID
	for _, relation := range installed {
		if expected[relation.WakerID] != relation.VersionID {
			return "", status_error.PluginVersionConflict
		}
		if relation.VersionID != previous {
			previous = ""
		}
		if relation.VersionID == target.ID {
			pinned = true
			continue
		}
		changed = true
		if checked[relation.VersionID] {
			continue
		}
		current, err := store.Version(ctx, relation.VersionID)
		if err != nil {
			return "", err
		}
		oldVersion := normalizedPluginVersion(current.Version)
		if !semver.IsValid(oldVersion) || semver.Compare(targetVersion, oldVersion) != switchDirection(o.Body.Rollback) {
			return "", status_error.PluginVersionConflict
		}
		checked[relation.VersionID] = true
	}
	if !changed || o.Body.Rollback && previous == "" || !pinned && currentTarget.PreparedUntil != 0 && currentTarget.PreparedUntil <= time.Now().UnixMilli() {
		return "", status_error.PluginVersionConflict
	}
	return previous, nil
}

func switchDirection(rollback bool) int {
	if rollback {
		return -1
	}
	return 1
}

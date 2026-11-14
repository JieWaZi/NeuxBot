package plugins

import (
	"context"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func publishProviderChange(provider string) {
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "global"}, map[string]string{"provider": provider})
	}
}

// removePluginReferences 与全量删除共用插件唯一身份边界。
func removePluginReferences(ctx context.Context, plugin, waker string, versions []models.PluginVersion, validate, remove func(context.Context) error) error {
	store := global.PluginControllerManager
	otherInstallations := func(readContext context.Context) (bool, error) {
		if waker == "" {
			return false, nil
		}
		current, err := store.Installations(readContext, plugin, "")
		return slices.ContainsFunc(current, func(relation models.WakerPlugin) bool { return relation.WakerID != waker }), err
	}
	retained, err := otherInstallations(ctx)
	if err != nil {
		return err
	}
	commit := func(txctx context.Context) error {
		if err := validate(txctx); err != nil {
			return err
		}
		current, err := otherInstallations(txctx)
		if err != nil {
			return err
		}
		if current != retained {
			return status_error.PluginVersionConflict
		}
		return remove(txctx)
	}
	if retained {
		// 共享账号还有其他真实安装，无需关闭消费者或触碰上游身份。
		return global.TransactionControllerManager.Transaction(ctx, commit)
	}
	if err := closeIdlePlugin(ctx, plugin); err != nil {
		return err
	}
	account, err := store.Account(ctx, plugin)
	if err != nil {
		return err
	}
	bridgeVersion, err := removalBridgeVersion(ctx, plugin, account.AuthorizedVersionID, versions)
	if err != nil {
		return err
	}
	var guard *authorization.ReferenceGuard
	resetRequired := bridgeVersion != nil
	if global.PluginBridges != nil {
		if bridgeVersion != nil {
			guard, err = installationService().BridgeRemoval(ctx, *bridgeVersion)
			if err != nil {
				return err
			}
		} else {
			var driver *commandbridge.Driver
			driver, guard, err = global.PluginBridges.GuardRemoval(plugin)
			if err != nil {
				return err
			}
			if driver != nil {
				resetRequired = true
				// 等待授权本身固定了包，清理进程也须持有相同内容。
				release, err := driver.Retain()
				if err != nil {
					guard.Close()
					return err
				}
				guard.BindRelease(release)
			}
		}
		defer guard.Close()
	}
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if err := validate(txctx); err != nil {
			return err
		}
		retained, err := otherInstallations(txctx)
		if err != nil {
			return err
		}
		if retained {
			return status_error.PluginVersionConflict
		}
		generation, err := store.AccountGeneration(txctx, plugin)
		if err != nil {
			return err
		}
		if generation != account.Generation {
			return status_error.PluginAccountConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	if guard != nil && resetRequired {
		if err := guard.StopLogin(ctx); err != nil {
			return err
		}
		if err := guard.Logout(ctx); err != nil {
			return err
		}
		publishProviderChange("plugin:" + plugin)
	}
	if err := global.TransactionControllerManager.Transaction(ctx, commit); err != nil {
		return err
	}
	if guard != nil && waker == "" {
		global.PluginBridges.Retire(plugin, guard)
	}
	return nil
}

func removalBridgeVersion(ctx context.Context, plugin string, authorizedVersionID *string, versions []models.PluginVersion) (*models.PluginVersion, error) {
	usable := func(version models.PluginVersion) bool {
		if version.Retired || version.Package.Integration.Authentication == nil || packageplugin.ValidateContract(version.Package) != nil {
			return false
		}
		_, err := packageplugin.ClientExtension(version.Package)
		return err == nil
	}
	store := global.PluginControllerManager
	if authorizedVersionID != nil {
		if *authorizedVersionID == "" {
			return nil, nil
		}
		version, err := store.Version(ctx, *authorizedVersionID)
		if err != nil {
			return nil, err
		}
		if version.PluginID != plugin || !usable(version) {
			return nil, status_error.PluginVersionConflict
		}
		return &version, nil
	}
	for _, version := range versions {
		if usable(version) {
			return &version, nil
		}
	}
	// 旧记录可能曾经授权后切换到无需账号的版本，最后卸载仍使用最近保留的合法驱动。
	retained, err := store.Versions(ctx, plugin)
	if err != nil {
		return nil, err
	}
	for _, version := range retained {
		if usable(version) {
			return &version, nil
		}
	}
	return nil, nil
}

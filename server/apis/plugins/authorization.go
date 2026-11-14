package plugins

import (
	"context"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
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
func removePluginReferences(ctx context.Context, plugin, waker string, validate, remove func(context.Context) error) error {
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
	var guard *authorization.ReferenceGuard
	resetRequired := false
	if global.PluginBridges != nil {
		if account.AuthorizedVersionID != "" {
			version, err := store.Version(ctx, account.AuthorizedVersionID)
			if err != nil {
				return err
			}
			if version.PluginID != plugin || version.Retired {
				return status_error.PluginVersionConflict
			}
			guard, err = installationService().BridgeRemoval(ctx, version)
			if err != nil {
				return err
			}
			resetRequired = true
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

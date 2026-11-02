package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func pluginAuthorizer(ctx context.Context, id, waker string, selectedVersion ...string) (*authorization.Authorizer, *packageplugin.Authentication, error) {
	resource, err := visiblePlugin(ctx, id, waker)
	if err != nil {
		return nil, nil, err
	}
	versions, err := global.PluginControllerManager.Versions(ctx, resource.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(versions) == 0 {
		return nil, nil, status_error.PluginNotFound
	}
	version := versions[0]
	if len(selectedVersion) > 0 && selectedVersion[0] != "" {
		found := false
		for _, candidate := range versions {
			if candidate.ID == selectedVersion[0] {
				version = candidate
				found = true
				break
			}
		}
		if !found {
			return nil, nil, status_error.PluginVersionConflict
		}
	}
	if _, err := packageplugin.ClientExtension(version.Package); err != nil {
		return nil, nil, err
	}
	if declaration := version.Package.Integration.Authentication; declaration != nil {
		auth, _, err := installationService().Bridge(version)
		return auth, declaration, err
	}
	return nil, nil, status_error.PluginNotReady
}
func publishProviderChange(provider string) {
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "global"}, map[string]string{"provider": provider})
	}
}

// removePluginReferences 为伙伴卸载和资源删除复用最后身份引用的清理边界。
// CLI 清理在事务外完成；清理失败保留关系，使同一操作可以安全重试。
func removePluginReferences(ctx context.Context, plugin, waker string, versions []models.PluginVersion, validate, remove func(context.Context) error) error {
	retained, err := global.PluginControllerManager.InstalledVersionsExcept(ctx, plugin, waker)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(retained, func(version models.PluginVersion) bool { return version.PluginID == plugin }) {
		// 当前插件仍有其他安装时不触碰共享身份，无关插件的安装不参与身份引用判断。
		return global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
			if err := validate(txctx); err != nil {
				return err
			}
			current, err := global.PluginControllerManager.InstalledVersionsExcept(txctx, plugin, waker)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(current, func(version models.PluginVersion) bool { return version.PluginID == plugin }) {
				return status_error.PluginVersionConflict
			}
			return remove(txctx)
		})
	}
	if err := closeIdlePlugin(ctx, plugin); err != nil {
		return err
	}
	providers := providerNames(versions)
	guards := map[string]*authorization.ReferenceGuard{}
	defer func() {
		for _, guard := range guards {
			guard.Close()
		}
	}()
	for _, provider := range providers {
		var auth *authorization.Authorizer
		for _, version := range versions {
			if version.Package.Integration.Authentication == nil {
				continue
			}
			if _, err := packageplugin.ClientExtension(version.Package); err != nil {
				continue
			}
			if "plugin:"+version.PluginID == provider {
				var bridgeErr error
				auth, _, bridgeErr = installationService().Bridge(version)
				if bridgeErr != nil {
					return bridgeErr
				}
				break
			}
		}
		if auth != nil {
			guard, err := auth.GuardReferences()
			if err != nil {
				return err
			}
			guards[provider] = guard
		}
	}
	remaining := []string{}
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if err := validate(txctx); err != nil {
			return err
		}
		if len(guards) == 0 {
			return nil
		}
		retained, err := global.PluginControllerManager.InstalledVersionsExcept(txctx, plugin, waker)
		if err != nil {
			return err
		}
		remaining = providerNames(retained)
		return nil
	})
	if err != nil {
		return err
	}
	// 先核验全部提供者，不能清理一半后才发现另一个身份仍有工作占用。
	for provider, guard := range guards {
		if !slices.Contains(remaining, provider) && !guard.CanReset() {
			return status_error.PluginBusy
		}
	}
	for _, provider := range providers {
		if guard := guards[provider]; guard != nil && !slices.Contains(remaining, provider) {
			if err := guard.Reset(ctx); err != nil {
				return err
			}
			publishProviderChange(provider)
		}
	}
	return global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if err := validate(txctx); err != nil {
			return err
		}
		return remove(txctx)
	})
}

func providerNames(versions []models.PluginVersion) []string {
	providers := []string{}
	for _, version := range versions {
		// 无效旧声明无法再取得桥接，清理不能重新启动已移除的供应商认证。
		if _, err := packageplugin.ClientExtension(version.Package); err != nil {
			continue
		}
		if version.Package.Integration.Authentication != nil {
			identity := "plugin:" + version.PluginID
			if !slices.Contains(providers, identity) {
				providers = append(providers, identity)
			}
		}
	}
	slices.Sort(providers)
	return providers
}

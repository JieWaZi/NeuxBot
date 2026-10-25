package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func pluginAuthorizer(ctx context.Context, id, waker string, selectedVersion ...string) (*authorization.Authorizer, packageplugin.Extension, error) {
	resource, err := visiblePlugin(ctx, id, waker)
	if err != nil {
		return nil, packageplugin.Extension{}, err
	}
	versions, err := global.PluginControllerManager.Versions(ctx, resource.ID)
	if err != nil {
		return nil, packageplugin.Extension{}, err
	}
	if len(versions) == 0 {
		return nil, packageplugin.Extension{}, status_error.PluginNotFound
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
			return nil, packageplugin.Extension{}, status_error.PluginVersionConflict
		}
	}
	if declaration := version.Package.Integration.Authentication; declaration != nil {
		auth, _, err := installationService().Bridge(version)
		return auth, packageplugin.Extension{Provider: "plugin-command", Scopes: declaration.Scopes}, err
	}
	extension, err := packageplugin.ClientExtension(version.Package)
	if err != nil {
		return nil, extension, err
	}
	if global.PluginAuthorizations[extension.Provider] == nil {
		return nil, extension, status_error.PluginNotReady
	}
	return global.PluginAuthorizations[extension.Provider], extension, nil
}
func publishProviderChange(provider string) {
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "global"}, map[string]string{"provider": provider})
	}
}

// removePluginReferences 为伙伴卸载和资源删除复用最后身份引用的清理边界。
// CLI 清理在事务外完成；清理失败保留关系，使同一操作可以安全重试。
func removePluginReferences(ctx context.Context, plugin, waker string, versions []models.PluginVersion, validate, remove func(context.Context) error) error {
	providers, err := providerNames(versions)
	if err != nil {
		return err
	}
	guards := map[string]*authorization.ReferenceGuard{}
	defer func() {
		for _, guard := range guards {
			guard.Close()
		}
	}()
	for _, provider := range providers {
		auth := global.PluginAuthorizations[provider]
		if strings.HasPrefix(provider, "plugin:") {
			for _, version := range versions {
				if "plugin:"+version.PluginID == provider {
					var bridgeErr error
					auth, _, bridgeErr = installationService().Bridge(version)
					if bridgeErr != nil {
						return bridgeErr
					}
					break
				}
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
		remaining, err = providerNames(retained)
		return err
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

func providerNames(versions []models.PluginVersion) ([]string, error) {
	providers := []string{}
	for _, version := range versions {
		if version.Package.Integration.Authentication != nil {
			identity := "plugin:" + version.PluginID
			if !slices.Contains(providers, identity) {
				providers = append(providers, identity)
			}
			continue
		}
		extension, err := packageplugin.ClientExtension(version.Package)
		if err != nil {
			return nil, err
		}
		if extension.Provider != "" && !slices.Contains(providers, extension.Provider) {
			providers = append(providers, extension.Provider)
		}
	}
	slices.Sort(providers)
	return providers, nil
}

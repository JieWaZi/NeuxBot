// Package plugininstallation 复用自有插件的内容占用、就绪检查和授权桥接。
package plugininstallation

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"path/filepath"
	"slices"
)

// Config 装配共享安装的持久化与私有内容边界。
type Config struct {
	// Contents 保护执行和安装的内容占用。
	Contents *packageplugin.ContentStore
	// DataDirectory 是应用私有数据根。
	DataDirectory string
	// Bridges 复用声明驱动的共享身份。
	Bridges *commandbridge.Registry
	// BridgeEnvironment 固定受信程序资产环境。
	BridgeEnvironment []string
	// Authorizers 复用旧版受保护身份。
	Authorizers map[string]*authorization.Authorizer
}

// Service 不持有第二份安装状态。
type Service struct {
	// config 是唯一安装依赖。
	config Config
}

// New 只装配，不启动外部工作。
func New(config Config) *Service { return &Service{config: config} }

// providerSnapshotKey 限定同一安装用例中的身份核验缓存。
type providerSnapshotKey struct {
	// Provider 是已核验的身份来源。
	Provider string
}

// providerSnapshot 保存同一调用中的一次核验结果。
type providerSnapshot struct {
	// Account 是公开账号摘要。
	Account authorization.Account
	// Err 是稳定的边界错误。
	Err error
}

// Readiness 核验已信任版本、共享配置和真实账号范围。
func (s *Service) Readiness(ctx context.Context, version models.PluginVersion, config models.PluginConfiguration, verify bool) (authorization.Account, error) {
	return s.readiness(ctx, version, config, verify, false)
}

// Summary 只从包的数据库快照、配置和最近核验摘要派生展示状态。
func (s *Service) Summary(version models.PluginVersion, config models.PluginConfiguration) (authorization.Account, error) {
	return s.readiness(context.Background(), version, config, false, true)
}

func (s *Service) readiness(ctx context.Context, version models.PluginVersion, config models.PluginConfiguration, verify, summary bool) (authorization.Account, error) {
	account := authorization.Account{Scopes: []string{}}
	if version.Package.RequiresTrust() && version.TrustedAt == 0 {
		return account, status_error.MCPConnectorTrustRequired
	}
	if err := packageplugin.ValidateValues(version.Package, config.Values, secretValues(config)); err != nil {
		return account, err
	}
	extension, err := packageplugin.ClientExtension(version.Package)
	if err != nil {
		return account, err
	}
	if err := packageplugin.ValidateProviderServers(extension, version.Package); err != nil {
		return account, err
	}
	available := map[string]bool{}
	for _, skill := range version.Package.Skills {
		available[skill.Key] = true
	}
	for _, server := range version.Package.Servers {
		available[server.Key] = true
	}
	for _, key := range extension.RequiredComponents {
		if !available[key] {
			return account, status_error.PluginNotReady
		}
	}
	if version.Package.Integration.Authentication != nil {
		var auth *authorization.Authorizer
		if summary {
			if s.config.Bridges != nil {
				account = s.config.Bridges.CachedAccount(version.PluginID, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath))
			}
		} else {
			auth, _, err = s.Bridge(version)
			if err != nil {
				return account, err
			}
			account, err = auth.Status(ctx, verify)
			if err != nil {
				return account, err
			}
		}
		if !account.Available {
			return account, status_error.PluginNotReady
		}
		for _, scope := range version.Package.Integration.Authentication.Scopes {
			if !slices.Contains(account.Scopes, scope) {
				return account, status_error.PluginNotReady
			}
		}
		return account, nil
	}
	if extension.Provider != "" {
		if s.config.Authorizers[extension.Provider] == nil {
			return account, status_error.PluginNotReady
		}
		if !summary {
			if _, err := os.Stat(s.config.Authorizers[extension.Provider].Binary()); err != nil {
				return account, status_error.PluginNotReady
			}
		}
		if summary {
			account = s.config.Authorizers[extension.Provider].CachedAccount()
		} else if snapshot, ok := ctx.Value(providerSnapshotKey{Provider: extension.Provider}).(providerSnapshot); ok && !verify {
			account, err = snapshot.Account, snapshot.Err
		} else {
			account, err = s.config.Authorizers[extension.Provider].Status(ctx, verify)
		}
		if err != nil {
			return account, err
		}
		if !account.Available {
			return account, status_error.PluginNotReady
		}
		for _, scope := range extension.Scopes {
			if !s.config.Authorizers[extension.Provider].ScopeGranted(account.Scopes, scope) {
				return account, status_error.PluginNotReady
			}
		}

	}
	return account, nil
}

// PrepareVersions 为首次安装与版本切换共同核验固定内容、依赖和授权，并保留占用到提交结束。
func (s *Service) PrepareVersions(ctx context.Context, versions []models.PluginVersion, config models.PluginConfiguration) (func(), error) {
	releases := []func(){}
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	prepared := false
	defer func() {
		if !prepared {
			release()
		}
	}()
	providers := map[string]bool{}
	for _, version := range versions {
		root, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
		if err != nil {
			return nil, err
		}
		if s.config.Contents != nil {
			retained, err := s.config.Contents.Acquire(root)
			if err != nil {
				return nil, err
			}
			releases = append(releases, retained)
		}
		if err := packageplugin.VerifyContent(root, version.Digest); err != nil {
			return nil, err
		}
		if version.Package.Integration.Authentication != nil {
			_, _, retained, err := s.BridgeLease(version)
			if err != nil {
				return nil, err
			}
			releases = append(releases, retained)
		}
		extension, err := packageplugin.ClientExtension(version.Package)
		if err != nil {
			return nil, err
		}
		if extension.Provider != "" {
			providers[extension.Provider] = true
		}
	}
	for provider := range providers {
		if auth := s.config.Authorizers[provider]; auth != nil {
			retained, err := auth.Acquire()
			if err != nil {
				return nil, err
			}
			releases = append(releases, retained)
			account, err := auth.Status(ctx, true)
			if err != nil {
				return nil, err
			}
			ctx = context.WithValue(ctx, providerSnapshotKey{Provider: provider}, providerSnapshot{Account: account})
		}
	}
	for _, version := range versions {
		if _, err := s.Readiness(ctx, version, config, false); err != nil {
			return nil, err
		}
	}
	prepared = true
	return release, nil
}
func secretValues(config models.PluginConfiguration) map[string]string {
	values := map[string]string{}
	for _, secret := range config.Secrets {
		if secret.Configured {
			values[secret.Name] = secret.Value
		}
	}
	return values
}

// Bridge 从已核验版本创建包内协议适配，共享身份始终由Core签发。
func (s *Service) Bridge(version models.PluginVersion) (*authorization.Authorizer, *commandbridge.Driver, error) {
	auth, driver, _, err := s.bridge(version, false)
	return auth, driver, err
}

// BridgeLease 在选定版本时立即取得唯一身份租约，防止并发版本替换。
func (s *Service) BridgeLease(version models.PluginVersion, uses ...authorization.Usage) (*authorization.Authorizer, *commandbridge.Driver, func(), error) {
	return s.bridge(version, true, uses...)
}

func (s *Service) bridge(version models.PluginVersion, acquire bool, uses ...authorization.Usage) (*authorization.Authorizer, *commandbridge.Driver, func(), error) {
	if version.TrustedAt == 0 {
		return nil, nil, nil, status_error.MCPConnectorTrustRequired
	}
	declaration := version.Package.Integration.Authentication
	if declaration == nil || declaration.Strategy != constants.PluginAuthCommandStrategy || declaration.Protocol != constants.PluginAuthProtocol || s.config.Bridges == nil {
		return nil, nil, nil, status_error.PluginNotReady
	}
	source, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := packageplugin.VerifyContent(source, version.Digest); err != nil {
		return nil, nil, nil, err
	}
	root, err := packageplugin.PackageRoot(source, version.Package)
	if err != nil {
		return nil, nil, nil, err
	}
	command, args, err := commandbridge.ResolveEntry(root, declaration.Command, declaration.Args)
	if err != nil {
		return nil, nil, nil, err
	}
	config := commandbridge.Config{Command: command, Args: args, Directory: root, DataDirectory: filepath.Join(s.config.DataDirectory, "plugins", "data", version.PluginID), PluginID: version.PluginID, Environment: s.config.BridgeEnvironment}
	// 同一来源快照的不同原生目录也必须各自固定入口，不能复用其他目标的Driver。
	bridgeDigest := version.Digest + ":" + filepath.ToSlash(version.Package.SourcePath)
	if acquire {
		return s.config.Bridges.Acquire(version.PluginID, bridgeDigest, config, declaration.Scopes, uses...)
	}
	auth, driver, err := s.config.Bridges.Get(version.PluginID, bridgeDigest, config, declaration.Scopes)
	return auth, driver, nil, err
}

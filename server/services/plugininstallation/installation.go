// Package plugininstallation 复用自有插件的内容占用、就绪检查和授权桥接。
package plugininstallation

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"path/filepath"
	"slices"
)

// Config 装配共享安装的持久化与私有内容边界。
type Config struct {
	// Transactions 绑定账号材料提交与配置/账号代次的同一短事务。
	Transactions controllers.TransactionController
	// Contents 保护执行和安装的内容占用。
	Contents *packageplugin.ContentStore
	// DataDirectory 是应用私有数据根。
	DataDirectory string
	// Bridges 复用声明驱动的共享身份。
	Bridges *commandbridge.Registry
	// BridgeEnvironment 固定受信程序资产环境。
	BridgeEnvironment []string
	// Store 复用唯一共享配置的加密持久化。
	Store controllers.PluginController
	// CloseIdlePlugin 在身份管理前关闭相关闲置原生消费者。
	CloseIdlePlugin func(context.Context, string) error
}

// Service 不持有第二份安装状态。
type Service struct {
	// config 是唯一安装依赖。
	config Config
}

// New 只装配，不启动外部工作。
func New(config Config) *Service { return &Service{config: config} }

// Summary 在显式配置预检中使用本地配置和精确上下文缓存，不启动外部程序。
func (s *Service) Summary(version models.PluginVersion, config models.PluginConfiguration, generation int64) (authorization.Account, error) {
	account := authorization.Account{Scopes: []string{}}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return account, err
	}
	if version.Package.RequiresTrust() && version.TrustedAt == 0 {
		return account, status_error.MCPConnectorTrustRequired
	}
	if err := packageplugin.ValidateValues(version.Package, config.Values, secretValues(config)); err != nil {
		return account, err
	}
	if declaration := version.Package.Integration.Authentication; declaration != nil {
		if s.config.Bridges != nil {
			account, _, _ = s.config.Bridges.CachedAuthorization(version.PluginID, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), commandbridge.OperationContext{PluginID: version.PluginID, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: config.Revision, AccountGeneration: generation})
		}
		if !account.Available {
			return account, status_error.PluginNotReady
		}
		for _, scope := range declaration.Scopes {
			if !slices.Contains(account.Scopes, scope) {
				return account, status_error.PluginNotReady
			}
		}
	}
	return account, nil
}

// PrepareVersions 为首次安装和版本切换核验固定内容及拟提交配置，并将私有材料交回同一持久化用例。
// 全部版本共享同一个身份占用，命令按固定版本顺序执行，返回占用须保留到提交结束。
func (s *Service) PrepareVersions(ctx context.Context, versions []models.PluginVersion, config *models.PluginConfiguration, preparedAccount *models.PluginAccount) (func(), error) {
	preparedConfig := *config
	accountMaterials := slices.Clone(preparedAccount.DeliveryMaterials)
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

	identities := map[string]bool{}
	primaryAuth := map[string]*authorization.Authorizer{}
	primaryDriver := map[string]*commandbridge.Driver{}
	primaryVersion := map[string]string{}
	for _, version := range versions {
		if err := s.CloseIdle(ctx, version.PluginID); err != nil {
			return nil, err
		}
		if version.Package.Integration.Authentication != nil && !identities[version.PluginID] {
			auth, driver, retained, err := s.BridgeLease(ctx, version)
			if err != nil {
				return nil, err
			}
			releases = append(releases, retained)
			identities[version.PluginID] = true
			primaryAuth[version.PluginID] = auth
			primaryDriver[version.PluginID] = driver
			primaryVersion[version.PluginID] = version.ID
		}
	}
	for _, version := range versions {
		if err := packageplugin.ValidateContract(version.Package); err != nil {
			return nil, err
		}
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
		if version.Package.RequiresTrust() && version.TrustedAt == 0 {
			return nil, status_error.MCPConnectorTrustRequired
		}
		if err := packageplugin.ValidateValues(version.Package, preparedConfig.Values, secretValues(preparedConfig)); err != nil {
			return nil, err
		}
		if version.Package.Integration.Authentication == nil {
			continue
		}
		command, err := s.commandConfig(ctx, version)
		if err != nil {
			return nil, err
		}
		if command.ConfigurationRevision != preparedConfig.Revision {
			return nil, status_error.PluginConfigurationConflict
		}
		if command.AccountGeneration != preparedAccount.Generation {
			return nil, status_error.PluginAccountConflict
		}
		driver := commandbridge.New(command)
		var account authorization.Account
		if primaryVersion[version.PluginID] == version.ID {
			driver = primaryDriver[version.PluginID]
			account, err = primaryAuth[version.PluginID].Status(ctx, true)
		} else {
			account, err = driver.Status(ctx, true)
		}
		if err != nil {
			return nil, err
		}
		if !account.Available {
			return nil, status_error.PluginNotReady
		}
		for _, scope := range version.Package.Integration.Authentication.Scopes {
			if !slices.Contains(account.Scopes, scope) {
				return nil, status_error.PluginNotReady
			}
		}
		for _, material := range accountMaterials {
			var saved deliveryMaterial
			if json.Unmarshal([]byte(material.Value), &saved) != nil {
				return nil, status_error.PluginForbidden
			}
			if saved.AccountID != "" && saved.AccountID != account.ID {
				return nil, status_error.PluginAccountConflict
			}
		}
		materials, err := ResolveDeliveryMaterials(ctx, driver, version, preparedConfig, account.ID)
		if err != nil {
			return nil, err
		}
		for _, material := range materials {
			accountMaterials = slices.DeleteFunc(accountMaterials, func(old models.MCPSecretDefinition) bool { return old.ID == material.ID })
			accountMaterials = append(accountMaterials, material)
		}
	}

	preparedAccount.DeliveryMaterials = accountMaterials
	*config = preparedConfig
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
func (s *Service) Bridge(ctx context.Context, version models.PluginVersion) (*authorization.Authorizer, *commandbridge.Driver, error) {
	auth, driver, _, err := s.bridge(ctx, version, false)
	return auth, driver, err
}

// BridgeLease 在选定版本时立即取得唯一身份租约，防止并发版本替换。
func (s *Service) BridgeLease(ctx context.Context, version models.PluginVersion, uses ...authorization.Usage) (*authorization.Authorizer, *commandbridge.Driver, func(), error) {
	return s.bridge(ctx, version, true, uses...)
}

func (s *Service) commandConfig(ctx context.Context, version models.PluginVersion) (commandbridge.Config, error) {
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return commandbridge.Config{}, err
	}
	if _, err := packageplugin.ClientExtension(version.Package); err != nil {
		return commandbridge.Config{}, err
	}
	if version.TrustedAt == 0 {
		return commandbridge.Config{}, status_error.MCPConnectorTrustRequired
	}
	declaration := version.Package.Integration.Authentication
	if declaration == nil || declaration.Strategy != constants.PluginAuthCommandStrategy || declaration.Protocol != constants.PluginAuthProtocol {
		return commandbridge.Config{}, status_error.PluginNotReady
	}
	source, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
	if err != nil {
		return commandbridge.Config{}, err
	}
	if err := packageplugin.VerifyContent(source, version.Digest); err != nil {
		return commandbridge.Config{}, err
	}
	root, err := packageplugin.PackageRoot(source, version.Package)
	if err != nil {
		return commandbridge.Config{}, err
	}
	command, args, err := commandbridge.ResolveEntry(root, declaration.Command, declaration.Args)
	if err != nil {
		return commandbridge.Config{}, err
	}
	config := commandbridge.Config{Command: command, Args: args, Directory: root, DataDirectory: filepath.Join(s.config.DataDirectory, "plugins", "data", version.PluginID), PluginID: version.PluginID, Environment: s.config.BridgeEnvironment}
	config.VersionID = version.ID
	config.Digest = version.Digest
	if s.config.Store != nil {
		current, err := s.config.Store.ConfigurationRevision(ctx, version.PluginID)
		if err != nil {
			return commandbridge.Config{}, err
		}
		generation, err := s.config.Store.AccountGeneration(ctx, version.PluginID)
		if err != nil {
			return commandbridge.Config{}, err
		}
		config.ConfigurationRevision = current
		config.AccountGeneration = generation
	}
	return config, nil
}

func (s *Service) bridge(ctx context.Context, version models.PluginVersion, acquire bool, uses ...authorization.Usage) (*authorization.Authorizer, *commandbridge.Driver, func(), error) {
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, nil, nil, err
	}
	if s.config.Bridges == nil {
		return nil, nil, nil, status_error.PluginNotReady
	}
	config, err := s.commandConfig(ctx, version)
	if err != nil {
		return nil, nil, nil, err
	}
	declaration := version.Package.Integration.Authentication

	if s.config.Store != nil {
		config.PrepareDelivery = func(ctx context.Context, driver *commandbridge.Driver, account authorization.Account) error {
			return s.persistDelivery(ctx, version, driver, account.ID)
		}
		config.ClearDelivery = func(ctx context.Context, driver *commandbridge.Driver) error {
			return s.clearDelivery(ctx, version.PluginID, driver.OperationContext().AccountGeneration)
		}
	}
	// 同一来源快照的不同原生目录也必须各自固定入口，不能复用其他目标的Driver。
	bridgeDigest := version.Digest + ":" + filepath.ToSlash(version.Package.SourcePath)
	if acquire {
		return s.config.Bridges.Acquire(version.PluginID, bridgeDigest, config, declaration.Scopes, uses...)
	}
	auth, driver, err := s.config.Bridges.Get(version.PluginID, bridgeDigest, config, declaration.Scopes)
	return auth, driver, nil, err
}

// CloseIdle 由原生消费者所有者回收闲置资源，活动执行保持忙状态。
func (s *Service) CloseIdle(ctx context.Context, id string) error {
	if s.config.CloseIdlePlugin != nil {
		return s.config.CloseIdlePlugin(ctx, id)
	}
	return nil
}

// IdentityLease 只固定已有材料引用的共享身份，不解析或创建授权程序。
func (s *Service) IdentityLease(id string, uses ...authorization.Usage) (func(...authorization.Usage), func(), error) {
	if s.config.Bridges == nil {
		return func(...authorization.Usage) {}, func() {}, nil
	}
	return s.config.Bridges.AcquireIdentity(id, uses...)
}

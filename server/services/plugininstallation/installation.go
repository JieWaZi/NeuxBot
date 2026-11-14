// Package plugininstallation 复用自有插件的内容占用、就绪检查和授权桥接。
package plugininstallation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
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

// PrepareVersion 在管理操作期间固定目标和共享身份，检查拟提交的配置并生成所需交付材料。
func (s *Service) PrepareVersion(ctx context.Context, version models.PluginVersion, config models.PluginConfiguration, account *models.PluginAccount, switching bool) (func(), error) {
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, err
	}
	if version.Package.RequiresTrust() && version.TrustedAt == 0 {
		return nil, status_error.MCPConnectorTrustRequired
	}
	if err := packageplugin.ValidateValues(version.Package, config.Values, secretValues(config)); err != nil {
		return nil, err
	}
	root, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
	if err != nil {
		return nil, err
	}
	releases := []func(){}
	release := sync.OnceFunc(func() {
		for _, close := range slices.Backward(releases) {
			close()
		}
	})
	prepared := false
	defer func() {
		if !prepared {
			release()
		}
	}()
	if s.config.Contents != nil {
		retained, err := s.config.Contents.Acquire(root)
		if err != nil {
			return nil, err
		}
		releases = append(releases, retained)
	}
	if err := s.CloseIdle(ctx, version.PluginID); err != nil {
		return nil, err
	}
	var resolved []models.MCPSecretDefinition
	var retained []models.MCPSecretDefinition
	declaration := version.Package.Integration.Authentication
	if declaration == nil {
		if s.config.Bridges != nil {
			guard, err := s.config.Bridges.BeginChange(version.PluginID, func() error {
				return s.ValidateOperation(ctx, commandbridge.OperationContext{PluginID: version.PluginID, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: config.Revision, AccountGeneration: account.Generation})
			})
			if err != nil {
				return nil, err
			}
			releases = append(releases, guard.Close)
		}
		if err := packageplugin.VerifyContent(root, version.Digest); err != nil {
			return nil, err
		}
	} else {
		// BridgeChange 核验固定内容并阻止其他操作替换其驱动。
		guard, driver, err := s.BridgeChange(ctx, version)
		if err != nil {
			return nil, err
		}
		releases = append(releases, guard.Close)
		operation := driver.OperationContext()
		if operation.ConfigurationRevision != config.Revision {
			return nil, status_error.PluginConfigurationConflict
		}
		if operation.AccountGeneration != account.Generation {
			return nil, status_error.PluginAccountConflict
		}
		identity, err := guard.Status(ctx, true)
		if err != nil {
			return nil, err
		}
		if !identity.Available {
			return nil, status_error.PluginNotReady
		}
		for _, scope := range declaration.Scopes {
			if !slices.Contains(identity.Scopes, scope) {
				return nil, status_error.PluginNotReady
			}
		}
		retained, err = s.installedMaterials(ctx, version, account.DeliveryMaterials)
		if err != nil {
			return nil, err
		}
		for _, material := range retained {
			var saved deliveryMaterial
			if json.Unmarshal([]byte(material.Value), &saved) != nil {
				return nil, status_error.PluginForbidden
			}
			if saved.AccountID != "" && saved.AccountID != identity.ID {
				return nil, status_error.PluginAccountConflict
			}
		}
		resolved, err = ResolveDeliveryMaterials(ctx, driver, version, identity.ID)
		if err != nil {
			return nil, err
		}
		account.AuthorizedVersionID = &version.ID
	}
	if switching {
		account.DeliveryMaterials = resolved
	} else {
		// 新增安装仍须保留其他伙伴使用的固定版本材料。
		if declaration == nil {
			retained, err = s.installedMaterials(ctx, version, account.DeliveryMaterials)
			if err != nil {
				return nil, err
			}
		}
		for _, material := range resolved {
			retained = slices.DeleteFunc(retained, func(old models.MCPSecretDefinition) bool { return old.ID == material.ID })
			retained = append(retained, material)
		}
		account.DeliveryMaterials = retained
	}
	prepared = true
	return release, nil
}

// installedMaterials 只保留本次目标和仍有伙伴安装的版本槽位。
func (s *Service) installedMaterials(ctx context.Context, target models.PluginVersion, input []models.MCPSecretDefinition) ([]models.MCPSecretDefinition, error) {
	if len(input) == 0 {
		return nil, nil
	}
	slots := map[string]bool{}
	add := func(version models.PluginVersion) {
		for _, runtime := range authenticationTargets(version.Package.Integration.Authentication) {
			slots[materialScope(version, runtime)] = true
		}
	}
	add(target)
	if s.config.Store != nil {
		relations, err := s.config.Store.Installations(ctx, target.PluginID, "")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{target.ID: true}
		for _, relation := range relations {
			if seen[relation.VersionID] {
				continue
			}
			version, err := s.config.Store.Version(ctx, relation.VersionID)
			if err != nil {
				return nil, err
			}
			seen[version.ID] = true
			add(version)
		}
	}
	result := []models.MCPSecretDefinition{}
	for _, material := range input {
		if slots[material.ID] {
			result = append(result, material)
		}
	}
	return result, nil
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

// BridgeChange 为管理操作固定驱动直到围栏释放。
func (s *Service) BridgeChange(ctx context.Context, version models.PluginVersion) (*authorization.ReferenceGuard, *commandbridge.Driver, error) {
	config, err := s.bridgeConfig(ctx, version)
	if err != nil {
		return nil, nil, err
	}
	_, driver, guard, err := s.config.Bridges.AcquireChange(version.PluginID, bridgeDigest(version), config, version.Package.Integration.Authentication.Scopes)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ValidateOperation(ctx, driver.OperationContext()); err != nil {
		guard.Close()
		s.forgetMissing(ctx, version.PluginID, err)
		return nil, nil, err
	}
	release, err := driver.Retain()
	if err != nil {
		guard.Close()
		return nil, nil, err
	}
	guard.BindRelease(release)
	return guard, driver, nil
}

// BridgeRemoval 固定共享身份的最后引用清理并保留当前等待使用的版本。
func (s *Service) BridgeRemoval(ctx context.Context, version models.PluginVersion) (*authorization.ReferenceGuard, error) {
	config, err := s.bridgeConfig(ctx, version)
	if err != nil {
		return nil, err
	}
	driver, guard, err := s.config.Bridges.AcquireRemoval(version.PluginID, bridgeDigest(version), config, version.Package.Integration.Authentication.Scopes)
	if err != nil {
		return nil, err
	}
	if err := s.ValidateOperation(ctx, driver.OperationContext()); err != nil {
		guard.Close()
		s.forgetMissing(ctx, version.PluginID, err)
		return nil, err
	}
	release, err := driver.Retain()
	if err != nil {
		guard.Close()
		return nil, err
	}
	guard.BindRelease(release)
	return guard, nil
}

// ValidateOperation 在调用方占用共享身份后复核持久化事实。
func (s *Service) ValidateOperation(ctx context.Context, operation commandbridge.OperationContext) error {
	if s.config.Store == nil {
		return nil
	}
	check := func(readContext context.Context) error {
		if err := readContext.Err(); err != nil {
			return status_error.OperationCanceled
		}
		current, err := s.config.Store.Version(readContext, operation.VersionID)
		if err != nil {
			return err
		}
		if current.Retired || current.PluginID != operation.PluginID || current.Digest != operation.Digest {
			return status_error.PluginVersionConflict
		}
		revision, err := s.config.Store.ConfigurationRevision(readContext, operation.PluginID)
		if err != nil {
			return err
		}
		if revision != operation.ConfigurationRevision {
			return status_error.PluginConfigurationConflict
		}
		generation, err := s.config.Store.AccountGeneration(readContext, operation.PluginID)
		if err != nil {
			return err
		}
		if generation != operation.AccountGeneration {
			return status_error.PluginAccountConflict
		}
		return nil
	}
	var err error
	if s.config.Transactions != nil {
		err = s.config.Transactions.Transaction(ctx, check)
	} else {
		err = check(ctx)
	}
	return err
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
	config, err := s.bridgeConfig(ctx, version)
	if err != nil {
		return nil, nil, nil, err
	}
	declaration := version.Package.Integration.Authentication
	if acquire {
		auth, driver, release, err := s.config.Bridges.Acquire(version.PluginID, bridgeDigest(version), config, declaration.Scopes, uses...)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := s.ValidateOperation(ctx, driver.OperationContext()); err != nil {
			release()
			s.forgetMissing(ctx, version.PluginID, err)
			return nil, nil, nil, err
		}
		retained, err := driver.Retain()
		if err != nil {
			release()
			return nil, nil, nil, err
		}
		return auth, driver, func() { release(); retained() }, nil
	}
	auth, driver, err := s.config.Bridges.Get(version.PluginID, bridgeDigest(version), config, declaration.Scopes)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := s.ValidateOperation(ctx, driver.OperationContext()); err != nil {
		s.forgetMissing(ctx, version.PluginID, err)
		return nil, nil, nil, err
	}
	return auth, driver, nil, nil
}

func (s *Service) forgetMissing(ctx context.Context, id string, err error) {
	if errors.Is(err, status_error.PluginNotFound) && s.config.Store != nil {
		if _, err := s.config.Store.Get(ctx, id); errors.Is(err, status_error.PluginNotFound) {
			s.config.Bridges.ForgetMissing(id)
		}
	}
}

func bridgeDigest(version models.PluginVersion) string {
	return version.Digest + ":" + filepath.ToSlash(version.Package.SourcePath)
}

func (s *Service) bridgeConfig(ctx context.Context, version models.PluginVersion) (commandbridge.Config, error) {
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return commandbridge.Config{}, err
	}
	if s.config.Bridges == nil {
		return commandbridge.Config{}, status_error.PluginNotReady
	}
	config, err := s.commandConfig(ctx, version)
	if err != nil {
		return config, err
	}
	if s.config.Contents != nil {
		config.Retain = func() (func(), error) {
			source, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(config.DataDirectory, 0700); err != nil {
				return nil, status_error.PluginStorageError
			}
			return s.config.Contents.Acquire(source, config.DataDirectory)
		}
	}
	if s.config.Store != nil {
		config.PrepareDelivery = func(ctx context.Context, driver *commandbridge.Driver, account authorization.Account) error {
			err := s.persistDelivery(ctx, version, driver, account.ID)
			if err == nil {
				operation := driver.OperationContext()
				s.config.Bridges.CommitPreparation(operation, bridgeDigest(version), operation.ConfigurationRevision, operation.AccountGeneration+1)
			}
			return err
		}
		config.ClearDelivery = func(ctx context.Context, driver *commandbridge.Driver) error {
			operation := driver.OperationContext()
			// 官方退出已生效，即使请求断开也要撤销对应代次的本机交付材料。
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(constants.PluginAccountInvalidationTimeoutSeconds)*time.Second)
			defer cancel()
			err := s.resetDelivery(cleanup, version.PluginID, operation.AccountGeneration, "")
			if err == nil {
				s.config.Bridges.CommitPreparation(operation, bridgeDigest(version), operation.ConfigurationRevision, operation.AccountGeneration+1)
			}
			return err
		}
	}
	return config, nil
}

// CloseIdle 由原生消费者所有者回收闲置资源，活动执行保持忙状态。
func (s *Service) CloseIdle(ctx context.Context, id string) error {
	if s.config.CloseIdlePlugin != nil {
		return s.config.CloseIdlePlugin(ctx, id)
	}
	return nil
}

// IdentityLease 只固定已有材料引用的共享身份，不解析或创建授权程序。
func (s *Service) IdentityLease(ctx context.Context, id string, uses ...authorization.Usage) (func(...authorization.Usage), func(), error) {
	if s.config.Bridges == nil {
		return func(...authorization.Usage) {}, func() {}, nil
	}
	update, release, err := s.config.Bridges.AcquireIdentity(id, uses...)
	if err != nil {
		return nil, nil, err
	}
	if s.config.Store != nil {
		if _, err := s.config.Store.Get(ctx, id); err != nil {
			release()
			s.forgetMissing(ctx, id, err)
			return nil, nil, err
		}
	}
	return update, release, nil
}

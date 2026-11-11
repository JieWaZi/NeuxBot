package plugins

import (
	"context"
	"maps"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// prepareInstallationVersions 为首次安装和版本切换复用内容、依赖与认证预检。
// 文件和官方 CLI 在事务外读取；返回的占用必须在数据库提交后释放。
func prepareInstallationVersions(ctx context.Context, versions []models.PluginVersion, config *models.PluginConfiguration, account *models.PluginAccount) (func(), error) {
	release, err := installationService().PrepareVersions(ctx, versions, config, account)
	if err != nil {
		return nil, err
	}
	for _, version := range versions {
		root, err := packageplugin.ContentPath(global.Config.DataDirectory, version.ContentPath)
		if err == nil {
			err = packageplugin.VerifyContent(root, version.Digest)
		}
		if err == nil {
			root, err = packageplugin.PackageRoot(root, version.Package)
		}
		if err == nil {
			_, err = packageplugin.PrepareTargets(ctx, filepath.Join(global.Config.DataDirectory, "plugins", "targets"), version.PluginID, version.Package, root)
		}
		if err != nil {
			release()
			return nil, err
		}
	}
	return release, nil
}

// commitInstallationAccount 在预检租约释放前同步已提交的配置和账号代次，保留同次核验的公开身份。
func commitInstallationAccount(versions []models.PluginVersion, expectedRevision, expectedGeneration, revision, generation int64) {
	if global.PluginBridges == nil {
		return
	}
	for _, version := range versions {
		if version.Package.Integration.Authentication == nil {
			continue
		}
		// 只更新持有的精确入口；其他版本的临时核验不能替换当前共享身份。
		global.PluginBridges.CommitPreparation(commandbridge.OperationContext{PluginID: version.PluginID, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: expectedRevision, AccountGeneration: expectedGeneration}, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), revision, generation)
	}
}

// configurationChange 生成请求后的独立配置副本，预检和事务使用同一规则。
func configurationChange(config models.PluginConfiguration, input UpdateConfigurationRequest) models.PluginConfiguration {
	config.Values = maps.Clone(config.Values)
	config.Secrets = slices.Clone(config.Secrets)
	if input.Values != nil {
		config.Values = maps.Clone(input.Values)
	}
	for _, secret := range input.Secrets {
		index := slices.IndexFunc(config.Secrets, func(value models.MCPSecretDefinition) bool { return value.Name == secret.Key })
		if index >= 0 {
			if secret.Clear {
				config.Secrets = append(config.Secrets[:index], config.Secrets[index+1:]...)
			} else if secret.Value != "" {
				config.Secrets[index].Value = secret.Value
				config.Secrets[index].Configured = true
			}
		} else if !secret.Clear && secret.Value != "" {
			config.Secrets = append(config.Secrets, models.MCPSecretDefinition{ID: secret.Key, Name: secret.Key, Configured: true, Value: secret.Value})
		}
	}
	return config
}

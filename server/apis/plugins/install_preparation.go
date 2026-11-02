package plugins

import (
	"context"
	"maps"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// prepareInstallationVersions 为首次安装和版本切换复用内容、依赖与认证预检。
// 文件和官方 CLI 在事务外读取；返回的占用必须在数据库提交后释放。
func prepareInstallationVersions(ctx context.Context, versions []models.PluginVersion, config *models.PluginConfiguration) (func(), error) {
	return installationService().PrepareVersions(ctx, versions, config)
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

package plugins

import (
	"maps"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// installationAccountChanged 判断是否需要原子提交材料或注销入口的新状态。
func installationAccountChanged(before, after models.PluginAccount) bool {
	return len(before.DeliveryMaterials) != 0 || len(after.DeliveryMaterials) != 0 ||
		before.AuthorizedVersionID != after.AuthorizedVersionID
}

// commitInstallationAccount 推进绑定固定版本的公开账号摘要，保留同次核验结果。
func commitInstallationAccount(version models.PluginVersion, expectedRevision, expectedGeneration, revision, generation int64) {
	if global.PluginBridges == nil || version.Package.Integration.Authentication == nil {
		return
	}
	global.PluginBridges.CommitPreparation(commandbridge.OperationContext{PluginID: version.PluginID, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: expectedRevision, AccountGeneration: expectedGeneration}, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), revision, generation)
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

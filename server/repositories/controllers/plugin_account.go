package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm/clause"
)

// AccountGeneration 查询公开并发事实，不读取私有账号材料。
func (c *pluginController) AccountGeneration(ctx context.Context, id string) (int64, error) {
	var account models.PluginAccount
	if err := controllerDB(ctx, c.db).Select("plugin_id", "generation").Where("plugin_id = ?", id).Limit(1).Find(&account).Error; err != nil {
		return 0, databaseError("plugin_account_generation", id, err)
	}
	return account.Generation, nil
}

// Account 只供明确的账号管理和执行材料读取，加密值不返回 HTTP。
func (c *pluginController) Account(ctx context.Context, id string) (models.PluginAccount, error) {
	account := models.PluginAccount{PluginID: id}
	db := controllerDB(ctx, c.db)
	if err := db.Where("plugin_id = ?", id).Limit(1).Find(&account).Error; err != nil {
		return account, databaseError("read_plugin_account", id, err)
	}
	if len(account.DeliveryMaterials) > 0 {
		key, err := getMCPStorageKey(db)
		if err != nil {
			return account, err
		}
		if err := decryptSecretDefinitions(key, id, "plugin-account", account.DeliveryMaterials); err != nil {
			return account, err
		}
	}
	return account, nil
}

// UpdateAccount 原子替换账号材料；并发代次变化时保留原记录供调用方重新确认。
func (c *pluginController) UpdateAccount(ctx context.Context, account models.PluginAccount, expected int64) error {
	db := controllerDB(ctx, c.db)
	if _, err := c.Get(ctx, account.PluginID); err != nil {
		return err
	}
	account.DeliveryMaterials = append([]models.MCPSecretDefinition(nil), account.DeliveryMaterials...)
	if len(account.DeliveryMaterials) > 0 {
		key, err := ensureMCPStorageKey(db)
		if err != nil {
			return err
		}
		if err := encryptSecretDefinitions(key, account.PluginID, "plugin-account", account.DeliveryMaterials); err != nil {
			return err
		}
	}
	account.Generation = expected + 1
	account.UpdatedAt = nowMillis()
	result := db.Model(&models.PluginAccount{}).Where("plugin_id = ? AND generation = ?", account.PluginID, expected).Select("Generation", "AuthorizedVersionID", "DeliveryMaterials", "UpdatedAt").Updates(&account)
	if result.Error == nil && result.RowsAffected == 0 && expected == 0 {
		result = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&account)
	}
	if result.Error != nil {
		return databaseError("update_plugin_account", account.PluginID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginAccountConflict
	}
	publishCommittedChange(ctx, db, constants.PluginEventTopic, eventsResource("plugin", account.PluginID))
	return nil
}

// ConfigurationRevision 读取账号操作所需的配置并发事实，不解密秘密。
func (c *pluginController) ConfigurationRevision(ctx context.Context, id string) (int64, error) {
	var config models.PluginConfiguration
	if err := controllerDB(ctx, c.db).Select("plugin_id", "revision").Where("plugin_id = ?", id).Limit(1).Find(&config).Error; err != nil {
		return 0, databaseError("plugin_configuration_revision", id, err)
	}
	return config.Revision, nil
}

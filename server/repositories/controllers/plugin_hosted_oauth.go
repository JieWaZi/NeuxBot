package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const hostedOAuthSecretKind = "plugin-hosted-mcp-oauth"

// HostedOAuth 读取共享插件账号内的托管 OAuth，不加载其他凭据。
func (c *pluginController) HostedOAuth(ctx context.Context, id string) (models.PluginHostedOAuth, error) {
	data := models.PluginAccount{PluginID: id}
	db := controllerDB(ctx, c.db)
	if err := db.Select("plugin_id", "generation", "hosted_oauth_revision", "hosted_oauth_resource", "hosted_oauth_ciphertext", "hosted_oauth_readiness").
		Where("plugin_id = ?", id).Limit(1).Find(&data).Error; err != nil {
		return models.PluginHostedOAuth{}, databaseError("read_plugin_hosted_oauth", id, err)
	}
	result := models.PluginHostedOAuth{
		PluginID: id, Generation: data.Generation, Revision: data.HostedOAuthRevision, Resource: data.HostedOAuthResource,
		Readiness: data.HostedOAuthReadiness,
	}
	if data.HostedOAuthCiphertext == "" {
		return result, nil
	}
	key, err := getMCPStorageKey(db)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.PluginHostedOAuth{}, mcpCredentialError("get_plugin_hosted_oauth_storage_key", id, err)
	}
	if err != nil {
		return models.PluginHostedOAuth{}, err
	}
	result.Secret, err = decryptMCPValue(key, data.HostedOAuthCiphertext, secretAdditionalData(id, hostedOAuthSecretKind, result.Resource))
	if err != nil {
		return models.PluginHostedOAuth{}, mcpCredentialError("decrypt_plugin_hosted_oauth", id, err)
	}
	return result, nil
}

// HostedOAuthBinding 只读取已授权的确切资源和并发代次，清理损坏凭据时不解密。
func (c *pluginController) HostedOAuthBinding(ctx context.Context, id string) (models.PluginHostedOAuth, error) {
	var data models.PluginAccount
	db := controllerDB(ctx, c.db)
	if err := db.Select("generation", "hosted_oauth_resource", "hosted_oauth_revision").Where("plugin_id = ?", id).Limit(1).Find(&data).Error; err != nil {
		return models.PluginHostedOAuth{}, databaseError("read_plugin_hosted_oauth_binding", id, err)
	}
	return models.PluginHostedOAuth{
		PluginID: id, Generation: data.Generation, Revision: data.HostedOAuthRevision, Resource: data.HostedOAuthResource,
	}, nil
}

// HostedOAuthReadiness 只投影匹配的资源授权摘要，不需要读取加密凭据。
func (c *pluginController) HostedOAuthReadiness(ctx context.Context, id, resource string) (models.PluginHostedOAuthReadiness, bool, error) {
	if resource == "" {
		return models.PluginHostedOAuthReadiness{}, false, nil
	}
	var data models.PluginAccount
	db := controllerDB(ctx, c.db)
	result := db.Select("hosted_oauth_readiness").Where("plugin_id = ? AND hosted_oauth_resource = ?", id, resource).Limit(1).Find(&data)
	if result.Error != nil {
		return models.PluginHostedOAuthReadiness{}, false, databaseError("read_plugin_hosted_oauth_readiness", id, result.Error)
	}
	return data.HostedOAuthReadiness, result.RowsAffected == 1, nil
}

// UpdateHostedOAuth 使用账号代次和凭据修订原子替换托管 OAuth。续期保持账号代次和资源地址。
func (c *pluginController) UpdateHostedOAuth(ctx context.Context, account models.PluginHostedOAuth, replaceIdentity bool) error {
	if account.PluginID == "" || (account.Resource == "") != (account.Secret == "") || (!replaceIdentity && account.Secret == "") {
		return status_error.BadRequest
	}
	if _, err := c.Get(ctx, account.PluginID); err != nil {
		return err
	}
	db := controllerDB(ctx, c.db)
	next := models.PluginAccount{
		PluginID: account.PluginID, Generation: account.Generation, UpdatedAt: nowMillis(),
		HostedOAuthRevision: account.Revision + 1, HostedOAuthResource: account.Resource,
	}
	if replaceIdentity {
		next.Generation++
	}
	if account.Secret != "" {
		next.HostedOAuthReadiness = account.Readiness
		key, err := ensureMCPStorageKey(db)
		if err != nil {
			return err
		}
		next.HostedOAuthCiphertext, err = encryptMCPValue(key, account.Secret, secretAdditionalData(account.PluginID, hostedOAuthSecretKind, account.Resource))
		if err != nil {
			return mcpCredentialError("encrypt_plugin_hosted_oauth", account.PluginID, err)
		}
	}
	query := db.Model(&models.PluginAccount{}).
		Where("plugin_id = ? AND generation = ? AND hosted_oauth_revision = ?", account.PluginID, account.Generation, account.Revision)
	if !replaceIdentity {
		query = query.Where("hosted_oauth_resource = ? AND hosted_oauth_ciphertext <> ''", account.Resource)
	}
	result := query.Select("Generation", "UpdatedAt", "HostedOAuthRevision", "HostedOAuthResource", "HostedOAuthCiphertext", "HostedOAuthReadiness").Updates(&next)
	if result.Error == nil && result.RowsAffected == 0 && replaceIdentity && account.Generation == 0 && account.Revision == 0 {
		result = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
	}
	if result.Error != nil {
		return databaseError("update_plugin_hosted_oauth", account.PluginID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginAccountConflict
	}
	if replaceIdentity {
		publishCommittedChange(ctx, db, constants.PluginEventTopic, eventsResource("plugin", account.PluginID))
	}
	return nil
}

// ResetAuthorization 清除上一绑定的所有私有材料，使已有令牌续期无法恢复旧账号。
func (c *pluginController) ResetAuthorization(ctx context.Context, id string, generation int64) error {
	db := controllerDB(ctx, c.db)
	now := nowMillis()
	result := db.Model(&models.PluginAccount{}).Where("plugin_id = ? AND generation = ?", id, generation).Updates(map[string]any{
		"generation": generation + 1, "updated_at": now, "delivery_materials": nil,
		"hosted_oauth_resource": "", "hosted_oauth_ciphertext": "", "hosted_oauth_readiness": nil,
		"hosted_oauth_revision": gorm.Expr("hosted_oauth_revision + 1"),
	})
	if result.Error == nil && result.RowsAffected == 0 && generation == 0 {
		account := models.PluginAccount{PluginID: id, Generation: 1, UpdatedAt: now, HostedOAuthRevision: 1}
		result = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&account)
	}
	if result.Error != nil {
		return databaseError("reset_plugin_authorization", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginAccountConflict
	}
	publishCommittedChange(ctx, db, constants.PluginEventTopic, eventsResource("plugin", id))
	return nil
}

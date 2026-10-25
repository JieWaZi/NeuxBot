package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IntegrationKeyController 保存集成主体及一次签发意图，不持久保存秘密。
type IntegrationKeyController interface {
	// Get 读取主体当前的权威授权。
	Get(context.Context, string) (models.IntegrationKey, error)
	// FindByDigest 通过唯一摘要索引查找凭据，不枚举主体。
	FindByDigest(context.Context, string) (models.IntegrationKey, error)
	// List 按服务端分页列出管理记录。
	List(context.Context, int, int) ([]models.IntegrationKey, int64, error)
	// ClaimMutation 原子预留签发意图，重放返回最初身份。
	ClaimMutation(context.Context, models.IntegrationKeyMutation) (models.IntegrationKeyMutation, bool, error)
	// CompleteMutation 原子提交已预留签发的摘要和完成事实。
	CompleteMutation(context.Context, string, string, models.IntegrationKey, int64) (models.IntegrationKey, error)
	// GetMutation 返回不含秘密的签发回执。
	GetMutation(context.Context, string) (models.IntegrationKeyMutation, error)
	// UpdateAuthorization 条件修改管理字段，不接受凭据或代次覆盖。
	UpdateAuthorization(context.Context, models.IntegrationKey, int64) (models.IntegrationKey, error)
	// RemoveResourceGrants 仅撤减指定资源引用，允许清理撤销记录且保留其他管理字段。
	RemoveResourceGrants(context.Context, string, int64, map[constants.IntegrationResourceKind][]string) (models.IntegrationKey, error)
	// Delete 按编辑版本删除密钥，保留原运行和签发回执。
	Delete(context.Context, string, int64) error
	// TouchLastUsed 以一分钟粒度更新使用时间，不改变授权与编辑版本。
	TouchLastUsed(context.Context, string, int64) error
}

// integrationKeyController 使用既有事务与数据库错误边界。
type integrationKeyController struct {
	// db 是没有上层事务时使用的业务主库。
	db *gorm.DB
	// transactions 在签发和多字段条件更新时维持单一事务快照。
	transactions TransactionController
}

// NewIntegrationKeyController 创建集成 Key 存储边界。
func NewIntegrationKeyController(db *gorm.DB) IntegrationKeyController {
	return &integrationKeyController{db: db, transactions: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

// Get 读取主体，包括撤销记录以保持审计归属。
func (c *integrationKeyController) Get(ctx context.Context, id string) (models.IntegrationKey, error) {
	var key models.IntegrationKey
	r := controllerDB(ctx, c.db).Where("id = ?", id).Limit(1).Find(&key)
	if r.Error != nil {
		return key, databaseError("get_integration_key", id, r.Error)
	}
	if r.RowsAffected == 0 {
		return key, status_error.IntegrationNotFound
	}
	return key, nil
}

// FindByDigest 只按唯一摘要查询，缺失时不暴露主体存在性。
func (c *integrationKeyController) FindByDigest(ctx context.Context, digest string) (models.IntegrationKey, error) {
	var key models.IntegrationKey
	r := controllerDB(ctx, c.db).Where("key_hash = ?", digest).Limit(1).Find(&key)
	if r.Error != nil {
		return key, databaseError("authenticate_integration_key", "", r.Error)
	}
	if r.RowsAffected == 0 {
		return key, status_error.IntegrationUnauthorized
	}
	return key, nil
}

// List 使用相同主体表返回实际条目与总数。
func (c *integrationKeyController) List(ctx context.Context, limit, offset int) ([]models.IntegrationKey, int64, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, status_error.IntegrationInvalid
	}
	var total int64
	if err := controllerDB(ctx, c.db).Model(&models.IntegrationKey{}).Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_integration_keys", "", err)
	}
	keys := []models.IntegrationKey{}
	if err := controllerDB(ctx, c.db).Order("created_at desc, id").Limit(limit).Offset(offset).Find(&keys).Error; err != nil {
		return nil, 0, databaseError("list_integration_keys", "", err)
	}
	return keys, total, nil
}

// ClaimMutation 在生成随机秘密前先登记意图；未完成意图也不能重新生成。
func (c *integrationKeyController) ClaimMutation(ctx context.Context, in models.IntegrationKeyMutation) (models.IntegrationKeyMutation, bool, error) {
	if in.RequestID == "" || len(in.RequestID) > 200 || in.Digest == "" || in.KeyID == "" || in.Action != "create" {
		return models.IntegrationKeyMutation{}, false, status_error.IntegrationInvalid
	}
	in.Completed = false
	r := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&in)
	if r.Error != nil {
		return in, false, databaseError("claim_integration_issuance", in.KeyID, r.Error)
	}
	if r.RowsAffected == 1 {
		return in, true, nil
	}
	stored, err := c.GetMutation(ctx, in.RequestID)
	if err != nil {
		return stored, false, err
	}
	if stored.Digest != in.Digest || stored.Action != in.Action {
		return stored, false, status_error.IntegrationConflict
	}
	return stored, false, nil
}

// GetMutation 返回原签发目标，永不尝试恢复秘密。
func (c *integrationKeyController) GetMutation(ctx context.Context, requestID string) (models.IntegrationKeyMutation, error) {
	var out models.IntegrationKeyMutation
	r := controllerDB(ctx, c.db).Where("request_id = ?", requestID).Limit(1).Find(&out)
	if r.Error != nil {
		return out, databaseError("get_integration_issuance", "", r.Error)
	}
	if r.RowsAffected == 0 {
		return out, status_error.IntegrationNotFound
	}
	return out, nil
}

// CompleteMutation 不在事务重试回调中生成秘密，避免 busy 重试重复签发。
func (c *integrationKeyController) CompleteMutation(ctx context.Context, requestID, digest string, key models.IntegrationKey, revision int64) (out models.IntegrationKey, err error) {
	err = c.transactions.Transaction(ctx, func(tx context.Context) error {
		mutation, e := c.GetMutation(tx, requestID)
		if e != nil {
			return e
		}
		if mutation.Completed || mutation.Digest != digest || mutation.KeyID != key.ID || len(key.KeyHash) != 64 {
			return status_error.IntegrationConflict
		}
		db := controllerDB(tx, c.db)
		if mutation.Action != "create" || revision != 0 {
			return status_error.IntegrationConflict
		}
		out = key
		out.Revision, out.CredentialGeneration, out.GrantVersion = 1, 1, 1
		if e = db.Create(&out).Error; e != nil {
			return databaseError("create_integration_key", key.ID, e)
		}
		r := db.Model(&models.IntegrationKeyMutation{}).Where("request_id = ? AND completed = ?", requestID, false).Update("completed", true)
		if r.Error != nil {
			return databaseError("complete_integration_issuance", key.ID, r.Error)
		}
		if r.RowsAffected != 1 {
			return status_error.IntegrationConflict
		}
		return nil
	})
	return out, err
}

// UpdateAuthorization 保留凭据，只按实际授权变化递增授权版本。
func (c *integrationKeyController) UpdateAuthorization(ctx context.Context, key models.IntegrationKey, revision int64) (out models.IntegrationKey, err error) {
	err = c.transactions.Transaction(ctx, func(tx context.Context) error {
		old, e := c.Get(tx, key.ID)
		if e != nil {
			return e
		}
		if old.Revision != revision || old.RevokedAt != nil {
			return status_error.IntegrationConflict
		}
		before, _ := json.Marshal(old.Grants)
		after, _ := json.Marshal(key.Grants)
		out = old
		out.Name, out.Grants, out.Enabled, out.ExpiresAt, out.RevokedAt = key.Name, key.Grants, key.Enabled, key.ExpiresAt, key.RevokedAt
		out.Revision++
		if !bytes.Equal(before, after) || old.Enabled != key.Enabled || !sameIntegrationTime(old.ExpiresAt, key.ExpiresAt) || !sameIntegrationTime(old.RevokedAt, key.RevokedAt) {
			out.GrantVersion++
		}
		r := controllerDB(tx, c.db).Model(&models.IntegrationKey{}).Where("id = ? AND revision = ?", key.ID, revision).Select("Name", "Grants", "Enabled", "ExpiresAt", "RevokedAt", "Revision", "GrantVersion", "UpdatedAt").Updates(&out)
		if r.Error != nil {
			return databaseError("update_integration_key", key.ID, r.Error)
		}
		if r.RowsAffected != 1 {
			return status_error.IntegrationConflict
		}
		return nil
	})
	return out, err
}

func sameIntegrationTime(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// TouchLastUsed 沿上游一分钟节流规则进行有条件写入，不触发编辑时间和版本更新。
func (c *integrationKeyController) TouchLastUsed(ctx context.Context, id string, now int64) error {
	r := controllerDB(ctx, c.db).Model(&models.IntegrationKey{}).Where("id = ? AND (last_used_at IS NULL OR last_used_at <= ?)", id, now-60000).UpdateColumn("last_used_at", now)
	if r.Error != nil {
		return databaseError("touch_integration_key", id, r.Error)
	}
	return nil
}

// Delete 条件删除凭据主体，删除重试不重新创建主体或清理历史运行。
func (c *integrationKeyController) Delete(ctx context.Context, id string, revision int64) error {
	if id == "" || revision < 1 {
		return status_error.IntegrationInvalid
	}
	r := controllerDB(ctx, c.db).Where("id = ? AND revision = ?", id, revision).Delete(&models.IntegrationKey{})
	if r.Error != nil {
		return databaseError("delete_integration_key", id, r.Error)
	}
	if r.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := controllerDB(ctx, c.db).Model(&models.IntegrationKey{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return databaseError("read_deleted_integration_key", id, err)
	}
	if count > 0 {
		return status_error.IntegrationConflict
	}
	return nil
}

// RemoveResourceGrants 仅按资源身份撤减授权，不接受名称、凭据、能力或启用状态覆盖。
func (c *integrationKeyController) RemoveResourceGrants(ctx context.Context, id string, revision int64, removals map[constants.IntegrationResourceKind][]string) (out models.IntegrationKey, err error) {
	err = c.transactions.Transaction(ctx, func(tx context.Context) error {
		key, e := c.Get(tx, id)
		if e != nil {
			return e
		}
		if key.Revision != revision {
			return status_error.IntegrationConflict
		}
		out, e = removeKeyResourceGrants(tx, c.db, key, removals)
		return e
	})
	return out, err
}

// removeKeyResourceGrants 只写撤减后的范围和版本，撤销记录也可清理且不能恢复授权。
func removeKeyResourceGrants(ctx context.Context, db *gorm.DB, key models.IntegrationKey, removals map[constants.IntegrationResourceKind][]string) (models.IntegrationKey, error) {
	grants := make([]models.IntegrationGrant, 0, len(key.Grants))
	changed := false
	for _, grant := range key.Grants {
		ids := make([]string, 0, len(grant.ResourceIDs))
		for _, id := range grant.ResourceIDs {
			remove := false
			for _, target := range removals[grant.ResourceKind] {
				if id == target {
					remove = true
					break
				}
			}
			if remove {
				changed = true
			} else {
				ids = append(ids, id)
			}
		}
		grant.ResourceIDs = ids
		if len(ids) > 0 {
			grants = append(grants, grant)
		}
	}
	if !changed {
		return key, nil
	}
	revision := key.Revision
	key.Grants = grants
	key.Revision++
	key.GrantVersion++
	result := controllerDB(ctx, db).Model(&models.IntegrationKey{}).Where("id = ? AND revision = ?", key.ID, revision).Select("Grants", "Revision", "GrantVersion").Updates(&key)
	if result.Error != nil {
		return key, databaseError("remove_integration_resource_grants", key.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return key, status_error.IntegrationConflict
	}
	return key, nil
}

// removeIntegrationResourceGrants 在调用方事务内分页撤销资源引用，保留凭据和其他授权。
func removeIntegrationResourceGrants(ctx context.Context, db *gorm.DB, kind constants.IntegrationResourceKind, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	removals := map[constants.IntegrationResourceKind][]string{kind: ids}
	cursor := ""
	for {
		var rows []models.IntegrationKey
		if err := controllerDB(ctx, db).Where("id > ?", cursor).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return databaseError("read_integration_resource_grants", cursor, err)
		}
		for _, key := range rows {
			if _, err := removeKeyResourceGrants(ctx, db, key, removals); err != nil {
				return err
			}
			cursor = key.ID
		}
		if len(rows) < 100 {
			return nil
		}
	}
}

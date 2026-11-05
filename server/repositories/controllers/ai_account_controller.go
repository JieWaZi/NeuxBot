package controllers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AIAccountController 读写账号元数据，并只向执行侧解密凭据。
type AIAccountController interface {
	// StoreCredential 按版本保存同一身份的刷新结果。
	StoreCredential(context.Context, string, int64, string) error
	// StoreRefreshedCredential 以当前配置和认证代次保存同主体刷新。
	StoreRefreshedCredential(context.Context, string, int64, uint64, string) error
	// StoreLoginCredential 按版本保存新授权并递增认证代次。
	StoreLoginCredential(context.Context, string, int64, string) error
	// ClearCredential 按版本移除凭据并递增认证代次。
	ClearCredential(context.Context, string, int64) error
	// List 返回全部不含凭据的账号。
	List(context.Context) ([]models.AIAccount, error)
	// Get 返回账号元数据。
	Get(context.Context, string) (models.AIAccount, error)
	// Create 原子保存账号与加密 API Key。
	Create(context.Context, models.AIAccount, string) error
	// Update 按版本更新名称、地址及可选的新 API Key。
	Update(context.Context, models.AIAccount, string) error
	// Delete 删除指定版本的账号。
	Delete(context.Context, string, int64) error
	// Credential 只解密执行选中的账号凭据。
	Credential(context.Context, string) (string, error)
}

// aiAccountController 使用 GORM 保存账号及独立的凭据密钥。
type aiAccountController struct {
	// db 是事务外使用的数据库连接。
	db *gorm.DB
}

// NewAIAccountController 创建账号持久化组件。
func NewAIAccountController(db *gorm.DB) AIAccountController { return &aiAccountController{db: db} }

// List 返回按创建顺序排列的账号元数据。
func (c *aiAccountController) List(ctx context.Context) ([]models.AIAccount, error) {
	accounts := []models.AIAccount{}
	if err := controllerDB(ctx, c.db).Omit("CredentialCiphertext").Order("created_at ASC, id ASC").Find(&accounts).Error; err != nil {
		return nil, databaseError("list_ai_accounts", "", err)
	}
	return accounts, nil
}

// Get 返回账号元数据，不查询加密凭据。
func (c *aiAccountController) Get(ctx context.Context, id string) (models.AIAccount, error) {
	var account models.AIAccount
	err := controllerDB(ctx, c.db).Omit("CredentialCiphertext").Where("id = ?", id).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account, status_error.AIAccountNotFound
	}
	if err != nil {
		return account, databaseError("get_ai_account", id, err)
	}
	return account, nil
}

// Create 加密凭据并保存新账号。
func (c *aiAccountController) Create(ctx context.Context, account models.AIAccount, secret string) error {
	account.Revision = 1
	account.AuthGeneration = 1
	account.HasCredential = secret != ""
	if secret != "" {
		encrypted, err := c.encrypt(ctx, account.ID, secret)
		if err != nil {
			return err
		}
		account.CredentialCiphertext = encrypted
	}
	if err := controllerDB(ctx, c.db).Create(&account).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.AIAccountNameConflict
		}
		return databaseError("create_ai_account", account.ID, err)
	}
	return nil
}

// Update 使用乐观锁保存用户配置，空 Key 保留已有凭据。
func (c *aiAccountController) Update(ctx context.Context, account models.AIAccount, secret string) error {
	fields := []string{"Name", "Endpoints", "ModelCatalog", "SelectedModels", "CustomModels", "ModelContextSizes", "ModelDisplayNames", "ModelReasoningLevels", "ClaudeModelRoles", "DefaultModel", "DefaultReasoning", "Revision", "UpdatedAt"}
	revision := account.Revision
	current, err := c.Get(ctx, account.ID)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return status_error.AIAccountRevisionConflict
	}
	account.Revision++
	account.UpdatedAt = time.Now().UnixMilli()
	if secret != "" || !slices.Equal(current.Endpoints, account.Endpoints) {
		account.AuthGeneration = current.AuthGeneration + 1
		fields = append(fields, "AuthGeneration")
	}
	if secret != "" {
		encrypted, err := c.encrypt(ctx, account.ID, secret)
		if err != nil {
			return err
		}
		account.CredentialCiphertext = encrypted
		account.HasCredential = true
		fields = append(fields, "CredentialCiphertext", "HasCredential")
	}
	result := controllerDB(ctx, c.db).Model(&models.AIAccount{}).Where("id = ? AND revision = ?", account.ID, revision).Select(fields).Updates(&account)
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.AIAccountNameConflict
		}
		return databaseError("update_ai_account", account.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.AIAccountRevisionConflict
	}
	return nil
}

// Delete 删除匹配版本的账号，引用检查由用例持有的账号锁保护。
func (c *aiAccountController) Delete(ctx context.Context, id string, revision int64) error {
	result := controllerDB(ctx, c.db).Where("id = ? AND revision = ?", id, revision).Delete(&models.AIAccount{})
	if result.Error != nil {
		return databaseError("delete_ai_account", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.AIAccountRevisionConflict
	}
	return nil
}

// Credential 解密 API Key，认证错误不泄漏底层密文或密钥内容。
func (c *aiAccountController) Credential(ctx context.Context, id string) (string, error) {
	var account models.AIAccount
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", status_error.AIAccountNotFound
	}
	if err != nil {
		return "", databaseError("get_ai_account_credential", id, err)
	}
	if account.CredentialCiphertext == "" {
		return "", nil
	}
	aead, err := c.aead(ctx, false)
	if err != nil {
		return "", err
	}
	data, err := base64.RawStdEncoding.DecodeString(account.CredentialCiphertext)
	if err != nil {
		return "", accountCredentialError("decode", id, err)
	}
	if len(data) < aead.NonceSize() {
		return "", status_error.AIAccountCredentialError
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(id))
	if err != nil {
		return "", accountCredentialError("decrypt", id, err)
	}
	return string(plain), nil
}

func (c *aiAccountController) encrypt(ctx context.Context, id, secret string) (string, error) {
	aead, err := c.aead(ctx, true)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", accountCredentialError("generate_nonce", id, err)
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(secret), []byte(id))), nil
}

func (c *aiAccountController) aead(ctx context.Context, create bool) (cipher.AEAD, error) {
	db := controllerDB(ctx, c.db)
	var record models.AIAccountStorageKey
	err := db.Where("id = ?", "default").First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && create {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, accountCredentialError("generate_key", "", err)
		}
		record = models.AIAccountStorageKey{ID: "default", Key: key}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			return nil, databaseError("create_ai_account_key", "", err)
		}
		err = db.Where("id = ?", "default").First(&record).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status_error.AIAccountCredentialError
	}
	if err != nil {
		return nil, databaseError("read_ai_account_key", "", err)
	}
	block, err := aes.NewCipher(record.Key)
	if err != nil {
		return nil, accountCredentialError("create_cipher", "", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, accountCredentialError("create_gcm", "", err)
	}
	return aead, nil
}

// StoreCredential 保护登录、刷新与原生凭据回收不覆盖较新的账号版本。
func (c *aiAccountController) StoreCredential(ctx context.Context, id string, revision int64, secret string) error {
	encrypted, err := c.encrypt(ctx, id, secret)
	if err != nil {
		return err
	}
	return c.writeCredential(ctx, id, revision, map[string]any{
		"credential_ciphertext": encrypted, "has_credential": true,
	})
}

// StoreRefreshedCredential 保留用户配置的修订及修改时间。
func (c *aiAccountController) StoreRefreshedCredential(ctx context.Context, id string, revision int64, generation uint64, secret string) error {
	if secret == "" {
		return status_error.AIAccountCredentialError
	}
	encrypted, err := c.encrypt(ctx, id, secret)
	if err != nil {
		return err
	}
	result := controllerDB(ctx, c.db).Model(&models.AIAccount{}).
		Where("id = ? AND revision = ? AND auth_generation = ? AND has_credential = ?", id, revision, generation, true).
		UpdateColumns(map[string]any{"credential_ciphertext": encrypted})
	if result.Error != nil {
		return databaseError("store_refreshed_ai_account_credential", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.AIAccountRevisionConflict
	}
	return nil
}

// StoreLoginCredential 保存显式登录并使旧的认证代次失效。
func (c *aiAccountController) StoreLoginCredential(ctx context.Context, id string, revision int64, secret string) error {
	if secret == "" {
		return status_error.AIAccountCredentialError
	}
	encrypted, err := c.encrypt(ctx, id, secret)
	if err != nil {
		return err
	}
	return c.writeCredential(ctx, id, revision, map[string]any{
		"credential_ciphertext": encrypted, "has_credential": true,
		"auth_generation": gorm.Expr("auth_generation + 1"),
	})
}

// ClearCredential 清除选定账号的凭据，并使旧的认证代次失效。
func (c *aiAccountController) ClearCredential(ctx context.Context, id string, revision int64) error {
	return c.writeCredential(ctx, id, revision, map[string]any{
		"credential_ciphertext": "", "has_credential": false,
		"auth_generation": gorm.Expr("auth_generation + 1"),
	})
}

func (c *aiAccountController) writeCredential(ctx context.Context, id string, revision int64, fields map[string]any) error {
	fields["revision"] = revision + 1
	fields["updated_at"] = time.Now().UnixMilli()
	result := controllerDB(ctx, c.db).Model(&models.AIAccount{}).Where("id = ? AND revision = ?", id, revision).Updates(fields)
	if result.Error != nil {
		return databaseError("store_ai_account_credential", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.AIAccountRevisionConflict
	}
	return nil
}

func accountCredentialError(operation, id string, err error) error {
	logx.Errorf("AI account credential failure: operation=%s account_id=%s error=%v", operation, id, err)
	return status_error.AIAccountCredentialError
}

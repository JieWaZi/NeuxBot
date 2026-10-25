package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
	"strings"
	"time"
)

// PrepareLibraryEmbeddingInput 是一次独立库配置保存，空密钥仅保留本库旧值。
type PrepareLibraryEmbeddingInput struct {
	// RequestID 是稳定保存身份。
	RequestID string
	// LibraryID 是唯一归属库。
	LibraryID string
	// ExpectedVersion 是配置CAS版本。
	ExpectedVersion int64
	// ExpectedLibraryVersion 是用户已读的核心版本，加入请求摘要。
	ExpectedLibraryVersion int64
	// BaseURL 是完整服务基础地址。
	BaseURL string
	// Model 是模型名称。
	Model string
	// APIKey 只输入，在持久化前加密。
	APIKey string
	// CoreInput 是首次请求派生的核心更新快照，不参与用户输入重放比较。
	CoreInput json.RawMessage
}

// NormalizeLibraryEmbedding 校验地址与模型，拒绝在URL中夹带凭据。
func NormalizeLibraryEmbedding(baseURL, model string) (string, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || model == "" {
		return "", "", status_error.KnowledgeInvalid
	}
	return baseURL, model, nil
}

// LibraryEmbeddingProfile 返回脱敏配置，不从任何其他模块补默认值。
func (c *knowledgeController) LibraryEmbeddingProfile(ctx context.Context, id string) (models.LibraryEmbeddingProfile, error) {
	out := models.LibraryEmbeddingProfile{LibraryID: id}
	err := controllerDB(ctx, c.db).Where("library_id = ?", id).Take(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.LibraryEmbeddingProfile{LibraryID: id}, nil
	}
	if err != nil {
		return out, databaseError("get_library_embedding", id, err)
	}
	return out, nil
}

// LibraryEmbeddingSnapshot 同一事务读取配置和对应本库密钥。
func (c *knowledgeController) LibraryEmbeddingSnapshot(ctx context.Context, id string) (out models.LibraryEmbeddingProfile, key string, err error) {
	err = controllerDB(ctx, c.db).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("library_id = ?", id).Take(&out).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return status_error.KnowledgeEmbeddingNotConfigured
			}
			return databaseError("snapshot_library_embedding", id, e)
		}
		var secret models.LibraryEmbeddingSecret
		if e := tx.Where("library_id = ?", id).Take(&secret).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return status_error.KnowledgeEmbeddingNotConfigured
			}
			return databaseError("snapshot_library_embedding_secret", id, e)
		}
		var e error
		key, e = decryptLibraryEmbedding(tx, id, secret.Ciphertext)
		return e
	})
	return
}
func decryptLibraryEmbedding(tx *gorm.DB, id, encoded string) (string, error) {
	aead, err := knowledgeConnectionCipher(tx, false)
	if err != nil {
		return "", status_error.KnowledgeEmbeddingNotConfigured
	}
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(data) < aead.NonceSize() {
		return "", status_error.KnowledgeEmbeddingNotConfigured
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("library-embedding:"+id))
	if err != nil {
		return "", status_error.KnowledgeEmbeddingNotConfigured
	}
	return string(plain), nil
}

// PrepareLibraryEmbedding 持久化原始意图与加密密钥，核心尚未成功时不会启用新配置。
func (c *knowledgeController) PrepareLibraryEmbedding(ctx context.Context, in PrepareLibraryEmbeddingInput) (out models.LibraryEmbeddingMutation, err error) {
	in.BaseURL, in.Model, err = NormalizeLibraryEmbedding(in.BaseURL, in.Model)
	if err != nil {
		return out, err
	}
	if in.RequestID == "" || in.LibraryID == "" || in.ExpectedVersion < 0 {
		return out, status_error.KnowledgeInvalid
	}
	raw, _ := json.Marshal([]any{in.LibraryID, in.ExpectedVersion, in.ExpectedLibraryVersion, in.BaseURL, in.Model, in.APIKey})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	db := controllerDB(ctx, c.db)
	err = NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(tc context.Context) error {
		tx := controllerDB(tc, db)
		out = models.LibraryEmbeddingMutation{}
		e := tx.Where("request_id = ?", in.RequestID).Take(&out).Error
		if e == nil {
			if out.Digest != digest || out.LibraryID != in.LibraryID {
				return status_error.KnowledgeConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var old models.LibraryEmbeddingProfile
		e = tx.Where("library_id = ?", in.LibraryID).Take(&old).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if old.Version != in.ExpectedVersion {
			return status_error.KnowledgeConflict
		}
		encrypted := ""
		if in.APIKey == "" {
			var secret models.LibraryEmbeddingSecret
			if e = tx.Where("library_id = ?", in.LibraryID).Take(&secret).Error; e != nil {
				return status_error.KnowledgeEmbeddingNotConfigured
			}
			encrypted = secret.Ciphertext
		} else {
			aead, e := knowledgeConnectionCipher(tx, true)
			if e != nil {
				return e
			}
			nonce := make([]byte, aead.NonceSize())
			if _, e = rand.Read(nonce); e != nil {
				return e
			}
			encrypted = base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(in.APIKey), []byte("library-embedding:"+in.LibraryID)))
		}
		out = models.LibraryEmbeddingMutation{RequestID: in.RequestID, LibraryID: in.LibraryID, Digest: digest, ExpectedVersion: in.ExpectedVersion, CoreInput: in.CoreInput, Ciphertext: encrypted, Snapshot: models.LibraryEmbeddingProfile{LibraryID: in.LibraryID, Version: in.ExpectedVersion + 1, BaseURL: in.BaseURL, Model: in.Model, HasAPIKey: true, UpdatedAt: time.Now().UnixMilli()}}
		return tx.Create(&out).Error
	})
	err = libraryEmbeddingStorageError("prepare_library_embedding", in.LibraryID, err)
	return
}

// CommitLibraryEmbedding 仅在核心回执确认后原子启用配置、密钥与幂等回执。
func (c *knowledgeController) CommitLibraryEmbedding(ctx context.Context, id string, libraryVersion int64) (out models.LibraryEmbeddingProfile, err error) {
	db := controllerDB(ctx, c.db)
	err = NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(tc context.Context) error {
		tx := controllerDB(tc, db)
		var receipt models.LibraryEmbeddingMutation
		if e := tx.Where("request_id = ?", id).Take(&receipt).Error; e != nil {
			return e
		}
		out = receipt.Snapshot
		if receipt.Committed {
			return nil
		}
		out.LibraryVersion = libraryVersion
		if receipt.ExpectedVersion == 0 {
			r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&out)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return status_error.KnowledgeConflict
			}
		} else {
			r := tx.Model(&models.LibraryEmbeddingProfile{}).Where("library_id = ? AND version = ?", receipt.LibraryID, receipt.ExpectedVersion).Select("Version", "LibraryVersion", "BaseURL", "Model", "HasAPIKey", "UpdatedAt").Updates(&out)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return status_error.KnowledgeConflict
			}
		}
		if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "library_id"}}, DoUpdates: clause.AssignmentColumns([]string{"ciphertext"})}).Create(&models.LibraryEmbeddingSecret{LibraryID: receipt.LibraryID, Ciphertext: receipt.Ciphertext}).Error; e != nil {
			return e
		}
		return tx.Model(&models.LibraryEmbeddingMutation{}).Where("request_id = ?", id).Updates(map[string]any{"snapshot": mustEmbeddingJSON(out), "committed": true, "ciphertext": ""}).Error
	})
	err = libraryEmbeddingStorageError("commit_library_embedding", id, err)
	return
}
func libraryEmbeddingStorageError(operation, id string, err error) error {
	if err == nil {
		return nil
	}
	var stable interface {
		error
		Code() int64
	}
	if errors.As(err, &stable) {
		return err
	}
	return databaseError(operation, id, err)
}
func mustEmbeddingJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

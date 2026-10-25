package controllers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
	"strings"
)

// KnowledgeConnectionController 只保存连接配置和加密凭据，不拥有远端知识。
type KnowledgeConnectionController interface {
	Save(context.Context, SaveKnowledgeConnectionInput) (models.KnowledgeConnectionMutation, error)
	Get(context.Context, string) (models.KnowledgeConnection, error)
	// Snapshot 在同一数据库快照中读取端点及对应凭据，避免连接更新导致凭据发往旧端点。
	Snapshot(context.Context, string) (models.KnowledgeConnection, string, error)
	List(context.Context) ([]models.KnowledgeConnection, error)
	Credential(context.Context, string) (string, error)
}

// SaveKnowledgeConnectionInput 表达完整连接变更，空凭据表示保留旧值。
type SaveKnowledgeConnectionInput struct {
	// RequestID 绑定相同请求内容的回执。
	RequestID string
	// ExpectedRevision 为创建时零或编辑时当前版本。
	ExpectedRevision int64
	// Connection 为目标完整配置。
	Connection models.KnowledgeConnection
	// Credential 仅输入，进入数据库前加密。
	Credential string
}

// knowledgeConnectionController 使用既有主库事务和错误转换规则。
type knowledgeConnectionController struct {
	// db 为主库句柄。
	db *gorm.DB
}

func NewKnowledgeConnectionController(db *gorm.DB) KnowledgeConnectionController {
	return &knowledgeConnectionController{db: db}
}
func validateKnowledgeConnection(v models.KnowledgeConnection) bool {
	u, err := url.Parse(v.BaseURL)
	return err == nil && u != nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" && v.ID != "" && v.ID != "local" && strings.TrimSpace(v.Name) != "" && (v.Backend == "weknora" || v.Backend == "custom" || v.Backend == "integration") && (v.AuthMode == "bearer" || ((v.Backend == "weknora" || v.Backend == "integration") && v.AuthMode == "api_key"))
}
func (c *knowledgeConnectionController) Save(ctx context.Context, in SaveKnowledgeConnectionInput) (out models.KnowledgeConnectionMutation, err error) {
	if in.RequestID == "" || in.ExpectedRevision < 0 || !validateKnowledgeConnection(in.Connection) {
		return out, status_error.KnowledgeInvalid
	}
	in.Connection.CredentialRef = ""
	in.Connection.Revision = 0
	raw, _ := json.Marshal(in)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	err = controllerDB(ctx, c.db).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("request_id = ?", in.RequestID).Take(&out).Error; e == nil {
			if out.Digest != digest {
				return status_error.KnowledgeConflict
			}
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var old models.KnowledgeConnection
		e := tx.Where("id = ?", in.Connection.ID).Take(&old).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			if in.ExpectedRevision != 0 || in.Credential == "" {
				return status_error.KnowledgeInvalid
			}
		} else if e != nil {
			return e
		} else if in.ExpectedRevision != old.Revision {
			return status_error.KnowledgeConflict
		}
		if old.ID != "" && old.BaseURL != in.Connection.BaseURL && in.Credential == "" {
			return status_error.KnowledgeInvalid
		}
		// 连接身份不能被重新指向另一协议；切换后端必须创建新的完整资源身份。
		if old.ID != "" && old.Backend != in.Connection.Backend {
			return status_error.KnowledgeConflict
		}
		if old.ID != "" {
			if e := saveKnowledgeConnectionRevision(tx, old); e != nil {
				return e
			}
		}
		value := in.Connection
		value.Revision = in.ExpectedRevision + 1
		value.CredentialRef = value.ID
		if old.ID == "" {
			if e = tx.Create(&value).Error; e != nil {
				return e
			}
		} else {
			r := tx.Model(&models.KnowledgeConnection{}).Where("id = ? AND revision = ?", value.ID, in.ExpectedRevision).Select("Name", "BaseURL", "AuthMode", "Enabled", "Revision", "CredentialRef", "Detection").Updates(&value)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return status_error.KnowledgeConflict
			}
		}
		if in.Credential != "" {
			aead, e := knowledgeConnectionCipher(tx, true)
			if e != nil {
				return e
			}
			nonce := make([]byte, aead.NonceSize())
			if _, e = rand.Read(nonce); e != nil {
				return e
			}
			encrypted := base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(in.Credential), []byte(value.ID)))
			secret := models.KnowledgeConnectionSecret{ID: value.ID, Ciphertext: encrypted}
			if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"ciphertext"})}).Create(&secret).Error; e != nil {
				return e
			}
		}
		if e := saveKnowledgeConnectionRevision(tx, value); e != nil {
			return e
		}
		out = models.KnowledgeConnectionMutation{RequestID: in.RequestID, Digest: digest, ConnectionID: value.ID, Revision: value.Revision}
		return tx.Create(&out).Error
	})
	if err != nil {
		var stable interface {
			error
			Code() int64
		}
		if errors.As(err, &stable) {
			return out, err
		}
		return out, databaseError("save_knowledge_connection", in.Connection.ID, err)
	}
	return out, nil
}
func (c *knowledgeConnectionController) Get(ctx context.Context, id string) (v models.KnowledgeConnection, err error) {
	err = controllerDB(ctx, c.db).Where("id = ?", id).Take(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, status_error.KnowledgeNotFound
	}
	if err != nil {
		return v, databaseError("get_knowledge_connection", id, err)
	}
	return v, nil
}
func (c *knowledgeConnectionController) List(ctx context.Context) ([]models.KnowledgeConnection, error) {
	v := []models.KnowledgeConnection{}
	if err := controllerDB(ctx, c.db).Order("name,id").Find(&v).Error; err != nil {
		return nil, databaseError("list_knowledge_connections", "", err)
	}
	return v, nil
}
func (c *knowledgeConnectionController) Credential(ctx context.Context, id string) (string, error) {
	return readKnowledgeConnectionCredential(controllerDB(ctx, c.db), id)
}

func (c *knowledgeConnectionController) Snapshot(ctx context.Context, id string) (v models.KnowledgeConnection, credential string, err error) {
	err = controllerDB(ctx, c.db).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("id = ?", id).Take(&v).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return status_error.KnowledgeNotFound
			}
			return databaseError("snapshot_knowledge_connection", id, e)
		}
		var e error
		credential, e = readKnowledgeConnectionCredential(tx, id)
		return e
	})
	if err != nil {
		return models.KnowledgeConnection{}, "", err
	}
	return v, credential, nil
}

func readKnowledgeConnectionCredential(db *gorm.DB, id string) (string, error) {
	var secret models.KnowledgeConnectionSecret
	if err := db.Where("id = ?", id).Take(&secret).Error; err != nil {
		return "", status_error.KnowledgeForbidden
	}
	return decryptKnowledgeConnectionCredential(db, id, secret.Ciphertext)
}
func decryptKnowledgeConnectionCredential(db *gorm.DB, id, ciphertext string) (string, error) {
	aead, err := knowledgeConnectionCipher(db, false)
	if err != nil {
		return "", status_error.KnowledgeForbidden
	}
	data, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil || len(data) < aead.NonceSize() {
		return "", status_error.KnowledgeForbidden
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(id))
	if err != nil {
		return "", status_error.KnowledgeForbidden
	}
	return string(plain), nil
}
func knowledgeConnectionCipher(db *gorm.DB, create bool) (cipher.AEAD, error) {
	var record models.KnowledgeConnectionStorageKey
	err := db.Where("id = ?", "default").Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && create {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		record = models.KnowledgeConnectionStorageKey{ID: "default", Key: key}
		if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			return nil, err
		}
		err = db.Where("id = ?", "default").Take(&record).Error
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(record.Key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

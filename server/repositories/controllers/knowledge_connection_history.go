package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// KnowledgeConnectionHistoryController 只用于原写入的只读恢复，不能用历史凭据发新操作。
type KnowledgeConnectionHistoryController interface {
	// SnapshotRevision 返回不可变的同代地址和凭据，绝不换用当前连接的秘密。
	SnapshotRevision(context.Context, string, int64) (models.KnowledgeConnection, string, error)
}

func saveKnowledgeConnectionRevision(db *gorm.DB, connection models.KnowledgeConnection) error {
	var secret models.KnowledgeConnectionSecret
	if err := db.Where("id = ?", connection.ID).Take(&secret).Error; err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.KnowledgeConnectionRevision{ConnectionID: connection.ID, Revision: connection.Revision, Configuration: connection, CredentialCiphertext: secret.Ciphertext}).Error
}
func (c *knowledgeConnectionController) SnapshotRevision(ctx context.Context, id string, revision int64) (connection models.KnowledgeConnection, credential string, err error) {
	if id == "" || revision < 1 {
		return connection, "", status_error.KnowledgeInvalid
	}
	var row models.KnowledgeConnectionRevision
	result := controllerDB(ctx, c.db).Where("connection_id = ? AND revision = ?", id, revision).Limit(1).Find(&row)
	if result.Error != nil {
		return connection, "", databaseError("read_knowledge_connection_revision", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return connection, "", status_error.KnowledgeNotFound
	}
	connection = row.Configuration
	if connection.ID != id || connection.Revision != revision || !validateKnowledgeConnection(connection) {
		return models.KnowledgeConnection{}, "", status_error.KnowledgeForbidden
	}
	credential, err = decryptKnowledgeConnectionCredential(controllerDB(ctx, c.db), id, row.CredentialCiphertext)
	return connection, credential, err
}

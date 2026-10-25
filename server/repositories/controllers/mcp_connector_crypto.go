package controllers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	mcpStorageKeyID   = "mcp_credentials"
	mcpStorageKeySize = 32
)

func encryptConnectorSecrets(db *gorm.DB, connector *models.MCPConnector) error {
	if !connectorHasConfiguredSecrets(*connector) {
		clearUnconfiguredCiphertexts(connector.Environment)
		clearUnconfiguredCiphertexts(connector.Headers)
		return nil
	}
	key, err := ensureMCPStorageKey(db)
	if err != nil {
		return err
	}
	if err := encryptSecretDefinitions(
		key,
		connector.ID,
		"environment",
		connector.Environment,
	); err != nil {
		return err
	}
	return encryptSecretDefinitions(
		key,
		connector.ID,
		"header",
		connector.Headers,
	)
}

func decryptConnectorSecrets(db *gorm.DB, connector *models.MCPConnector) error {
	if !connectorHasConfiguredSecrets(*connector) {
		return nil
	}
	key, err := getMCPStorageKey(db)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mcpCredentialError("get_mcp_storage_key", connector.ID, err)
		}
		return err
	}
	if err := decryptSecretDefinitions(
		key,
		connector.ID,
		"environment",
		connector.Environment,
	); err != nil {
		return err
	}
	return decryptSecretDefinitions(
		key,
		connector.ID,
		"header",
		connector.Headers,
	)
}

func ensureMCPStorageKey(db *gorm.DB) ([]byte, error) {
	key, err := getMCPStorageKey(db)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	value := make([]byte, mcpStorageKeySize)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return nil, mcpCredentialError("generate_mcp_storage_key", "", err)
	}
	record := models.MCPStorageKey{
		ID:         mcpStorageKeyID,
		EncodedKey: base64.RawStdEncoding.EncodeToString(value),
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
		return nil, databaseError("create_mcp_storage_key", "", err)
	}
	return getMCPStorageKey(db)
}

func getMCPStorageKey(db *gorm.DB) ([]byte, error) {
	var record models.MCPStorageKey
	if err := db.Where("id = ?", mcpStorageKeyID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, databaseError("get_mcp_storage_key", "", err)
	}
	value, err := base64.RawStdEncoding.DecodeString(record.EncodedKey)
	if err != nil || len(value) != mcpStorageKeySize {
		return nil, mcpCredentialError("decode_mcp_storage_key", "", err)
	}
	return value, nil
}

func encryptSecretDefinitions(
	key []byte,
	connectorID string,
	kind string,
	definitions []models.MCPSecretDefinition,
) error {
	for index := range definitions {
		definition := &definitions[index]
		if !definition.Configured {
			definition.Ciphertext = ""
			definition.Value = ""
			continue
		}
		if definition.Value == "" {
			return status_error.BadRequest
		}
		ciphertext, err := encryptMCPValue(
			key,
			definition.Value,
			secretAdditionalData(connectorID, kind, definition.ID),
		)
		if err != nil {
			return mcpCredentialError("encrypt_mcp_credential", connectorID, err)
		}
		definition.Ciphertext = ciphertext
		definition.Value = ""
	}
	return nil
}

func decryptSecretDefinitions(
	key []byte,
	connectorID string,
	kind string,
	definitions []models.MCPSecretDefinition,
) error {
	for index := range definitions {
		definition := &definitions[index]
		if !definition.Configured {
			definition.Ciphertext = ""
			definition.Value = ""
			continue
		}
		value, err := decryptMCPValue(
			key,
			definition.Ciphertext,
			secretAdditionalData(connectorID, kind, definition.ID),
		)
		if err != nil {
			return mcpCredentialError("decrypt_mcp_credential", connectorID, err)
		}
		definition.Ciphertext = ""
		definition.Value = value
	}
	return nil
}

func encryptMCPValue(key []byte, value string, additionalData []byte) (string, error) {
	aead, err := newMCPAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), additionalData)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptMCPValue(key []byte, value string, additionalData []byte) (string, error) {
	aead, err := newMCPAEAD(key)
	if err != nil {
		return "", err
	}
	encoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(encoded) < aead.NonceSize() {
		return "", status_error.MCPConnectorCredentialError
	}
	nonce, ciphertext := encoded[:aead.NonceSize()], encoded[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func newMCPAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// mcpCredentialError 记录凭据加解密的底层故障并返回稳定状态错误。
func mcpCredentialError(operation string, connectorID string, err error) error {
	logx.Errorf(
		"mcp credential operation failed: operation=%s connector_id=%s error=%v",
		operation,
		connectorID,
		err,
	)
	return status_error.MCPConnectorCredentialError
}

func connectorHasConfiguredSecrets(connector models.MCPConnector) bool {
	for _, definition := range append(
		append([]models.MCPSecretDefinition{}, connector.Environment...),
		connector.Headers...,
	) {
		if definition.Configured {
			return true
		}
	}
	return false
}

func clearUnconfiguredCiphertexts(definitions []models.MCPSecretDefinition) {
	for index := range definitions {
		definitions[index].Ciphertext = ""
		definitions[index].Value = ""
	}
}

func secretAdditionalData(connectorID string, kind string, entryID string) []byte {
	return []byte(connectorID + "/" + kind + "/" + entryID)
}

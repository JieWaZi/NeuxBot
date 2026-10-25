package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LibraryExecutionDefaults 只读库所属偏好；无记录时明确返回version0，不写入推断默认。
func (c *knowledgeController) LibraryExecutionDefaults(ctx context.Context, libraryID string) (models.LibraryExecutionDefaults, error) {
	out := models.LibraryExecutionDefaults{LibraryID: libraryID}
	err := controllerDB(ctx, c.db).Where("library_id = ?", libraryID).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.LibraryExecutionDefaults{LibraryID: libraryID}, nil
	}
	if err != nil {
		return out, databaseError("get_knowledge_execution_defaults", libraryID, err)
	}
	return out, nil
}

// UpdateLibraryExecutionDefaults 将CAS配置和稳定请求回执提交在同一宿主事务。
func (c *knowledgeController) UpdateLibraryExecutionDefaults(ctx context.Context, requestID string, expectedVersion int64, in models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	if requestID == "" || in.LibraryID == "" || strings.TrimSpace(in.RuntimeID) == "" || expectedVersion < 0 {
		return models.LibraryExecutionDefaults{}, status_error.KnowledgeInvalid
	}
	in.Version = 0
	in.UpdatedAt = 0
	digest := executionDefaultsDigest(expectedVersion, in)
	var out models.LibraryExecutionDefaults
	db := controllerDB(ctx, c.db)
	err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(transactionContext context.Context) error {
		// busy 后从新事务重读回执和版本；不复用前次尚未提交的结果。
		tx := controllerDB(transactionContext, db)
		out = models.LibraryExecutionDefaults{}
		candidate := in
		var receipt models.LibraryExecutionDefaultsMutation
		err := tx.Where("request_id = ?", requestID).First(&receipt).Error
		if err == nil {
			if receipt.LibraryID != in.LibraryID || receipt.Digest != digest {
				return status_error.KnowledgeConflict
			}
			out = receipt.Snapshot
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		candidate.Version = expectedVersion + 1
		candidate.UpdatedAt = time.Now().UnixMilli()
		if expectedVersion == 0 {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return status_error.KnowledgeConflict
			}
		} else {
			result := tx.Model(&models.LibraryExecutionDefaults{}).Where("library_id = ? AND version = ?", in.LibraryID, expectedVersion).Updates(map[string]any{"version": candidate.Version, "runtime_id": candidate.RuntimeID, "account_id": candidate.AccountID, "model": candidate.Model, "updated_at": candidate.UpdatedAt})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return status_error.KnowledgeConflict
			}
		}
		if err = tx.Create(&models.LibraryExecutionDefaultsMutation{RequestID: requestID, LibraryID: in.LibraryID, Digest: digest, Snapshot: candidate}).Error; err != nil {
			return err
		}
		out = candidate
		return nil
	})
	return out, err
}

// ReplayLibraryExecutionDefaults 在重新验证可变Runtime配置前恢复已提交的精确请求。
func (c *knowledgeController) ReplayLibraryExecutionDefaults(ctx context.Context, requestID string, expectedVersion int64, in models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, bool, error) {
	var receipt models.LibraryExecutionDefaultsMutation
	err := controllerDB(ctx, c.db).Where("request_id = ?", requestID).First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.LibraryExecutionDefaults{}, false, nil
	}
	if err != nil {
		return models.LibraryExecutionDefaults{}, false, databaseError("replay_knowledge_execution_defaults", requestID, err)
	}
	if receipt.LibraryID != in.LibraryID || receipt.Digest != executionDefaultsDigest(expectedVersion, in) {
		return models.LibraryExecutionDefaults{}, false, status_error.KnowledgeConflict
	}
	return receipt.Snapshot, true, nil
}

func executionDefaultsDigest(expectedVersion int64, in models.LibraryExecutionDefaults) string {
	in.Version = 0
	in.UpdatedAt = 0
	payload, _ := json.Marshal([]any{expectedVersion, in})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

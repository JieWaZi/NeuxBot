package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// ChatDeletionIntentController 持久化聚合目录删除的 prepared/committed 提交事实。
type ChatDeletionIntentController interface {
	// CreatePrepared 在任何文件 rename 前持久化 prepared tombstone。
	CreatePrepared(ctx context.Context, intent models.ChatDeletionIntent) error
	// Get 返回 tombstone 与是否存在。
	Get(ctx context.Context, intentID string) (models.ChatDeletionIntent, bool, error)
	// List 返回启动恢复后用于清理无磁盘 manifest 孤儿的全部 tombstone。
	List(ctx context.Context) ([]models.ChatDeletionIntent, error)
	// MarkCommitted 在删除 Chat/Team 数据的同一事务中提交 tombstone。
	MarkCommitted(ctx context.Context, intentID string, updatedAt int64) error
	// Delete 在 staging 已恢复或清理后移除 tombstone。
	Delete(ctx context.Context, intentID string) error
}

// chatDeletionIntentController 使用 GORM 持久化删除提交事实。
type chatDeletionIntentController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewChatDeletionIntentController 创建聚合删除 tombstone Controller。
func NewChatDeletionIntentController(db *gorm.DB) ChatDeletionIntentController {
	return &chatDeletionIntentController{db: db}
}

// CreatePrepared 创建 prepared tombstone。
func (c *chatDeletionIntentController) CreatePrepared(
	ctx context.Context,
	intent models.ChatDeletionIntent,
) error {
	intent.State = "prepared"
	if intent.UpdatedAt == 0 {
		intent.UpdatedAt = time.Now().UnixMilli()
	}
	if err := controllerDB(ctx, c.db).Create(&intent).Error; err != nil {
		return databaseError("create_chat_deletion_intent", "intent_id="+intent.ID, err)
	}
	return nil
}

// Get 返回 tombstone 与是否存在。
func (c *chatDeletionIntentController) Get(
	ctx context.Context,
	intentID string,
) (models.ChatDeletionIntent, bool, error) {
	var intent models.ChatDeletionIntent
	err := controllerDB(ctx, c.db).Where("id = ?", intentID).First(&intent).Error
	if err == nil {
		if intent.ChatIDs == nil {
			intent.ChatIDs = []string{}
		}
		return intent, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChatDeletionIntent{}, false, nil
	}
	return models.ChatDeletionIntent{}, false, databaseError(
		"get_chat_deletion_intent",
		"intent_id="+intentID,
		err,
	)
}

// List 返回启动恢复后用于清理无磁盘 manifest 孤儿的全部 tombstone。
func (c *chatDeletionIntentController) List(
	ctx context.Context,
) ([]models.ChatDeletionIntent, error) {
	intents := []models.ChatDeletionIntent{}
	if err := controllerDB(ctx, c.db).
		Order("updated_at ASC, id ASC").
		Find(&intents).Error; err != nil {
		return nil, databaseError("list_chat_deletion_intents", "chat_deletion_intent", err)
	}
	for index := range intents {
		if intents[index].ChatIDs == nil {
			intents[index].ChatIDs = []string{}
		}
	}
	return intents, nil
}

// MarkCommitted 仅把 prepared tombstone 推进为 committed。
func (c *chatDeletionIntentController) MarkCommitted(
	ctx context.Context,
	intentID string,
	updatedAt int64,
) error {
	result := controllerDB(ctx, c.db).Model(&models.ChatDeletionIntent{}).
		Where("id = ? AND state = ?", intentID, "prepared").
		Updates(map[string]interface{}{"state": "committed", "updated_at": updatedAt})
	if result.Error != nil {
		return databaseError("commit_chat_deletion_intent", "intent_id="+intentID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.DatabaseError
	}
	return nil
}

// Delete 移除已经完成文件恢复或清理的 tombstone。
func (c *chatDeletionIntentController) Delete(ctx context.Context, intentID string) error {
	if err := controllerDB(ctx, c.db).
		Where("id = ?", intentID).
		Delete(&models.ChatDeletionIntent{}).Error; err != nil {
		return databaseError("delete_chat_deletion_intent", "intent_id="+intentID, err)
	}
	return nil
}

package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// WorkflowCreationController 保存创作会话、输入接纳锚点和版本批注。
type WorkflowCreationController interface {
	// CreateCreationSession 在派发前保存能力与身份。
	CreateCreationSession(context.Context, models.WorkflowCreationSession) error
	// GetCreationSession 读取 scoped 工具身份。
	GetCreationSession(context.Context, string) (models.WorkflowCreationSession, error)
	// ListCreationSessions 读取工作流实际创作会话。
	ListCreationSessions(context.Context, string) ([]models.WorkflowCreationSession, error)
	// UpdateCreationSession 只更新当前轮身份、配置和接纳收据。
	UpdateCreationSession(context.Context, models.WorkflowCreationSession) error
	// DeleteCreationSessionsByChatID 删除已移除 Chat 的创作会话关系，不删除工作流或共享目录。
	DeleteCreationSessionsByChatID(context.Context, string) error
	// RecordCreationSave 在源码事务中记录真实保存回执。
	RecordCreationSave(context.Context, string, string) error
	// CreateAnnotation 插入用户要求。
	CreateAnnotation(context.Context, models.WorkflowAnnotation) error
	// ListAnnotations 读取有界批注。
	ListAnnotations(context.Context, string) ([]models.WorkflowAnnotation, error)
	// ListFilteredAnnotations 在数据库中先按未处理状态和来源版本筛选，再读取有界结果。
	ListFilteredAnnotations(context.Context, string, string, string) ([]models.WorkflowAnnotation, error)
	// ResolveAnnotations 只在成功保存事务中处理同一工作流的批注。
	ResolveAnnotations(context.Context, string, []string, string) error
}

// workflowCreationController 使用上层统一事务连接。
type workflowCreationController struct {
	// db 是默认连接。
	db *gorm.DB
}

// NewWorkflowCreationController 构造创作持久边界。
func NewWorkflowCreationController(db *gorm.DB) WorkflowCreationController {
	return &workflowCreationController{db: db}
}
func (c *workflowCreationController) CreateCreationSession(ctx context.Context, value models.WorkflowCreationSession) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_creation", value.ID, err)
	}
	return nil
}
func (c *workflowCreationController) GetCreationSession(ctx context.Context, id string) (models.WorkflowCreationSession, error) {
	var value models.WorkflowCreationSession
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_creation", id, err)
	}
	return value, nil
}
func (c *workflowCreationController) ListCreationSessions(ctx context.Context, id string) ([]models.WorkflowCreationSession, error) {
	values := []models.WorkflowCreationSession{}
	if err := controllerDB(ctx, c.db).Where("definition_id = ?", id).Order("created_at DESC").Limit(50).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_creations", id, err)
	}
	return values, nil
}
func (c *workflowCreationController) UpdateCreationSession(ctx context.Context, value models.WorkflowCreationSession) error {
	// 首次绑定可能晚于智能体发出交互；绑定提交后对账，避免请求事件先到而永久漏通知。
	linking := false
	if _, enabled := c.db.Get(eventPublisherSetting); enabled && value.ChatID != "" {
		var previous models.WorkflowCreationSession
		if err := controllerDB(ctx, c.db).Select("chat_id").Where("id = ?", value.ID).Find(&previous).Error; err != nil {
			return databaseError("read_creation_notification_link", value.ID, err)
		}
		linking = previous.ChatID != value.ChatID
	}
	result := controllerDB(ctx, c.db).Model(&value).Where("id = ?", value.ID).Select("chat_id", "active_execution_id", "accepted_execution_id", "request_id", "content", "defaults", "auth_token").Updates(&value)
	if result.Error != nil {
		return databaseError("update_workflow_creation", value.ID, result.Error)
	}
	if linking && result.RowsAffected > 0 {
		var chat models.Chat
		if err := controllerDB(ctx, c.db).Select("id", "active_execution_id", "waiting_approval", "pending_approvals", "waiting_elicitation", "pending_elicitations").Where("id = ?", value.ChatID).Find(&chat).Error; err != nil {
			return databaseError("read_creation_pending_notifications", value.ChatID, err)
		}
		upserts, _ := noticeSetDifference(nil, chatPendingNoticeIDs(chat))
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", value.ChatID))
		publishCommittedNoticeIDs(ctx, c.db, upserts, nil)
	}
	return nil
}
func (c *workflowCreationController) RecordCreationSave(ctx context.Context, id, revisionID string) error {
	if err := controllerDB(ctx, c.db).Model(&models.WorkflowCreationSession{}).Where("id = ?", id).Updates(map[string]any{"last_saved_revision_id": revisionID}).Error; err != nil {
		return databaseError("record_workflow_creation_save", id, err)
	}
	return nil
}
func (c *workflowCreationController) CreateAnnotation(ctx context.Context, value models.WorkflowAnnotation) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_annotation", value.ID, err)
	}
	return nil
}
func (c *workflowCreationController) ListAnnotations(ctx context.Context, id string) ([]models.WorkflowAnnotation, error) {
	return c.ListFilteredAnnotations(ctx, id, "", "")
}
func (c *workflowCreationController) ListFilteredAnnotations(ctx context.Context, id, status, revisionID string) ([]models.WorkflowAnnotation, error) {
	values := []models.WorkflowAnnotation{}
	query := controllerDB(ctx, c.db).Where("definition_id = ?", id)
	if status == "pending" {
		query = query.Where("resolved_revision_id = ''")
	}
	if status == "resolved" {
		query = query.Where("resolved_revision_id <> ''")
	}
	if revisionID != "" {
		query = query.Where("base_revision_id = ?", revisionID)
	}
	if err := query.Order("created_at DESC, id DESC").Limit(500).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_annotations", id, err)
	}
	return values, nil
}
func (c *workflowCreationController) ResolveAnnotations(ctx context.Context, id string, ids []string, revisionID string) error {
	if len(ids) == 0 {
		return nil
	}
	values := []models.WorkflowAnnotation{}
	db := controllerDB(ctx, c.db)
	if err := db.Where("definition_id = ? AND id IN ? AND resolved_revision_id = ''", id, ids).Find(&values).Error; err != nil {
		return databaseError("read_workflow_annotations", id, err)
	}
	if len(values) != len(ids) {
		return status_error.WorkflowStateConflict
	}
	if err := db.Model(&models.WorkflowAnnotation{}).Where("definition_id = ? AND id IN ? AND resolved_revision_id = ''", id, ids).Updates(map[string]any{"resolved_revision_id": revisionID}).Error; err != nil {
		return databaseError("resolve_workflow_annotations", id, err)
	}
	return nil
}

// DeleteCreationSessionsByChatID 与上层 Chat 删除事务共同提交关系移除。
func (c *workflowCreationController) DeleteCreationSessionsByChatID(ctx context.Context, chatID string) error {
	if err := controllerDB(ctx, c.db).Where("chat_id = ?", chatID).Delete(&models.WorkflowCreationSession{}).Error; err != nil {
		return databaseError("delete_workflow_creations_by_chat", chatID, err)
	}
	return nil
}

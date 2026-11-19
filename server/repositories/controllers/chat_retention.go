package controllers

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// ChatRetentionCandidate 是与具体权威日志版本绑定的过期候选。
type ChatRetentionCandidate struct {
	// ChatID 是会话的稳定身份。
	ChatID string `gorm:"column:id" json:"chatId"`
	// WakerID 是拥有此会话的伙伴身份。
	WakerID string `json:"wakerId"`
	// Title 是当前展示标题。
	Title string `json:"title"`
	// Sequence 是预览时已投影的日志序号。
	Sequence int64 `gorm:"column:latest_sequence" json:"sequence"`
	// LastActivityAt 是已核验的最近工作活动时间（Unix 毫秒）。
	LastActivityAt int64 `json:"lastActivityAt"`
}

// ChatRetentionReader 在同一作用域内读取已核验的工作活动候选。
type ChatRetentionReader interface {
	// ListRetentionCandidates 以工作活动时间和会话身份升序分页返回过期候选。
	ListRetentionCandidates(ctx context.Context, cutoff, afterActivity int64, afterID string, limit int) ([]ChatRetentionCandidate, error)
	// ListRetentionCandidatesByIDs 仅复核用户明确选中的有界会话集合。
	ListRetentionCandidatesByIDs(ctx context.Context, cutoff int64, ids []string) ([]ChatRetentionCandidate, error)
	// CountRetentionCandidates 按分页查询的相同条件统计过期候选。
	CountRetentionCandidates(ctx context.Context, cutoff int64) (int64, error)
}

func (c *chatController) ExcludingRetentionChats(ids []string) ChatRetentionReader {
	return &chatController{db: c.db, retentionExcluded: ids}
}

func (c *chatController) retentionQuery(ctx context.Context, cutoff int64) *gorm.DB {
	query := retentionCandidateQuery(controllerDB(ctx, c.db), cutoff)
	if len(c.retentionExcluded) > 0 {
		// 单个 JSON 绑定避免大量候选同时隔离时超过 SQLite 参数数限制。
		excluded, _ := json.Marshal(c.retentionExcluded)
		query = query.Where("id NOT IN (SELECT value FROM json_each(?))", string(excluded))
	}
	return query
}

func retentionCandidateQuery(db *gorm.DB, cutoff int64) *gorm.DB {
	return db.Model(&models.Chat{}).
		Where("last_activity_at > 0 AND last_activity_at < ? AND latest_sequence > 0", cutoff).
		Where("waker_id <> '' AND retention_protected = 0 AND hidden_from_waker_history = 0 AND active_execution_id = '' AND waiting_approval = 0 AND waiting_elicitation = 0").
		Where("NOT EXISTS (SELECT 1 FROM wakers WHERE wakers.id = chats.waker_id AND onboarding_chat_id = chats.id AND onboarding_status = ?)", constants.WakerOnboardingStatusPending).
		Where("NOT EXISTS (SELECT 1 FROM channel_threads WHERE lead_chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM channel_thread_members WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM channel_task_attempts WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM workflow_creation_sessions WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM workflow_node_instances WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM workflow_effects WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM automatic_runs WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM im_conversation_links WHERE chat_id = chats.id)").
		Where("NOT EXISTS (SELECT 1 FROM im_deliveries WHERE chat_id = chats.id)")
}

func (c *chatController) ListRetentionCandidates(ctx context.Context, cutoff, afterActivity int64, afterID string, limit int) ([]ChatRetentionCandidate, error) {
	q := c.retentionQuery(ctx, cutoff)
	if afterID != "" {
		q = q.Where("(last_activity_at, id) > (?, ?)", afterActivity, afterID)
	}
	var rows []ChatRetentionCandidate
	err := q.Select("id, waker_id, title, latest_sequence, last_activity_at").Order("last_activity_at, id").Limit(max(0, min(limit, 256))).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_chat_retention_candidates", "", err)
	}
	return rows, nil
}

func (c *chatController) ListRetentionCandidatesByIDs(ctx context.Context, cutoff int64, ids []string) ([]ChatRetentionCandidate, error) {
	if len(ids) > 256 {
		return nil, status_error.BadRequest
	}
	var rows []ChatRetentionCandidate
	err := c.retentionQuery(ctx, cutoff).Where("id IN ?", ids).Select("id, waker_id, title, latest_sequence, last_activity_at").Order("last_activity_at, id").Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_selected_chat_retention_candidates", "", err)
	}
	return rows, nil
}

func (c *chatController) CountRetentionCandidates(ctx context.Context, cutoff int64) (int64, error) {
	var count int64
	err := c.retentionQuery(ctx, cutoff).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_chat_retention_candidates", "", err)
	}
	return count, nil
}

func (c *chatController) IsRetentionCandidate(ctx context.Context, candidate ChatRetentionCandidate, cutoff int64) (bool, error) {
	var count int64
	err := c.retentionQuery(ctx, cutoff).Where("id = ? AND latest_sequence = ? AND last_activity_at = ?", candidate.ChatID, candidate.Sequence, candidate.LastActivityAt).Count(&count).Error
	if err != nil {
		return false, databaseError("check_chat_retention_candidate", candidate.ChatID, err)
	}
	return count == 1, nil
}

func (c *chatController) ListStorageChats(ctx context.Context, ids []string) ([]models.Chat, error) {
	return readRelatedRows[models.Chat](controllerDB(ctx, c.db), "id", "id, title, waker_id, hidden_from_waker_history, latest_sequence, fork_source_chat_id", ids)
}

func (c *chatController) ListRetentionBackfill(ctx context.Context, afterID string, limit int) ([]ChatRetentionCandidate, error) {
	var rows []ChatRetentionCandidate
	err := controllerDB(ctx, c.db).Model(&models.Chat{}).
		Where("id > ? AND last_activity_at = 0 AND latest_sequence > 0 AND hidden_from_waker_history = 0 AND waker_id <> ''", afterID).
		Select("id, latest_sequence").Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_chat_retention_backfill", "", err)
	}
	return rows, nil
}

func (c *chatController) BackfillRetentionProjection(ctx context.Context, id string, sequence, activity int64, protected bool) error {
	result := controllerDB(ctx, c.db).Model(&models.Chat{}).
		Where("id = ? AND latest_sequence = ? AND last_activity_at = 0", id, sequence).
		UpdateColumns(map[string]any{"last_activity_at": activity, "retention_protected": protected})
	if result.Error != nil {
		return databaseError("backfill_chat_retention", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.Conflict
	}
	return nil
}

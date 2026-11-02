package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatController 定义 Chat 查询投影的持久化操作。
type ChatController interface {
	// ReadExecutionLocation 按原身份定位权威执行入口。
	ReadExecutionLocation(context.Context, string, string, string) (*models.ChatExecutionLocation, error)
	// ReadExecutionLocationCursor 返回执行定位已覆盖的持久游标。
	ReadExecutionLocationCursor(context.Context, string) (int64, error)
	// AdvanceExecutionLocations 在同一事务提交尾部定位及覆盖游标。
	AdvanceExecutionLocations(context.Context, string, int64, int64, []models.ChatExecutionLocation) error
	// Create 创建一条可由权威 JSONL 重建的 Chat 索引。
	Create(ctx context.Context, chat models.Chat) error
	// Get 按标识返回 Chat 索引。
	Get(ctx context.Context, id string) (models.Chat, error)
	// ListByWakerBefore 按稳定活动游标返回一位伙伴更早的 Chat。
	ListByWakerBefore(ctx context.Context, wakerID string, beforeUpdatedAt int64, beforeID string, limit int) ([]models.Chat, error)
	// ListByWakerUpdatedRange 按标题和最近活动时间范围分页返回一位伙伴的 Chat。
	ListByWakerUpdatedRange(ctx context.Context, wakerID string, titleQuery string, updatedAtStart int64, updatedAtEnd int64, limit int, offset int) ([]models.Chat, error)
	// ListRecentProjectCandidates 按稳定活动游标读取有界项目候选。
	ListRecentProjectCandidates(ctx context.Context, wakerID string, beforeUpdatedAt int64, beforeID string, limit int) ([]RecentProjectCandidate, error)
	// ListIDsByWaker 返回伙伴拥有的全部 Chat 标识。
	ListIDsByWaker(ctx context.Context, wakerID string) ([]string, error)
	// CountByWaker 返回伙伴对话总数。
	CountByWaker(ctx context.Context, wakerID string) (int64, error)
	// CountByWakerUpdatedRange 返回伙伴符合标题和最近活动时间筛选的对话总数。
	CountByWakerUpdatedRange(ctx context.Context, wakerID string, titleQuery string, updatedAtStart int64, updatedAtEnd int64) (int64, error)
	// CountActiveByWaker 返回伙伴普通对话中 active_execution_id 非空的数量。
	CountActiveByWaker(ctx context.Context, wakerID string) (int64, error)
	// ListActivitySummariesByWaker 批量返回伙伴普通 Chat 的聚合活动计数。
	ListActivitySummariesByWaker(
		ctx context.Context,
		wakerIDs []string,
	) (map[string]models.ActivitySummary, error)
	// CountCreatedByWakerGroupedByDay 返回时间窗内按自然日分组的 created_at 计数。
	CountCreatedByWakerGroupedByDay(ctx context.Context, wakerID string, startMs int64, endMs int64, loc *time.Location) (map[string]int64, error)
	// CountUpdatedByWakerBetween 返回 updated_at 落在时间窗内的对话数。
	CountUpdatedByWakerBetween(ctx context.Context, wakerID string, startMs int64, endMs int64) (int64, error)
	// AdvanceProjection 仅在当前游标匹配时原子推进由 JSONL 派生的字段。
	AdvanceProjection(ctx context.Context, id string, afterSequence int64, fields map[string]any) error
	// Delete 删除一条 Chat 索引。
	Delete(ctx context.Context, id string) error
	// ListByWakerForDelete 返回删除伙伴时需要清理的全部 Chat。
	ListByWakerForDelete(ctx context.Context, wakerID string) ([]models.Chat, error)
}

// chatController 使用 GORM 实现 Chat 查询投影。
type chatController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// RecentProjectCandidate 是最近项目查询所需的最小 Chat 投影。
type RecentProjectCandidate struct {
	// ID 是 Chat 的稳定标识。
	ID string `gorm:"column:id"`
	// UpdatedAt 是 Chat 最近活动时间。
	UpdatedAt int64 `gorm:"column:updated_at"`
	// ProjectID 是 Chat 创建时冻结的项目标识。
	ProjectID string `gorm:"column:project_id"`
}

// chatIDRow 是只读取 Chat 标识的最小数据库行。
type chatIDRow struct {
	// ID 是 Chat 的稳定标识。
	ID string `gorm:"column:id"`
}

// wakerActivityRow 是伙伴活动计数聚合查询使用的数据库行。
type wakerActivityRow struct {
	// WakerID 是聚合计数所属伙伴标识。
	WakerID string `gorm:"column:waker_id"`
	// WaitingCount 是等待审批或输入的 Chat 数量。
	WaitingCount int `gorm:"column:waiting_count"`
	// RunningCount 是存在活跃执行且未等待审批或输入的 Chat 数量。
	RunningCount int `gorm:"column:running_count"`
}

// NewChatController 创建 Chat Repository Controller。
func NewChatController(db *gorm.DB) ChatController {
	return &chatController{db: db}
}

// ListActivitySummariesByWaker 批量返回伙伴普通 Chat 的聚合活动计数。
func (c *chatController) ListActivitySummariesByWaker(
	ctx context.Context,
	wakerIDs []string,
) (map[string]models.ActivitySummary, error) {
	summaries := make(map[string]models.ActivitySummary, len(wakerIDs))
	for _, wakerID := range wakerIDs {
		summaries[wakerID] = models.ActivitySummary{}
	}
	if len(wakerIDs) == 0 {
		return summaries, nil
	}

	rows := make([]wakerActivityRow, 0, len(wakerIDs))
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Select(`waker_id,
			SUM(CASE WHEN waiting_approval = 1 OR waiting_elicitation = 1 THEN 1 ELSE 0 END) AS waiting_count,
			SUM(CASE
				WHEN active_execution_id <> ''
					AND NOT (waiting_approval = 1 OR waiting_elicitation = 1) THEN 1
				ELSE 0
			END) AS running_count`).
		Where("waker_id IN ? AND hidden_from_waker_history = ?", wakerIDs, false).
		Group("waker_id").
		Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_waker_activity_summaries", "wakers", err)
	}
	for _, row := range rows {
		summaries[row.WakerID] = models.ActivitySummary{
			RunningCount: row.RunningCount,
			WaitingCount: row.WaitingCount,
		}
	}
	return summaries, nil
}

// Create 创建一条可由权威 JSONL 重建的 Chat 索引。
func (c *chatController) Create(
	ctx context.Context,
	chat models.Chat,
) error {
	err := controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&chat).Error
	if err != nil {
		return databaseError(
			"create_chat_projection",
			"chat_id="+chat.ID,
			err,
		)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", chat.ID))
	return nil
}

// Get 按标识返回 Chat 索引。
func (c *chatController) Get(
	ctx context.Context,
	id string,
) (models.Chat, error) {
	var chat models.Chat
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&chat).Error
	if err == nil {
		return chat, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Chat{}, status_error.ChatNotFound.WithParams(
			map[string]interface{}{"ChatID": id},
		)
	}
	return models.Chat{}, databaseError(
		"get_chat",
		"chat_id="+id,
		err,
	)
}

// ListByWakerBefore 按稳定活动游标返回一位伙伴更早的 Chat。
func (c *chatController) ListByWakerBefore(
	ctx context.Context,
	wakerID string,
	beforeUpdatedAt int64,
	beforeID string,
	limit int,
) ([]models.Chat, error) {
	query := controllerDB(ctx, c.db).
		Where("waker_id = ? AND hidden_from_waker_history = ?", wakerID, false)
	if beforeUpdatedAt > 0 && beforeID != "" {
		query = query.Where(
			"updated_at < ? OR (updated_at = ? AND id < ?)",
			beforeUpdatedAt,
			beforeUpdatedAt,
			beforeID,
		)
	}
	var chats []models.Chat
	err := query.
		Order("updated_at DESC").
		Order("id DESC").
		Limit(limit).
		Find(&chats).Error
	if err != nil {
		return nil, databaseError(
			"list_chats_by_activity_cursor",
			"waker_id="+wakerID,
			err,
		)
	}
	return chats, nil
}

// ListByWakerUpdatedRange 按标题和最近活动时间范围分页返回一位伙伴的 Chat。
func (c *chatController) ListByWakerUpdatedRange(
	ctx context.Context,
	wakerID string,
	titleQuery string,
	updatedAtStart int64,
	updatedAtEnd int64,
	limit int,
	offset int,
) ([]models.Chat, error) {
	query := filterWakerChats(
		controllerDB(ctx, c.db),
		wakerID,
		titleQuery,
		updatedAtStart,
		updatedAtEnd,
	)
	var chats []models.Chat
	err := query.
		Order("updated_at DESC").
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&chats).Error
	if err != nil {
		return nil, databaseError(
			"list_chats_by_updated_range",
			"waker_id="+wakerID,
			err,
		)
	}
	return chats, nil
}

// ListRecentProjectCandidates 按活动时间与标识倒序返回有界候选。
func (c *chatController) ListRecentProjectCandidates(ctx context.Context, wakerID string, beforeUpdatedAt int64, beforeID string, limit int) ([]RecentProjectCandidate, error) {
	var rows []RecentProjectCandidate
	query := controllerDB(ctx, c.db).Model(&models.Chat{}).Select("id, updated_at, project_id").
		Where("waker_id = ? AND project_id <> '' AND hidden_from_waker_history = ?", wakerID, false)
	if beforeID != "" {
		query = query.Where("updated_at < ? OR (updated_at = ? AND id < ?)", beforeUpdatedAt, beforeUpdatedAt, beforeID)
	}
	err := query.Order("updated_at DESC").Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_recent_chat_project_candidates", "waker_id="+wakerID, err)
	}
	return rows, nil
}

// ListIDsByWaker 返回伙伴拥有的全部 Chat 标识。
func (c *chatController) ListIDsByWaker(
	ctx context.Context,
	wakerID string,
) ([]string, error) {
	var rows []chatIDRow
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Select("id").
		Where("waker_id = ?", wakerID).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, databaseError(
			"list_chat_ids_by_waker",
			"waker_id="+wakerID,
			err,
		)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// CountByWaker 返回伙伴对话总数。
func (c *chatController) CountByWaker(
	ctx context.Context,
	wakerID string,
) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Where("waker_id = ?", wakerID).
		Count(&count).Error
	if err != nil {
		return 0, databaseError(
			"count_chats_by_waker",
			"waker_id="+wakerID,
			err,
		)
	}
	return count, nil
}

// CountByWakerUpdatedRange 返回伙伴符合标题和最近活动时间筛选的对话总数。
func (c *chatController) CountByWakerUpdatedRange(
	ctx context.Context,
	wakerID string,
	titleQuery string,
	updatedAtStart int64,
	updatedAtEnd int64,
) (int64, error) {
	var count int64
	err := filterWakerChats(
		controllerDB(ctx, c.db).Model(&models.Chat{}),
		wakerID,
		titleQuery,
		updatedAtStart,
		updatedAtEnd,
	).Count(&count).Error
	if err != nil {
		return 0, databaseError(
			"count_chats_by_updated_range",
			"waker_id="+wakerID,
			err,
		)
	}
	return count, nil
}

func filterWakerChats(
	query *gorm.DB,
	wakerID string,
	titleQuery string,
	updatedAtStart int64,
	updatedAtEnd int64,
) *gorm.DB {
	query = query.Where("waker_id = ? AND hidden_from_waker_history = ?", wakerID, false)
	if normalizedTitle := strings.ToLower(strings.TrimSpace(titleQuery)); normalizedTitle != "" {
		query = query.Where("LOWER(title) LIKE ?", "%"+normalizedTitle+"%")
	}
	if updatedAtStart > 0 {
		query = query.Where("updated_at >= ?", updatedAtStart)
	}
	if updatedAtEnd > 0 {
		query = query.Where("updated_at <= ?", updatedAtEnd)
	}
	return query
}

// CountActiveByWaker 返回伙伴普通对话中 active_execution_id 非空的数量。
func (c *chatController) CountActiveByWaker(
	ctx context.Context,
	wakerID string,
) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Where("waker_id = ? AND active_execution_id <> '' AND hidden_from_waker_history = ?", wakerID, false).
		Count(&count).Error
	if err != nil {
		return 0, databaseError(
			"count_active_chats_by_waker",
			"waker_id="+wakerID,
			err,
		)
	}
	return count, nil
}

// CountCreatedByWakerGroupedByDay 返回时间窗内按自然日分组的 created_at 计数。
func (c *chatController) CountCreatedByWakerGroupedByDay(
	ctx context.Context,
	wakerID string,
	startMs int64,
	endMs int64,
	loc *time.Location,
) (map[string]int64, error) {
	if loc == nil {
		loc = time.Local
	}
	var rows []struct {
		// CreatedAt 是对话建立时的 Unix 毫秒时间。
		CreatedAt int64 `gorm:"column:created_at"`
	}
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Select("created_at").
		Where(
			"waker_id = ? AND created_at >= ? AND created_at <= ?",
			wakerID,
			startMs,
			endMs,
		).
		Find(&rows).Error
	if err != nil {
		return nil, databaseError(
			"count_chats_created_by_waker_grouped_by_day",
			"waker_id="+wakerID,
			err,
		)
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		day := time.UnixMilli(row.CreatedAt).In(loc).Format("2006-01-02")
		counts[day]++
	}
	return counts, nil
}

// CountUpdatedByWakerBetween 返回 updated_at 落在时间窗内的对话数。
func (c *chatController) CountUpdatedByWakerBetween(
	ctx context.Context,
	wakerID string,
	startMs int64,
	endMs int64,
) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Where(
			"waker_id = ? AND updated_at >= ? AND updated_at <= ?",
			wakerID,
			startMs,
			endMs,
		).
		Count(&count).Error
	if err != nil {
		return 0, databaseError(
			"count_chats_updated_by_waker_between",
			"waker_id="+wakerID,
			err,
		)
	}
	return count, nil
}

// AdvanceProjection 仅在当前游标匹配时原子推进由 JSONL 派生的字段。
func (c *chatController) AdvanceProjection(
	ctx context.Context,
	id string,
	afterSequence int64,
	fields map[string]any,
) error {
	publish, err := c.chatNoticeChanges(ctx, id, fields)
	if err != nil {
		return err
	}
	result := controllerDB(ctx, c.db).
		Model(&models.Chat{}).
		Where("id = ? AND latest_sequence = ?", id, afterSequence).
		Updates(fields)
	if result.Error != nil {
		return databaseError(
			"update_chat_projection",
			"chat_id="+id,
			result.Error,
		)
	}
	if result.RowsAffected == 0 {
		return status_error.Conflict
	}
	publish()
	return nil
}

// Delete 删除一条 Chat 索引。
func (c *chatController) Delete(ctx context.Context, id string) error {
	db := controllerDB(ctx, c.db)
	var bindings []models.IMBinding
	var affectedEntities []events.Resource
	value, registered := c.db.Get(eventPublisherSetting)
	publisher, publishesEvents := value.(events.Publisher)
	if registered && publishesEvents && publisher != nil {
		// 删除前保留普通 Chat 的伙伴归属，提交后不依赖已经移除的索引行恢复活动范围。
		var chat models.Chat
		if err := db.Select("id", "waker_id", "hidden_from_waker_history").Where("id = ?", id).Find(&chat).Error; err != nil {
			return databaseError("capture_deleted_chat_activity", "chat_id="+id, err)
		}
		if chat.ID != "" && chat.WakerID != "" && !chat.HiddenFromWakerHistory {
			affectedEntities = append(affectedEntities, eventsResource("waker", chat.WakerID))
		}
		// 团队成员关系在聚合删除末尾才清理，在此前捕获受影响的 IM 会话范围。
		threads := db.Model(&models.ChannelThreadMember{}).Select("thread_id").Where("chat_id = ?", id)
		links := db.Model(&models.IMConversationLink{}).Select("binding_id").Where("chat_id = ? OR thread_id IN (?)", id, threads)
		if err := db.Select("id", "connection_id").Where("id IN (?)", links).Find(&bindings).Error; err != nil {
			return databaseError("capture_deleted_chat_im_bindings", "chat_id="+id, err)
		}
	}
	// 执行定位属于同一可重建 Chat 查询投影，随原删除事务一起清理。
	if err := db.Where("chat_id = ?", id).Delete(&models.ChatExecutionLocation{}).Error; err != nil {
		return databaseError("delete_chat_execution_locations", "chat_id="+id, err)
	}
	if err := db.Where("chat_id = ?", id).Delete(&models.ChatExecutionLocationCursor{}).Error; err != nil {
		return databaseError("delete_chat_execution_location_cursor", "chat_id="+id, err)
	}
	err := db.
		Where("id = ?", id).
		Delete(&models.Chat{}).Error
	if err != nil {
		return databaseError("delete_chat_projection", "chat_id="+id, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", id), affectedEntities...)
	connections := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		publishCommittedChange(ctx, c.db, "im", eventsResource("binding", binding.ID))
		if !connections[binding.ConnectionID] {
			connections[binding.ConnectionID] = true
			publishCommittedChange(ctx, c.db, "im", eventsResource("connection", binding.ConnectionID))
		}
	}
	return nil
}

// ListByWakerForDelete 返回删除伙伴时需要清理的全部 Chat。
func (c *chatController) ListByWakerForDelete(
	ctx context.Context,
	wakerID string,
) ([]models.Chat, error) {
	var chats []models.Chat
	err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Order("id ASC").
		Find(&chats).Error
	if err != nil {
		return nil, databaseError(
			"list_chats_for_delete",
			"waker_id="+wakerID,
			err,
		)
	}
	return chats, nil
}

// RuntimeSessionController 定义 Runtime 续接索引查询投影的持久化操作。
type RuntimeSessionController interface {
	// Get 返回 Chat 与 Runtime 对应的 Session 续接索引。
	Get(ctx context.Context, chatID string, runtimeID string) (models.RuntimeSession, error)
	// ListByChat 返回一 Chat 当前保存的全部 Runtime Session。
	ListByChat(ctx context.Context, chatID string) ([]models.RuntimeSession, error)
	// Upsert 创建或替换一条由权威日志派生的 Session 索引。
	Upsert(ctx context.Context, session models.RuntimeSession) error
	// Delete 删除一条失效 Session 索引。
	Delete(ctx context.Context, chatID string, runtimeID string) error
	// DeleteByChat 删除 Chat 的全部 Session 索引。
	DeleteByChat(ctx context.Context, chatID string) error
}

// runtimeSessionController 使用 GORM 实现 Runtime 续接索引查询投影。
type runtimeSessionController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewRuntimeSessionController 创建 Runtime 续接索引 Repository Controller。
func NewRuntimeSessionController(db *gorm.DB) RuntimeSessionController {
	return &runtimeSessionController{db: db}
}

// Get 返回 Chat 与 Runtime 对应的 Session 续接索引。
func (c *runtimeSessionController) Get(
	ctx context.Context,
	chatID string,
	runtimeID string,
) (models.RuntimeSession, error) {
	var session models.RuntimeSession
	err := controllerDB(ctx, c.db).
		Where("chat_id = ? AND runtime_id = ?", chatID, runtimeID).
		First(&session).Error
	if err == nil {
		return session, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.RuntimeSession{}, nil
	}
	return models.RuntimeSession{}, databaseError(
		"get_runtime_session",
		fmt.Sprintf("chat_id=%s runtime_id=%s", chatID, runtimeID),
		err,
	)
}

// ListByChat 返回一 Chat 当前保存的全部 Runtime Session。
func (c *runtimeSessionController) ListByChat(
	ctx context.Context,
	chatID string,
) ([]models.RuntimeSession, error) {
	var sessions []models.RuntimeSession
	err := controllerDB(ctx, c.db).
		Where("chat_id = ?", chatID).
		Order("runtime_id ASC").
		Find(&sessions).Error
	if err != nil {
		return nil, databaseError(
			"list_runtime_sessions_by_chat",
			"chat_id="+chatID,
			err,
		)
	}
	return sessions, nil
}

// Upsert 创建或替换一条由权威日志派生的 Session 索引。
func (c *runtimeSessionController) Upsert(
	ctx context.Context,
	session models.RuntimeSession,
) error {
	err := controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "chat_id"},
				{Name: "runtime_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"session_id",
				"last_execution_id",
				"updated_at",
			}),
		}).
		Create(&session).Error
	if err != nil {
		return databaseError(
			"upsert_runtime_session",
			fmt.Sprintf(
				"chat_id=%s runtime_id=%s",
				session.ChatID,
				session.RuntimeID,
			),
			err,
		)
	}
	return nil
}

// Delete 删除一条失效 Session 索引。
func (c *runtimeSessionController) Delete(
	ctx context.Context,
	chatID string,
	runtimeID string,
) error {
	err := controllerDB(ctx, c.db).
		Where("chat_id = ? AND runtime_id = ?", chatID, runtimeID).
		Delete(&models.RuntimeSession{}).Error
	if err != nil {
		return databaseError(
			"delete_runtime_session",
			fmt.Sprintf("chat_id=%s runtime_id=%s", chatID, runtimeID),
			err,
		)
	}
	return nil
}

// DeleteByChat 删除 Chat 的全部 Session 索引。
func (c *runtimeSessionController) DeleteByChat(
	ctx context.Context,
	chatID string,
) error {
	err := controllerDB(ctx, c.db).
		Where("chat_id = ?", chatID).
		Delete(&models.RuntimeSession{}).Error
	if err != nil {
		return databaseError(
			"delete_runtime_sessions_by_chat",
			"chat_id="+chatID,
			err,
		)
	}
	return nil
}

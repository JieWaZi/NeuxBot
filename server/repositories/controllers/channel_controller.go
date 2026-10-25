package controllers

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// ChannelController 定义团队配置及其成员默认值的持久化操作。
type ChannelController interface {
	// Create 原子事务内创建团队主体和全部成员配置。
	Create(ctx context.Context, channel models.Channel, members []models.ChannelMember) error
	// Get 按标识返回团队主体。
	Get(ctx context.Context, id string) (models.Channel, error)
	// ListMembers 按配置顺序返回团队当前成员默认值。
	ListMembers(ctx context.Context, channelID string) ([]models.ChannelMember, error)
	// List 分页返回符合搜索条件的团队。
	List(ctx context.Context, query string, limit int, offset int) ([]models.Channel, error)
	// Count 统计符合搜索条件的团队数量。
	Count(ctx context.Context, query string) (int64, error)
	// ListActivitySummaries 用一次聚合查询返回多个团队的活动计数。
	ListActivitySummaries(ctx context.Context, channelIDs []string) (map[string]models.ActivitySummary, error)
	// NameExists 判断名称是否已被另一团队使用。
	NameExists(ctx context.Context, name string, excludeID string) (bool, error)
	// Update 原子事务内更新团队主体并完整替换成员配置。
	Update(ctx context.Context, channel models.Channel, members []models.ChannelMember) error
	// CountActiveWork 统计阻止删除的待执行任务、运行任务、负责人输入和活跃 Chat Execution。
	CountActiveWork(ctx context.Context, channelID string) (int64, error)
	// Delete 删除团队主体及其当前成员配置；团队对话必须先由上层清理。
	Delete(ctx context.Context, channelID string) error
	// ListIDsByMemberWaker 返回指定伙伴当前参与的全部团队标识。
	ListIDsByMemberWaker(ctx context.Context, wakerID string) ([]string, error)
	// ListOrphanMemberWakerIDs 返回成员引用已不存在伙伴的 waker 标识集合。
	ListOrphanMemberWakerIDs(ctx context.Context) ([]string, error)
	// ListIDsWithOrphanLead 返回负责人已不存在伙伴的团队标识集合。
	ListIDsWithOrphanLead(ctx context.Context) ([]string, error)
	// RemoveMember 从团队中移除一位成员。
	RemoveMember(ctx context.Context, channelID string, wakerID string) error
	// UpdateLead 更新团队负责人。
	UpdateLead(ctx context.Context, channelID string, leadWakerID string, updatedAt int64) error
}

// channelController 使用 GORM 实现团队持久化。
type channelController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewChannelController 创建团队 Repository Controller。
func NewChannelController(db *gorm.DB) ChannelController {
	return &channelController{db: db}
}

// Create 原子事务内创建团队主体和全部成员配置。
func (c *channelController) Create(
	ctx context.Context,
	channel models.Channel,
	members []models.ChannelMember,
) error {
	db := controllerDB(ctx, c.db)
	if err := db.Create(&channel).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.ChannelNameConflict
		}
		return databaseError("create_channel", "channel_id="+channel.ID, err)
	}
	if err := db.Create(&members).Error; err != nil {
		return databaseError("create_channel_members", "channel_id="+channel.ID, err)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("channel", channel.ID))
	return nil
}

// Get 按标识返回团队主体。
func (c *channelController) Get(ctx context.Context, id string) (models.Channel, error) {
	var channel models.Channel
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&channel).Error
	if err == nil {
		return channel, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Channel{}, status_error.ChannelNotFound.WithParams(
			map[string]interface{}{"ChannelID": id},
		)
	}
	return models.Channel{}, databaseError("get_channel", "channel_id="+id, err)
}

// ListMembers 按配置顺序返回团队当前成员默认值。
func (c *channelController) ListMembers(
	ctx context.Context,
	channelID string,
) ([]models.ChannelMember, error) {
	members := []models.ChannelMember{}
	err := controllerDB(ctx, c.db).
		Where("channel_id = ?", channelID).
		Order("position ASC, waker_id ASC").
		Find(&members).Error
	if err != nil {
		return nil, databaseError("list_channel_members", "channel_id="+channelID, err)
	}
	return members, nil
}

// List 分页返回符合搜索条件的团队。
func (c *channelController) List(
	ctx context.Context,
	query string,
	limit int,
	offset int,
) ([]models.Channel, error) {
	channels := []models.Channel{}
	db := filterChannels(controllerDB(ctx, c.db), query)
	err := db.Order("updated_at DESC, id ASC").Limit(limit).Offset(offset).Find(&channels).Error
	if err != nil {
		return nil, databaseError("list_channels", "channel", err)
	}
	return channels, nil
}

// Count 统计符合搜索条件的团队数量。
func (c *channelController) Count(
	ctx context.Context,
	query string,
) (int64, error) {
	var count int64
	err := filterChannels(
		controllerDB(ctx, c.db).Model(&models.Channel{}),
		query,
	).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_channels", "channel", err)
	}
	return count, nil
}

// ListActivitySummaries 用一次聚合查询返回多个团队的活动计数。
func (c *channelController) ListActivitySummaries(
	ctx context.Context,
	channelIDs []string,
) (map[string]models.ActivitySummary, error) {
	summaries := make(map[string]models.ActivitySummary, len(channelIDs))
	for _, channelID := range channelIDs {
		summaries[channelID] = models.ActivitySummary{}
	}
	if len(channelIDs) == 0 {
		return summaries, nil
	}
	type activityRow struct {
		// ChannelID 是聚合计数所属团队标识。
		ChannelID string `gorm:"column:channel_id"`
		// WaitingCount 是等待用户处理的团队对话数量。
		WaitingCount int `gorm:"column:waiting_count"`
		// RunningCount 是存在活跃执行的团队对话数量。
		RunningCount int `gorm:"column:running_count"`
	}
	rows := []activityRow{}
	err := controllerDB(ctx, c.db).
		Table("channel_threads AS thread").
		Select(`thread.channel_id AS channel_id,
			COALESCE(SUM(CASE
				WHEN thread.ready_at IS NOT NULL AND `+channelThreadWaitingSQL+` THEN 1
				ELSE 0
			END), 0) AS waiting_count,
			COALESCE(SUM(CASE
				WHEN thread.ready_at IS NOT NULL
					AND NOT (`+channelThreadWaitingSQL+`)
					AND `+channelThreadRunningSQL+` THEN 1
				ELSE 0
			END), 0) AS running_count`).
		Where("thread.channel_id IN ? AND thread.channel_id IN (SELECT id FROM channels)", channelIDs).
		Group("thread.channel_id").
		Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_channel_activity_summaries", "channels", err)
	}
	for _, row := range rows {
		summaries[row.ChannelID] = models.ActivitySummary{
			RunningCount: row.RunningCount,
			WaitingCount: row.WaitingCount,
		}
	}
	return summaries, nil
}

// NameExists 判断名称是否已被另一团队使用。
func (c *channelController) NameExists(
	ctx context.Context,
	name string,
	excludeID string,
) (bool, error) {
	db := controllerDB(ctx, c.db).Model(&models.Channel{}).Where("name = ?", name)
	if excludeID != "" {
		db = db.Where("id <> ?", excludeID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return false, databaseError("check_channel_name", "name="+name, err)
	}
	return count > 0, nil
}

// Update 原子事务内更新团队主体并完整替换成员配置。
func (c *channelController) Update(
	ctx context.Context,
	channel models.Channel,
	members []models.ChannelMember,
) error {
	db := controllerDB(ctx, c.db)
	result := db.Model(&models.Channel{}).
		Where("id = ?", channel.ID).
		Updates(map[string]interface{}{
			"name":          channel.Name,
			"description":   channel.Description,
			"lead_waker_id": channel.LeadWakerID,
			"updated_at":    channel.UpdatedAt,
		})
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.ChannelNameConflict
		}
		return databaseError("update_channel", "channel_id="+channel.ID, result.Error)
	}
	if err := db.Where("channel_id = ?", channel.ID).Delete(&models.ChannelMember{}).Error; err != nil {
		return databaseError("delete_channel_members", "channel_id="+channel.ID, err)
	}
	if err := db.Create(&members).Error; err != nil {
		return databaseError("replace_channel_members", "channel_id="+channel.ID, err)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("channel", channel.ID))
	return nil
}

// CountActiveWork 统计阻止删除的待执行任务、负责人输入与成员（含负责人）活跃 Chat Execution。
func (c *channelController) CountActiveWork(
	ctx context.Context,
	channelID string,
) (int64, error) {
	db := controllerDB(ctx, c.db)
	var taskCount int64
	err := db.Model(&models.ChannelTask{}).
		Where("thread_id IN (SELECT id FROM channel_threads WHERE channel_id = ?)", channelID).
		Where("channel_tasks.status IN ?", []constants.ChannelTaskStatus{
			constants.ChannelTaskStatusPending,
			constants.ChannelTaskStatusInProgress,
		}).Count(&taskCount).Error
	if err != nil {
		return 0, databaseError("count_channel_active_tasks", "channel_id="+channelID, err)
	}
	var executionCount int64
	err = db.Model(&models.Chat{}).
		Where("id IN (SELECT chat_id FROM channel_thread_members WHERE thread_id IN (SELECT id FROM channel_threads WHERE channel_id = ?))", channelID).
		Where("chats.active_execution_id <> ''").
		Count(&executionCount).Error
	if err != nil {
		return 0, databaseError("count_channel_active_executions", "channel_id="+channelID, err)
	}
	var inputCount int64
	err = db.Model(&models.ChannelLeadInput{}).
		Where("thread_id IN (SELECT id FROM channel_threads WHERE channel_id = ?)", channelID).
		Where("channel_lead_inputs.status IN ?", []constants.ChannelInputStatus{
			constants.ChannelInputStatusPending,
			constants.ChannelInputStatusDelivering,
		}).Count(&inputCount).Error
	if err != nil {
		return 0, databaseError("count_channel_active_inputs", "channel_id="+channelID, err)
	}
	return taskCount + executionCount + inputCount, nil
}

// ListIDsByMemberWaker 返回指定伙伴当前参与的全部团队标识。
func (c *channelController) ListIDsByMemberWaker(
	ctx context.Context,
	wakerID string,
) ([]string, error) {
	wakerID = strings.TrimSpace(wakerID)
	if wakerID == "" {
		return nil, status_error.BadRequest
	}
	channelIDs := []string{}
	err := controllerDB(ctx, c.db).
		Model(&models.ChannelMember{}).
		Where("waker_id = ?", wakerID).
		Order("channel_id ASC").
		Pluck("channel_id", &channelIDs).Error
	if err != nil {
		return nil, databaseError("list_channels_by_member_waker", "waker_id="+wakerID, err)
	}
	return channelIDs, nil
}

// ListOrphanMemberWakerIDs 返回成员引用已不存在伙伴的 waker 标识集合。
func (c *channelController) ListOrphanMemberWakerIDs(ctx context.Context) ([]string, error) {
	wakerIDs := []string{}
	err := controllerDB(ctx, c.db).
		Table(models.ChannelMember{}.TableName()+" AS member").
		Select("DISTINCT member.waker_id").
		Where("NOT EXISTS (SELECT 1 FROM wakers WHERE id = member.waker_id)").
		Order("member.waker_id ASC").
		Pluck("member.waker_id", &wakerIDs).Error
	if err != nil {
		return nil, databaseError("list_orphan_channel_member_waker_ids", "channel_members", err)
	}
	return wakerIDs, nil
}

// ListIDsWithOrphanLead 返回负责人已不存在伙伴的团队标识集合。
func (c *channelController) ListIDsWithOrphanLead(ctx context.Context) ([]string, error) {
	channelIDs := []string{}
	err := controllerDB(ctx, c.db).
		Table(models.Channel{}.TableName()+" AS channel").
		Select("channel.id").
		Where("NOT EXISTS (SELECT 1 FROM wakers WHERE id = channel.lead_waker_id)").
		Order("channel.id ASC").
		Pluck("channel.id", &channelIDs).Error
	if err != nil {
		return nil, databaseError("list_channels_with_orphan_lead", "channels", err)
	}
	return channelIDs, nil
}

// RemoveMember 从团队中移除一位成员。
func (c *channelController) RemoveMember(
	ctx context.Context,
	channelID string,
	wakerID string,
) error {
	channelID = strings.TrimSpace(channelID)
	wakerID = strings.TrimSpace(wakerID)
	if channelID == "" || wakerID == "" {
		return status_error.BadRequest
	}
	result := controllerDB(ctx, c.db).
		Where("channel_id = ? AND waker_id = ?", channelID, wakerID).
		Delete(&models.ChannelMember{})
	if result.Error != nil {
		return databaseError(
			"remove_channel_member",
			"channel_id="+channelID+" waker_id="+wakerID,
			result.Error,
		)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("channel", channelID))
	return nil
}

// UpdateLead 更新团队负责人。
func (c *channelController) UpdateLead(
	ctx context.Context,
	channelID string,
	leadWakerID string,
	updatedAt int64,
) error {
	channelID = strings.TrimSpace(channelID)
	leadWakerID = strings.TrimSpace(leadWakerID)
	if channelID == "" || leadWakerID == "" {
		return status_error.BadRequest
	}
	result := controllerDB(ctx, c.db).
		Model(&models.Channel{}).
		Where("id = ?", channelID).
		Updates(map[string]interface{}{
			"lead_waker_id": leadWakerID,
			"updated_at":    updatedAt,
		})
	if result.Error != nil {
		return databaseError(
			"update_channel_lead",
			"channel_id="+channelID+" lead_waker_id="+leadWakerID,
			result.Error,
		)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("channel", channelID))
	return nil
}

// Delete 删除团队主体及其当前成员配置；团队对话必须先由上层清理。
func (c *channelController) Delete(ctx context.Context, channelID string) error {
	db := controllerDB(ctx, c.db)
	if err := db.Where("channel_id = ?", channelID).Delete(&models.ChannelMember{}).Error; err != nil {
		return databaseError("delete_channel_members", "channel_id="+channelID, err)
	}
	if err := db.Where("id = ?", channelID).Delete(&models.Channel{}).Error; err != nil {
		return databaseError("delete_channel", "channel_id="+channelID, err)
	}
	publishCommittedChange(ctx, c.db, "entities", eventsResource("channel", channelID))
	return nil
}

func filterChannels(db *gorm.DB, query string) *gorm.DB {
	query = strings.TrimSpace(query)
	if query == "" {
		return db
	}
	pattern := "%" + strings.ToLower(query) + "%"
	return db.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", pattern, pattern)
}

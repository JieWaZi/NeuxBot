package controllers

import (
	"context"
	"strconv"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkResultController 提供交付与用户确认的窄持久化接口。
type WorkResultController interface {
	// EnableTracking 在启动恢复前固定首次启用边界。
	EnableTracking(context.Context, int64) error
	// Publish 单调发布原结果引用，保留用户已有确认事实。
	Publish(context.Context, models.WorkResultReceipt) error
	// PublishChat 解析原归属并排除内部成员、流程工作器和未收尾团队。
	PublishChat(context.Context, string, int64, int64, models.WorkResultReference) error
	// Get 返回容器的最新交付与用户确认事实。
	Get(context.Context, constants.WorkResultOwnerKind, string) (models.WorkResultReceipt, bool, error)
	// MarkViewed 仅接纳已发布版本，返回目标是否合法。
	MarkViewed(context.Context, constants.WorkResultOwnerKind, string, int64, int64) (bool, error)
	// ClearUnread 原子确认全部当前待查看版本，后续发布的新版本仍待查看。
	ClearUnread(context.Context, int64) error
	// Delete 只在真实容器删除时清理结果确认事实。
	Delete(context.Context, constants.WorkResultOwnerKind, []string) error
}

// workResultController 保存数据库及启动时固定的启用边界。
type workResultController struct {
	// db 是未进入事务时的默认 GORM 连接。
	db *gorm.DB
	// startedAt 是持久启用边界的进程内快照。
	startedAt atomic.Int64
}

// NewWorkResultController 创建仓储，不启动后台工作。
func NewWorkResultController(db *gorm.DB) WorkResultController {
	return &workResultController{db: db}
}

// EnableTracking 幂等保存并读取首次启用时间。
func (c *workResultController) EnableTracking(ctx context.Context, at int64) error {
	value := models.WorkResultTracking{ID: constants.WorkResultTrackingID, StartedAt: at}
	db := controllerDB(ctx, c.db)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&value).Error; err != nil {
		return databaseError("enable_work_result_tracking", value.ID, err)
	}
	if err := db.Where("id = ?", value.ID).Limit(1).Find(&value).Error; err != nil {
		return databaseError("read_work_result_tracking", value.ID, err)
	}
	c.startedAt.Store(value.StartedAt)
	return nil
}

// Publish 条件 upsert 保证重放和乱序不会覆盖较新结果。
func (c *workResultController) Publish(ctx context.Context, value models.WorkResultReceipt) error {
	if value.OwnerKind == constants.WorkResultOwnerKindUnspecified || value.OwnerID == "" || value.LatestVersion <= 0 || value.AvailableAt <= 0 {
		return status_error.BadRequest
	}
	if c.startedAt.Load() == 0 {
		var tracking models.WorkResultTracking
		result := controllerDB(ctx, c.db).Where("id = ?", constants.WorkResultTrackingID).Limit(1).Find(&tracking)
		if result.Error != nil {
			return databaseError("read_work_result_tracking", value.OwnerID, result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}
		c.startedAt.Store(tracking.StartedAt)
	}
	if value.AvailableAt < c.startedAt.Load() {
		return nil
	}
	where := "excluded.latest_version > work_result_receipts.latest_version"
	value.ViewedVersion, value.ViewedAt = 0, nil
	result := controllerDB(ctx, c.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "owner_kind"}, {Name: "owner_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"latest_version", "reference", "available_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: where}}},
	}).Create(&value)
	if result.Error != nil {
		return databaseError("publish_work_result", value.OwnerID, result.Error)
	}
	if result.RowsAffected > 0 {
		publishCommittedChange(ctx, c.db, "work-results", eventsResource(string(value.OwnerKind), value.OwnerID))
		publishCommittedNoticeIDs(ctx, c.db, []string{"result:" + string(value.OwnerKind) + ":" + value.OwnerID + ":" + strconv.FormatInt(value.LatestVersion, 10)}, nil)
	}
	return nil
}

// PublishChat 只保存归属明确、已经交付的原结果引用。
func (c *workResultController) PublishChat(ctx context.Context, chatID string, version, at int64, reference models.WorkResultReference) error {
	db := controllerDB(ctx, c.db)
	var thread models.ChannelThread
	result := db.Where("lead_chat_id = ? AND ready_at IS NOT NULL", chatID).Limit(1).Find(&thread)
	if result.Error != nil {
		return databaseError("resolve_work_result_owner", chatID, result.Error)
	}
	kind, id := constants.WorkResultOwnerKindChat, chatID
	if result.RowsAffected > 0 {
		var unfinished int64
		if err := db.Model(&models.ChannelTask{}).Where("thread_id = ? AND status IN ?", thread.ID, []constants.ChannelTaskStatus{constants.ChannelTaskStatusPending, constants.ChannelTaskStatusInProgress, constants.ChannelTaskStatusInReview}).Count(&unfinished).Error; err != nil {
			return databaseError("qualify_team_result", thread.ID, err)
		}
		if unfinished > 0 {
			return nil
		}
		kind, id = constants.WorkResultOwnerKindTeamThread, thread.ID
	} else {
		var count int64
		if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM channel_thread_members WHERE chat_id=?) OR EXISTS(SELECT 1 FROM workflow_effects WHERE chat_id=?) OR EXISTS(SELECT 1 FROM workflow_creation_sessions WHERE chat_id=?)`, chatID, chatID, chatID).Find(&count).Error; err != nil {
			return databaseError("resolve_work_result_owner", chatID, err)
		}
		if count > 0 {
			return nil
		}
		var chat models.Chat
		result := db.Where("id = ?", chatID).Limit(1).Find(&chat)
		if result.Error != nil {
			return databaseError("resolve_work_result_chat", chatID, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.ChatNotFound
		}
		if chat.HiddenFromWakerHistory {
			if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM automatic_runs WHERE chat_id=?) OR EXISTS(SELECT 1 FROM im_conversation_links WHERE chat_id=?) OR EXISTS(SELECT 1 FROM work_result_receipts WHERE owner_kind='chat' AND owner_id=?)`, chatID, chatID, chatID).Find(&count).Error; err != nil {
				return databaseError("resolve_hidden_work_result", chatID, err)
			}
			if count == 0 {
				return nil
			}
		}
	}
	return c.Publish(ctx, models.WorkResultReceipt{OwnerKind: kind, OwnerID: id, LatestVersion: version, AvailableAt: at, Reference: reference})
}

// Get 只查询指定容器的一行交付与确认事实。
func (c *workResultController) Get(ctx context.Context, kind constants.WorkResultOwnerKind, id string) (models.WorkResultReceipt, bool, error) {
	var value models.WorkResultReceipt
	result := controllerDB(ctx, c.db).Where("owner_kind = ? AND owner_id = ?", kind, id).Limit(1).Find(&value)
	if result.Error != nil {
		return value, false, databaseError("get_work_result", id, result.Error)
	}
	return value, result.RowsAffected > 0, nil
}

// MarkViewed 保证旧阅读不清新结果，已读版本不倒退。
func (c *workResultController) MarkViewed(ctx context.Context, kind constants.WorkResultOwnerKind, id string, version, at int64) (bool, error) {
	if version <= 0 {
		return false, status_error.BadRequest
	}
	result := controllerDB(ctx, c.db).Model(&models.WorkResultReceipt{}).
		Where("owner_kind = ? AND owner_id = ? AND latest_version >= ?", kind, id, version).
		Updates(map[string]any{
			"viewed_version": gorm.Expr("MAX(viewed_version, ?)", version),
			"viewed_at":      gorm.Expr("CASE WHEN viewed_version < ? THEN ? ELSE viewed_at END", version, at),
		})
	if result.Error != nil {
		return false, databaseError("view_work_result", id, result.Error)
	}
	if result.RowsAffected > 0 {
		publishCommittedChange(ctx, c.db, "work-results", eventsResource(string(kind), id))
		publishCommittedNoticeIDs(ctx, c.db, nil, []string{"result:" + string(kind) + ":" + id + ":" + strconv.FormatInt(version, 10)})
	}
	return result.RowsAffected > 0, nil
}

// ClearUnread 以单条条件更新清除当前待查看标记，不修改交付与原结果引用。
func (c *workResultController) ClearUnread(ctx context.Context, at int64) error {
	err := controllerDB(ctx, c.db).Model(&models.WorkResultReceipt{}).
		Where("latest_version > viewed_version").
		Updates(map[string]any{
			"viewed_version": gorm.Expr("latest_version"),
			"viewed_at":      at,
		}).Error
	if err != nil {
		return databaseError("clear_work_results", "", err)
	}
	publishCommittedChange(ctx, c.db, "work-results", eventsResource("global", ""))
	return nil
}

// Delete 不用于 Chat 投影重建，防止丢失用户确认事实。
func (c *workResultController) Delete(ctx context.Context, kind constants.WorkResultOwnerKind, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	err := controllerDB(ctx, c.db).Where("owner_kind = ? AND owner_id IN ?", kind, ids).Delete(&models.WorkResultReceipt{}).Error
	if err != nil {
		return databaseError("delete_work_results", string(kind), err)
	}
	publishCommittedChange(ctx, c.db, "work-results", eventsResource("global", ""))
	return nil
}

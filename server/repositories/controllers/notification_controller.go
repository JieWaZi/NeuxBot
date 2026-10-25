package controllers

import (
	"context"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotificationQuery 描述通知分桶、过滤和稳定相邻续页位置。
type NotificationQuery struct {
	// Bucket 只接受 action 或 unread。
	Bucket string
	// ExecutorID 限定原伙伴、团队或工作流。
	ExecutorID string
	// Source 限定既有入口来源。
	Source string
	// Query 是安全标题的字面搜索。
	Query string
	// NoticeID 非空时定位原请求或交付版本。
	NoticeID string
	// CursorAt 是上一页最后一项的发生时间。
	CursorAt int64
	// CursorID 是相同时间下的稳定通知身份。
	CursorID string
	// Offset 仅用于显式数字跳页。
	Offset int
	// Limit 是本页数据上限。
	Limit int
}

// NotificationController 复用原工作查询，提供通知组合和无成果失败确认。
type NotificationController interface {
	Snapshot(context.Context) (models.NotificationFacts, error)
	List(context.Context, NotificationQuery) ([]models.NotificationItem, int64, error)
	Get(context.Context, string) (models.NotificationItem, error)
	ConfirmFailure(context.Context, string, int64) error
	ClearFailures(context.Context, int64) error
}

// notificationController 将全部方言与组合查询保留在仓储层。
type notificationController struct {
	// db 是默认连接，显式事务由 Context 传递。
	db *gorm.DB
}

// NewNotificationController 构造不启动后台流程的通知仓储。
func NewNotificationController(db *gorm.DB) NotificationController {
	return &notificationController{db: db}
}

func notificationText(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}

// Get 按精确请求或成果版本读取仍有效的通知。
func (c *notificationController) Get(ctx context.Context, id string) (models.NotificationItem, error) {
	bucket := "action"
	if strings.HasPrefix(id, "result:") || strings.HasPrefix(id, "automatic-run-failed:") {
		bucket = "unread"
	}
	items, _, err := c.List(ctx, NotificationQuery{Bucket: bucket, NoticeID: id, Limit: 1})
	if err != nil {
		return models.NotificationItem{}, err
	}
	if len(items) == 0 {
		return models.NotificationItem{}, status_error.BadRequest
	}
	return items[0], nil
}

// ConfirmFailure 幂等确认当前自动失败，不改变原运行或已交付成果。
func (c *notificationController) ConfirmFailure(ctx context.Context, id string, at int64) error {
	var previous models.AttentionNoticeAck
	if err := controllerDB(ctx, c.db).Where("notice_id = ? AND viewed_at IS NOT NULL", id).Find(&previous).Error; err != nil {
		return databaseError("read_failure_confirmation", id, err)
	}
	if previous.NoticeID != "" {
		return nil
	}
	item, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Kind != "automatic_failure" {
		return status_error.BadRequest
	}
	ack := models.AttentionNoticeAck{NoticeID: id, ViewedAt: &at}
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "notice_id"}}, DoUpdates: clause.Assignments(map[string]any{"viewed_at": gorm.Expr("COALESCE(attention_notice_acks.viewed_at,excluded.viewed_at)")})}).Create(&ack).Error; err != nil {
		return databaseError("view_failure_notification", id, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	return nil
}

// ClearFailures 只确认当前可见失败，分批写入并保留既有阅读时间。
func (c *notificationController) ClearFailures(ctx context.Context, at int64) error {
	return NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(ctx context.Context) error {
		// 先按任务主键续批，避免每一批失败都重新计算全部任务的当前运行集合。
		db := controllerDB(ctx, c.db)
		err := walkRepositoryRows(db.Model(&models.AutomaticTask{}).Select("id").Where("deleting=0"), func(tasks []models.AutomaticTask) error {
			ids := make([]string, 0, len(tasks))
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
			var runs []models.AutomaticRun
			if err := c.failureBase(ctx, NotificationQuery{}, ids).Select("id").Find(&runs).Error; err != nil {
				return databaseError("read_failure_batch", "", err)
			}
			if len(runs) == 0 {
				return nil
			}
			acks := make([]models.AttentionNoticeAck, 0, len(runs))
			for _, run := range runs {
				acks = append(acks, models.AttentionNoticeAck{NoticeID: "automatic-run-failed:" + run.ID, ViewedAt: &at})
			}
			if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "notice_id"}}, DoUpdates: clause.Assignments(map[string]any{"viewed_at": gorm.Expr("COALESCE(attention_notice_acks.viewed_at,excluded.viewed_at)")})}).CreateInBatches(&acks, repositoryReadBatch).Error; err != nil {
				return databaseError("clear_failure_notifications", strconv.FormatInt(at, 10), err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
		return nil
	})
}

package controllers

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AttentionNoticeController 持久化应用内提醒的关闭记录。
type AttentionNoticeController interface {
	// Dismiss 按通知标识写入或更新关闭时间，幂等。
	Dismiss(context.Context, string, int64) error
	// ListDismissedIDs 返回全部已关闭的通知标识，供桌面恢复去重。
	ListDismissedIDs(context.Context) ([]string, error)
	// IsDismissed 判断通知是否已关闭。
	IsDismissed(context.Context, string) (bool, error)
}

type attentionNoticeController struct {
	// db 是写入提醒已读记录的 GORM 连接。
	db *gorm.DB
}

// NewAttentionNoticeController 创建提醒已读 Controller。
func NewAttentionNoticeController(db *gorm.DB) AttentionNoticeController {
	return &attentionNoticeController{db: db}
}

// Dismiss 按通知标识写入或更新关闭时间，幂等。
func (c *attentionNoticeController) Dismiss(ctx context.Context, noticeID string, dismissedAt int64) error {
	if dismissedAt == 0 {
		dismissedAt = time.Now().UnixMilli()
	}
	ack := models.AttentionNoticeAck{NoticeID: noticeID, DismissedAt: dismissedAt}
	err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "notice_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"dismissed_at"}),
	}).Create(&ack).Error
	if err != nil {
		return databaseError("dismiss_attention_notice", "notice_id="+noticeID, err)
	}
	publishCommittedChange(ctx, c.db, "notification-state", eventsResource("global", ""))
	return nil
}

// ListDismissedIDs 返回全部已关闭的通知标识，供桌面恢复去重。
func (c *attentionNoticeController) ListDismissedIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := controllerDB(ctx, c.db).Model(&models.AttentionNoticeAck{}).
		Where("dismissed_at > 0").
		Order("dismissed_at ASC, notice_id ASC").
		Pluck("notice_id", &ids).Error
	if err != nil {
		return nil, databaseError("list_dismissed_attention_notices", "", err)
	}
	return ids, nil
}

// IsDismissed 判断通知是否已关闭。
func (c *attentionNoticeController) IsDismissed(ctx context.Context, noticeID string) (bool, error) {
	var count int64
	err := controllerDB(ctx, c.db).Model(&models.AttentionNoticeAck{}).
		Where("notice_id = ? AND dismissed_at > 0", noticeID).Count(&count).Error
	if err != nil {
		return false, databaseError("get_attention_notice_ack", "notice_id="+noticeID, err)
	}
	return count > 0, nil
}

var _ AttentionNoticeController = (*attentionNoticeController)(nil)

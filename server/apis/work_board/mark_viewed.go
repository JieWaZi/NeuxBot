package work_board

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// MarkWorkBoardViewed 按已固定的原边界确认一批工作。
type MarkWorkBoardViewed struct {
	// MethodPost 声明写入操作。
	ginx.MethodPost
	// Body 保存本批原工作目标。
	Body MarkWorkBoardViewedRequest `in:"body"`
}

// MarkWorkBoardViewedRequest 保存最多 128 个固定目标。
type MarkWorkBoardViewedRequest struct {
	// Targets 可在传输中断后原样重试。
	Targets []models.WorkViewTarget `json:"targets" validate:"required,min=1,max=128"`
}

func (*MarkWorkBoardViewed) Path() string { return "/mark-viewed" }
func (d *MarkWorkBoardViewed) Output(ctx *gin.Context) (interface{}, error) {
	value, err := markViewedBatch(ctx.Request.Context(), constants.LocalReaderID, d.Body.Targets)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}

// markViewedBatch 在单事务中校验原归属并提交已固定的阅读边界。
func markViewedBatch(ctx context.Context, reader string, targets []models.WorkViewTarget) (models.MarkViewedResult, error) {
	var value models.MarkViewedResult
	if len(targets) == 0 || len(targets) > 128 {
		return value, status_error.BadRequest
	}
	err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		value = models.MarkViewedResult{}
		refs := make([]models.WorkRef, 0, len(targets))
		for _, target := range targets {
			if target.Work.ID == "" {
				return status_error.BadRequest
			}
			refs = append(refs, target.Work)
		}
		owners, err := global.WorkBoardControllerManager.ResolveWorkRefs(tx, refs)
		if err != nil {
			return err
		}
		changed := false
		for _, target := range targets {
			owner := owners[target.Work]
			if owner.OwnerID == "" {
				value.SkippedDeleted++
				continue
			}
			var applied bool
			if target.ChatID != "" {
				if target.ChatID != owner.ChatID || target.OwnerKind != "" || target.OwnerID != "" || target.ThroughMessageID == "" || target.ObservedUnreadGeneration < 0 {
					return status_error.BadRequest
				}
				if global.WorkspaceAttention.Incomplete != nil && global.WorkspaceAttention.Incomplete([]string{target.ChatID}) != "" {
					return status_error.ReadingProjectionIncomplete
				}
				applied, err = global.ChatReadingControllerManager.MarkReadForBoard(tx, reader, target.ChatID, target.ThroughMessageID, target.ObservedUnreadGeneration)
				if errors.Is(err, status_error.BadRequest) {
					value.SkippedDeleted++
					continue
				}
			} else {
				if owner.ChatID != "" || target.OwnerID != owner.OwnerID || target.OwnerKind != constants.RunOwnerKind(owner.OwnerKind) || target.ThroughMessageID != "" {
					return status_error.BadRequest
				}
				applied, err = global.RunViewingControllerManager.MarkViewedForBoard(tx, reader, models.RunOwnerRef{OwnerKind: target.OwnerKind, OwnerID: target.OwnerID})
			}
			switch {
			case err == nil:
				value.Applied++
				changed = changed || applied
			case errors.Is(err, status_error.ReadingIntentConflict), errors.Is(err, status_error.BadRequest):
				value.SkippedConflict++
			case errors.Is(err, status_error.ChatNotFound), errors.Is(err, status_error.WorkflowNotFound), errors.Is(err, status_error.AutomaticRunNotFound):
				value.SkippedDeleted++
			default:
				return err
			}
		}
		if changed {
			global.WorkBoardControllerManager.PublishReadingChanged(tx)
		}
		return nil
	})
	return value, err
}

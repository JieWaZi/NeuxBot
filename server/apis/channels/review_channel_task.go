package channels

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 验收团队任务请求
type ReviewChannelTaskRequest struct {
	// 是否通过
	Approved bool `json:"approved"`
	// 驳回反馈
	Feedback string `json:"feedback" validate:"max=1048576"`
}

// 验收团队任务
type ReviewChannelTask struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
	// 请求体
	Body ReviewChannelTaskRequest `in:"body"`
}

// Path 返回用户验收接口路径。
func (r *ReviewChannelTask) Path() string {
	return "/:channelId/threads/:threadId/tasks/:taskId/review"
}

// Output 由用户通过或驳回等待人工验收的任务。
func (r *ReviewChannelTask) Output(ctx *gin.Context) (interface{}, error) {
	operationContext, release, lockErr := teamwork.Acquire(ctx, r.ChannelID)
	if lockErr != nil {
		return nil, ginx.WithStack(lockErr)
	}
	defer release()
	thread, err := writableChannelThread(operationContext, r.ChannelID, r.ThreadID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	task, err := channelTask(operationContext, r.ThreadID, r.TaskID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if task.Status != constants.ChannelTaskStatusInReview {
		return nil, ginx.WithStack(status_error.ChannelTaskStateConflict)
	}
	feedback := strings.TrimSpace(r.Body.Feedback)
	if !r.Body.Approved && feedback == "" {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	now := time.Now().UnixMilli()
	err = global.TransactionControllerManager.Transaction(operationContext, func(transactionContext context.Context) error {
		if writableErr := ensureChannelWritable(transactionContext, thread.ChannelID); writableErr != nil {
			return writableErr
		}
		current, currentErr := global.ChannelTaskControllerManager.Get(transactionContext, task.ID)
		if currentErr != nil {
			return currentErr
		}
		if current.Status != constants.ChannelTaskStatusInReview {
			return status_error.ChannelTaskStateConflict
		}
		if !r.Body.Approved {
			payload := execution.WorkInput{
				Actor:   string(constants.ChannelMessageActorUser),
				Content: feedback,
			}
			followUp := models.ChannelTaskFollowUp{
				ID:        util.New(),
				TaskID:    current.ID,
				Text:      feedback,
				Actor:     constants.ChannelMessageActorUser,
				Payload:   payload,
				CreatedAt: now,
			}
			if createErr := global.ChannelTaskControllerManager.CreateFollowUp(
				transactionContext,
				followUp,
			); createErr != nil {
				return createErr
			}
			if updateErr := global.ChannelTaskControllerManager.UpdateFields(
				transactionContext,
				current.ID,
				map[string]interface{}{
					"status":      constants.ChannelTaskStatusPending,
					"finished_at": nil,
					"updated_at":  now,
				},
			); updateErr != nil {
				return updateErr
			}
			_, eventErr := appendChannelEvent(
				transactionContext,
				thread.ChannelID,
				thread.ID,
				constants.ChannelEventTaskFollowUpQueued,
				map[string]interface{}{"taskId": current.ID, "followUpId": followUp.ID},
			)
			return eventErr
		}
		if updateErr := global.ChannelTaskControllerManager.UpdateFields(
			transactionContext,
			current.ID,
			map[string]interface{}{
				"status":      constants.ChannelTaskStatusCompleted,
				"finished_at": now,
				"updated_at":  now,
			},
		); updateErr != nil {
			return updateErr
		}
		if current.AssigneeWakerID != thread.LeadWakerID {
			if createErr := global.ChannelTaskControllerManager.CreateNotification(
				transactionContext,
				models.ChannelLeadNotification{
					ID:        util.New(),
					ThreadID:  thread.ID,
					TaskID:    current.ID,
					AttemptID: reviewNotificationAttemptID(current.ID, current.CurrentFence),
					Status:    constants.ChannelNotificationStatusPending,
					Payload:   current.Result,
					CreatedAt: now,
					UpdatedAt: now,
				},
			); createErr != nil {
				return createErr
			}
		}
		_, eventErr := appendChannelEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskReviewApproved,
			map[string]interface{}{"taskId": current.ID},
		)
		return eventErr
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChannelService.ReconcileThread(operationContext, thread.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// reviewNotificationAttemptID 为一次人工验收生成与真实尝试隔离的稳定去重标识。
func reviewNotificationAttemptID(taskID string, fence int) string {
	sum := sha256.Sum256([]byte("review-approved:" + taskID + ":" + strconv.Itoa(fence)))
	return hex.EncodeToString(sum[:16])
}

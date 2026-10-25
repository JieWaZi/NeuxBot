package channel

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// FollowUpTask 把保留真实作者的补充持久化到原任务并在当前尝试结束后续跑。
func (s *service) FollowUpTask(
	ctx context.Context,
	taskID string,
	input execution.WorkInput,
) error {
	taskID = strings.TrimSpace(taskID)
	text := strings.TrimSpace(input.Content)
	input.Actor = strings.TrimSpace(input.Actor)
	if taskID == "" || text == "" || len(text) > constants.ChannelMaximumInputBytes ||
		(input.Actor != string(constants.ChannelMessageActorUser) &&
			input.Actor != string(constants.ChannelMessageActorLead)) {
		return status_error.BadRequest
	}
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != constants.ChannelTaskStatusInProgress {
		return status_error.ChannelTaskStateConflict
	}
	locked, release, lockErr := s.acquireThreadOperation(ctx, task.ThreadID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	defer release()
	task, err = s.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != constants.ChannelTaskStatusInProgress {
		return status_error.ChannelTaskStateConflict
	}
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	if _, err := s.channels.Get(ctx, thread.ChannelID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	followUp := models.ChannelTaskFollowUp{
		ID:        util.New(),
		TaskID:    task.ID,
		Text:      text,
		Actor:     constants.ChannelMessageActor(input.Actor),
		Payload:   input,
		CreatedAt: now,
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		current, getErr := s.tasks.Get(transactionContext, task.ID)
		if getErr != nil {
			return getErr
		}
		if current.Status != constants.ChannelTaskStatusInProgress {
			return status_error.ChannelTaskStateConflict
		}
		if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
			return writableErr
		}
		if err := s.tasks.CreateFollowUp(transactionContext, followUp); err != nil {
			return err
		}
		_, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskFollowUpQueued, map[string]interface{}{
			"taskId":     task.ID,
			"followUpId": followUp.ID,
		})
		if err != nil {
			return err
		}
		return agentcommand.Commit(transactionContext, map[string]any{"taskId": task.ID, "followUpId": followUp.ID, "accepted": true})
	})
	if err != nil {
		return err
	}
	return nil
}

// retryTask 由负责人短期任务能力重试一次失败任务，可在重试前更换执行伙伴。
func (s *service) retryTask(
	ctx context.Context,
	taskID string,
	assigneeWakerID string,
) error {
	taskID = strings.TrimSpace(taskID)
	assigneeWakerID = strings.TrimSpace(assigneeWakerID)
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != constants.ChannelTaskStatusFailed {
		return status_error.ChannelTaskStateConflict
	}
	locked, release, lockErr := s.acquireThreadOperation(ctx, task.ThreadID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	defer release()
	task, err = s.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != constants.ChannelTaskStatusFailed {
		return status_error.ChannelTaskStateConflict
	}
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return err
	}
	failureAttempts, err := s.tasks.CountFailureAttempts(ctx, task.ID)
	if err != nil {
		return err
	}
	if failureAttempts >= int64(thread.MaxFailureAttempts) {
		return status_error.ChannelTaskRetryLimitReached
	}
	if assigneeWakerID == "" {
		assigneeWakerID = task.AssigneeWakerID
	}
	if assigneeWakerID == thread.LeadWakerID {
		return status_error.BadRequest
	}
	if _, err := s.threads.GetMember(ctx, thread.ID, assigneeWakerID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
			return writableErr
		}
		if err := s.tasks.UpdateFields(transactionContext, task.ID, map[string]interface{}{
			"assignee_waker_id": assigneeWakerID,
			"status":            constants.ChannelTaskStatusPending,
			"error_message":     "",
			"finished_at":       nil,
			"updated_at":        now,
		}); err != nil {
			return err
		}
		_, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskRetryQueued, map[string]interface{}{
			"taskId":          task.ID,
			"assigneeWakerId": assigneeWakerID,
		})
		if err != nil {
			return err
		}
		return agentcommand.Commit(transactionContext, map[string]any{"taskId": task.ID, "retryQueued": true})
	})
	if err != nil {
		return err
	}
	_, err = s.schedule(ctx, thread.ID)
	return err
}

// StopTask 取消单个任务对应的当前 Chat Execution。
func (s *service) StopTask(ctx context.Context, taskID string) error {
	task, err := s.tasks.Get(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return err
	}
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return err
	}
	if task.Status == constants.ChannelTaskStatusPending ||
		task.Status == constants.ChannelTaskStatusInReview {
		now := time.Now().UnixMilli()
		err = s.transaction(ctx, func(transactionContext context.Context) error {
			if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
				return writableErr
			}
			current, getErr := s.tasks.Get(transactionContext, task.ID)
			if getErr != nil {
				return getErr
			}
			if current.Status != task.Status || current.CurrentFence != task.CurrentFence {
				return status_error.ChannelTaskStateConflict
			}
			if err := s.tasks.UpdateFields(transactionContext, task.ID, map[string]interface{}{
				"status":      constants.ChannelTaskStatusCancelled,
				"finished_at": now,
				"updated_at":  now,
			}); err != nil {
				return err
			}
			_, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskCancelled, map[string]interface{}{
				"taskId": task.ID,
			})
			if err != nil {
				return err
			}
			return agentcommand.Commit(transactionContext, map[string]any{"taskId": task.ID, "cancellationRequested": true})
		})
		if err != nil {
			return err
		}
		_, err = s.schedule(ctx, thread.ID)
		return err
	}
	if task.Status != constants.ChannelTaskStatusInProgress {
		return status_error.ChannelTaskStateConflict
	}
	attempt, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
	if err != nil {
		return err
	}
	if !agentcommand.Active(ctx) {
		return s.executions.CancelAndWait(ctx, attempt.ChatID)
	}
	now := time.Now().UnixMilli()
	if err := s.transaction(ctx, func(ctx context.Context) error {
		current, err := s.tasks.Get(ctx, task.ID)
		if err != nil {
			return err
		}
		if current.Status != constants.ChannelTaskStatusInProgress || current.CurrentFence != attempt.Fence {
			return status_error.ChannelTaskAttemptStale
		}
		if err := s.tasks.UpdateFields(ctx, task.ID, map[string]interface{}{"cancel_requested_at": now, "cancel_requested_fence": attempt.Fence}); err != nil {
			return err
		}
		return agentcommand.Commit(ctx, map[string]any{"taskId": task.ID, "cancellationRequested": true})
	}); err != nil {
		return err
	}
	s.watchTaskCancellation(attempt)
	return nil
}

// StopWork 先持久化停止意图并冻结调度，再收敛真实执行和开放队列。
func (s *service) StopWork(
	ctx context.Context,
	channelID string,
	threadID string,
) error {
	channelID = strings.TrimSpace(channelID)
	threadID = strings.TrimSpace(threadID)
	if channelID == "" || threadID == "" {
		return status_error.BadRequest
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if thread.ChannelID != channelID || thread.ReadyAt == nil {
		return status_error.ChannelThreadNotFound.WithParams(
			map[string]interface{}{"ThreadID": threadID},
		)
	}
	if _, err := s.channels.Get(ctx, channelID); err != nil {
		return err
	}
	if thread.CancelingAt == nil {
		cancelingAt := time.Now().UnixMilli()
		err = s.transaction(ctx, func(transactionContext context.Context) error {
			if _, getErr := s.channels.Get(transactionContext, channelID); getErr != nil {
				return getErr
			}
			begun, beginErr := s.threads.BeginCanceling(transactionContext, thread.ID, cancelingAt)
			if beginErr != nil {
				return beginErr
			}
			if !begun {
				return status_error.ChannelTaskStateConflict
			}
			_, eventErr := s.appendEvent(
				transactionContext,
				thread.ChannelID,
				thread.ID,
				constants.ChannelEventThreadWorkStopping,
				map[string]interface{}{"cancelingAt": cancelingAt},
			)
			return eventErr
		})
		if err != nil {
			return err
		}
		thread.CancelingAt = &cancelingAt
	}
	return s.finishStopWork(ctx, thread)
}

// finishStopWork 继续一个已经持久化的停止意图；失败时保留意图供重试或重启恢复。
func (s *service) finishStopWork(ctx context.Context, thread models.ChannelThread) error {
	if thread.CancelingAt == nil {
		return status_error.ChannelTaskStateConflict
	}
	members, err := s.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		return err
	}
	chatIDs := make([]string, 0, len(members)+1)
	seen := make(map[string]struct{}, len(members)+1)
	for _, member := range members {
		if member.ChatID == "" || member.ChatID == thread.LeadChatID {
			continue
		}
		if _, exists := seen[member.ChatID]; exists {
			continue
		}
		seen[member.ChatID] = struct{}{}
		chatIDs = append(chatIDs, member.ChatID)
	}
	if thread.LeadChatID != "" {
		if _, exists := seen[thread.LeadChatID]; !exists {
			chatIDs = append(chatIDs, thread.LeadChatID)
		}
	}
	var firstCancelError error
	for _, chatID := range chatIDs {
		if cancelErr := s.executions.CancelAndWait(ctx, chatID); cancelErr != nil {
			logx.Errorf(
				"cancel channel execution failed: operation=stop_thread_work thread_id=%s chat_id=%s canceling_at=%d error=%v",
				thread.ID,
				chatID,
				*thread.CancelingAt,
				cancelErr,
			)
			if firstCancelError == nil {
				firstCancelError = cancelErr
			}
		}
	}
	if firstCancelError != nil {
		return firstCancelError
	}
	stoppedAt := time.Now().UnixMilli()
	return s.transaction(ctx, func(transactionContext context.Context) error {
		current, getErr := s.threads.Get(transactionContext, thread.ID)
		if getErr != nil {
			return getErr
		}
		if current.CancelingAt == nil || *current.CancelingAt != *thread.CancelingAt {
			return status_error.ChannelTaskStateConflict
		}
		if _, getErr := s.channels.Get(transactionContext, current.ChannelID); getErr != nil {
			return getErr
		}
		if cancelErr := s.tasks.FinalizeThreadCancellation(transactionContext, current.ID, stoppedAt); cancelErr != nil {
			return cancelErr
		}
		if consumeErr := s.tasks.ConsumeOpenNotifications(transactionContext, current.ID, stoppedAt); consumeErr != nil {
			return consumeErr
		}
		if cancelErr := s.threads.CancelOpenLeadInputs(transactionContext, current.ID, stoppedAt); cancelErr != nil {
			return cancelErr
		}
		if _, eventErr := s.appendEvent(
			transactionContext,
			current.ChannelID,
			current.ID,
			constants.ChannelEventTeamLeaderProcessing,
			map[string]interface{}{"active": false, "agentId": current.LeadWakerID},
		); eventErr != nil {
			return eventErr
		}
		if _, eventErr := s.appendEvent(
			transactionContext,
			current.ChannelID,
			current.ID,
			constants.ChannelEventThreadWorkStopped,
			map[string]interface{}{
				"cancelingAt": *thread.CancelingAt,
				"stoppedAt":   stoppedAt,
			},
		); eventErr != nil {
			return eventErr
		}
		finished, finishErr := s.threads.FinishCanceling(
			transactionContext,
			current.ID,
			*thread.CancelingAt,
			stoppedAt,
		)
		if finishErr != nil {
			return finishErr
		}
		if !finished {
			return status_error.ChannelTaskStateConflict
		}
		return nil
	})
}

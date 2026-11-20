package channel

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
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
		if err := s.checkStoppedThreadSubmission(thread, input.SubmissionID); err != nil {
			return err
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
		return capability.Commit(transactionContext, map[string]any{"taskId": task.ID, "followUpId": followUp.ID, "accepted": true})
	})
	if err != nil {
		return err
	}
	return nil
}

// retryTask 以当前负责人回合的来源重试失败任务，可同时更换执行伙伴。
func (s *service) retryTask(
	ctx context.Context,
	taskID string,
	assigneeWakerID string,
	replyLinkID string,
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
	inputContext := ctx
	locked, releaseTeam, lockErr := s.acquireThreadOperation(ctx, task.ThreadID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	release := sync.OnceFunc(releaseTeam)
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
			"assignee_waker_id":  assigneeWakerID,
			"delegation_sent_at": now,
			"im_reply_link_id":   replyLinkID,
			"status":             constants.ChannelTaskStatusPending,
			"error_message":      "",
			"finished_at":        nil,
			"updated_at":         now,
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
		return capability.Commit(transactionContext, map[string]any{"taskId": task.ID, "retryQueued": true})
	})
	if err != nil {
		return err
	}
	release()
	_, err = s.schedule(inputContext, thread.ID)
	return err
}

// StopTask 持久请求停止单个任务对应的当前尝试。
func (s *service) StopTask(ctx context.Context, taskID string) error {
	return s.stopTask(ctx, strings.TrimSpace(taskID), false)
}

func (s *service) stopTask(ctx context.Context, taskID string, fromSubmission bool) error {
	var attempt models.ChannelTaskAttempt
	var scheduleThreadID string
	now := time.Now().UnixMilli()
	if err := s.transaction(ctx, func(ctx context.Context) error {
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return err
		}
		thread, err := s.threads.Get(ctx, task.ThreadID)
		if err != nil {
			return err
		}
		if thread.CancelingAt != nil {
			if fromSubmission {
				return nil
			}
			return status_error.ChannelTaskStateConflict
		}
		if _, err := s.channels.Get(ctx, thread.ChannelID); err != nil {
			return err
		}
		switch task.Status {
		case constants.ChannelTaskStatusPending, constants.ChannelTaskStatusInReview:
			if err := s.tasks.UpdateFields(ctx, task.ID, map[string]interface{}{
				"status":      constants.ChannelTaskStatusCancelled,
				"finished_at": now,
				"updated_at":  now,
			}); err != nil {
				return err
			}
			if thread.ReadyAt != nil {
				scheduleThreadID = thread.ID
				if _, err := s.appendEvent(ctx, thread.ChannelID, thread.ID, constants.ChannelEventTaskCancelled, map[string]string{"taskId": task.ID}); err != nil {
					return err
				}
			}
		case constants.ChannelTaskStatusInProgress:
			attempt, err = s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
			if err != nil {
				return err
			}
			if !taskCancellationRequested(task, attempt.Fence) {
				if err := s.tasks.UpdateFields(ctx, task.ID, map[string]interface{}{"cancel_requested_at": now, "cancel_requested_fence": attempt.Fence}); err != nil {
					return err
				}
			}
		default:
			if fromSubmission {
				return nil
			}
			return status_error.ChannelTaskStateConflict
		}
		return capability.Commit(ctx, map[string]any{"taskId": task.ID, "cancellationRequested": true})
	}); err != nil {
		return err
	}
	if scheduleThreadID != "" {
		_, err := s.schedule(ctx, scheduleThreadID)
		return err
	}
	if attempt.ID == "" {
		return nil
	}
	if !capability.Active(ctx) {
		return s.executions.CancelExecutionAndWait(ctx, attempt.ChatID, attempt.ExecutionID)
	}
	s.watchTaskCancellation(attempt)
	return nil
}

func (s *service) StopWorkForSubmission(ctx context.Context, channelID, threadID, submissionID string) error {
	identity, err := requestidempotency.New(submissionID, nil)
	if err != nil {
		return err
	}
	thread, err := s.threadForStop(ctx, channelID, threadID)
	if err != nil {
		return err
	}
	cancelErr := s.executions.CancelSubmissionAndWait(ctx, thread.LeadChatID, identity.ID)
	// 等待已经进入持久化边界的同团队输入，之后同标识不能再入队。
	locked, release, err := teamwork.Acquire(ctx, thread.ChannelID)
	if err != nil {
		return err
	}
	leadInputID := LeadInputID(thread.ID, identity.ID)
	_, queued, err := s.threads.GetLeadInput(locked, leadInputID)
	if err == nil {
		if queued {
			err = s.threads.CancelLeadInput(locked, leadInputID, time.Now().UnixMilli())
		} else {
			err = s.tasks.DeletePendingFollowUp(locked, thread.ID, identity.ID)
		}
	}
	release()
	if err != nil {
		return err
	}
	if !queued {
		if err := s.stopMentionTask(ctx, thread, identity.ID); err != nil {
			return err
		}
	}
	return cancelErr
}

func (s *service) StopFirstWork(ctx context.Context, channelID, submissionID string) error {
	channelID = strings.TrimSpace(channelID)
	identity, err := requestidempotency.New(submissionID, nil)
	if err != nil || channelID == "" {
		return status_error.BadRequest
	}
	leadChatID := initialLeadChatID(channelID, identity)
	cancelErr := s.executions.CancelSubmissionAndWait(ctx, leadChatID, identity.ID)
	locked, release, err := teamwork.Acquire(ctx, channelID)
	if err != nil {
		return err
	}
	thread, exists, err := s.threads.GetByClientRequestID(locked, channelID, identity.ID)
	release()
	if err != nil {
		return err
	}
	if exists {
		if err := s.stopMentionTask(ctx, thread, identity.ID); err != nil {
			return err
		}
	}
	return cancelErr
}

// stopMentionTask 只收敛由原提交直接创建的任务。
func (s *service) stopMentionTask(ctx context.Context, thread models.ChannelThread, submissionID string) error {
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.DirectInput != nil && task.SenderWakerID == "" && task.DirectInput.SubmissionID == submissionID {
			return s.stopTask(ctx, task.ID, true)
		}
	}
	return nil
}

func (s *service) checkStoppedThreadSubmission(thread models.ChannelThread, submissionID string) error {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID != "" && s.executions.IsSubmissionStopped(thread.LeadChatID, submissionID) {
		return status_error.OperationCanceled
	}
	return nil
}

func (s *service) threadForStop(ctx context.Context, channelID, threadID string) (models.ChannelThread, error) {
	channelID = strings.TrimSpace(channelID)
	threadID = strings.TrimSpace(threadID)
	if channelID == "" || threadID == "" {
		return models.ChannelThread{}, status_error.BadRequest
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return models.ChannelThread{}, err
	}
	if thread.ChannelID != channelID || thread.ReadyAt == nil {
		return models.ChannelThread{}, status_error.ChannelThreadNotFound.WithParams(
			map[string]interface{}{"ThreadID": threadID},
		)
	}
	_, err = s.channels.Get(ctx, channelID)
	return thread, err
}

// StopWork 登记整个对话的停止意图，再收敛原生执行、工作流与排队协作。
func (s *service) StopWork(ctx context.Context, channelID, threadID string) error {
	thread, err := s.threadForStop(ctx, channelID, threadID)
	if err != nil {
		return err
	}
	if thread.CancelingAt == nil {
		cancelingAt := time.Now().UnixMilli()
		var begun bool
		err = s.transaction(ctx, func(transactionContext context.Context) error {
			if _, getErr := s.channels.Get(transactionContext, thread.ChannelID); getErr != nil {
				return getErr
			}
			var beginErr error
			begun, beginErr = s.threads.BeginCanceling(transactionContext, thread.ID, thread.StopGeneration, cancelingAt)
			if beginErr != nil {
				return beginErr
			}
			if !begun {
				return nil
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
		if !begun {
			return nil
		}
		thread.StopGeneration++
		thread.CancelingAt = &cancelingAt
	}
	if err := s.finishStopWork(ctx, thread); err != nil {
		s.watchRecoveryDrain(thread.ID)
		return err
	}
	return nil
}

// finishStopWork 继续一个已经持久化的停止意图；失败时保留意图供重试或重启恢复。
func (s *service) finishStopWork(ctx context.Context, thread models.ChannelThread) error {
	if thread.CancelingAt == nil {
		return status_error.ChannelTaskStateConflict
	}
	// 停止收敛只串行化同一对话的请求与恢复。
	_, releaseStop, err := teamwork.Acquire(ctx, "thread-stop:"+thread.ID)
	if err != nil {
		return err
	}
	defer releaseStop()
	// 等待已有团队接纳结束。持久停止意图继续阻止本对话的新接纳。
	locked, releaseAdmission, err := acquireThreadAdmission(ctx, thread.ID)
	if err != nil {
		return err
	}
	// 重复停止可能在等待期间已经完成，重新核对代次后才能取消当前回合。
	current, err := s.threads.Get(locked, thread.ID)
	if err != nil {
		releaseAdmission()
		return err
	}
	if current.CancelingAt == nil || current.StopGeneration != thread.StopGeneration {
		releaseAdmission()
		return nil
	}
	members, err := s.threads.ListMembers(locked, thread.ID)
	if err != nil {
		releaseAdmission()
		return err
	}
	chatIDs := make([]string, 0, len(members)+1)
	for _, member := range members {
		if member.ChatID != "" && member.ChatID != thread.LeadChatID {
			chatIDs = append(chatIDs, member.ChatID)
		}
	}
	chatIDs = append(chatIDs, thread.LeadChatID)
	awaitNative := s.executions.StopNativeExecutions(chatIDs)
	releaseAdmission()
	var workflowStopErr error
	if s.stopChatWorkflows != nil {
		workflowStopErr = s.stopChatWorkflows(ctx, thread.ID, chatIDs, *thread.CancelingAt)
	}
	if err := awaitNative(ctx); err != nil {
		logx.Errorf("wait channel execution stop failed: thread_id=%s canceling_at=%d error=%v", thread.ID, *thread.CancelingAt, err)
		return err
	}
	if workflowStopErr != nil {
		return workflowStopErr
	}
	stoppedAt := time.Now().UnixMilli()
	return s.transaction(ctx, func(transactionContext context.Context) error {
		current, getErr := s.threads.Get(transactionContext, thread.ID)
		if getErr != nil {
			return getErr
		}
		if current.CancelingAt == nil || current.StopGeneration != thread.StopGeneration {
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
			thread.StopGeneration,
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

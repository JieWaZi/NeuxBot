package channel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	messageview "github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func (s *service) watchAttempt(
	attempt models.ChannelTaskAttempt,
	threadID string,
	capabilityToken string,
) {
	if attempt.ID == "" || attempt.ExecutionID == "" {
		return
	}
	watcherContext, registered := s.registerWatcher(attempt.ID)
	if !registered {
		return
	}
	go func() {
		defer s.unregisterWatcher(attempt.ID)
		defer s.capabilities.Revoke(capabilityToken)
		policy := s.effectiveWatcherPolicy()
		retryDelay := policy.initialDelay
		consecutiveFailures := 0
		for {
			subscription, err := s.chats.SubscribeExecutionEvents(watcherContext, attempt.ChatID, attempt.ExecutionID)
			if err != nil {
				logAttemptWatcherError("subscribe_chat_events", attempt, threadID, err)
				consecutiveFailures++
				if consecutiveFailures >= policy.maximumFailures {
					if exhaustErr := s.exhaustAttemptWatcher(watcherContext, attempt, threadID); exhaustErr == nil {
						return
					} else {
						logAttemptWatcherError("exhaust_watcher", attempt, threadID, exhaustErr)
						consecutiveFailures = 0
						retryDelay = policy.maximumDelay
					}
				}
				if !waitAttemptWatcherRetry(watcherContext, retryDelay) {
					return
				}
				retryDelay = nextAttemptWatcherRetryDelay(retryDelay, policy.maximumDelay)
				continue
			}
			resubscribe := false
			for _, event := range subscription.Backlog {
				handled, terminalErr := s.acceptAttemptTerminal(attempt, threadID, event)
				if terminalErr != nil {
					logAttemptWatcherError("commit_attempt_terminal", attempt, threadID, terminalErr)
					consecutiveFailures++
					resubscribe = true
					break
				}
				if handled {
					subscription.Close()
					return
				}
			}
			for !resubscribe {
				select {
				case <-watcherContext.Done():
					subscription.Close()
					return
				case event, open := <-subscription.Events:
					if !open {
						logx.Errorf(
							"channel attempt event subscription closed before terminal: operation=watch_attempt task_id=%s attempt_id=%s thread_id=%s execution_id=%s chat_id=%s",
							attempt.TaskID,
							attempt.ID,
							threadID,
							attempt.ExecutionID,
							attempt.ChatID,
						)
						consecutiveFailures++
						resubscribe = true
						continue
					}
					handled, terminalErr := s.acceptAttemptTerminal(attempt, threadID, event)
					if terminalErr != nil {
						logAttemptWatcherError("commit_attempt_terminal", attempt, threadID, terminalErr)
						consecutiveFailures++
						resubscribe = true
						continue
					}
					if handled {
						subscription.Close()
						return
					}
				}
			}
			subscription.Close()
			if consecutiveFailures >= policy.maximumFailures {
				if exhaustErr := s.exhaustAttemptWatcher(watcherContext, attempt, threadID); exhaustErr == nil {
					return
				} else {
					logAttemptWatcherError("exhaust_watcher", attempt, threadID, exhaustErr)
					consecutiveFailures = 0
					retryDelay = policy.maximumDelay
				}
			}
			if !waitAttemptWatcherRetry(watcherContext, retryDelay) {
				return
			}
			retryDelay = nextAttemptWatcherRetryDelay(retryDelay, policy.maximumDelay)
		}
	}()
}

// exhaustAttemptWatcher 取消无法可靠监听的旧执行并把当前围栏收敛为可见失败。
func (s *service) exhaustAttemptWatcher(
	ctx context.Context,
	attempt models.ChannelTaskAttempt,
	threadID string,
) error {
	if err := s.executions.CancelAndWait(ctx, attempt.ChatID); err != nil {
		return err
	}
	task, err := s.tasks.Get(ctx, attempt.TaskID)
	if err != nil {
		return err
	}
	if task.ThreadID != threadID || task.CurrentFence != attempt.Fence ||
		task.Status != constants.ChannelTaskStatusInProgress {
		return nil
	}
	if err := s.failAttempt(
		ctx,
		task,
		attempt,
		"channel execution watcher retry exhausted",
	); err != nil {
		return err
	}
	if err := s.ReconcileThread(ctx, threadID); err != nil {
		logAttemptWatcherError("reconcile_after_exhaustion", attempt, threadID, err)
	}
	return nil
}

func (s *service) acceptAttemptTerminal(
	attempt models.ChannelTaskAttempt,
	threadID string,
	event chat.PublicEvent,
) (bool, error) {
	if event.ExecutionID != attempt.ExecutionID || !executionTerminal(event.ExecutionStatus) {
		return false, nil
	}
	messages, err := s.chats.ReadExecutionMessages(s.lifecycleContext, attempt.ChatID, attempt.ExecutionID)
	if err != nil {
		return false, err
	}
	message := messageview.Message{ID: attempt.ExecutionID, ExecutionID: attempt.ExecutionID, Role: constants.MessageRoleAssistant, Status: event.ExecutionStatus, ErrorCode: event.ErrorCode, ErrorMessage: event.ErrorMessage}
	for _, candidate := range messages {
		if candidate.ExecutionID == attempt.ExecutionID && candidate.Role == constants.MessageRoleAssistant {
			message = candidate
		}
	}
	message.Status = event.ExecutionStatus
	var terminalErr error
	for retry := 1; retry <= constants.ChannelAttemptWatcherMaximumImmediateRetries; retry++ {
		terminalErr = s.handleAttemptTerminal(attempt, message)
		if terminalErr == nil {
			return true, nil
		}
		if retry < constants.ChannelAttemptWatcherMaximumImmediateRetries &&
			!waitAttemptWatcherRetry(
				s.lifecycleContext,
				time.Duration(constants.ChannelAttemptWatcherInitialRetryDelayMilliseconds)*time.Millisecond,
			) {
			return false, s.lifecycleContext.Err()
		}
	}
	return false, terminalErr
}

func (s *service) handleAttemptTerminal(
	attempt models.ChannelTaskAttempt,
	message messageview.Message,
) error {
	ctx := s.lifecycleContext
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := s.tasks.Get(ctx, attempt.TaskID)
	if err != nil {
		return err
	}
	if task.CurrentFence != attempt.Fence || task.Status != constants.ChannelTaskStatusInProgress {
		return nil
	}
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	followUps, err := s.tasks.ListPendingFollowUps(ctx, task.ID)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	result := finalResultContent(message)
	currentAttempt, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
	if err != nil {
		return err
	}
	if submittedReport := strings.TrimSpace(currentAttempt.Result); submittedReport != "" {
		result = submittedReport
	}
	changedPaths := make([]string, 0, len(message.FileChanges))
	for _, change := range message.FileChanges {
		if path := strings.TrimSpace(change.Path); path != "" {
			changedPaths = append(changedPaths, path)
		}
	}
	taskStatus := constants.ChannelTaskStatusCompleted
	if message.Status == constants.ExecutionStatusFailed {
		taskStatus = constants.ChannelTaskStatusFailed
	} else if message.Status == constants.ExecutionStatusCanceled {
		taskStatus = constants.ChannelTaskStatusCancelled
	} else if task.RequiresHumanReview {
		taskStatus = constants.ChannelTaskStatusInReview
	}
	if len(followUps) > 0 {
		taskStatus = constants.ChannelTaskStatusPending
	}
	reportContent, _ := teammateReportContent(taskStatus, result, message.ErrorMessage, changedPaths)
	payload, marshalErr := json.Marshal(map[string]interface{}{
		"taskId":       task.ID,
		"attemptId":    attempt.ID,
		"status":       taskStatus,
		"result":       result,
		"errorCode":    message.ErrorCode,
		"errorMessage": message.ErrorMessage,
		"changedPaths": changedPaths,
	})
	if marshalErr != nil {
		return status_error.InternalServerError
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		current, err := s.tasks.Get(transactionContext, task.ID)
		if err != nil {
			return err
		}
		if current.CurrentFence != attempt.Fence || current.Status != constants.ChannelTaskStatusInProgress {
			return nil
		}
		if err := s.tasks.UpdateAttemptFields(transactionContext, attempt.ID, map[string]interface{}{
			"status":        message.Status,
			"result":        result,
			"error_message": message.ErrorMessage,
			"finished_at":   now,
			"updated_at":    now,
		}); err != nil {
			return err
		}
		if err := s.tasks.UpdateFields(transactionContext, task.ID, map[string]interface{}{
			"status":        taskStatus,
			"result":        result,
			"error_message": message.ErrorMessage,
			"changed_paths": changedPaths,
			"finished_at":   now,
			"updated_at":    now,
		}); err != nil {
			return err
		}
		// 负责人任务已在同一个负责人 Chat 中产生结果，不再向自己重复投递报告。
		if task.AssigneeWakerID != thread.LeadWakerID {
			if err := s.tasks.CreateNotification(transactionContext, models.ChannelLeadNotification{
				ID:        attempt.ID,
				ThreadID:  thread.ID,
				TaskID:    task.ID,
				AttemptID: attempt.ID,
				Status:    constants.ChannelNotificationStatusPending,
				Payload:   reportContent,
				CreatedAt: now,
				UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		_, err = s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskFinished, json.RawMessage(payload))
		return err
	})
	if err != nil {
		return err
	}
	if projectionErr := s.projectPendingMemberReports(ctx, thread.ID); projectionErr != nil {
		logx.Errorf("project member report before channel scheduling failed: task_id=%s attempt_id=%s thread_id=%s execution_id=%s error=%v", task.ID, attempt.ID, thread.ID, attempt.ExecutionID, projectionErr)
		s.watchRecoveryDrain(thread.ID)
		return nil
	}
	if _, scheduleErr := s.schedule(ctx, thread.ID); scheduleErr != nil {
		logx.Errorf("schedule after channel task terminal failed: task_id=%s attempt_id=%s thread_id=%s execution_id=%s error=%v", task.ID, attempt.ID, thread.ID, attempt.ExecutionID, scheduleErr)
	}
	if _, drainErr := s.drainLeadInputs(ctx, thread.ID); drainErr == nil {
		if notificationErr := s.drainNotifications(ctx, thread.ID); notificationErr != nil {
			logx.Errorf("drain notifications after task terminal failed: task_id=%s attempt_id=%s thread_id=%s execution_id=%s error=%v", task.ID, attempt.ID, thread.ID, attempt.ExecutionID, notificationErr)
		}
	} else {
		logx.Errorf("drain lead inputs after task terminal failed: task_id=%s attempt_id=%s thread_id=%s execution_id=%s error=%v", task.ID, attempt.ID, thread.ID, attempt.ExecutionID, drainErr)
	}
	return nil
}

// logAttemptWatcherError 记录任务尝试监听失败的可检索技术上下文。
func logAttemptWatcherError(
	operation string,
	attempt models.ChannelTaskAttempt,
	threadID string,
	err error,
) {
	logx.Errorf(
		"channel attempt watcher failed: operation=%s task_id=%s attempt_id=%s thread_id=%s execution_id=%s chat_id=%s error=%v",
		operation,
		attempt.TaskID,
		attempt.ID,
		threadID,
		attempt.ExecutionID,
		attempt.ChatID,
		err,
	)
}

// waitAttemptWatcherRetry 在可取消等待后允许监听器恢复订阅或终态提交。
func waitAttemptWatcherRetry(ctx context.Context, delay time.Duration) bool {
	if ctx == nil {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// nextAttemptWatcherRetryDelay 按有上界的指数退避计算下次监听恢复间隔。
func nextAttemptWatcherRetryDelay(current time.Duration, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}

func executionTerminal(status constants.ExecutionStatus) bool {
	return status == constants.ExecutionStatusCompleted ||
		status == constants.ExecutionStatusFailed ||
		status == constants.ExecutionStatusCanceled
}

// recoverReservedAttempt 精确确认预留 Execution 是否已进入 Chat 权威事实，再提交或释放 reservation。
func (s *service) recoverReservedAttempt(
	ctx context.Context,
	thread models.ChannelThread,
	task models.ChannelTask,
	attempt models.ChannelTaskAttempt,
) (bool, error) {
	recorded, err := s.attemptExecutionRecorded(ctx, attempt)
	if err != nil {
		return false, err
	}
	if !recorded {
		return false, s.releaseUnstartedAttempt(ctx, attempt)
	}
	startedAt := time.Now().UnixMilli()
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		bound, bindErr := s.tasks.BindAttemptExecution(
			transactionContext,
			attempt.ID,
			attempt.ExecutionID,
			startedAt,
		)
		if bindErr != nil {
			return bindErr
		}
		if !bound {
			return status_error.ChannelTaskAttemptStale
		}
		_, eventErr := s.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskStarted,
			map[string]interface{}{
				"taskId":      task.ID,
				"attemptId":   attempt.ID,
				"executionId": attempt.ExecutionID,
				"fence":       attempt.Fence,
				"reason":      attempt.Reason,
				"recovered":   true,
			},
		)
		return eventErr
	})
	if err != nil {
		return false, err
	}
	attempt.Status = constants.ExecutionStatusRunning
	attempt.StartedAt = &startedAt
	s.watchAttempt(attempt, task.ThreadID, "")
	return true, nil
}

// attemptExecutionRecorded 判断 reservation 的精确 Execution 是否已被 Chat 接纳或完成。
func (s *service) attemptExecutionRecorded(
	ctx context.Context,
	attempt models.ChannelTaskAttempt,
) (bool, error) {
	if attempt.ExecutionID == "" {
		return false, nil
	}
	_, err := s.chats.ReadExecution(ctx, attempt.ChatID, attempt.ExecutionID)
	if hasStatusErrorCode(err, status_error.ExecutionNotFound.Code()) {
		return false, nil
	}
	return err == nil, err
}

// failMissingAttempt 为旧数据或损坏状态补建非空 attempt，再形成可追踪失败事实。
func (s *service) failMissingAttempt(
	ctx context.Context,
	task models.ChannelTask,
) error {
	member, err := s.threads.GetMember(ctx, task.ThreadID, task.AssigneeWakerID)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	attempt := models.ChannelTaskAttempt{
		ID:              util.New(),
		TaskID:          task.ID,
		ChatID:          member.ChatID,
		AssigneeWakerID: task.AssigneeWakerID,
		Fence:           task.CurrentFence,
		Reason:          constants.ChannelAttemptReasonRetry,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.tasks.CreateAttempt(ctx, attempt); err != nil {
		return err
	}
	return s.failInterruptedAttempt(ctx, task, attempt)
}

func (s *service) failInterruptedAttempt(
	ctx context.Context,
	task models.ChannelTask,
	attempt models.ChannelTaskAttempt,
) error {
	return s.failAttempt(ctx, task, attempt, "channel execution was interrupted during recovery")
}

// failAttempt 按当前围栏把无法继续的尝试、任务、负责人通知和诊断事件原子提交。
func (s *service) failAttempt(
	ctx context.Context,
	task models.ChannelTask,
	attempt models.ChannelTaskAttempt,
	errorMessage string,
) error {
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	payload, marshalErr := json.Marshal(map[string]interface{}{
		"taskId":       task.ID,
		"attemptId":    attempt.ID,
		"status":       constants.ChannelTaskStatusFailed,
		"result":       "",
		"errorMessage": errorMessage,
	})
	if marshalErr != nil {
		logx.Errorf(
			"marshal interrupted channel task payload failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s error=%v",
			thread.ID,
			task.ID,
			attempt.ID,
			attempt.ExecutionID,
			marshalErr,
		)
		return status_error.InternalServerError
	}
	return s.transaction(ctx, func(transactionContext context.Context) error {
		transitioned, err := s.tasks.FailRunningAttempt(
			transactionContext,
			task.ID,
			attempt.ID,
			attempt.Fence,
			errorMessage,
			now,
		)
		if err != nil || !transitioned {
			return err
		}
		if task.AssigneeWakerID != thread.LeadWakerID {
			reportContent, _ := teammateReportContent(
				constants.ChannelTaskStatusFailed,
				"",
				errorMessage,
				nil,
			)
			if err := s.tasks.CreateNotification(transactionContext, models.ChannelLeadNotification{
				ID:        util.New(),
				ThreadID:  thread.ID,
				TaskID:    task.ID,
				AttemptID: attempt.ID,
				Status:    constants.ChannelNotificationStatusPending,
				Payload:   reportContent,
				CreatedAt: now,
				UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		_, err = s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskRecoveryFailed, json.RawMessage(payload))
		return err
	})
}

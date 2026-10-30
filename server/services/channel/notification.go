package channel

import (
	"context"
	"hash/fnv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// leaderProcessingEvent 是负责人结果批次开始或结束时写入持久团队事件的载荷。
type leaderProcessingEvent struct {
	// Active 表示负责人是否正在接收或处理成员结果批次。
	Active bool `json:"active"`
	// AgentID 是当前团队对话冻结的负责人伙伴标识。
	AgentID string `json:"agentId"`
	// Tasks 是本次结果批次包含的通知数量。
	Tasks int `json:"tasks"`
	// ExecutionID 是已经接纳结果批次的负责人 Chat Execution 标识。
	ExecutionID string `json:"executionId,omitempty"`
	// RetryExhausted 表示本次负责人投递因监听连续失败而进入持久补偿。
	RetryExhausted bool `json:"retryExhausted,omitempty"`
	// Reason 是不包含底层错误细节的稳定补偿原因。
	Reason string `json:"reason,omitempty"`
}

func (s *service) drainNotifications(ctx context.Context, threadID string) error {
	locked, release, lockErr := s.acquireThreadOperation(ctx, threadID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	defer release()
	shard := fnv.New32a()
	_, _ = shard.Write([]byte(threadID))
	lock := &s.notificationDrainLocks[shard.Sum32()%uint32(len(s.notificationDrainLocks))]
	lock.Lock()
	defer lock.Unlock()
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return err
	}
	summary, err := s.chats.GetChatSummary(ctx, thread.LeadChatID)
	if err != nil {
		return err
	}
	if summary.Chat.ActiveExecutionID != "" {
		return nil
	}
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return err
	}
	entries, ids, err := s.pendingNotificationEntries(ctx, thread, tasks)
	if err != nil || len(entries) == 0 {
		return err
	}
	projected, err := s.appendNotificationReports(ctx, thread, entries)
	if err != nil {
		return err
	}
	if !projected {
		return nil
	}
	// 真实成员报告立即进入负责人 Chat；只有唤醒负责人统一协调/汇总才等待任务板稳定。
	if teamTasksStillRunning(tasks) {
		return nil
	}
	direct, err := s.directDeliveryReady(ctx, entries, tasks)
	if err != nil {
		return err
	}
	if direct {
		var receipt *models.WorkResultReceipt
		if s.prepareDirectResult != nil && s.results != nil {
			receipt, err = s.prepareDirectResult(ctx, thread.LeadChatID, entries[0].NotificationID)
			if err != nil {
				return err
			}
			if receipt != nil {
				receipt.OwnerKind = constants.WorkResultOwnerKindTeamThread
				receipt.OwnerID = thread.ID
			}
		}
		return s.transaction(ctx, func(transactionContext context.Context) error {
			consumed, consumeErr := s.tasks.ConsumePendingNotification(
				transactionContext, entries[0].NotificationID, time.Now().UnixMilli(),
			)
			if consumeErr != nil || !consumed {
				return consumeErr
			}
			deliveryEvent, eventErr := s.appendEvent(
				transactionContext, thread.ChannelID, thread.ID,
				constants.ChannelEventMemberDirectDelivered,
				map[string]string{"notificationId": entries[0].NotificationID},
			)
			if eventErr != nil {
				return eventErr
			}
			if receipt != nil {
				receipt.AvailableAt = deliveryEvent.CreatedAt
				return s.results.Publish(transactionContext, *receipt)
			}
			return nil
		})
	}
	lead, err := s.threads.GetMember(ctx, thread.ID, thread.LeadWakerID)
	if err != nil {
		return err
	}
	automaticRunID, err := s.automaticWorkSource(ctx, thread)
	if err != nil {
		return err
	}
	prompt := teamNotificationPrompt(
		entries,
		buildTaskBoardSnapshot(tasks),
		tasks,
	)
	leadExecutionID := util.New()
	capability, capabilityToken, err := s.mintLeadCapability(ctx, thread, leadExecutionID, false)
	if err != nil {
		return err
	}
	teamInstruction, err := s.teamInstruction(ctx, thread, thread.LeadWakerID)
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		return err
	}
	// 协调任务终态与负责人汇总：在最后一个 task terminal 与负责人 announce execution
	// started 之间先发布 processing，使 UI 不会把极短的接力窗口误判为空闲。
	if _, err := s.appendEvent(
		ctx,
		thread.ChannelID,
		thread.ID,
		constants.ChannelEventTeamLeaderProcessing,
		leaderProcessingEvent{
			Active:  true,
			AgentID: thread.LeadWakerID,
			Tasks:   len(entries),
		},
	); err != nil {
		s.capabilities.Revoke(capabilityToken)
		return err
	}
	options := leaderWorkOptions(
		lead.RuntimeID,
		lead.Model,
		lead.Reasoning,
		lead.PermissionLevel,
		capability,
		teamInstruction,
		false,
	)
	options.AccountID = lead.AccountID
	for _, entry := range entries {
		options.HistoryMessageIDsToOmit = append(options.HistoryMessageIDsToOmit, entry.NotificationID)
	}
	accepted, err := s.executions.Start(ctx, execution.WorkIntent{AutomaticRunID: automaticRunID,
		ExecutionID: leadExecutionID,
		Chat:        execution.ChatTarget{ChatID: thread.LeadChatID},
		Input: execution.WorkInput{
			Actor:   string(constants.ChannelMessageActorSystem),
			Content: prompt,
		},
		Options:   options,
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		// Start 未接纳时必须用配对事件结束 bridge；持久通知仍留在队列中，
		// 因而详情快照可以继续表达“有结果待处理”，而 SSE 不会卡在一次已失败的启动尝试上。
		if _, eventErr := s.appendEvent(
			ctx,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTeamLeaderProcessing,
			leaderProcessingEvent{
				Active:  false,
				AgentID: thread.LeadWakerID,
				Tasks:   len(entries),
			},
		); eventErr != nil {
			logx.Errorf(
				"close failed leader processing bridge failed: thread_id=%s chat_id=%s notification_count=%d error=%v",
				thread.ID,
				thread.LeadChatID,
				len(entries),
				eventErr,
			)
		}
		s.watchRecoveryDrain(thread.ID)
		if isChatBusy(err) {
			return nil
		}
		return err
	}
	now := time.Now().UnixMilli()
	if err := s.tasks.UpdateNotifications(
		ctx,
		ids,
		constants.ChannelNotificationStatusDelivering,
		accepted.Execution.ID,
		now,
	); err != nil {
		if cancelErr := s.executions.Cancel(thread.LeadChatID); cancelErr != nil {
			logx.Errorf(
				"cancel uncommitted leader notification execution failed: thread_id=%s execution_id=%s chat_id=%s notification_count=%d error=%v",
				thread.ID,
				accepted.Execution.ID,
				thread.LeadChatID,
				len(ids),
				cancelErr,
			)
		}
		if _, eventErr := s.appendEvent(
			ctx,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTeamLeaderProcessing,
			leaderProcessingEvent{
				Active:      false,
				AgentID:     thread.LeadWakerID,
				Tasks:       len(entries),
				ExecutionID: accepted.Execution.ID,
			},
		); eventErr != nil {
			logx.Errorf(
				"close rejected leader processing bridge failed: thread_id=%s execution_id=%s chat_id=%s notification_count=%d error=%v",
				thread.ID,
				accepted.Execution.ID,
				thread.LeadChatID,
				len(ids),
				eventErr,
			)
		}
		s.watchRecoveryDrain(thread.ID)
		return err
	}
	if _, eventErr := s.appendEvent(
		ctx,
		thread.ChannelID,
		thread.ID,
		constants.ChannelEventTeamLeaderProcessing,
		leaderProcessingEvent{
			Active:      true,
			AgentID:     thread.LeadWakerID,
			Tasks:       len(entries),
			ExecutionID: accepted.Execution.ID,
		},
	); eventErr != nil {
		logx.Errorf(
			"append accepted leader processing event failed: thread_id=%s execution_id=%s chat_id=%s notification_count=%d error=%v",
			thread.ID,
			accepted.Execution.ID,
			thread.LeadChatID,
			len(ids),
			eventErr,
		)
	}
	s.watchLeadExecution(thread, accepted.Execution.ID, ids, nil, capabilityToken)
	return nil
}

// pendingNotificationEntries 读取等待交付的成员结果，并补齐稳定的作者与内容投影。
func (s *service) pendingNotificationEntries(
	ctx context.Context,
	thread models.ChannelThread,
	tasks []models.ChannelTask,
) ([]announceEntry, []string, error) {
	notifications, err := s.tasks.ListPendingNotifications(
		ctx,
		thread.ID,
		constants.ChannelNotificationBatchLimit,
	)
	if err != nil || len(notifications) == 0 {
		return nil, nil, err
	}
	tasksByID := make(map[string]models.ChannelTask, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
	}
	membersByID := make(map[string]models.ChannelThreadMember)
	entries := make([]announceEntry, 0, len(notifications))
	ids := make([]string, 0, len(notifications))
	for _, notification := range notifications {
		task, exists := tasksByID[notification.TaskID]
		if !exists {
			continue
		}
		member, exists := membersByID[task.AssigneeWakerID]
		if !exists {
			member, err = s.threads.GetMember(ctx, thread.ID, task.AssigneeWakerID)
			if err != nil {
				return nil, nil, err
			}
			membersByID[task.AssigneeWakerID] = member
		}
		entries = append(entries, announceEntry{
			NotificationID:    notification.ID,
			TaskID:            notification.TaskID,
			AttemptID:         notification.AttemptID,
			MemberWakerID:     member.WakerID,
			MemberChatID:      member.ChatID,
			MemberDisplayName: member.DisplayName,
			Content:           notification.Payload,
		})
		entry := &entries[len(entries)-1]
		entry.Content = strings.TrimSpace(entry.Content)
		if entry.Content == "" {
			entry.Content, entry.Truncated = teammateReportContent(
				task.Status,
				task.Result,
				task.ErrorMessage,
				[]string(task.ChangedPaths),
			)
		} else {
			entry.Content, entry.Truncated = truncateMemberReport(entry.Content)
		}
		ids = append(ids, notification.ID)
	}
	return entries, ids, nil
}

// appendNotificationReports 幂等地把真实成员报告投影到负责人 Chat，不启动负责人执行。
func (s *service) appendNotificationReports(
	ctx context.Context,
	thread models.ChannelThread,
	entries []announceEntry,
) (bool, error) {
	reports := make([]chat.ParticipantMessageInput, 0, len(entries))
	for _, entry := range entries {
		reports = append(reports, chat.ParticipantMessageInput{
			ID:            entry.NotificationID,
			Actor:         string(constants.ChannelMessageActorMemberReport),
			Content:       entry.Content,
			SourceWakerID: entry.MemberWakerID,
			SourceChatID:  entry.MemberChatID,
			Truncated:     entry.Truncated,
		})
	}
	if err := s.chats.AppendParticipantMessages(ctx, thread.LeadChatID, reports); err != nil {
		if isChatBusy(err) {
			s.watchRecoveryDrain(thread.ID)
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// projectPendingMemberReports 在启动后续任务前投影报告，避免报告落在评审输出之后。
func (s *service) projectPendingMemberReports(ctx context.Context, threadID string) error {
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return err
	}
	summary, err := s.chats.GetChatSummary(ctx, thread.LeadChatID)
	if err != nil {
		return err
	}
	if summary.Chat.ActiveExecutionID != "" {
		return nil
	}
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return err
	}
	entries, _, err := s.pendingNotificationEntries(ctx, thread, tasks)
	if err != nil || len(entries) == 0 {
		return err
	}
	_, err = s.appendNotificationReports(ctx, thread, entries)
	return err
}

// directDeliveryReady 只允许负责人和成员双向确认的单项完整成功报告跳过负责人收尾。
func (s *service) directDeliveryReady(
	ctx context.Context,
	entries []announceEntry,
	tasks []models.ChannelTask,
) (bool, error) {
	if len(entries) != 1 || entries[0].Truncated || strings.TrimSpace(entries[0].Content) == "" {
		return false, nil
	}
	entry := entries[0]
	for _, task := range tasks {
		if task.Status == constants.ChannelTaskStatusInReview {
			return false, nil
		}
	}
	for _, task := range tasks {
		if task.ID != entry.TaskID {
			continue
		}
		if !task.DirectDelivery || task.RequiresHumanReview || task.Status != constants.ChannelTaskStatusCompleted {
			return false, nil
		}
		attempt, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
		if err != nil {
			return false, err
		}
		return attempt.ID == entry.AttemptID && attempt.UserReady &&
			strings.TrimSpace(attempt.Result) == entry.Content, nil
	}
	return false, nil
}

func teamTasksStillRunning(tasks []models.ChannelTask) bool {
	for _, task := range tasks {
		if task.Status == constants.ChannelTaskStatusPending ||
			task.Status == constants.ChannelTaskStatusInProgress {
			return true
		}
	}
	return false
}

func (s *service) watchLeadExecution(
	thread models.ChannelThread,
	executionID string,
	notificationIDs []string,
	inputIDs []string,
	capabilityToken string,
) {
	if executionID == "" {
		return
	}
	watcherID := "lead:" + executionID
	watcherContext, registered := s.registerWatcher(watcherID)
	if !registered {
		return
	}
	go func() {
		defer s.unregisterWatcher(watcherID)
		defer s.capabilities.Revoke(capabilityToken)
		policy := s.effectiveWatcherPolicy()
		retryDelay := policy.initialDelay
		consecutiveFailures := 0
		for {
			subscription, err := s.chats.SubscribeExecutionEvents(watcherContext, thread.LeadChatID, executionID)
			if err != nil {
				logx.Errorf("subscribe lead execution events failed: thread_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, executionID, thread.LeadChatID, err)
				consecutiveFailures++
				if consecutiveFailures >= policy.maximumFailures {
					if exhaustErr := s.exhaustLeadWatcher(
						watcherContext,
						thread,
						executionID,
						notificationIDs,
						inputIDs,
					); exhaustErr == nil {
						return
					} else {
						logx.Errorf("compensate exhausted lead execution watcher failed: thread_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, executionID, thread.LeadChatID, exhaustErr)
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
				handled, handleErr := s.completeLeadExecution(
					watcherContext, thread, executionID, notificationIDs, inputIDs, event,
				)
				if handleErr != nil {
					logx.Errorf("commit lead execution terminal failed: thread_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, executionID, thread.LeadChatID, handleErr)
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
						logx.Errorf("lead execution event subscription closed before terminal: thread_id=%s execution_id=%s chat_id=%s", thread.ID, executionID, thread.LeadChatID)
						consecutiveFailures++
						resubscribe = true
						continue
					}
					handled, handleErr := s.completeLeadExecution(
						watcherContext, thread, executionID, notificationIDs, inputIDs, event,
					)
					if handleErr != nil {
						logx.Errorf("commit lead execution terminal failed: thread_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, executionID, thread.LeadChatID, handleErr)
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
				if exhaustErr := s.exhaustLeadWatcher(
					watcherContext,
					thread,
					executionID,
					notificationIDs,
					inputIDs,
				); exhaustErr == nil {
					return
				} else {
					logx.Errorf("compensate exhausted lead execution watcher failed: thread_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, executionID, thread.LeadChatID, exhaustErr)
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

// exhaustLeadWatcher 取消旧负责人执行并精确收敛它持有的通知和输入投递租约。
func (s *service) exhaustLeadWatcher(
	ctx context.Context,
	thread models.ChannelThread,
	executionID string,
	notificationIDs []string,
	inputIDs []string,
) error {
	if err := s.executions.CancelAndWait(ctx, thread.LeadChatID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if err := s.transaction(ctx, func(transactionContext context.Context) error {
		if err := s.tasks.UpdateNotifications(
			transactionContext,
			notificationIDs,
			constants.ChannelNotificationStatusPending,
			"",
			now,
		); err != nil {
			return err
		}
		// 用户输入在 Execution 接纳前已经成为负责人 Chat 的权威消息，不能重新排队制造重复气泡。
		if err := s.threads.UpdateLeadInputs(
			transactionContext,
			inputIDs,
			constants.ChannelInputStatusConsumed,
			executionID,
			now,
		); err != nil {
			return err
		}
		_, err := s.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTeamLeaderProcessing,
			leaderProcessingEvent{
				Active:         false,
				AgentID:        thread.LeadWakerID,
				Tasks:          len(notificationIDs),
				ExecutionID:    executionID,
				RetryExhausted: true,
				Reason:         "event_watcher_retry_exhausted",
			},
		)
		return err
	}); err != nil {
		return err
	}
	s.watchRecoveryDrain(thread.ID)
	return nil
}

// completeLeadExecution 把 exact execution 的终态与对应通知/输入批次原子提交。
func (s *service) completeLeadExecution(
	ctx context.Context,
	thread models.ChannelThread,
	executionID string,
	notificationIDs []string,
	inputIDs []string,
	event chat.PublicEvent,
) (bool, error) {
	if event.ExecutionID != executionID || !executionTerminal(event.ExecutionStatus) {
		return false, nil
	}
	notificationStatus := constants.ChannelNotificationStatusConsumed
	inputStatus := constants.ChannelInputStatusConsumed
	leadExecutionID := executionID
	if event.ExecutionStatus != constants.ExecutionStatusCompleted {
		notificationStatus = constants.ChannelNotificationStatusPending
		inputStatus = constants.ChannelInputStatusPending
		leadExecutionID = ""
	}
	now := time.Now().UnixMilli()
	if err := s.transaction(ctx, func(transactionContext context.Context) error {
		if len(notificationIDs) > 0 {
			if err := s.tasks.UpdateNotifications(
				transactionContext, notificationIDs, notificationStatus, leadExecutionID, now,
			); err != nil {
				return err
			}
			if _, err := s.appendEvent(
				transactionContext,
				thread.ChannelID,
				thread.ID,
				constants.ChannelEventTeamLeaderProcessing,
				leaderProcessingEvent{
					Active: false, AgentID: thread.LeadWakerID, Tasks: len(notificationIDs), ExecutionID: executionID,
				},
			); err != nil {
				return err
			}
		}
		if len(inputIDs) > 0 {
			return s.threads.UpdateLeadInputs(
				transactionContext, inputIDs, inputStatus, leadExecutionID, now,
			)
		}
		return nil
	}); err != nil {
		return false, err
	}
	if _, err := s.schedule(ctx, thread.ID); err != nil {
		logx.Errorf("schedule after leader terminal failed: thread_id=%s execution_id=%s error=%v", thread.ID, executionID, err)
	}
	s.watchRecoveryDrain(thread.ID)
	if _, err := s.drainLeadInputs(ctx, thread.ID); err == nil {
		if drainErr := s.drainNotifications(ctx, thread.ID); drainErr != nil {
			logx.Errorf("drain notifications after leader terminal failed: thread_id=%s execution_id=%s error=%v", thread.ID, executionID, drainErr)
		}
	} else {
		logx.Errorf("drain inputs after leader terminal failed: thread_id=%s execution_id=%s error=%v", thread.ID, executionID, err)
	}
	return true, nil
}

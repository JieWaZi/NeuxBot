package channel

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// recoverLeadInputAttachments 用现有队列收据校正崩溃时留下的附件副本。
func (s *service) recoverLeadInputAttachments(ctx context.Context) error {
	if s.managedFilesDirectory == "" {
		return nil
	}
	layout, err := chatstorage.NewLayout(s.managedFilesDirectory)
	if err != nil {
		return err
	}
	threadIDs, err := layout.TeamsWithQueuedAttachments()
	if err != nil {
		return err
	}
	for _, threadID := range threadIDs {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		if err := s.recoverThreadLeadInputAttachments(ctx, layout, threadID); err != nil {
			logx.Warnf("recover queued lead attachments: thread_id=%s error=%v", threadID, err)
		}
	}
	return nil
}

func (s *service) recoverThreadLeadInputAttachments(ctx context.Context, layout *chatstorage.Layout, threadID string) error {
	thread, err := s.threads.Get(ctx, threadID)
	if hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) {
		return nil
	}
	if err != nil {
		return err
	}
	locked, release, err := teamwork.Acquire(ctx, thread.ChannelID)
	if err != nil {
		return err
	}
	defer release()
	directory, err := layout.TeamRoot(threadID)
	if err != nil {
		return err
	}
	owner, err := workspace.ReadExecutionWorkspaceOwner(directory)
	if err != nil {
		return err
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != threadID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	ids, err := workspace.RecoverQueuedAttachmentIDs(directory)
	if err != nil {
		return err
	}
	for batch := range slices.Chunk(ids, constants.ChannelLeadInputReceiptBatchLimit) {
		receipts, err := s.threads.ListLeadInputReceipts(locked, threadID, batch)
		if err != nil {
			return err
		}
		pending := make(map[string]bool, len(receipts))
		for _, receipt := range receipts {
			pending[receipt.ID] = receipt.Status == constants.ChannelInputStatusPending
		}
		for _, id := range batch {
			if !pending[id] {
				if err := workspace.RemoveQueuedAttachments(directory, thread.LeadChatID, id); err != nil {
					logx.Warnf("release recovered lead input attachments: thread_id=%s input_id=%s error=%v", threadID, id, err)
				}
			}
		}
	}
	return nil
}

// recoverInitializingThread 补交已接纳的首发。启动或同键重试可回收明确未接纳的计划。
func (s *service) recoverInitializingThread(ctx context.Context, thread models.ChannelThread, reclaimUnadmitted bool) (models.ChannelThread, error) {
	locked, release, err := teamwork.Acquire(ctx, thread.ChannelID)
	if err != nil {
		return models.ChannelThread{}, err
	}
	defer release()
	ctx = locked
	thread, err = s.threads.Get(ctx, thread.ID)
	if reclaimUnadmitted && hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) {
		return models.ChannelThread{}, nil
	}
	if err != nil || thread.ReadyAt != nil {
		return thread, err
	}
	if thread.ProvisioningID == "" {
		return models.ChannelThread{}, status_error.CreateRequestInProgress
	}
	receipt, err := s.readInitialThreadExecution(ctx, thread)
	if err != nil {
		return models.ChannelThread{}, err
	}
	if receipt == nil {
		if reclaimUnadmitted {
			return models.ChannelThread{}, s.reclaimUnadmittedThread(ctx, thread)
		}
		return models.ChannelThread{}, status_error.CreateRequestInProgress
	}
	terminal := executionTerminal(receipt.Status)
	if !terminal && !s.executions.IsExecutionActive(thread.LeadChatID, receipt.ID) {
		// 异常退出留下的活动事实由 Chat 启动恢复收敛，线上没有执行所有者时不重新派发。
		return models.ChannelThread{}, status_error.CreateRequestInProgress
	}
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return models.ChannelThread{}, err
	}
	if err := s.publishInitialThread(context.WithoutCancel(ctx), &thread, receipt.ID, tasks); err != nil {
		logx.Warnf("recover accepted channel thread pending: thread_id=%s execution_id=%s error=%v", thread.ID, receipt.ID, err)
		return models.ChannelThread{}, status_error.CreateRequestInProgress
	}
	var token string
	if scope, ok := s.capabilities.Resolve(thread.ID); ok && scope.ExecutionID == receipt.ID {
		token = scope.LeaseID
	}
	if terminal {
		if token != "" {
			s.capabilities.Revoke(token)
		}
	} else {
		s.watchLeadExecution(thread, receipt.ID, nil, nil, token)
	}
	return thread, nil
}

// reclaimUnadmittedThread 回收没有首轮接纳记录的团队计划及其可能已建立的成员 Chat。
func (s *service) reclaimUnadmittedThread(ctx context.Context, thread models.ChannelThread) error {
	members, err := s.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		return err
	}
	chatIDs := make([]string, 0, len(members))
	for _, member := range members {
		chatIDs = append(chatIDs, member.ChatID)
		if err := s.executions.CancelAndWait(ctx, member.ChatID); err != nil {
			return err
		}
	}
	return s.deleteInitializingThread(ctx, thread, chatIDs)
}

// ReconcileThreadPublication 补齐已接纳的首发，只读取原 Chat 收据和提交团队发布。
func (s *service) ReconcileThreadPublication(ctx context.Context, threadID string) (models.ChannelThread, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return models.ChannelThread{}, status_error.BadRequest
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil || thread.ReadyAt != nil {
		return thread, err
	}
	return s.recoverInitializingThread(ctx, thread, false)
}

// ReconcileThread 补齐已接纳的首发，再推进任务、负责人输入和结果通知队列。
func (s *service) ReconcileThread(ctx context.Context, threadID string) error {
	thread, err := s.ReconcileThreadPublication(ctx, threadID)
	if err != nil {
		return err
	}
	threadID = thread.ID
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return err
	}
	if err := s.projectPendingMemberReports(ctx, threadID); err != nil {
		return err
	}
	if _, err := s.schedule(ctx, threadID); err != nil {
		return err
	}
	if _, err := s.drainLeadInputs(ctx, threadID); err != nil {
		if ctx.Err() != nil {
			return err
		}
		logx.Warnf("defer channel lead input recovery: thread_id=%s error=%v", threadID, err)
		s.watchRecoveryDrain(threadID)
		return nil
	}
	if err := s.drainNotifications(ctx, threadID); err != nil {
		return err
	}
	s.watchRecoveryDrain(threadID)
	return nil
}

// watchRecoveryDrain 在启动恢复或即时派发遇到故障后推进已有输入和结果队列。
func (s *service) watchRecoveryDrain(threadID string) {
	if threadID == "" {
		return
	}
	watcherID := "recovery:" + threadID
	watcherContext, registered := s.registerWatcher(watcherID)
	if !registered {
		return
	}
	go func() {
		ticker := time.NewTicker(
			time.Duration(constants.ChannelRecoveryPollIntervalMilliseconds) * time.Millisecond,
		)
		defer ticker.Stop()
		for {
			locked, release, err := s.acquireThreadOperation(watcherContext, threadID)
			if hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) {
				s.unregisterWatcher(watcherID)
				return
			}
			if err == nil {
				if s.recoveryQueuesEmpty(locked, threadID) {
					// 清空检查与退出登记共享接纳锁，后续入队可以重新启动监听。
					s.unregisterWatcher(watcherID)
					release()
					return
				}
				release()
			}
			select {
			case <-watcherContext.Done():
				s.unregisterWatcher(watcherID)
				return
			case <-ticker.C:
				thread, err := s.threads.Get(watcherContext, threadID)
				if err == nil && thread.CancelingAt != nil {
					if stopErr := s.finishStopWork(watcherContext, thread); stopErr != nil && watcherContext.Err() == nil {
						logx.Errorf("recover channel stop failed: thread_id=%s error=%v", threadID, stopErr)
					}
					continue
				}
				if err := s.projectPendingMemberReports(watcherContext, threadID); err != nil {
					logx.Errorf("recover channel member report projection failed: thread_id=%s error=%v", threadID, err)
					continue
				}
				if _, err := s.schedule(watcherContext, threadID); err != nil {
					logx.Errorf("recover channel task scheduling failed: thread_id=%s error=%v", threadID, err)
				}
				if _, err := s.drainLeadInputs(watcherContext, threadID); err == nil {
					if drainErr := s.drainNotifications(watcherContext, threadID); drainErr != nil {
						logx.Errorf("recover channel notifications failed: thread_id=%s error=%v", threadID, drainErr)
					}
				} else {
					logx.Errorf("recover channel lead inputs failed: thread_id=%s error=%v", threadID, err)
				}
			}
		}
	}()
}

// recoveryQueuesEmpty 判断等待派发的任务、负责人输入和结果队列是否已经取空。
func (s *service) recoveryQueuesEmpty(ctx context.Context, threadID string) bool {
	thread, err := s.threads.Get(ctx, threadID)
	if hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) {
		return true
	}
	if err != nil {
		return false
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return false
	}
	snapshot, err := s.tasks.LoadDispatchSnapshot(ctx, threadID)
	if err != nil {
		return false
	}
	for _, task := range snapshot.Tasks {
		if dispatchableTask(thread, snapshot, task) {
			return false
		}
	}
	inputs, err := s.threads.ListLeadInputs(
		ctx,
		threadID,
		[]constants.ChannelInputStatus{constants.ChannelInputStatusPending},
		1,
	)
	if err != nil || len(inputs) > 0 {
		return false
	}
	notifications, err := s.tasks.ListPendingNotifications(ctx, threadID, 1)
	return err == nil && len(notifications) == 0
}

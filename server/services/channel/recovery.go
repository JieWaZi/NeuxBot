package channel

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// ReconcileThread 推进可调度任务以及负责人耐久输入和结果通知队列。
func (s *service) ReconcileThread(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return status_error.BadRequest
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
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
		return err
	}
	if err := s.drainNotifications(ctx, threadID); err != nil {
		return err
	}
	s.watchRecoveryDrain(threadID)
	return nil
}

// watchRecoveryDrain 在服务重启后持续推进仍被活跃负责人 Chat 阻塞的持久输入与结果队列。
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
		defer s.unregisterWatcher(watcherID)
		ticker := time.NewTicker(
			time.Duration(constants.ChannelRecoveryPollIntervalMilliseconds) * time.Millisecond,
		)
		defer ticker.Stop()
		for {
			if s.recoveryQueuesEmpty(threadID) {
				return
			}
			select {
			case <-watcherContext.Done():
				return
			case <-ticker.C:
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

// recoveryQueuesEmpty 判断重启恢复需要照看的负责人输入和结果队列是否已经进入投递态或完成态。
func (s *service) recoveryQueuesEmpty(threadID string) bool {
	thread, err := s.threads.Get(s.lifecycleContext, threadID)
	if err != nil {
		return false
	}
	if err := s.ensureThreadWritable(s.lifecycleContext, thread); err != nil {
		return false
	}
	tasks, err := s.tasks.ListByThread(s.lifecycleContext, threadID)
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if task.Status == constants.ChannelTaskStatusPending && task.DeferUntilLeadIdle {
			return false
		}
	}
	inputs, err := s.threads.ListLeadInputs(
		s.lifecycleContext,
		threadID,
		[]constants.ChannelInputStatus{constants.ChannelInputStatusPending},
		1,
	)
	if err != nil || len(inputs) > 0 {
		return false
	}
	notifications, err := s.tasks.ListPendingNotifications(s.lifecycleContext, threadID, 1)
	return err == nil && len(notifications) == 0
}

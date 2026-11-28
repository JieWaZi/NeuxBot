package schedule

import (
	"context"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Delete 封闭新触发，并通过 Chat 的统一 staging 事务删除任务完整聚合。
func (s *Service) Delete(ctx context.Context, wakerID, id string) error {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	if _, err := s.getTask(ctx, wakerID, id); err != nil {
		return err
	}
	if err := s.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return s.config.Tasks.SetDeleting(ctx, id, true) }); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.WithAuthorizationWrite(context.WithoutCancel(ctx), func(ctx context.Context) error { return s.config.Tasks.SetDeleting(ctx, id, false) })
		}
	}()
	runs, _, err := s.config.Tasks.ListRuns(ctx, id, 0, 0)
	if err != nil {
		return err
	}
	channelIDs := map[string]bool{}
	for _, run := range runs {
		if run.Configuration.ChannelID != "" {
			channelIDs[run.Configuration.ChannelID] = true
		}
	}
	ordered := make([]string, 0, len(channelIDs))
	for id := range channelIDs {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		locked, release, lockErr := teamwork.Acquire(ctx, id)
		if lockErr != nil {
			return lockErr
		}
		ctx = locked
		defer release()
	}
	chatIDs := []string{}
	threadIDs := []string{}
	for _, run := range runs {
		if err := s.ReconcileRun(ctx, &run); err != nil {
			return err
		}
		if run.WorkflowRunID != "" {
			view, err := s.ReadRun(ctx, run)
			if err != nil {
				return err
			}
			if Unfinished(view.State) {
				return status_error.AutomaticTaskBusy
			}
			continue
		}
		if run.ChatID == "" {
			continue
		}
		if run.ThreadID != "" {
			view, err := s.ReadRun(ctx, run)
			if err != nil {
				return err
			}
			if Unfinished(view.State) {
				return status_error.AutomaticTaskBusy
			}
			active, err := s.config.Threads.CountActiveWork(ctx, run.ThreadID)
			if err != nil {
				return err
			}
			if active > 0 {
				return status_error.AutomaticTaskBusy
			}
			ids, err := s.config.Threads.ListChatIDs(ctx, run.ThreadID)
			if err != nil {
				return err
			}
			chatIDs = append(chatIDs, ids...)
			threadIDs = append(threadIDs, run.ThreadID)
			continue
		}
		state, err := s.config.Chats.ReadInitialExecution(ctx, run.ChatID)
		if util.HasStatusCode(err, status_error.ChatNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if state.Chat.ActiveExecutionID != "" {
			return status_error.AutomaticTaskBusy.WithParams(map[string]interface{}{"ChatID": run.ChatID})
		}
		chatIDs = append(chatIDs, run.ChatID)
	}
	finalize := func(tx context.Context) error {
		for _, threadID := range threadIDs {
			if err := s.config.Threads.Delete(tx, threadID); err != nil {
				return err
			}
		}
		if err := s.config.Tasks.Delete(tx, id); err != nil {
			return err
		}
		return capability.Commit(tx, map[string]any{"taskId": id, "deleted": true})
	}
	// 最终删除必须先进入授权屏障，再获取数据库写锁；同一 Context 让任务仓库重入，
	// 避免 Chat 索引已写入后才等待撤权锁，与 Key 更新形成反向锁序。
	err = s.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
		if len(chatIDs) == 0 {
			return s.config.Transactions.Transaction(ctx, finalize)
		}
		return s.config.Chats.DeleteChatsAtomically(ctx, chatIDs, finalize)
	})
	committed = err == nil
	if committed {
		s.ForgetTask(ctx, id)
	}
	return err
}

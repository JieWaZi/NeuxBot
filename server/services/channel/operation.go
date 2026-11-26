package channel

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// threadWorkSnapshotKey 携带本次派发读取的停止代次，只用于同一团队对话的执行接纳。
type threadWorkSnapshotKey struct{}

type threadWorkSnapshot struct {
	// id 是派发所属团队对话。
	id string
	// generation 是读出候选工作时的停止代次。
	generation int64
}

func (s *service) startThreadExecution(ctx context.Context, thread models.ChannelThread, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	ctx = context.WithValue(ctx, threadWorkSnapshotKey{}, threadWorkSnapshot{thread.ID, thread.StopGeneration})
	return s.executions.Start(ctx, intent)
}

// acquireThreadAdmission 只串行化单个团队对话的执行占位交接与停止捕获。
func acquireThreadAdmission(ctx context.Context, threadID string) (context.Context, func(), error) {
	return teamwork.Acquire(ctx, "thread-admission:"+threadID)
}

// acquireLeadDispatch 保证负责人用户输入和结果汇总按同一 Chat 顺序派发。
func acquireLeadDispatch(ctx context.Context, threadID string) (context.Context, func(), error) {
	return teamwork.Acquire(ctx, "thread-lead:"+threadID)
}

// acquireThreadOperation 先确定团队身份，再复用同步接纳链路的聚合保护。
func (s *service) acquireThreadOperation(ctx context.Context, threadID string) (context.Context, func(), error) {
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	return teamwork.Acquire(ctx, thread.ChannelID)
}

// freezeBeforeUserInput 在写入新的用户工作前保存首次整体结束的自动运行结果。
func (s *service) freezeBeforeUserInput(ctx context.Context, threadID string) error {
	if s.beforeUserInput != nil {
		return s.beforeUserInput(ctx, threadID)
	}
	return nil
}

// AcquireChatInput 保护直接成员 Chat 输入与团队删除的互斥，并在续聊前冻结自动结果。
func (s *service) AcquireChatInput(ctx context.Context, chatID string, userInput bool) (context.Context, func(), error) {
	thread, exists, err := s.threads.FindByChatID(ctx, chatID)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return ctx, func() {}, nil
	}
	if thread.LeadChatID == chatID && teamwork.IsMemberReplyRoute(ctx) {
		return nil, nil, status_error.ChatReplyTargetUnavailable
	}
	locked, release, err := teamwork.Acquire(ctx, thread.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	if userInput {
		if err := s.freezeBeforeUserInput(locked, thread.ID); err != nil {
			release()
			return nil, nil, err
		}
	}
	admission, releaseAdmission, err := acquireThreadAdmission(locked, thread.ID)
	if err != nil {
		release()
		return nil, nil, err
	}
	releaseAll := func() { releaseAdmission(); release() }
	current, err := s.threads.Get(admission, thread.ID)
	if err == nil {
		snapshot, dispatch := ctx.Value(threadWorkSnapshotKey{}).(threadWorkSnapshot)
		if current.CancelingAt != nil || (dispatch && (snapshot.id != current.ID || snapshot.generation != current.StopGeneration)) {
			err = status_error.ChannelTaskStateConflict
		} else {
			_, err = s.channels.Get(admission, current.ChannelID)
		}
	}
	if err != nil {
		releaseAll()
		return nil, nil, err
	}
	return admission, releaseAll, nil
}

// commitLeadDispatch 将负责人通知链接及界面状态写入同一停止代次。
func (s *service) commitLeadDispatch(ctx context.Context, thread models.ChannelThread, commit func(context.Context) error) error {
	return s.transaction(ctx, func(ctx context.Context) error {
		current, err := s.threads.Get(ctx, thread.ID)
		if err != nil {
			return err
		}
		if current.CancelingAt != nil || current.StopGeneration != thread.StopGeneration {
			return status_error.ChannelTaskStateConflict
		}
		return commit(ctx)
	})
}

// automaticWorkSource 只在原运行尚未整体封闭时沿内部接力保留自动来源。
// Thread 始终保留最初来源用于追溯；新用户工作和它产生的成员/汇总从封闭事实判断身份。
func (s *service) automaticWorkSource(ctx context.Context, thread models.ChannelThread) (string, error) {
	if thread.AutomaticRunID == "" {
		return "", nil
	}
	if s.automaticRun == nil {
		return "", status_error.AutomaticRunNotFound
	}
	run, err := s.automaticRun(ctx, thread.AutomaticRunID)
	if err != nil {
		return "", err
	}
	if run.FinalState != constants.AutomaticRunStateUnspecified {
		return "", nil
	}
	return thread.AutomaticRunID, nil
}

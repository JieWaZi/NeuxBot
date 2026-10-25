package channel

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

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
	locked, release, err := teamwork.Acquire(ctx, thread.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	if err = s.ensureThreadWritable(locked, thread); err == nil && userInput {
		err = s.freezeBeforeUserInput(locked, thread.ID)
	}
	if err != nil {
		release()
		return nil, nil, err
	}
	return locked, release, nil
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

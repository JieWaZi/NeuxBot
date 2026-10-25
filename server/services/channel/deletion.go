package channel

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
)

// DeleteThread 永久删除一个团队对话及其成员 Chat。
func (s *service) DeleteThread(ctx context.Context, channelID string, threadID string) error {
	channelID = strings.TrimSpace(channelID)
	threadID = strings.TrimSpace(threadID)
	if channelID == "" || threadID == "" {
		return status_error.BadRequest
	}
	locked, release, lockErr := teamwork.Acquire(ctx, channelID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	defer release()
	if _, err := s.channels.Get(ctx, channelID); err != nil {
		return err
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
	active, err := s.threads.CountActiveWork(ctx, threadID)
	if err != nil {
		return err
	}
	if active > 0 {
		return status_error.ChannelHasActiveWork
	}
	chatIDs, err := s.threads.ListChatIDs(ctx, threadID)
	if err != nil {
		return err
	}
	return s.chats.DeleteChatsAtomically(ctx, chatIDs, func(transactionContext context.Context) error {
		return s.threads.Delete(transactionContext, threadID)
	})
}

// DeleteChannel 永久删除团队、全部团队对话及其成员 Chat。
func (s *service) DeleteChannel(ctx context.Context, channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return status_error.BadRequest
	}
	locked, release, lockErr := teamwork.Acquire(ctx, channelID)
	if lockErr != nil {
		return lockErr
	}
	ctx = locked
	defer release()
	if _, err := s.channels.Get(ctx, channelID); err != nil {
		return err
	}
	active, err := s.channels.CountActiveWork(ctx, channelID)
	if err != nil {
		return err
	}
	if active > 0 {
		return status_error.ChannelHasActiveWork
	}
	threadIDs, err := s.threads.ListIDsByChannel(ctx, channelID)
	if err != nil {
		return err
	}
	chatIDs := make([]string, 0)
	for _, threadID := range threadIDs {
		ids, listErr := s.threads.ListChatIDs(ctx, threadID)
		if listErr != nil {
			return listErr
		}
		chatIDs = append(chatIDs, ids...)
	}
	return s.chats.DeleteChatsAtomically(ctx, chatIDs, func(transactionContext context.Context) error {
		if err := s.threads.DeleteByChannel(transactionContext, channelID); err != nil {
			return err
		}
		return s.channels.Delete(transactionContext, channelID)
	})
}

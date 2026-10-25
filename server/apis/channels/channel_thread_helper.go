package channels

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
)

// memberExecutionConfigInputs 转换对话首发的成员执行配置覆盖。
func memberExecutionConfigInputs(members []ChannelMemberInput) []channel.MemberExecutionConfigInput {
	configs := make([]channel.MemberExecutionConfigInput, 0, len(members))
	for _, member := range members {
		configs = append(configs, channel.MemberExecutionConfigInput{
			WakerID:         member.WakerID,
			RuntimeID:       member.RuntimeID,
			AccountID:       member.AccountID,
			Model:           member.Model,
			Reasoning:       member.Reasoning,
			PermissionLevel: member.PermissionLevel,
		})
	}
	return configs
}

// channelThread 校验团队对话已经就绪且属于当前团队。
func channelThread(
	ctx context.Context,
	channelID string,
	threadID string,
) (models.ChannelThread, error) {
	thread, err := global.ChannelThreadControllerManager.Get(ctx, threadID)
	if err != nil {
		return models.ChannelThread{}, err
	}
	if thread.ChannelID != channelID || thread.ReadyAt == nil {
		return models.ChannelThread{}, status_error.ChannelThreadNotFound.WithParams(
			map[string]interface{}{"ThreadID": threadID},
		)
	}
	return thread, nil
}

// writableChannelThread 校验团队对话存在、已经就绪且属于当前团队。
func writableChannelThread(
	ctx context.Context,
	channelID string,
	threadID string,
) (models.ChannelThread, error) {
	thread, err := channelThread(ctx, channelID, threadID)
	if err != nil {
		return models.ChannelThread{}, err
	}
	if err := ensureChannelWritable(ctx, channelID); err != nil {
		return models.ChannelThread{}, err
	}
	if thread.CancelingAt != nil {
		return models.ChannelThread{}, status_error.ChannelTaskStateConflict
	}
	return thread, nil
}

// ensureChannelWritable 在当前事务中校验团队仍然存在。
func ensureChannelWritable(ctx context.Context, channelID string) error {
	_, err := global.ChannelControllerManager.Get(ctx, channelID)
	return err
}

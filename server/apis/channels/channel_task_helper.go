package channels

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// channelTask 校验正式任务属于当前团队对话。
func channelTask(
	ctx context.Context,
	threadID string,
	taskID string,
) (models.ChannelTask, error) {
	task, err := global.ChannelTaskControllerManager.Get(ctx, taskID)
	if err != nil {
		return models.ChannelTask{}, err
	}
	if task.ThreadID != threadID {
		return models.ChannelTask{}, status_error.ChannelTaskNotFound.WithParams(
			map[string]interface{}{"TaskID": taskID},
		)
	}
	return task, nil
}

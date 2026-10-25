package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 停止团队任务
type StopChannelTask struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
}

// Path 返回单任务停止接口路径。
func (s *StopChannelTask) Path() string {
	return "/:channelId/threads/:threadId/tasks/:taskId/stop"
}

// Output 取消任务当前成员 Chat Execution。
func (s *StopChannelTask) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := writableChannelThread(ctx, s.ChannelID, s.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := channelTask(ctx, s.ThreadID, s.TaskID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChannelService.StopTask(ctx, s.TaskID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

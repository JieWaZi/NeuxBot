package channels

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队任务补充请求
type FollowUpChannelTaskRequest struct {
	// 补充内容
	Text string `json:"text" validate:"required,max=1048576"`
}

// 补充团队任务
type FollowUpChannelTask struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
	// 请求体
	Body FollowUpChannelTaskRequest `in:"body"`
}

// Path 返回团队任务补充接口路径。
func (f *FollowUpChannelTask) Path() string {
	return "/:channelId/threads/:threadId/tasks/:taskId/follow-ups"
}

// Output 持久化补充并沿用原任务和原成员 Chat。
func (f *FollowUpChannelTask) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := writableChannelThread(ctx, f.ChannelID, f.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := channelTask(ctx, f.ThreadID, f.TaskID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChannelService.FollowUpTask(ctx, f.TaskID, execution.WorkInput{
		Actor:   string(constants.ChannelMessageActorUser),
		Content: f.Body.Text,
	}); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

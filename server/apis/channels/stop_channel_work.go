package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 停止团队当前工作
type StopChannelWork struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
}

// Path 返回团队对话停止全部当前工作接口路径。
func (s *StopChannelWork) Path() string {
	return "/:channelId/threads/:threadId/work/stop"
}

// Output 通过可恢复停止意图取消负责人和全部成员当前执行，同时保留对话及 in_review 任务。
func (s *StopChannelWork) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ChannelService.StopWork(ctx, s.ChannelID, s.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

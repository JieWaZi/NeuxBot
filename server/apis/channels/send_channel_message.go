package channels

import (
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/global"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发送团队消息
type SendChannelMessage struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 请求体
	Body ChannelWorkInput `in:"body"`
}

// Path 返回既有团队对话发送接口路径。
func (s *SendChannelMessage) Path() string {
	return "/:channelId/threads/:threadId/messages"
}

// Output 复用原团队对话和成员 Chat 上下文路由用户输入。
func (s *SendChannelMessage) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := channelThread(ctx, s.ChannelID, s.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	input := s.Body.workInput()
	release, err := uploads.ClaimWork(ctx, &input)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() { release(accepted) }()
	result, err := global.ChannelService.SubmitMessage(ctx, channelservice.SubmitMessageInput{Continue: &channelservice.SendMessageInput{
		ThreadID:       s.ThreadID,
		MentionWakerID: s.Body.MentionWakerID,
		Input:          input,
	}})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	accepted = true
	return *result.Message, nil
}

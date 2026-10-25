package channels

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建团队对话请求
type CreateChannelThreadRequest struct {
	// 首次发送幂等标识
	ClientRequestID string `json:"clientRequestId" validate:"required,max=64"`
	// 项目标识
	ProjectID string `json:"projectId" validate:"max=32"`
	// 工作目录
	WorkingDirectory string `json:"workingDirectory" validate:"max=4096"`
	// 成员配置
	MemberConfigs []ChannelMemberInput `json:"memberConfigs" validate:"max=5,dive"`
	// 消息输入
	ChannelWorkInput
}

// 创建团队对话
type CreateChannelThread struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 请求体
	Body CreateChannelThreadRequest `in:"body"`
}

// Path 返回团队对话首次发送接口路径。
func (c *CreateChannelThread) Path() string { return "/:channelId/threads" }

// Validate 校验项目与临时目录不能同时选择，并规范化成员配置。
func (c *CreateChannelThread) Validate(*gin.Context) error {
	hasProject := strings.TrimSpace(c.Body.ProjectID) != ""
	hasDirectory := strings.TrimSpace(c.Body.WorkingDirectory) != ""
	if hasProject && hasDirectory {
		return ginx.WithStack(status_error.BadRequest)
	}
	members, err := normalizeChannelMemberInputs(c.Body.MemberConfigs)
	if err != nil {
		return ginx.WithStack(err)
	}
	c.Body.MemberConfigs = members
	return nil
}

// Output 首次发送时才建立团队对话和全部成员独立 Chat。
func (c *CreateChannelThread) Output(ctx *gin.Context) (interface{}, error) {
	input := c.Body.workInput()
	release, err := uploads.ClaimWork(ctx, &input, ctx.Request.URL.Path+":"+strings.TrimSpace(c.Body.ClientRequestID))
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() { release(accepted) }()
	result, err := global.ChannelService.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{
		ChannelID:        c.ChannelID,
		ClientRequestID:  c.Body.ClientRequestID,
		ProjectID:        c.Body.ProjectID,
		WorkingDirectory: c.Body.WorkingDirectory,
		MentionWakerID:   c.Body.MentionWakerID,
		MemberConfigs:    memberExecutionConfigInputs(c.Body.MemberConfigs),
		Input:            input,
	}})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	accepted = true
	return *result.Thread, nil
}

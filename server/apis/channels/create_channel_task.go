package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建团队任务请求
type CreateChannelTaskRequest struct {
	// 执行伙伴标识
	AssigneeWakerID string `json:"assigneeWakerId" validate:"required,max=32"`
	// 任务主题
	Subject string `json:"subject" validate:"required,max=200"`
	// 任务说明
	Description string `json:"description" validate:"required,max=1048576"`
	// 优先级
	Priority int `json:"priority"`
	// 上游任务标识
	DependencyTaskIDs []string `json:"dependencyTaskIds" validate:"max=12,dive,max=32"`
	// 需要验收
	RequiresHumanReview bool `json:"requiresHumanReview"`
}

// 创建团队任务
type CreateChannelTask struct {
	ginx.MethodPost
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 请求体
	Body CreateChannelTaskRequest `in:"body"`
}

// Path 返回正式任务创建接口路径。
func (c *CreateChannelTask) Path() string {
	return "/:channelId/threads/:threadId/tasks"
}

// Output 创建任务并在依赖和并发约束允许时派发。
func (c *CreateChannelTask) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := writableChannelThread(ctx, c.ChannelID, c.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	_, err := global.ChannelService.CreateTask(ctx, channelservice.CreateTaskInput{
		ThreadID:            c.ThreadID,
		AssigneeWakerID:     c.Body.AssigneeWakerID,
		Subject:             c.Body.Subject,
		Description:         c.Body.Description,
		Priority:            c.Body.Priority,
		DependencyTaskIDs:   c.Body.DependencyTaskIDs,
		RequiresHumanReview: c.Body.RequiresHumanReview,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

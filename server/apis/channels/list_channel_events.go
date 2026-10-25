package channels

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队事件列表
type ListChannelEvents struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 恢复游标
	After string `in:"query" name:"after" validate:"max=32"`
	// 返回上限
	Limit int `in:"query" name:"limit"`
}

// Path 返回团队持久事件列表接口路径。
func (l *ListChannelEvents) Path() string {
	return "/:channelId/threads/:threadId/events/list"
}

// Validate 校验事件游标和窗口上限。
func (l *ListChannelEvents) Validate(*gin.Context) error {
	if l.Limit < 0 || l.Limit > constants.ChannelEventMaximumLimit {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.Limit == 0 {
		l.Limit = constants.ChannelEventDefaultLimit
	}
	return nil
}

// Output 返回游标之后已经提交的团队事件。
func (l *ListChannelEvents) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := channelThread(ctx, l.ChannelID, l.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	events, err := global.ChannelThreadControllerManager.ListEvents(
		ctx,
		l.ThreadID,
		l.After,
		l.Limit,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return events, nil
}

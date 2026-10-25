package channels

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队时间线页
type ChannelTimelinePage struct {
	// 消息列表
	Resources []channelservice.TimelineMessage `json:"resources"`
	// 更早页游标
	NextCursor string `json:"nextCursor,omitempty"`
	// 更早消息
	HasMore bool `json:"hasMore"`
}

// 获取团队时间线
type ListChannelTimeline struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 不透明游标
	Cursor string `in:"query" name:"cursor" validate:"max=16384"`
	// 每页数量
	Limit int `in:"query" name:"limit"`
}

// Path 返回团队聚合时间线分页接口路径。
func (l *ListChannelTimeline) Path() string {
	return "/:channelId/threads/:threadId/timeline"
}

// Validate 校验分页数量并补充默认值。
func (l *ListChannelTimeline) Validate(*gin.Context) error {
	if l.Limit < 0 || l.Limit > constants.ChannelTimelineMaxLimit {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.Limit == 0 {
		l.Limit = constants.ChannelTimelineDefaultLimit
	}
	return nil
}

// Output 从各成员权威 Chat JSONL 读取一页更早消息。
func (l *ListChannelTimeline) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := channelThread(ctx, l.ChannelID, l.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	page, err := global.ChannelService.ReadTimeline(ctx, channelservice.TimelineQuery{
		ThreadID: l.ThreadID,
		Cursor:   l.Cursor,
		Limit:    l.Limit,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ChannelTimelinePage{
		Resources:  page.Resources,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	}, nil
}

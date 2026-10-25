package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取团队
type GetChannel struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
}

// Path 返回团队详情接口路径。
func (g *GetChannel) Path() string { return "/:channelId" }

// Output 返回团队主体和成员默认配置。
// @err[ChannelNotFound][40400000012][团队不存在]
func (g *GetChannel) Output(ctx *gin.Context) (interface{}, error) {
	channel, err := global.ChannelControllerManager.Get(ctx, g.ChannelID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	activitySummaries, err := global.ChannelControllerManager.ListActivitySummaries(ctx, []string{channel.ID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	detail, err := loadChannelDetail(ctx, channel, activitySummaries[channel.ID])
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return detail, nil
}

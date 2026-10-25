package channels

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队列表
type ListChannels struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 团队列表响应
type ListChannelsResponse struct {
	// 资源列表
	Resources []ChannelDetail `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回团队列表接口路径。
func (l *ListChannels) Path() string { return "" }

// Validate 校验分页范围并补充默认值。
func (l *ListChannels) Validate(*gin.Context) error {
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > constants.ChannelCollectionMaximumPageSize {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 {
		l.PageNum = 1
	}
	if l.PageSize == 0 {
		l.PageSize = constants.ChannelCollectionDefaultPageSize
	}
	return nil
}

// Output 返回团队及其少量成员详情。
func (l *ListChannels) Output(ctx *gin.Context) (interface{}, error) {
	query := strings.TrimSpace(l.Query)
	channels, err := global.ChannelControllerManager.List(
		ctx,
		query,
		l.PageSize,
		(l.PageNum-1)*l.PageSize,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	channelIDs := make([]string, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}
	activitySummaries, err := global.ChannelControllerManager.ListActivitySummaries(ctx, channelIDs)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]ChannelDetail, 0, len(channels))
	for _, channel := range channels {
		detail, err := loadChannelDetail(ctx, channel, activitySummaries[channel.ID])
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		resources = append(resources, detail)
	}
	total, err := global.ChannelControllerManager.Count(ctx, query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListChannelsResponse{
		Resources: resources,
		PageNum:   l.PageNum,
		PageSize:  l.PageSize,
		TotalSize: total,
	}, nil
}

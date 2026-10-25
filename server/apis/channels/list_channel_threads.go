package channels

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队对话列表
type ListChannelThreads struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
	// 不透明下一页游标
	Cursor string `in:"query" name:"cursor" validate:"max=160"`
	// 搜索关键字
	Query string `in:"query" name:"query" validate:"max=120"`
	// 聚合活动状态
	Status constants.ChannelActivityState `in:"query" name:"status"`
	// 时间起点
	UpdatedAtStart int64 `in:"query" name:"updatedAtStart"`
	// 时间终点
	UpdatedAtEnd int64 `in:"query" name:"updatedAtEnd"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
}

// 团队对话列表响应
type ListChannelThreadsResponse struct {
	// 资源列表
	Resources []models.ChannelThread `json:"resources"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 下一页游标
	NextCursor string `json:"nextCursor,omitempty"`
	// 是否存在下一页
	HasMore bool `json:"hasMore"`
	// 页码
	PageNum int `json:"pageNum"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// channelThreadPageCursor 是 API 不透明游标编码的稳定载荷。
type channelThreadPageCursor struct {
	// UpdatedAt 是上一页最后一项的最近活动时间。
	UpdatedAt int64 `json:"updatedAt"`
	// ID 是上一页最后一项的团队对话标识。
	ID string `json:"id"`
}

func decodeChannelThreadCursor(value string) (*controllers.ChannelThreadListCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, status_error.BadRequest
	}
	var cursor channelThreadPageCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.UpdatedAt <= 0 || cursor.ID == "" || len(cursor.ID) > 32 {
		return nil, status_error.BadRequest
	}
	return &controllers.ChannelThreadListCursor{UpdatedAt: cursor.UpdatedAt, ID: cursor.ID}, nil
}

func encodeChannelThreadCursor(cursor *controllers.ChannelThreadListCursor) string {
	if cursor == nil {
		return ""
	}
	data, err := json.Marshal(channelThreadPageCursor{UpdatedAt: cursor.UpdatedAt, ID: cursor.ID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// Path 返回团队对话列表接口路径。
func (l *ListChannelThreads) Path() string { return "/:channelId/threads" }

// Validate 校验分页范围并补充默认值。
func (l *ListChannelThreads) Validate(*gin.Context) error {
	if l.UpdatedAtStart < 0 || l.UpdatedAtEnd < 0 ||
		(l.UpdatedAtStart > 0 && l.UpdatedAtEnd > 0 && l.UpdatedAtStart > l.UpdatedAtEnd) ||
		l.PageNum < 0 || l.PageSize < 0 || l.PageSize > constants.ChannelCollectionMaximumPageSize {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageSize == 0 {
		l.PageSize = constants.ChannelCollectionDefaultPageSize
	}
	switch l.Status {
	case "", constants.ChannelActivityStateIdle, constants.ChannelActivityStateRunning, constants.ChannelActivityStateWaiting:
	default:
		return ginx.WithStack(status_error.BadRequest)
	}
	if _, err := decodeChannelThreadCursor(l.Cursor); err != nil {
		return ginx.WithStack(err)
	}
	if l.PageNum > 0 && (l.Cursor != "" || l.Status != "") {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 && (l.UpdatedAtStart > 0 || l.UpdatedAtEnd > 0) {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 返回团队全部已完成首发的可见对话。
func (l *ListChannelThreads) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.ChannelControllerManager.Get(ctx, l.ChannelID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if l.PageNum > 0 {
		query := strings.TrimSpace(l.Query)
		resources, err := global.ChannelThreadControllerManager.ListByChannelUpdatedRange(
			ctx,
			l.ChannelID,
			query,
			l.UpdatedAtStart,
			l.UpdatedAtEnd,
			l.PageSize,
			(l.PageNum-1)*l.PageSize,
		)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		total, err := global.ChannelThreadControllerManager.CountByChannelUpdatedRange(
			ctx,
			l.ChannelID,
			query,
			l.UpdatedAtStart,
			l.UpdatedAtEnd,
		)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		return ListChannelThreadsResponse{
			Resources: resources,
			PageNum:   l.PageNum,
			PageSize:  l.PageSize,
			TotalSize: total,
		}, nil
	}
	cursor, err := decodeChannelThreadCursor(l.Cursor)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result, err := global.ChannelThreadControllerManager.ListByChannel(ctx, controllers.ChannelThreadListQuery{
		ChannelID: l.ChannelID,
		Search:    l.Query,
		Status:    l.Status,
		Cursor:    cursor,
		Limit:     l.PageSize,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListChannelThreadsResponse{
		Resources:  result.Threads,
		PageSize:   l.PageSize,
		NextCursor: encodeChannelThreadCursor(result.NextCursor),
		HasMore:    result.NextCursor != nil,
	}, nil
}

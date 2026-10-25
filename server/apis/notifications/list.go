package notifications

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// 通知列表
type ListNotifications struct {
	ginx.MethodGet
	// 关注分桶
	Bucket string `in:"query" name:"bucket" validate:"required,oneof=action unread"`
	// 页码
	PageNum int `in:"query" name:"pageNum" validate:"min=0,max=10000"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize" validate:"min=0,max=100"`
	// 执行对象
	ExecutorID string `in:"query" name:"executorId" validate:"max=32"`
	// 原始来源
	Source string `in:"query" name:"source" validate:"omitempty,oneof=chat team workflow automatic im"`
	// 标题搜索
	Query string `in:"query" name:"query" validate:"max=200"`
	// 续页游标
	Cursor string `in:"query" name:"cursor" validate:"max=1024"`
	// cursor 保存本次解码后的排序锚点，不暴露于响应。
	cursor *notificationCursor
}

// notificationCursor 将排序身份绑定全部筛选条件。
type notificationCursor struct {
	// Filter 是完整查询摘要，用于拒绝跨筛选续页。
	Filter string `json:"filter"`
	// At 是上一页最后一项的原发生时间。
	At int64 `json:"at"`
	// ID 是上一页最后一项的稳定通知身份。
	ID string `json:"id"`
}

// 通知分页响应
type ListNotificationsResponse struct {
	// 通知条目
	Resources []models.NotificationItem `json:"resources"`
	// 总条目数
	TotalSize int64 `json:"totalSize"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 存在续页
	HasMore bool `json:"hasMore"`
	// 下页游标
	NextCursor string `json:"nextCursor,omitempty"`
	// 生命周期
	Epoch string `json:"epoch"`
	// 观察水位
	Revision int64 `json:"revision"`
}

func (*ListNotifications) Path() string { return "" }
func (o *ListNotifications) filter() string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s", o.Bucket, o.ExecutorID, o.Source, o.Query)
}
func (o *ListNotifications) Validate(*gin.Context) error {
	if o.PageNum == 0 {
		o.PageNum = 1
	}
	if o.PageSize == 0 {
		o.PageSize = 50
	}
	if o.Cursor != "" {
		encoded, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		if err != nil {
			return status_error.BadRequest
		}
		cursor := notificationCursor{}
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursor); err != nil || cursor.Filter != o.filter() || cursor.ID == "" || cursor.At < 0 {
			return status_error.BadRequest
		}
		o.cursor = &cursor
	}
	return nil
}
func (o *ListNotifications) Output(ctx *gin.Context) (interface{}, error) {
	query := controllers.NotificationQuery{Bucket: o.Bucket, ExecutorID: o.ExecutorID, Source: o.Source, Query: o.Query, Limit: o.PageSize, Offset: (o.PageNum - 1) * o.PageSize}
	if o.cursor != nil {
		query.CursorAt, query.CursorID, query.Offset = o.cursor.At, o.cursor.ID, 0
	}
	values, total, err := global.NotificationControllerManager.List(ctx.Request.Context(), query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	snapshot, err := global.NotificationService.Snapshot()
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	hasMore := len(values) > o.PageSize
	if hasMore {
		values = values[:o.PageSize]
	}
	value := ListNotificationsResponse{Resources: values, TotalSize: total, PageNum: o.PageNum, PageSize: o.PageSize, HasMore: hasMore, Epoch: snapshot.Epoch, Revision: snapshot.Revision}
	if hasMore && len(values) > 0 {
		last := values[len(values)-1]
		cursor, _ := json.Marshal(notificationCursor{Filter: o.filter(), At: last.CreatedAt, ID: last.ID})
		value.NextCursor = base64.RawURLEncoding.EncodeToString(cursor)
	}
	return value, nil
}

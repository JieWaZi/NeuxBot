package wakers

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴对话列表
type ListWakerChats struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 时间起点
	UpdatedAtStart int64 `in:"query" name:"updatedAtStart"`
	// 时间终点
	UpdatedAtEnd int64 `in:"query" name:"updatedAtEnd"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 伙伴对话列表响应
type ListWakerChatsResponse struct {
	// 资源列表
	Resources []models.Chat `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回伙伴对话任务列表接口路径。
func (l *ListWakerChats) Path() string {
	return "/:wakerId/chats"
}

// Validate 校验最近活动时间范围和分页参数并补充默认值。
func (l *ListWakerChats) Validate(*gin.Context) error {
	if l.UpdatedAtStart < 0 || l.UpdatedAtEnd < 0 ||
		(l.UpdatedAtStart > 0 && l.UpdatedAtEnd > 0 && l.UpdatedAtStart > l.UpdatedAtEnd) ||
		l.PageNum < 0 || l.PageSize < 0 || l.PageSize > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 {
		l.PageNum = 1
	}
	if l.PageSize == 0 {
		l.PageSize = 50
	}
	return nil
}

// Output 返回伙伴在最近活动时间范围内的对话任务分页结果。
func (l *ListWakerChats) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, l.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	query := strings.TrimSpace(l.Query)
	resources, err := global.ChatControllerManager.ListByWakerUpdatedRange(
		ctx,
		l.WakerID,
		query,
		l.UpdatedAtStart,
		l.UpdatedAtEnd,
		l.PageSize,
		(l.PageNum-1)*l.PageSize,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	total, err := global.ChatControllerManager.CountByWakerUpdatedRange(
		ctx,
		l.WakerID,
		query,
		l.UpdatedAtStart,
		l.UpdatedAtEnd,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListWakerChatsResponse{
		Resources: resources,
		PageNum:   l.PageNum,
		PageSize:  l.PageSize,
		TotalSize: total,
	}, nil
}

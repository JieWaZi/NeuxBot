package wakers

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴列表
type ListWakers struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 伙伴列表响应
type ListWakersResponse struct {
	// 资源列表
	Resources []models.Waker `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回伙伴列表接口路径。
func (l *ListWakers) Path() string {
	return ""
}

// Validate 校验筛选和分页范围并补充默认值。
func (l *ListWakers) Validate(*gin.Context) error {
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > 100 {
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

// Output 返回伙伴分页列表。
func (l *ListWakers) Output(ctx *gin.Context) (interface{}, error) {
	query := strings.TrimSpace(l.Query)
	resources, err := global.WakerControllerManager.List(
		ctx,
		query,
		l.PageSize,
		(l.PageNum-1)*l.PageSize,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	wakerIDs := make([]string, 0, len(resources))
	for _, waker := range resources {
		wakerIDs = append(wakerIDs, waker.ID)
	}
	activitySummaries, err := global.ChatControllerManager.ListActivitySummariesByWaker(
		ctx,
		wakerIDs,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for index := range resources {
		_, resources[index].IsDefault = builtinrolepreset.DefinitionByWakerID(
			resources[index].ID,
		)
		summary := activitySummaries[resources[index].ID]
		resources[index].RunningCount = summary.RunningCount
		resources[index].WaitingCount = summary.WaitingCount
		resources[index].ActivityState = summary.WakerActivityState()
		if resources[index].ActivityState == "" {
			resources[index].ActivityState = constants.WakerActivityStateIdle
		}
	}
	total, err := global.WakerControllerManager.Count(ctx, query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListWakersResponse{
		Resources: resources,
		PageNum:   l.PageNum,
		PageSize:  l.PageSize,
		TotalSize: total,
	}, nil
}

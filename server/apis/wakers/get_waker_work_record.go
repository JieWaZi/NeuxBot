package wakers

import (
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 日消息统计
type DailyChatCount struct {
	// 自然日
	Date string `json:"date"`
	// 消息数量
	Count int64 `json:"count"`
}

// 工作记录响应
type WakerWorkRecordResponse struct {
	// 对话总数
	TotalChats int64 `json:"totalChats"`
	// 活跃对话数
	ActiveChats int64 `json:"activeChats"`
	// 每日统计
	DailyChats []DailyChatCount `json:"dailyChats"`
	// 今日活跃
	ActiveToday bool `json:"activeToday"`
	// 自动任务总数
	TotalAutomaticTasks int64 `json:"totalAutomaticTasks"`
}

// 获取工作记录
type GetWakerWorkRecord struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 客户端时区
	Timezone string `in:"query" name:"timezone"`
}

// Path 返回伙伴工作记录接口路径。
func (g *GetWakerWorkRecord) Path() string {
	return "/:wakerId/work-record"
}

// Output 返回伙伴工作记录统计。
func (g *GetWakerWorkRecord) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, g.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}

	total, err := global.ChatControllerManager.CountByWaker(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	active, err := global.ChatControllerManager.CountActiveByWaker(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	automaticCounts, err := global.AutomaticTaskControllerManager.Counts(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}

	loc := resolveWorkRecordLocation(g.Timezone)
	now := time.Now().In(loc)
	startOfYear, endOfYear, daysInYear := currentYearDailyRange(loc, now)

	counts, err := global.ChatService.CountMessagesByWakerGroupedByDay(
		ctx,
		g.WakerID,
		startOfYear.UnixMilli(),
		endOfYear.UnixMilli(),
		loc,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}

	daily := make([]DailyChatCount, daysInYear)
	for index := range daily {
		currentDay := startOfYear.AddDate(0, 0, index)
		key := currentDay.Format("2006-01-02")
		daily[index] = DailyChatCount{
			Date:  key,
			Count: counts[key],
		}
	}

	return WakerWorkRecordResponse{
		TotalChats:          total,
		ActiveChats:         active,
		DailyChats:          daily,
		ActiveToday:         counts[now.Format("2006-01-02")] > 0,
		TotalAutomaticTasks: automaticCounts.Total,
	}, nil
}

func resolveWorkRecordLocation(timezone string) *time.Location {
	if timezone == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func currentYearDailyRange(loc *time.Location, now time.Time) (
	startOfYear time.Time,
	endOfYear time.Time,
	daysInYear int,
) {
	year := now.In(loc).Year()
	startOfYear = time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	endOfYear = time.Date(year, time.December, 31, 23, 59, 59, 999999999, loc)
	daysInYear = time.Date(year, time.December, 31, 0, 0, 0, 0, loc).YearDay()
	return startOfYear, endOfYear, daysInYear
}

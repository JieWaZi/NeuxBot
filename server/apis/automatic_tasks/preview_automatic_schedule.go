package automatic_tasks

import (
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/schedulecalc"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 预览定时规则
type PreviewAutomaticSchedule struct {
	// 请求方法
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body models.AutomaticTaskSchedule `in:"body"`
}

// 定时规则预览
type SchedulePreview struct {
	// 未来运行时间
	NextRuns []int64 `json:"nextRuns"`
	// 全天触发次数
	RunsIn24Hours int64 `json:"runsIn24Hours"`
}

// Path 返回预览规则的相对路由。
func (*PreviewAutomaticSchedule) Path() string { return "/schedule-preview" }

// Output 返回未来机会，不持久化任务或建立 Chat。
func (o *PreviewAutomaticSchedule) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return previewSchedule(o.Body)
}

func previewSchedule(schedule models.AutomaticTaskSchedule) (SchedulePreview, error) {
	rule, err := schedulecalc.Compile(schedule)
	if err != nil {
		return SchedulePreview{}, err
	}
	now := time.Now()
	preview := SchedulePreview{NextRuns: rule.Preview(now, 3)}
	for next := rule.Next(now); !next.IsZero() && !next.After(now.Add(24*time.Hour)); next = rule.Next(next) {
		preview.RunsIn24Hours++
	}
	return preview, nil
}

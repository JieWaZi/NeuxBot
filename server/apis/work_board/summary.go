package work_board

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取工作概览
type GetWorkBoardSummary struct {
	// MethodGet 声明只读 HTTP 方法。
	ginx.MethodGet
	// ExecutorID 是原执行对象范围。
	ExecutorID string `in:"query" name:"executorId" validate:"omitempty,len=32"`
	// Source 是原入口范围。
	Source string `in:"query" name:"source" validate:"omitempty,oneof=chat team workflow automatic im"`
	// 活动日期下界
	From int64 `in:"query" name:"from" validate:"min=0"`
	// 活动日期上界
	To int64 `in:"query" name:"to" validate:"min=0"`
}

// WorkBoardSummary 不把不同统计单位相加为任务总数。
type WorkBoardSummary struct {
	// 工作总数
	WorkCount int64 `json:"workCount"`
	// 进行中工作数
	RunningCount int64 `json:"runningCount"`
	// 已结束工作数
	EndedCount int64 `json:"endedCount"`
	// 需操作工作数
	ActionWorkCount int64 `json:"actionWorkCount"`
	// AttentionCount 是全部有效请求数量。
	AttentionCount int64 `json:"attentionCount"`
	// ResultCount 是存在最新未读成果的容器数。
	ResultCount int64 `json:"resultCount"`
	// Attention 是最多五个最久等待的有效请求。
	Attention []models.WorkBoardAttention `json:"attention"`
	// Results 是最多五个最新未读结果的原容器引用。
	Results []models.WorkBoardEntry `json:"results"`
}

// Path 返回顶部关注区路径。
func (*GetWorkBoardSummary) Path() string { return "/summary" }

// Validate 拒绝倒置日期，关注区的待办与成果仍不限日期。
func (d *GetWorkBoardSummary) Validate(*gin.Context) error {
	if d.To > 0 && d.From > d.To {
		return status_error.BadRequest
	}
	return nil
}

// Output 使用准确计数和有界预览，不应用下方标题或历史日期。
func (d *GetWorkBoardSummary) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := queryContext(ctx)
	defer cancel()
	var value WorkBoardSummary
	err := global.TransactionControllerManager.Transaction(request, func(txctx context.Context) error {
		var readErr error
		value, readErr = d.readSummary(txctx)
		return readErr
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}

// readSummary 使各计数与预览观察同一份数据库状态。
func (d *GetWorkBoardSummary) readSummary(request context.Context) (WorkBoardSummary, error) {
	query := controllers.WorkBoardQuery{ExecutorID: d.ExecutorID, Source: d.Source, Limit: 5}
	var value WorkBoardSummary
	var err error
	history := query
	history.From, history.To = d.From, d.To
	if history.From == 0 {
		history.From = time.Now().AddDate(0, 0, -30).UnixMilli()
	}
	counts, err := global.WorkBoardControllerManager.Counts(request, history)
	if err != nil {
		return WorkBoardSummary{}, ginx.WithStack(err)
	}
	value.WorkCount, value.RunningCount, value.EndedCount = counts.Total, counts.Running, counts.Ended
	value.Attention, value.AttentionCount, value.ActionWorkCount, err = global.WorkBoardControllerManager.AttentionOverview(request, query)
	if err != nil {
		return WorkBoardSummary{}, ginx.WithStack(err)
	}
	value.ResultCount, err = global.WorkBoardControllerManager.ResultCount(request, query)
	if err != nil {
		return WorkBoardSummary{}, ginx.WithStack(err)
	}
	if value.ResultCount > 0 {
		value.Results, err = global.WorkBoardControllerManager.Results(request, query)
		if err != nil {
			return WorkBoardSummary{}, ginx.WithStack(err)
		}
	}
	if len(value.Attention) > 5 {
		value.Attention = value.Attention[:5]
	}
	if len(value.Results) > 5 {
		value.Results = value.Results[:5]
	}
	if value.Attention == nil {
		value.Attention = []models.WorkBoardAttention{}
	}
	if value.Results == nil {
		value.Results = []models.WorkBoardEntry{}
	}
	return value, nil
}

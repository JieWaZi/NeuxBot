package work_board

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
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
	// AttentionMetrics 是独立的阅读、待办和运行计数。
	models.AttentionMetrics
	// AttentionCompleteness 是范围完整性及当前 Core 进程。
	models.AttentionCompleteness
	// ActionPreview 是最多五个原有效请求。
	ActionPreview []models.WorkBoardAttention `json:"actionPreview"`
	// AwaitingViewPreview 是最多五个待查看原工作。
	AwaitingViewPreview []models.WorkBoardEntry `json:"awaitingViewPreview"`
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
	if isolated := global.WorkspaceAttention.Isolated; isolated != nil {
		query.IsolatedChats = isolated()
	}
	board := global.WorkBoardControllerManager
	metrics, missing, err := board.ReadingMetrics(request, constants.LocalReaderID, query)
	if err != nil {
		return value, err
	}
	value.AttentionMetrics = metrics
	actions, pending, waiting, err := board.Attention(request, query)
	if err != nil {
		return value, err
	}
	value.PendingRequestCount, value.WaitingWorkCount = pending, waiting
	value.ActionPreview = append([]models.WorkBoardAttention{}, actions[:min(query.Limit, len(actions))]...)
	awaiting, err := board.AwaitingView(request, constants.LocalReaderID, query)
	if err != nil {
		return value, err
	}
	awaiting = awaiting[:min(query.Limit, len(awaiting))]
	if _, err := board.AttachReadings(request, constants.LocalReaderID, awaiting, query.IsolatedChats); err != nil {
		return value, err
	}
	value.AwaitingViewPreview = append([]models.WorkBoardEntry{}, awaiting...)
	value.AttentionCompleteness, err = boardCompleteness(request, query, missing)
	return value, err
}

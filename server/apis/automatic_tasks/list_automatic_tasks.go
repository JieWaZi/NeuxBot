package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 自动任务列表响应
type ListAutomaticTasksResponse struct {
	// 任务列表
	Resources []schedule.TaskView `json:"resources"`
	// 总数量
	TotalSize int64 `json:"totalSize"`
	// 概览统计
	Counts controllers.AutomaticTaskCounts `json:"counts"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// 自动任务列表
type ListAutomaticTasks struct {
	// 请求方法
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 查询参数
	Query string `in:"query" name:"query"`
	// 查询参数
	Trigger constants.AutomaticTaskTrigger `in:"query" name:"trigger" validate:"omitempty,oneof=schedule api"`
	// 启停状态
	Enabled *bool `in:"query" name:"enabled"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回操作的相对路由。
func (l *ListAutomaticTasks) Path() string { return "" }

// Validate 补充分页默认值并校验范围。
func (o *ListAutomaticTasks) Validate(*gin.Context) error {
	return pagination(&o.PageNum, &o.PageSize)
}

// Output 完成校验后的任务管理请求。
func (o *ListAutomaticTasks) Output(ctx *gin.Context) (interface{}, error) {
	resources, total, counts, err := global.ScheduleService.List(ctx, controllers.AutomaticTaskFilter{WakerID: o.WakerID, Query: o.Query, Trigger: o.Trigger, Enabled: o.Enabled, Limit: o.PageSize, Offset: (o.PageNum - 1) * o.PageSize})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListAutomaticTasksResponse{Resources: resources, TotalSize: total, Counts: counts, PageNum: o.PageNum, PageSize: o.PageSize}, nil
}

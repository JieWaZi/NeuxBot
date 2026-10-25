package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 自动运行列表响应
type ListAutomaticRunsResponse struct {
	// 任务列表
	Resources []schedule.RunView `json:"resources"`
	// 总数量
	TotalSize int64 `json:"totalSize"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// 自动运行历史
type ListAutomaticRuns struct {
	// 请求方法
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回操作的相对路由。
func (l *ListAutomaticRuns) Path() string { return "/:taskId/runs" }

// Validate 补充分页默认值并校验范围。
func (o *ListAutomaticRuns) Validate(*gin.Context) error { return pagination(&o.PageNum, &o.PageSize) }

// Output 完成校验后的任务管理请求。
func (o *ListAutomaticRuns) Output(ctx *gin.Context) (interface{}, error) {
	resources, total, err := currentTaskAPI().Runs(ctx, o.WakerID, o.TaskID, o.PageSize, (o.PageNum-1)*o.PageSize)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListAutomaticRunsResponse{Resources: resources, TotalSize: total, PageNum: o.PageNum, PageSize: o.PageSize}, nil
}

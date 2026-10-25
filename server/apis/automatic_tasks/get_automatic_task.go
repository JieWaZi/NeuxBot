package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取自动任务
type GetAutomaticTask struct {
	// 请求方法
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required"`
}

// Path 返回操作的相对路由。
func (g *GetAutomaticTask) Path() string { return "/:taskId" }

// Output 完成校验后的任务管理请求。
func (o *GetAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ScheduleService.Get(ctx, o.WakerID, o.TaskID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

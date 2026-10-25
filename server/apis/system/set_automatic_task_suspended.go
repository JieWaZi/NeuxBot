package system

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 设备休眠状态请求
type SetAutomaticTaskSuspendedRequest struct {
	// 休眠状态
	Suspended bool `json:"suspended"`
}

// 同步设备休眠状态
type SetAutomaticTaskSuspended struct {
	// 请求方法
	ginx.MethodPost
	// 请求体
	Body SetAutomaticTaskSuspendedRequest `in:"body"`
}

// Path 返回桌面自动任务生命周期路径。
func (*SetAutomaticTaskSuspended) Path() string { return "/automatic-tasks-suspended" }

// Output 恢复时先推进错过机会，再开放后续计划。
func (o *SetAutomaticTaskSuspended) Output(ctx *gin.Context) (interface{}, error) {
	if global.IMService != nil {
		global.IMService.SetSuspended(o.Body.Suspended)
	}
	if err := global.ScheduleService.SetSuspended(ctx, o.Body.Suspended); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

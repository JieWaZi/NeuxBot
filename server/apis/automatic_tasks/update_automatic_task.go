package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 自动任务编辑请求
type UpdateAutomaticTaskRequest struct {
	// 任务配置
	models.AutomaticTaskConfiguration
	// 配置版本
	Revision int64 `json:"revision" validate:"required,min=1"`
}

// 编辑自动任务
type UpdateAutomaticTask struct {
	// 请求方法
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 任务标识
	TaskID string `in:"path" name:"taskId" validate:"required"`
	// 请求体
	Body UpdateAutomaticTaskRequest `in:"body"`
}

// Path 返回操作的相对路由。
func (u *UpdateAutomaticTask) Path() string { return "/:taskId" }

// Output 完成校验后的任务管理请求。
func (o *UpdateAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ScheduleService.SaveConfiguration(ctx, o.WakerID, o.TaskID, o.Body.Revision, o.Body.AutomaticTaskConfiguration, true)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// API 触发类型同样使用集成 Key，不随任务编辑颁发凭据。
	return AutomaticTaskReference{TaskID: result.TaskID}, nil
}

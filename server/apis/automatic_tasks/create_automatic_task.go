package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建自动任务
type CreateAutomaticTask struct {
	// 请求方法
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body models.AutomaticTaskConfiguration `in:"body"`
}

// Path 返回操作的相对路由。
func (c *CreateAutomaticTask) Path() string { return "" }

// Output 完成校验后的任务管理请求。
func (o *CreateAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ScheduleService.SaveConfiguration(ctx, o.WakerID, "", 0, o.Body, true)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 调用凭据统一在 API 集成中管理，任务保存只返回资源身份。
	return AutomaticTaskReference{TaskID: result.TaskID}, nil
}

// 已保存的自动任务身份
type AutomaticTaskReference struct {
	// 任务标识
	TaskID string `json:"taskId"`
}
